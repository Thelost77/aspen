package inlineimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type Protocol uint8

const (
	Unsupported Protocol = iota
	Kitty
	ITerm2
)

const (
	maxSourceSize  = 50 * 1024 * 1024
	maxPixels      = 40_000_000
	maxPixelWidth  = 1600
	maxPixelHeight = 1200
	kittyChunkSize = 4096
)

type Rendered struct {
	Protocol    Protocol
	ID          uint32
	Columns     int
	Rows        int
	PixelWidth  int
	PixelHeight int
	transfer    string
	display     string
}

func Detect(getenv func(string) string) Protocol {
	termProgram := strings.ToLower(getenv("TERM_PROGRAM"))
	term := strings.ToLower(getenv("TERM"))
	if termProgram == "ghostty" || strings.Contains(term, "ghostty") || getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return Kitty
	}
	if termProgram == "iterm.app" || strings.Contains(strings.ToLower(getenv("LC_TERMINAL")), "iterm") || getenv("ITERM_SESSION_ID") != "" {
		return ITerm2
	}
	return Unsupported
}

func Render(ctx context.Context, path string, protocol Protocol, id uint32, maxColumns, maxRows int) (Rendered, error) {
	if protocol == Unsupported {
		return Rendered{}, errors.New("terminal does not support inline images")
	}
	if maxColumns <= 0 || maxRows <= 0 {
		return Rendered{}, errors.New("image has no available terminal space")
	}
	actualPath, cleanup, err := prepareImage(ctx, path)
	if err != nil {
		return Rendered{}, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	file, err := os.Open(actualPath)
	if err != nil {
		return Rendered{}, errors.New("cannot open image attachment")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return Rendered{}, errors.New("cannot inspect image attachment")
	}
	if !info.Mode().IsRegular() || info.Size() > maxSourceSize {
		return Rendered{}, errors.New("image attachment is not a regular file under 50 MB")
	}
	configuration, _, err := image.DecodeConfig(file)
	if err != nil {
		return Rendered{}, fmt.Errorf("read image dimensions: %w", err)
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || int64(configuration.Width)*int64(configuration.Height) > maxPixels {
		return Rendered{}, errors.New("image dimensions exceed the safe inline limit")
	}
	if _, err := file.Seek(0, 0); err != nil {
		return Rendered{}, errors.New("cannot rewind image attachment")
	}
	source, _, err := image.Decode(file)
	if err != nil {
		return Rendered{}, fmt.Errorf("decode image attachment: %w", err)
	}

	columns, rows := fitCells(configuration.Width, configuration.Height, protocol, maxColumns, maxRows)
	encodedImage := resizeForTransfer(source)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, encodedImage); err != nil {
		return Rendered{}, fmt.Errorf("encode inline PNG: %w", err)
	}
	payload := encoded.Bytes()
	result := Rendered{
		Protocol:    protocol,
		ID:          id,
		Columns:     columns,
		Rows:        rows,
		PixelWidth:  encodedImage.Bounds().Dx(),
		PixelHeight: encodedImage.Bounds().Dy(),
	}
	switch protocol {
	case Kitty:
		result.transfer = kittyTransfer(id, encodedImage.Bounds().Dx(), encodedImage.Bounds().Dy(), payload)
	case ITerm2:
		encodedPayload := base64.StdEncoding.EncodeToString(payload)
		result.display = fmt.Sprintf("\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1;size=%d:%s\x07", columns, rows, len(payload), encodedPayload)
	}
	return result, nil
}

func (r Rendered) TransferSequence() string {
	return wrapTmux(r.transfer)
}

func (r Rendered) DisplaySequence(placementID uint32) string {
	return r.DisplayRegionSequence(placementID, 0, r.Rows)
}

func (r Rendered) DisplayRegionSequence(placementID uint32, clippedTopRows, visibleRows int) string {
	if visibleRows <= 0 || clippedTopRows < 0 || clippedTopRows+visibleRows > r.Rows {
		return ""
	}
	switch r.Protocol {
	case Kitty:
		if r.PixelWidth <= 0 || r.PixelHeight <= 0 || r.Rows <= 0 {
			if clippedTopRows != 0 || visibleRows != r.Rows {
				return ""
			}
			return wrapTmux(fmt.Sprintf("\x1b_Ga=p,i=%d,p=%d,c=%d,r=%d,C=1,z=1,q=2\x1b\\", r.ID, placementID, r.Columns, r.Rows))
		}
		sourceY := r.PixelHeight * clippedTopRows / r.Rows
		sourceEnd := r.PixelHeight * (clippedTopRows + visibleRows) / r.Rows
		sourceHeight := max(1, sourceEnd-sourceY)
		return wrapTmux(fmt.Sprintf(
			"\x1b_Ga=p,i=%d,p=%d,x=0,y=%d,w=%d,h=%d,c=%d,r=%d,C=1,z=1,q=2\x1b\\",
			r.ID, placementID, sourceY, r.PixelWidth, sourceHeight, r.Columns, visibleRows,
		))
	case ITerm2:
		if clippedTopRows == 0 && visibleRows == r.Rows {
			return wrapTmux(r.display)
		}
		return ""
	default:
		return ""
	}
}

func (r Rendered) DeletePlacementSequence(placementID uint32) string {
	if r.Protocol != Kitty {
		return ""
	}
	return wrapTmux(fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,p=%d,q=2\x1b\\", r.ID, placementID))
}

func (r Rendered) DeleteImageSequence() string {
	if r.Protocol != Kitty {
		return ""
	}
	return wrapTmux(fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", r.ID))
}

func kittyTransfer(id uint32, width, height int, payload []byte) string {
	encoded := base64.StdEncoding.EncodeToString(payload)
	var output strings.Builder
	first := true
	for len(encoded) > 0 {
		length := min(kittyChunkSize, len(encoded))
		chunk := encoded[:length]
		encoded = encoded[length:]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		if first {
			_, _ = fmt.Fprintf(&output, "\x1b_Ga=t,f=100,t=d,i=%d,s=%d,v=%d,q=2,m=%d;%s\x1b\\", id, width, height, more, chunk)
			first = false
		} else {
			_, _ = fmt.Fprintf(&output, "\x1b_Gm=%d,q=2;%s\x1b\\", more, chunk)
		}
	}
	return output.String()
}

func fitCells(width, height int, protocol Protocol, maxColumns, maxRows int) (int, int) {
	cellWidth, cellHeight := 9, 18
	if protocol == ITerm2 {
		cellWidth, cellHeight = 7, 14
	}
	columns := maxColumns
	rows := max(1, int(float64(height*columns*cellWidth)/float64(width*cellHeight)+0.5))
	if rows > maxRows {
		rows = maxRows
		columns = max(1, int(float64(width*rows*cellHeight)/float64(height*cellWidth)+0.5))
	}
	return min(columns, maxColumns), min(rows, maxRows)
}

func resizeForTransfer(source image.Image) image.Image {
	width := source.Bounds().Dx()
	height := source.Bounds().Dy()
	scale := min(float64(maxPixelWidth)/float64(width), float64(maxPixelHeight)/float64(height), 1)
	if scale >= 1 {
		return source
	}
	target := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(width)*scale)), max(1, int(float64(height)*scale))))
	draw.CatmullRom.Scale(target, target.Bounds(), source, source.Bounds(), draw.Over, nil)
	return target
}

func prepareImage(ctx context.Context, path string) (string, func(), error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, errors.New("image attachment file is unavailable")
	}
	if info.Size() > maxSourceSize {
		return "", nil, errors.New("image attachment exceeds the 50 MB inline limit")
	}
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".heic" && extension != ".heif" {
		return path, nil, nil
	}
	temporary, err := os.CreateTemp("", "aspen-inline-*.png")
	if err != nil {
		return "", nil, fmt.Errorf("create secure inline image file: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", nil, fmt.Errorf("secure inline image file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", nil, fmt.Errorf("close inline image file: %w", err)
	}
	cleanup := func() { _ = os.Remove(temporaryPath) }
	command := exec.CommandContext(ctx, "/usr/bin/sips", "-s", "format", "png", path, "--out", temporaryPath)
	if err := command.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("convert HEIC image with sips: %w", err)
	}
	return temporaryPath, cleanup, nil
}

func wrapTmux(sequence string) string {
	if sequence == "" || os.Getenv("TMUX") == "" {
		return sequence
	}
	return "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + "\x1b\\"
}

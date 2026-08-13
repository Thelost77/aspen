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

	ansikitty "github.com/charmbracelet/x/ansi/kitty"
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
	maxSourceSize    = 50 * 1024 * 1024
	maxPixels        = 40_000_000
	maxPixelWidth    = 1600
	maxPixelHeight   = 1200
	kittyChunkSize   = 4096
	kittyPlacementID = 1
)

type ErrorKind string

const (
	ErrorUnavailable    ErrorKind = "unavailable"
	ErrorSourceLimit    ErrorKind = "source_limit"
	ErrorDimensionLimit ErrorKind = "dimension_limit"
	ErrorConversion     ErrorKind = "conversion"
	ErrorDecode         ErrorKind = "decode"
	ErrorEncode         ErrorKind = "encode"
	ErrorTimeout        ErrorKind = "timeout"
	ErrorCanceled       ErrorKind = "canceled"
	ErrorRender         ErrorKind = "render"
)

type renderError struct {
	kind    ErrorKind
	message string
	cause   error
}

func (e *renderError) Error() string {
	if e.cause == nil {
		return e.message
	}
	return fmt.Sprintf("%s: %v", e.message, e.cause)
}

func (e *renderError) Unwrap() error {
	return e.cause
}

func newRenderError(kind ErrorKind, message string, cause ...error) error {
	var err error
	if len(cause) > 0 {
		err = cause[0]
	}
	return &renderError{kind: kind, message: message, cause: err}
}

func ErrorKindOf(err error) ErrorKind {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorTimeout
	case errors.Is(err, context.Canceled):
		return ErrorCanceled
	}
	var typed *renderError
	if errors.As(err, &typed) {
		return typed.kind
	}
	return ErrorRender
}

type Rendered struct {
	Protocol Protocol
	ID       uint32
	Columns  int
	Rows     int
	transfer string
	display  string
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
	if err := ctx.Err(); err != nil {
		return Rendered{}, err
	}
	if protocol == Unsupported {
		return Rendered{}, newRenderError(ErrorRender, "terminal does not support inline images")
	}
	if maxColumns <= 0 || maxRows <= 0 {
		return Rendered{}, newRenderError(ErrorDimensionLimit, "image has no available terminal space")
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
		return Rendered{}, newRenderError(ErrorUnavailable, "cannot open image attachment")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return Rendered{}, newRenderError(ErrorUnavailable, "cannot inspect image attachment")
	}
	if !info.Mode().IsRegular() || info.Size() > maxSourceSize {
		return Rendered{}, newRenderError(ErrorSourceLimit, "image attachment is not a regular file under 50 MB")
	}
	configuration, _, err := image.DecodeConfig(file)
	if err != nil {
		return Rendered{}, newRenderError(ErrorDecode, "read image dimensions", err)
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || int64(configuration.Width)*int64(configuration.Height) > maxPixels {
		return Rendered{}, newRenderError(ErrorDimensionLimit, "image dimensions exceed the safe inline limit")
	}
	if _, err := file.Seek(0, 0); err != nil {
		return Rendered{}, newRenderError(ErrorDecode, "cannot rewind image attachment")
	}
	source, _, err := image.Decode(file)
	if err != nil {
		return Rendered{}, newRenderError(ErrorDecode, "decode image attachment", err)
	}
	if err := ctx.Err(); err != nil {
		return Rendered{}, err
	}

	columns, rows := fitCells(configuration.Width, configuration.Height, protocol, maxColumns, maxRows)
	encodedImage := resizeForTransfer(source)
	if err := ctx.Err(); err != nil {
		return Rendered{}, err
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, encodedImage); err != nil {
		return Rendered{}, newRenderError(ErrorEncode, "encode inline PNG", err)
	}
	if err := ctx.Err(); err != nil {
		return Rendered{}, err
	}
	payload := encoded.Bytes()
	result := Rendered{
		Protocol: protocol,
		ID:       id,
		Columns:  columns,
		Rows:     rows,
	}
	switch protocol {
	case Kitty:
		result.transfer = wrapTmux(kittyTransfer(id, columns, rows, payload))
	case ITerm2:
		encodedPayload := base64.StdEncoding.EncodeToString(payload)
		result.display = wrapTmux(fmt.Sprintf("\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1;size=%d:%s\x07", columns, rows, len(payload), encodedPayload))
	}
	return result, nil
}

func (r Rendered) PayloadBytes() int {
	return len(r.transfer) + len(r.display)
}

func (r Rendered) TransferSequence() string {
	return r.transfer
}

func (r Rendered) ITerm2DisplaySequence() string {
	if r.Protocol != ITerm2 {
		return ""
	}
	return r.display
}

func (r Rendered) ITerm2DisplayRegionSequence(clippedTopRows, visibleRows int) string {
	if clippedTopRows != 0 || visibleRows != r.Rows {
		return ""
	}
	return r.ITerm2DisplaySequence()
}

func (r Rendered) PlaceholderRow(row int) string {
	if r.Protocol != Kitty || row < 0 || row >= r.Rows || r.Columns <= 0 {
		return ""
	}
	red := (r.ID >> 16) & 0xff
	green := (r.ID >> 8) & 0xff
	blue := r.ID & 0xff
	var output strings.Builder
	_, _ = fmt.Fprintf(&output, "\x1b[38;2;%d;%d;%dm\x1b[58;5;%dm", red, green, blue, kittyPlacementID)
	for column := range r.Columns {
		output.WriteRune(ansikitty.Placeholder)
		output.WriteRune(ansikitty.Diacritic(row))
		output.WriteRune(ansikitty.Diacritic(column))
		output.WriteRune(ansikitty.Diacritic(int(r.ID >> 24)))
	}
	output.WriteString("\x1b[39m\x1b[59m")
	return output.String()
}

func (r Rendered) DeleteImageSequence() string {
	if r.Protocol != Kitty {
		return ""
	}
	return wrapTmux(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", r.ID))
}

func kittyTransfer(id uint32, columns, rows int, payload []byte) string {
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
			_, _ = fmt.Fprintf(
				&output,
				"\x1b_Ga=T,f=100,t=d,i=%d,p=%d,c=%d,r=%d,U=1,C=1,N=1,q=2,m=%d;%s\x1b\\",
				id, kittyPlacementID, columns, rows, more, chunk,
			)
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
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, newRenderError(ErrorUnavailable, "image attachment file is unavailable")
	}
	if info.Size() > maxSourceSize {
		return "", nil, newRenderError(ErrorSourceLimit, "image attachment exceeds the 50 MB inline limit")
	}
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".heic" && extension != ".heif" {
		return path, nil, nil
	}
	temporary, err := os.CreateTemp("", "aspen-inline-*.png")
	if err != nil {
		return "", nil, newRenderError(ErrorConversion, "create secure inline image file", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", nil, newRenderError(ErrorConversion, "secure inline image file", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", nil, newRenderError(ErrorConversion, "close inline image file", err)
	}
	cleanup := func() { _ = os.Remove(temporaryPath) }
	command := exec.CommandContext(ctx, "/usr/bin/sips", "-s", "format", "png", path, "--out", temporaryPath)
	if err := command.Run(); err != nil {
		cleanup()
		if contextErr := ctx.Err(); contextErr != nil {
			return "", nil, contextErr
		}
		return "", nil, newRenderError(ErrorConversion, "convert HEIC image with sips", err)
	}
	return temporaryPath, cleanup, nil
}

func wrapTmux(sequence string) string {
	if sequence == "" || os.Getenv("TMUX") == "" {
		return sequence
	}
	return "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + "\x1b\\"
}

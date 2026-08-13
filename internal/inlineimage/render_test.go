package inlineimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	ansikitty "github.com/charmbracelet/x/ansi/kitty"
)

func TestDetectSupportedTerminals(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Protocol
	}{
		{name: "Ghostty", env: map[string]string{"TERM_PROGRAM": "ghostty"}, want: Kitty},
		{name: "Ghostty through SSH", env: map[string]string{"TERM": "xterm-ghostty"}, want: Kitty},
		{name: "Ghostty through tmux", env: map[string]string{"GHOSTTY_RESOURCES_DIR": "/Applications/Ghostty.app"}, want: Kitty},
		{name: "iTerm2", env: map[string]string{"TERM_PROGRAM": "iTerm.app"}, want: ITerm2},
		{name: "unsupported", env: map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, want: Unsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect(func(key string) string { return tt.env[key] }); got != tt.want {
				t.Fatalf("Detect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectProtocol(t *testing.T) {
	detectorCalled := false
	protocol, err := SelectProtocol("auto", func() Protocol {
		detectorCalled = true
		return Kitty
	})
	if err != nil || protocol != Kitty || !detectorCalled {
		t.Fatalf("SelectProtocol(auto) = %v, %v", protocol, err)
	}
	protocol, err = SelectProtocol("iterm2", func() Protocol { return Unsupported })
	if err != nil || protocol != ITerm2 {
		t.Fatalf("SelectProtocol(iterm2) = %v, %v", protocol, err)
	}
	if _, err := SelectProtocol("invalid", func() Protocol { return Unsupported }); err == nil {
		t.Fatal("SelectProtocol(invalid) succeeded")
	}
}

func TestUnidentifiedSSHSessionFallsBackToKitty(t *testing.T) {
	protocol := protocolOrSSHFallback(Unsupported, func(key string) string {
		if key == "SSH_TTY" {
			return "/dev/ttys003"
		}
		return ""
	})
	if protocol != Kitty {
		t.Fatalf("SSH fallback = %v, want Kitty", protocol)
	}
	if protocol := protocolOrSSHFallback(ITerm2, func(string) string { return "/dev/tty" }); protocol != ITerm2 {
		t.Fatalf("detected iTerm2 was replaced by %v", protocol)
	}
}

func TestTerminalProbeResponses(t *testing.T) {
	if !responseMatches(Kitty, "\x1b_Gi=31;OK\x1b\\") {
		t.Fatal("Kitty capability response was not recognized")
	}
	if !responseMatches(ITerm2, "\x1b]1337;ReportCellSize=9;18;2\x07") {
		t.Fatal("iTerm2 capability response was not recognized")
	}
	if responseMatches(Kitty, "\x1b_Gi=31;ENOENT\x1b\\") {
		t.Fatal("Kitty error response was accepted")
	}
}

func TestRenderProducesNativeKittyImage(t *testing.T) {
	path := writeFixture(t)
	rendered, err := Render(context.Background(), path, Kitty, 42, 30, 12)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Columns > 30 || rendered.Rows > 12 || rendered.Columns <= 0 || rendered.Rows <= 0 {
		t.Fatalf("native size = %dx%d", rendered.Columns, rendered.Rows)
	}
	transfer := rendered.TransferSequence()
	control := strings.SplitN(strings.TrimPrefix(transfer, "\x1b_G"), ";", 2)[0]
	for _, required := range []string{"a=T", "f=100", "i=42", "p=1", "U=1", "C=1", "N=1"} {
		if !strings.Contains(control, required) {
			t.Fatalf("Kitty transfer lacks %s: %q", required, transfer)
		}
	}
	if strings.Contains(control, "s=") || strings.Contains(control, "v=") || strings.Contains(transfer, "▀") {
		t.Fatalf("invalid Kitty PNG transfer: %q", transfer)
	}
	row := rendered.PlaceholderRow(0)
	if width := ansi.StringWidth(row); width != rendered.Columns {
		t.Fatalf("placeholder width = %d, want %d: %q", width, rendered.Columns, row)
	}
	if count := strings.Count(row, string(ansikitty.Placeholder)); count != rendered.Columns {
		t.Fatalf("placeholder count = %d, want %d", count, rendered.Columns)
	}
	placementControl := fmt.Sprintf("p=%d", kittyPlacementID)
	underlineControl := fmt.Sprintf("\x1b[58;5;%dm", kittyPlacementID)
	if !strings.Contains(control, placementControl) || !strings.Contains(row, underlineControl) {
		t.Fatalf("Kitty placement ID differs between transfer and placeholder: control=%q row=%q", control, row)
	}
	if display := rendered.ITerm2DisplaySequence(); display != "" {
		t.Fatalf("Kitty unexpectedly used a cursor placement: %q", display)
	}
	if cleanup := rendered.DeleteImageSequence(); !strings.Contains(cleanup, "a=d,d=I,i=42") {
		t.Fatalf("Kitty cleanup does not free image data: %q", cleanup)
	}
}

func TestKittyTransferChunksRoundTrip(t *testing.T) {
	payload := bytes.Repeat([]byte{0x00, 0x7f, 0xff}, 5000)
	sequence := kittyTransfer(42, 20, 10, payload)
	var encoded strings.Builder
	chunks := strings.Split(sequence, "\x1b_G")
	for _, chunk := range chunks[1:] {
		chunk = strings.TrimSuffix(chunk, "\x1b\\")
		parts := strings.SplitN(chunk, ";", 2)
		if len(parts) != 2 {
			t.Fatalf("invalid Kitty chunk: %q", chunk)
		}
		if len(parts[1]) > kittyChunkSize {
			t.Fatalf("encoded chunk size = %d, want <= %d", len(parts[1]), kittyChunkSize)
		}
		encoded.WriteString(parts[1])
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatal("chunked Kitty payload did not round trip")
	}
}

func TestKittyPlaceholderCarriesFullImageID(t *testing.T) {
	rendered := Rendered{Protocol: Kitty, ID: 0x0200002a, Columns: 1, Rows: 1}
	row := rendered.PlaceholderRow(0)
	if !strings.Contains(row, string(ansikitty.Diacritic(2))) {
		t.Fatalf("placeholder omitted high image ID byte: %q", row)
	}
}

func TestTmuxWrapsFullMultiChunkKittyTransferButNotPlaceholderText(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test/default,1,0")
	payload := bytes.Repeat([]byte("multi-chunk-fixture"), 1000)
	raw := kittyTransfer(42, 2, 1, payload)
	want := "\x1bPtmux;" + strings.ReplaceAll(raw, "\x1b", "\x1b\x1b") + "\x1b\\"
	rendered := Rendered{
		Protocol: Kitty,
		ID:       42,
		Columns:  2,
		Rows:     1,
		transfer: wrapTmux(raw),
	}
	transfer := rendered.TransferSequence()
	if transfer != want || strings.Count(transfer, "\x1b\x1b_G") < 2 {
		t.Fatalf("multi-chunk Kitty transfer was not fully wrapped for tmux: %q", transfer)
	}
	if row := rendered.PlaceholderRow(0); strings.Contains(row, "tmux;") {
		t.Fatalf("placeholder text was incorrectly wrapped for tmux: %q", row)
	}
}

func TestRenderErrorKindsAreTypedAtSource(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{name: "conversion", err: newRenderError(ErrorConversion, "conversion failed"), want: ErrorConversion},
		{name: "decode", err: newRenderError(ErrorDecode, "decode failed"), want: ErrorDecode},
		{name: "timeout", err: context.DeadlineExceeded, want: ErrorTimeout},
		{name: "source limit", err: newRenderError(ErrorSourceLimit, "too large"), want: ErrorSourceLimit},
		{name: "dimension limit", err: newRenderError(ErrorDimensionLimit, "too many pixels"), want: ErrorDimensionLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ErrorKindOf(test.err); got != test.want {
				t.Fatalf("ErrorKindOf() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRenderClassifiesDecodeAndLimitFailures(t *testing.T) {
	invalidPath := filepath.Join(t.TempDir(), "invalid.png")
	if err := os.WriteFile(invalidPath, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(context.Background(), invalidPath, Kitty, 1, 20, 10); ErrorKindOf(err) != ErrorDecode {
		t.Fatalf("invalid image kind = %q, want %q", ErrorKindOf(err), ErrorDecode)
	}

	largePath := filepath.Join(t.TempDir(), "large.png")
	file, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxSourceSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(context.Background(), largePath, Kitty, 1, 20, 10); ErrorKindOf(err) != ErrorSourceLimit {
		t.Fatalf("large image kind = %q, want %q", ErrorKindOf(err), ErrorSourceLimit)
	}
}

func TestRenderProducesNativeITerm2Image(t *testing.T) {
	path := writeFixture(t)
	rendered, err := Render(context.Background(), path, ITerm2, 42, 30, 12)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.TransferSequence() != "" {
		t.Fatal("iTerm2 unexpectedly produced a separate transfer")
	}
	display := rendered.ITerm2DisplaySequence()
	if !strings.Contains(display, "\x1b]1337;File=inline=1") || !strings.Contains(display, "preserveAspectRatio=1") || strings.Contains(display, "▀") {
		t.Fatalf("invalid iTerm2 display: %q", display)
	}
	if rendered.Rows > 1 && rendered.ITerm2DisplayRegionSequence(1, rendered.Rows-1) != "" {
		t.Fatal("iTerm2 partial placement would stretch instead of crop")
	}
}

func TestRenderMissingFileDoesNotEchoPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-name.heic")
	_, err := Render(context.Background(), path, Kitty, 1, 20, 10)
	if err == nil {
		t.Fatal("Render() succeeded for missing file")
	}
	if strings.Contains(err.Error(), path) {
		t.Fatalf("error leaked attachment path: %v", err)
	}
}

func writeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			fixture.Set(x, y, color.NRGBA{R: uint8(x * 20), G: uint8(y * 40), B: 180, A: 255})
		}
	}
	if err := png.Encode(file, fixture); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

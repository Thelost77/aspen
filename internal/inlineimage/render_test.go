package inlineimage

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if transfer := rendered.TransferSequence(); !strings.Contains(transfer, "\x1b_Ga=t,f=100") || strings.Contains(transfer, "▀") {
		t.Fatalf("invalid Kitty transfer: %q", transfer)
	}
	if display := rendered.DisplaySequence(7); !strings.Contains(display, "a=p,i=42,p=7") || !strings.Contains(display, "C=1") {
		t.Fatalf("invalid Kitty placement: %q", display)
	}
	if rendered.Rows > 1 {
		partial := rendered.DisplayRegionSequence(7, 1, rendered.Rows-1)
		if !strings.Contains(partial, "a=p,i=42,p=7") || !strings.Contains(partial, fmt.Sprintf("r=%d", rendered.Rows-1)) || !strings.Contains(partial, "y=") {
			t.Fatalf("invalid cropped Kitty placement: %q", partial)
		}
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
	display := rendered.DisplaySequence(7)
	if !strings.Contains(display, "\x1b]1337;File=inline=1") || !strings.Contains(display, "preserveAspectRatio=1") || strings.Contains(display, "▀") {
		t.Fatalf("invalid iTerm2 display: %q", display)
	}
	if rendered.Rows > 1 && rendered.DisplayRegionSequence(7, 1, rendered.Rows-1) != "" {
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

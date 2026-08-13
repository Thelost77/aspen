package inlineimage

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestTerminalOutputWritesOperationsAroundRendererFrame(t *testing.T) {
	var captured bytes.Buffer
	output := NewTerminalOutput(&captured)
	output.QueueBeforeFrame("upload")
	output.QueueAfterFrame("placement")
	written, err := output.Write([]byte("frame"))
	if err != nil {
		t.Fatal(err)
	}
	if written != len("frame") || captured.String() != "uploadframeplacement" {
		t.Fatalf("terminal output = %q, written = %d", captured.String(), written)
	}

	captured.Reset()
	if _, err := output.Write([]byte("steady")); err != nil {
		t.Fatal(err)
	}
	if captured.String() != "steady" {
		t.Fatalf("terminal operations were repeated: %q", captured.String())
	}
}

func TestTerminalOutputReplacesStaleCursorPlacements(t *testing.T) {
	var captured bytes.Buffer
	output := NewTerminalOutput(&captured)
	output.QueueAfterFrame("stale")
	output.ClearAfterFrame()
	output.QueueAfterFrame("current")
	if _, err := output.Write([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	if captured.String() != "framecurrent" {
		t.Fatalf("terminal output retained stale placement: %q", captured.String())
	}
}

func TestTerminalOutputFlushesCleanupWithoutFrame(t *testing.T) {
	var captured bytes.Buffer
	output := NewTerminalOutput(&captured)
	output.QueueBeforeFrame("delete")
	if err := output.Flush(); err != nil {
		t.Fatal(err)
	}
	if captured.String() != "delete" {
		t.Fatalf("flushed output = %q", captured.String())
	}
}

func TestTerminalOutputReportsShortFrameWrite(t *testing.T) {
	output := NewTerminalOutput(shortWriter{})
	if _, err := output.Write([]byte("frame")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v, want io.ErrShortWrite", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) {
	return max(0, len(data)-1), nil
}

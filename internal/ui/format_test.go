package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapUsesDisplayWidthAndPreservesNewlines(t *testing.T) {
	text := "emoji 🦊 and 世界\nhttps://example.test/abcdefghijklmnopqrstuvwxyz"
	wrapped := Wrap(text, 12)
	if !strings.Contains(wrapped, "\n") {
		t.Fatalf("Wrap() did not preserve or add line breaks: %q", wrapped)
	}
	for i, line := range strings.Split(wrapped, "\n") {
		if width := lipgloss.Width(line); width > 12 {
			t.Fatalf("line %d width = %d: %q", i, width, line)
		}
	}
}

func TestConversationTime(t *testing.T) {
	location := time.FixedZone("fixture", 3600)
	now := time.Date(2026, 8, 5, 14, 30, 0, 0, location)
	tests := []struct {
		value time.Time
		want  string
	}{
		{value: now.Add(-time.Hour), want: "13:30"},
		{value: now.AddDate(0, 0, -1), want: "Aug 4"},
		{value: now.AddDate(0, 0, -2), want: "Aug 3"},
		{value: time.Date(2026, 1, 2, 10, 0, 0, 0, location), want: "Jan 2"},
		{value: time.Date(2025, 1, 2, 10, 0, 0, 0, location), want: "Jan 2, 2025"},
	}
	for _, tt := range tests {
		if got := ConversationTime(tt.value, now); got != tt.want {
			t.Errorf("ConversationTime(%v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestCanvasAvoidsBottomRightCell(t *testing.T) {
	canvas := Canvas(strings.Repeat("x", 100), UsableWidth(40), 15)
	lines := strings.Split(canvas, "\n")
	if len(lines) != 15 {
		t.Fatalf("lines = %d", len(lines))
	}
	for i, line := range lines {
		if width := lipgloss.Width(line); width >= 40 {
			t.Fatalf("line %d width = %d", i, width)
		}
	}
}

func TestByteSize(t *testing.T) {
	if got := ByteSize(2048); got != "2.0 KB" {
		t.Fatalf("ByteSize() = %q", got)
	}
	if got := ByteSize(0); got != "" {
		t.Fatalf("ByteSize(0) = %q", got)
	}
}

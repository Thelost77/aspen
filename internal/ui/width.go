package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func UsableWidth(width int) int {
	if width > 1 {
		return width - 1
	}
	return max(width, 0)
}

func Canvas(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	canvas := make([]string, 0, height)
	for _, line := range lines {
		line = ansi.Truncate(line, width, "")
		if current := lipgloss.Width(line); current < width {
			line += strings.Repeat(" ", width-current)
		}
		canvas = append(canvas, line)
	}
	for len(canvas) < height {
		canvas = append(canvas, strings.Repeat(" ", width))
	}
	return strings.Join(canvas, "\n")
}

func Overlay(base, overlay string, width, height int) string {
	if overlay == "" {
		return base
	}
	if width <= 0 || height <= 0 {
		return overlay
	}
	baseLines := strings.Split(Canvas(base, width, height), "\n")
	overlayLines := strings.Split(overlay, "\n")
	overlayWidth := lipgloss.Width(overlay)
	if overlayWidth <= 0 || len(overlayLines) == 0 {
		return base
	}
	x := max(0, (width-overlayWidth)/2)
	y := max(0, (height-len(overlayLines))/2)
	for i, line := range overlayLines {
		if y+i >= len(baseLines) {
			break
		}
		line = ansi.Truncate(line, width-x, "")
		lineWidth := lipgloss.Width(line)
		left := ansi.Truncate(baseLines[y+i], x, "")
		right := ansi.TruncateLeft(baseLines[y+i], x+lineWidth, "")
		baseLines[y+i] = left + line + right
	}
	return strings.Join(baseLines, "\n")
}

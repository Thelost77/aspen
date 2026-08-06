package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func ConversationTime(value, now time.Time) string {
	if value.IsZero() {
		return ""
	}
	value = value.In(now.Location())
	switch {
	case value.Year() == now.Year() && value.YearDay() == now.YearDay():
		return value.Format("15:04")
	case value.Year() == now.Year():
		return value.Format("Jan 2")
	default:
		return value.Format("Jan 2, 2006")
	}
}

func MessageTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("15:04")
}

func DateLabel(value time.Time) string {
	if value.IsZero() {
		return "Unknown date"
	}
	return value.Local().Format("Mon, Jan 2, 2006")
}

func SameLocalDay(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return a.IsZero() && b.IsZero()
	}
	a = a.Local()
	b = b.Local()
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func ByteSize(size int64) string {
	switch {
	case size <= 0:
		return ""
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	case size < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", float64(size)/(1024*1024*1024))
	}
}

func Wrap(text string, width int) string {
	if width <= 0 {
		return ""
	}
	var wrapped []string
	for _, original := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := ansi.Wordwrap(original, width, "")
		for _, wordWrapped := range strings.Split(line, "\n") {
			wrapped = append(wrapped, ansi.Hardwrap(wordWrapped, width, true))
		}
	}
	return strings.Join(wrapped, "\n")
}

func Truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(text, width, "…")
}

func PadRight(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = ansi.Truncate(text, width, "")
	if current := lipgloss.Width(text); current < width {
		text += strings.Repeat(" ", width-current)
	}
	return text
}

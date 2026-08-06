package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestStatusStyleHasNoBackground(t *testing.T) {
	if background := DefaultStyles().Status.GetBackground(); background != (lipgloss.NoColor{}) {
		t.Fatalf("status background = %v, want none", background)
	}
}

func TestMessageStylesHaveNoBackground(t *testing.T) {
	styles := DefaultStyles()
	if background := styles.Incoming.GetBackground(); background != (lipgloss.NoColor{}) {
		t.Fatalf("incoming background = %v, want none", background)
	}
	if background := styles.Outgoing.GetBackground(); background != (lipgloss.NoColor{}) {
		t.Fatalf("outgoing background = %v, want none", background)
	}
}

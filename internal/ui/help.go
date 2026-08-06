package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type HelpBinding struct {
	Key  string
	Desc string
}

type HelpGroup struct {
	Title    string
	Bindings []HelpBinding
}

type HelpOverlay struct {
	visible bool
	width   int
	height  int
	styles  Styles
	groups  []HelpGroup
}

func NewHelpOverlay(styles Styles) HelpOverlay {
	return HelpOverlay{
		styles: styles,
		groups: []HelpGroup{
			{Title: "Navigation", Bindings: []HelpBinding{
				{Key: "j/k · ↑/↓", Desc: "move or scroll"},
				{Key: "enter · →", Desc: "open conversation"},
				{Key: "esc · ←", Desc: "back or dismiss"},
				{Key: "tab", Desc: "switch pane"},
				{Key: "g/G", Desc: "top / bottom"},
				{Key: "H/L, pgup/pgdn", Desc: "jump one page"},
				{Key: "A", Desc: "load all older messages / stop"},
				{Key: "/", Desc: "filter loaded messages"},
				{Key: "mouse click", Desc: "open conversation / focus pane"},
			}},
			{Title: "Messages", Bindings: []HelpBinding{
				{Key: "i", Desc: "write a message"},
				{Key: "enter", Desc: "send while composing"},
				{Key: "ctrl+j", Desc: "newline while composing"},
				{Key: "r", Desc: "refresh"},
				{Key: "/", Desc: "filter conversations"},
			}},
			{Title: "Global", Bindings: []HelpBinding{
				{Key: "?", Desc: "toggle this help"},
				{Key: "q", Desc: "quit outside text input"},
				{Key: "ctrl+c", Desc: "quit"},
			}},
		},
	}
}

func (h *HelpOverlay) Toggle()      { h.visible = !h.visible }
func (h *HelpOverlay) Hide()        { h.visible = false }
func (h HelpOverlay) Visible() bool { return h.visible }
func (h *HelpOverlay) SetSize(width, height int) {
	h.width = width
	h.height = height
}

func (h HelpOverlay) View() string {
	if !h.visible {
		return ""
	}
	maxLines := 100
	if h.height > 0 {
		maxLines = max(4, h.height-4)
	}
	lines := []string{h.styles.Title.Render("Keybindings"), ""}
	for _, group := range h.groups {
		if len(lines)+2 >= maxLines {
			break
		}
		lines = append(lines, h.styles.Accent.Bold(true).Render(group.Title))
		for _, binding := range group.Bindings {
			if len(lines)+1 >= maxLines {
				break
			}
			lines = append(lines, h.styles.Muted.Render(PadRight(binding.Key, 16))+binding.Desc)
		}
		lines = append(lines, "")
	}
	lines = append(lines, h.styles.Muted.Render("? or Esc closes help"))

	boxWidth := 58
	if h.width > 0 {
		boxWidth = min(boxWidth, max(20, h.width-4))
	}
	content := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(h.styles.Border.GetForeground()).
		Padding(1, 2).
		Width(max(1, boxWidth-6)).
		Render(content)
	if h.width > 0 && h.height > 0 {
		return lipgloss.Place(h.width, h.height, lipgloss.Center, lipgloss.Center, box)
	}
	return fmt.Sprint(box)
}

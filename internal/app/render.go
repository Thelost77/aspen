package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/messages"
	msgsender "github.com/Thelost77/aspen/internal/sender"
	"github.com/Thelost77/aspen/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	width := ui.UsableWidth(m.width)
	if width <= 0 || m.height <= 0 {
		return ""
	}

	body := m.viewBody()
	footer := m.viewFooter(width)
	base := ui.Canvas(lipgloss.JoinVertical(lipgloss.Left, body, footer), width, m.height)
	if m.help.Visible() {
		help := ui.Canvas(m.help.View(), width, m.height)
		return help
	}
	return base
}

func (m Model) viewBody() string {
	if m.fatalErr != nil {
		return m.viewFatal()
	}
	if len(m.chats) == 0 && !m.loadingChats {
		return lipgloss.Place(m.bodyWidth, m.bodyHeight, lipgloss.Center, lipgloss.Center, m.styles.Muted.Render("No conversations found."))
	}
	if m.layout == LayoutNarrow {
		if m.narrowPane == NarrowConversation {
			return m.paneStyle(true).Render(m.viewConversation())
		}
		return m.paneStyle(true).Render(m.chatList.View())
	}

	listActive := m.focus == FocusList || m.chatList.IsFiltering()
	conversationActive := !listActive
	listPane := m.paneStyle(listActive).Render(m.chatList.View())
	conversationPane := m.paneStyle(conversationActive).Render(m.viewConversation())
	return lipgloss.JoinHorizontal(lipgloss.Top, listPane, " ", conversationPane)
}

func (m Model) paneStyle(active bool) lipgloss.Style {
	color := m.styles.Border.GetForeground()
	if active {
		color = m.styles.Accent.GetForeground()
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color)
}

func (m Model) viewConversation() string {
	width := max(1, m.conversationContentWidth)
	chat, ok := m.chatByID[m.selectedID]
	if !ok {
		return lipgloss.Place(width, m.paneContentHeight, lipgloss.Center, lipgloss.Center, m.styles.Muted.Render("Select a conversation"))
	}

	title := m.styles.Subtitle.Render(ui.Truncate(chat.Title(), width))
	if m.focus == FocusViewport {
		title = m.styles.Accent.Bold(true).Render(ui.Truncate(chat.Title(), width))
	}
	state := m.threads[m.selectedID]
	details := chat.Service
	if chat.IsGroup {
		details = fmt.Sprintf("%d participants", len(chat.Participants))
		if chat.Service != "" {
			details += " · " + chat.Service
		}
	}
	if state != nil && (state.loadingAll || state.loadingOlder) {
		if details != "" {
			details += " · "
		}
		if state.loadingAll {
			details += fmt.Sprintf("loading all… %d messages", len(state.messages))
		} else {
			details += "loading older…"
		}
	}
	titleLine := ui.PadRight(title, width) + m.inlineImageFrameMarker()
	detailLine := ui.PadRight(m.styles.Muted.Render(ui.Truncate(details, width)), width)
	if searchLine, ok := m.conversationSearchHeader(chat.ID, width); ok {
		detailLine = searchLine
	}
	header := titleLine + "\n" + detailLine

	var content string
	switch {
	case state == nil || state.loading && !state.loaded:
		content = lipgloss.Place(width, m.viewport.Height, lipgloss.Center, lipgloss.Center, m.styles.Muted.Render("Loading messages…"))
	case state.err != nil && !state.loaded:
		content = lipgloss.Place(width, m.viewport.Height, lipgloss.Center, lipgloss.Center, m.styles.Error.Render("Unable to load messages"))
	case len(state.messages) == 0:
		content = lipgloss.Place(width, m.viewport.Height, lipgloss.Center, lipgloss.Center, m.styles.Muted.Render("No messages in this conversation"))
	case m.conversationFilterActive(chat.ID) && len(m.search.matches) == 0:
		query := strings.TrimSpace(m.search.input.Value())
		empty := ui.Truncate(fmt.Sprintf("No loaded messages match %q", query), width)
		content = lipgloss.Place(width, m.viewport.Height, lipgloss.Center, lipgloss.Center, m.styles.Muted.Render(empty))
	default:
		content = m.viewport.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, content, m.viewComposer(chat, width))
}

func (m Model) viewComposer(chat messages.Chat, width int) string {
	label := " Message "
	if _, sending := m.pending[chat.ID]; sending {
		label = " Sending… "
	}
	lineWidth := max(0, width-lipgloss.Width(label))
	separator := m.styles.Border.Render(label + strings.Repeat("─", lineWidth))

	_, reason, supported := m.targetForChat(chat)
	if !supported || m.messageSender == nil {
		if m.messageSender == nil {
			reason = "Sending adapter unavailable"
		}
		body := ui.Canvas(m.styles.Muted.Render(ui.Truncate(reason, width)), width, m.composer.Height())
		return separator + "\n" + body
	}
	body := ui.Canvas(m.composer.View(), width, m.composer.Height())
	return separator + "\n" + body
}

func (m Model) viewFatal() string {
	width := max(1, m.bodyWidth)
	height := max(1, m.bodyHeight)
	title := m.styles.Error.Render("Messages unavailable")
	detail := m.styles.Muted.Render(displayError(m.fatalErr))
	actions := m.styles.Accent.Render("r") + m.styles.Muted.Render(" retry") + m.styles.Muted.Render(" • ") + m.styles.Accent.Render("q") + m.styles.Muted.Render(" quit")
	content := lipgloss.JoinVertical(lipgloss.Center, title, "", ui.Wrap(detail, max(12, width-8)), "", actions)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) viewFooter(width int) string {
	if m.bannerErr != nil {
		text := "⚠ " + displayError(m.bannerErr) + " · Esc dismisses"
		return m.styles.ErrorBanner.Width(width).Render(ui.Truncate(text, max(1, width-2)))
	}
	sep := m.styles.Muted.Render(" • ")
	key := func(binding, description string) string {
		return m.styles.Accent.Render(binding) + m.styles.Muted.Render(" "+description)
	}
	parts := []string{}
	if m.search.editing {
		parts = append(parts, key("enter", "apply"), key("esc", "cancel"))
	} else if m.focus == FocusComposer {
		parts = append(parts, key("enter", "send"), key("ctrl+j", "newline"), key("esc", "keep draft"))
	} else if m.layout == LayoutNarrow && m.narrowPane == NarrowConversation {
		if m.conversationFilterActive(m.selectedID) {
			parts = append(parts, key("esc", "clear"), key("/", "filter"))
		} else {
			parts = append(parts, key("esc", "back"), key("/", "filter"))
		}
		if binding, description, ok := m.allHistoryBinding(); ok {
			parts = append(parts, key(binding, description))
		}
	} else if m.focus == FocusList {
		parts = append(parts, key("enter", "open"), key("/", "filter"))
	} else {
		parts = append(parts, key("j/k", "scroll"), key("H/L", "page"), key("g/G", "ends"), key("/", "filter"))
		if m.conversationFilterActive(m.selectedID) {
			parts = append(parts, key("esc", "clear"))
		}
		if binding, description, ok := m.allHistoryBinding(); ok {
			parts = append(parts, key(binding, description))
		}
	}
	if m.focus != FocusComposer && !m.search.editing {
		parts = append(parts, key("i", "write"), key("tab", "focus"), key("r", "refresh"))
		if m.imageProtocol != inlineimage.Unsupported {
			parts = append(parts, key("R", "retry images"))
		}
		parts = append(parts, key("?", "help"), key("q", "quit"))
	}
	footer := strings.Join(parts, sep)
	if status := m.footerStatus(); status != "" {
		footer = m.styles.Muted.Render(status) + sep + footer
	}
	return m.styles.Status.Width(width).Render(ui.Truncate(footer, max(1, width-2)))
}

func (m Model) allHistoryBinding() (string, string, bool) {
	state := m.threads[m.selectedID]
	if state == nil {
		return "", "", false
	}
	if state.loadingAll {
		return "A", "stop", true
	}
	if state.older != nil {
		return "A", "load all", true
	}
	return "", "", false
}

func (m Model) footerStatus() string {
	if m.loadingChats {
		return "loading…"
	}
	if m.status == "" || m.status == "Refreshed" {
		return ""
	}
	return m.status
}

func displayError(err error) string {
	if err == nil {
		return ""
	}
	var sendErr *msgsender.SendError
	if errors.As(err, &sendErr) {
		return sendErrorMessage(err)
	}
	switch {
	case messages.IsDatabaseError(err, messages.ErrorNotFound):
		return "Messages database not found. Open Messages and sign in."
	case messages.IsDatabaseError(err, messages.ErrorPermission):
		return "Cannot read Messages data. Grant Full Disk Access to the process launching Aspen."
	case messages.IsDatabaseError(err, messages.ErrorUnsupported):
		return "This macOS Messages database schema is not supported."
	case messages.IsDatabaseError(err, messages.ErrorBusy):
		return "Messages database is busy. Retry in a moment."
	case messages.IsDatabaseError(err, messages.ErrorCorrupt):
		return "Messages database could not be read safely."
	case errors.Is(err, context.DeadlineExceeded):
		return "Messages did not respond before the operation timed out."
	default:
		return err.Error()
	}
}

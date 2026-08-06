package app

import (
	"fmt"
	"strings"

	"github.com/Thelost77/aspen/internal/messages"
	"github.com/Thelost77/aspen/internal/ui"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type conversationSearchState struct {
	input         textinput.Model
	chatID        messages.ChatID
	editing       bool
	matches       []int
	restoreOffset int
	editOffset    int
	beforeEdit    string
}

func newConversationSearch(styles ui.Styles) conversationSearchState {
	input := textinput.New()
	input.Prompt = "Filter: "
	input.PromptStyle = styles.Accent.Bold(true)
	input.TextStyle = styles.Subtitle
	input.Cursor.Style = styles.Accent
	input.CharLimit = 256
	return conversationSearchState{input: input}
}

func (m Model) beginConversationSearch() (tea.Model, tea.Cmd) {
	if m.selectedID == 0 {
		return m, nil
	}
	if m.search.chatID != m.selectedID || strings.TrimSpace(m.search.input.Value()) == "" {
		m.search = newConversationSearch(m.styles)
		m.search.chatID = m.selectedID
		m.search.restoreOffset = m.viewport.YOffset
	} else {
		m.search.beforeEdit = m.search.input.Value()
		m.search.editOffset = m.viewport.YOffset
	}
	m.search.editing = true
	m.search.input.Width = max(1, m.conversationContentWidth-len(m.search.input.Prompt))
	m.search.input.CursorEnd()
	return m, m.search.input.Focus()
}

func (m Model) handleConversationSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.search.beforeEdit != "" {
			m.search.input.SetValue(m.search.beforeEdit)
			m.search.beforeEdit = ""
			m.search.editing = false
			m.search.input.Blur()
			m.recomputeConversationSearch()
			m.syncViewport(false)
			m.viewport.SetYOffset(m.search.editOffset)
			m.saveViewportOffset()
			return m, nil
		}
		m.clearConversationSearch(true)
		return m, nil
	case "enter":
		if strings.TrimSpace(m.search.input.Value()) == "" {
			m.clearConversationSearch(true)
			return m, nil
		}
		m.search.beforeEdit = ""
		m.search.editing = false
		m.search.input.Blur()
		m.recomputeConversationSearch()
		m.syncViewport(false)
		m.viewport.GotoTop()
		m.saveViewportOffset()
		return m, nil
	default:
		var cmd tea.Cmd
		m.search.input, cmd = m.search.input.Update(msg)
		m.recomputeConversationSearch()
		m.syncViewport(false)
		m.viewport.GotoTop()
		return m, cmd
	}
}

func (m *Model) recomputeConversationSearch() {
	m.search.matches = nil
	query := strings.ToLower(strings.TrimSpace(m.search.input.Value()))
	state := m.threads[m.search.chatID]
	if query == "" || state == nil {
		return
	}
	for index, message := range state.messages {
		var searchable strings.Builder
		searchable.WriteString(message.Text)
		for _, attachment := range message.Attachments {
			searchable.WriteByte('\n')
			searchable.WriteString(attachment.Name)
		}
		if strings.Contains(strings.ToLower(searchable.String()), query) {
			m.search.matches = append(m.search.matches, index)
		}
	}
}

func (m *Model) clearConversationSearch(sync bool) {
	restoreOffset := m.search.restoreOffset
	m.search = newConversationSearch(m.styles)
	if sync {
		m.syncViewport(false)
		m.viewport.SetYOffset(restoreOffset)
		m.saveViewportOffset()
	}
}

func (m Model) conversationFilterActive(chatID messages.ChatID) bool {
	return m.search.chatID == chatID && strings.TrimSpace(m.search.input.Value()) != ""
}

func (m Model) filteredConversationMessages(chatID messages.ChatID, all []messages.Message) []messages.Message {
	if !m.conversationFilterActive(chatID) {
		return all
	}
	filtered := make([]messages.Message, 0, len(m.search.matches))
	for _, index := range m.search.matches {
		if index >= 0 && index < len(all) {
			filtered = append(filtered, all[index])
		}
	}
	return filtered
}

func (m Model) conversationSearchHeader(chatID messages.ChatID, width int) (string, bool) {
	if m.search.chatID != chatID {
		return "", false
	}
	if m.search.editing {
		return ui.PadRight(m.search.input.View(), width), true
	}
	query := strings.TrimSpace(m.search.input.Value())
	if query == "" {
		return "", false
	}
	status := fmt.Sprintf("%d matches", len(m.search.matches))
	if len(m.search.matches) == 1 {
		status = "1 match"
	}
	line := fmt.Sprintf("Filter: %s · %s · Esc clear", query, status)
	return ui.PadRight(m.styles.Muted.Render(ui.Truncate(line, width)), width), true
}

func (m *Model) refreshConversationSearch(chatID messages.ChatID) {
	if !m.conversationFilterActive(chatID) {
		return
	}
	m.recomputeConversationSearch()
}

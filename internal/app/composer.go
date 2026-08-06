package app

import (
	"context"
	"errors"
	"strings"

	"github.com/Thelost77/aspen/internal/messages"
	msgsender "github.com/Thelost77/aspen/internal/sender"
	"github.com/Thelost77/aspen/internal/ui"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

func newComposer(styles ui.Styles) textarea.Model {
	composer := textarea.New()
	composer.Prompt = ""
	composer.Placeholder = "Message…"
	composer.ShowLineNumbers = false
	composer.CharLimit = 0
	composer.MaxHeight = 5
	composer.SetHeight(3)
	composer.FocusedStyle.Text = composer.FocusedStyle.Text.Foreground(styles.Subtitle.GetForeground())
	composer.FocusedStyle.Placeholder = composer.FocusedStyle.Placeholder.Foreground(styles.Muted.GetForeground())
	composer.FocusedStyle.CursorLine = composer.FocusedStyle.CursorLine.Background(nil)
	composer.BlurredStyle.Text = composer.BlurredStyle.Text.Foreground(styles.Subtitle.GetForeground())
	composer.BlurredStyle.Placeholder = composer.BlurredStyle.Placeholder.Foreground(styles.Muted.GetForeground())
	composer.Blur()
	return composer
}

func (m *Model) focusComposer() tea.Cmd {
	chat, ok := m.chatByID[m.selectedID]
	if !ok {
		m.status = "Select a conversation first"
		return nil
	}
	if _, reason, supported := m.targetForChat(chat); !supported {
		m.status = reason
		return nil
	}
	m.loadSelectedDraft()
	m.focus = FocusComposer
	return m.composer.Focus()
}

func (m *Model) loadSelectedDraft() {
	draft := m.drafts[m.selectedID]
	if draft == nil {
		draft = &draftState{}
		m.drafts[m.selectedID] = draft
	}
	m.composer.SetValue(draft.text)
	m.composer.CursorEnd()
}

func (m *Model) saveSelectedDraft() {
	if m.selectedID == 0 {
		return
	}
	value := m.composer.Value()
	draft := m.drafts[m.selectedID]
	if draft == nil {
		draft = &draftState{}
		m.drafts[m.selectedID] = draft
	}
	if draft.text != value {
		draft.text = value
		draft.generation++
	}
}

func (m Model) handleComposerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.saveSelectedDraft()
		m.composer.Blur()
		m.focus = FocusViewport
		return m, nil
	case "tab":
		m.saveSelectedDraft()
		m.composer.Blur()
		if m.layout == LayoutSplit {
			m.focus = FocusList
		} else {
			m.focus = FocusViewport
		}
		return m, nil
	case "ctrl+j", "alt+enter":
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m.saveSelectedDraft()
		m.setSizes()
		return m, cmd
	case "enter":
		return m.startSend()
	}

	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	m.saveSelectedDraft()
	m.setSizes()
	return m, cmd
}

func (m Model) startSend() (tea.Model, tea.Cmd) {
	m.saveSelectedDraft()
	chat, ok := m.chatByID[m.selectedID]
	if !ok {
		m.status = "No conversation selected"
		return m, nil
	}
	if m.messageSender == nil {
		m.status = "Sending adapter is unavailable"
		return m, nil
	}
	target, reason, supported := m.targetForChat(chat)
	if !supported {
		m.status = reason
		return m, nil
	}
	if _, exists := m.pending[chat.ID]; exists {
		m.status = "Already sending…"
		return m, nil
	}
	draft := m.drafts[chat.ID]
	if draft == nil || strings.TrimSpace(draft.text) == "" {
		m.status = "Message is empty"
		return m, nil
	}

	m.sendGeneration++
	generation := m.sendGeneration
	m.pending[chat.ID] = pendingSend{
		generation:      generation,
		draftGeneration: draft.generation,
		text:            draft.text,
	}
	m.status = "Sending…"
	return m, sendMessageCmd(m.messageSender, target, draft.text, generation, draft.generation)
}

func (m Model) handleSendFinished(msg SendFinishedMsg) (tea.Model, tea.Cmd) {
	pending, ok := m.pending[msg.ChatID]
	if !ok || pending.generation != msg.Generation {
		return m, nil
	}
	delete(m.pending, msg.ChatID)
	if msg.Err != nil {
		m.bannerErr = msg.Err
		m.status = "Send failed"
		if msg.ChatID == m.selectedID {
			m.loadSelectedDraft()
			m.focus = FocusComposer
			return m, m.composer.Focus()
		}
		return m, nil
	}

	if draft := m.drafts[msg.ChatID]; draft != nil && draft.generation == msg.DraftGeneration && draft.text == pending.text {
		draft.text = ""
		draft.generation++
		if msg.ChatID == m.selectedID {
			m.composer.Reset()
		}
	}
	m.bannerErr = nil
	m.status = "Sent"
	if m.store == nil {
		return m, nil
	}
	m.chatGeneration++
	return m, tea.Batch(loadChatsCmd(m.store, m.chatGeneration), m.startHistoryLoad(msg.ChatID))
}

func (m Model) targetForChat(chat messages.Chat) (msgsender.SendTarget, string, bool) {
	target := msgsender.SendTarget{ChatID: chat.ID, GUID: chat.GUID, Service: chat.Service, IsGroup: chat.IsGroup}
	switch {
	case chat.GUID == "":
		return target, "This conversation has no stable Messages identifier", false
	case chat.IsGroup:
		return target, "Group sending awaits manual exact-target verification", false
	case !strings.EqualFold(chat.Service, "iMessage") && !strings.EqualFold(chat.Service, "SMS"):
		return target, "Sending for " + displayService(chat.Service) + " is not verified", false
	default:
		return target, "", true
	}
}

func displayService(service string) string {
	if strings.EqualFold(service, "RCS") {
		return "RCS"
	}
	if strings.TrimSpace(service) == "" {
		return "this service"
	}
	return "this service"
}

func sendErrorMessage(err error) string {
	switch {
	case msgsender.IsError(err, msgsender.ErrorPermission):
		return "Cannot send. Allow Aspen to control Messages in System Settings → Privacy & Security → Automation."
	case msgsender.IsError(err, msgsender.ErrorUnavailable):
		return "Messages is not configured or responding. Open Messages and retry."
	case msgsender.IsError(err, msgsender.ErrorNotFound):
		return "Selected conversation no longer exists in Messages. Refresh and retry."
	case msgsender.IsError(err, msgsender.ErrorRecipient):
		return "The recipient is unavailable in Messages. Draft was preserved."
	case msgsender.IsError(err, msgsender.ErrorTimeout):
		return "Messages did not respond before the send timed out. Draft was preserved."
	case msgsender.IsError(err, msgsender.ErrorUnsupported):
		return "Sending to this conversation type is not supported yet."
	case errors.Is(err, context.Canceled):
		return "Send was canceled. Draft was preserved."
	default:
		return "Messages rejected the send. Draft was preserved."
	}
}

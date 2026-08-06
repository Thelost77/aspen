package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Thelost77/aspen/internal/messages"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConversationFilterShowsAllMatchingLoadedMessages(t *testing.T) {
	model, _ := loadTestModel(t)
	threadMessages := make([]messages.Message, 30)
	for i := range threadMessages {
		text := fmt.Sprintf("ordinary message %d", i)
		if i == 5 || i == 22 {
			text = fmt.Sprintf("needle result %d", i)
		}
		threadMessages[i] = messages.Message{ID: messages.MessageID(i + 1), ChatID: 1, Text: text, SentAt: time.Now().Add(time.Duration(i) * time.Minute)}
	}
	model.threads[1].messages = threadMessages
	model.threads[1].loaded = true
	model.focus = FocusViewport
	model.syncViewport(false)
	model.viewport.SetYOffset(20)
	originalOffset := model.viewport.YOffset

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("needle")})
	model = updated.(Model)
	if !model.search.editing || len(model.search.matches) != 2 {
		t.Fatalf("live filter state = %#v", model.search)
	}
	filtered := model.filteredConversationMessages(1, threadMessages)
	if len(filtered) != 2 || filtered[0].Text != "needle result 5" || filtered[1].Text != "needle result 22" {
		t.Fatalf("filtered messages = %#v", filtered)
	}
	view := model.View()
	if !strings.Contains(view, "needle result 5") || !strings.Contains(view, "needle result 22") || strings.Contains(view, "ordinary message") {
		t.Fatalf("conversation does not show only matching messages:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.search.editing || !strings.Contains(model.View(), "2 matches") {
		t.Fatal("Enter did not apply conversation filter")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.conversationFilterActive(1) || model.viewport.YOffset != originalOffset {
		t.Fatalf("Esc did not clear filter and restore offset: active=%v offset=%d want=%d", model.conversationFilterActive(1), model.viewport.YOffset, originalOffset)
	}
}

func TestConversationFilterMatchesAttachmentNamesAndReportsNoMatches(t *testing.T) {
	model, _ := loadTestModel(t)
	model.focus = FocusViewport
	model.threads[1].messages[0].Attachments = []messages.Attachment{{ID: 9, Name: "BoardingPass.HEIC"}}
	model.syncViewport(false)

	model = applyConversationFilter(t, model, "boardingpass")
	if len(model.search.matches) != 1 || !strings.Contains(model.View(), "1 match") {
		t.Fatalf("attachment filter matches = %d, want 1", len(model.search.matches))
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("absent")})
	model = updated.(Model)
	if len(model.search.matches) != 0 || !strings.Contains(model.View(), "No loaded messages match") {
		t.Fatal("live no-match filter state is not visible")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.search.input.Value() != "boardingpass" || len(model.search.matches) != 1 {
		t.Fatal("Esc while editing did not restore previously applied filter")
	}
}

func applyConversationFilter(t *testing.T, model Model, query string) Model {
	t.Helper()
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(query)})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return updated.(Model)
}

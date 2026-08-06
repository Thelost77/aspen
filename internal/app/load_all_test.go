package app

import (
	"strings"
	"testing"
	"time"

	"github.com/Thelost77/aspen/internal/messages"
	tea "github.com/charmbracelet/bubbletea"
)

func TestLoadAllChainsOlderPagesAndUpdatesFilter(t *testing.T) {
	model, _ := loadTestModel(t)
	model.focus = FocusViewport
	model.threads[1].older = &messages.HistoryCursor{Date: 300, MessageID: 30}
	model = applyConversationFilter(t, model, "needle")

	updated, firstCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	model = updated.(Model)
	state := model.threads[1]
	if firstCmd == nil || !state.loadingAll || !state.loadingOlder {
		t.Fatalf("load-all did not start: loadingAll=%v loadingOlder=%v", state.loadingAll, state.loadingOlder)
	}
	generation := state.generation

	updated, nextCmd := model.Update(HistoryLoadedMsg{
		ChatID:     1,
		Generation: generation,
		Prepend:    true,
		LoadedAt:   time.Now(),
		Page: messages.HistoryPage{
			Messages: []messages.Message{{ID: 20, ChatID: 1, Text: "needle in older page", SentAt: time.Now().Add(-time.Hour)}},
			Older:    &messages.HistoryCursor{Date: 200, MessageID: 20},
		},
	})
	model = updated.(Model)
	state = model.threads[1]
	if nextCmd == nil || !state.loadingAll || !state.loadingOlder {
		t.Fatalf("load-all did not chain: loadingAll=%v loadingOlder=%v", state.loadingAll, state.loadingOlder)
	}
	if len(model.search.matches) != 1 {
		t.Fatalf("active filter matches = %d after older page, want 1", len(model.search.matches))
	}

	updated, _ = model.Update(HistoryLoadedMsg{
		ChatID:     1,
		Generation: generation,
		Prepend:    true,
		LoadedAt:   time.Now(),
		Page: messages.HistoryPage{
			Messages: []messages.Message{{ID: 10, ChatID: 1, Text: "oldest", SentAt: time.Now().Add(-2 * time.Hour)}},
		},
	})
	model = updated.(Model)
	state = model.threads[1]
	if state.loadingAll || state.loadingOlder || state.older != nil {
		t.Fatalf("load-all did not finish: loadingAll=%v loadingOlder=%v older=%v", state.loadingAll, state.loadingOlder, state.older)
	}
	if !strings.Contains(model.status, "Full conversation loaded") {
		t.Fatalf("completion status = %q", model.status)
	}
}

func TestLoadAllCanStopAndChatSwitchCancels(t *testing.T) {
	model, _ := loadTestModel(t)
	model.focus = FocusViewport
	model.threads[1].older = &messages.HistoryCursor{Date: 300, MessageID: 30}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	model = updated.(Model)
	if model.threads[1].loadingAll || !model.threads[1].loadingOlder {
		t.Fatalf("second A did not stop chaining after in-flight page: %#v", model.threads[1])
	}

	model.threads[1].loadingAll = true
	_ = model.selectChat(2)
	if model.threads[1].loadingAll {
		t.Fatal("switching conversations did not stop load-all")
	}
}

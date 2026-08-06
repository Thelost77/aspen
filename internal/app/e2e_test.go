package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBrowseComposeSendRefreshAndQuitJourney(t *testing.T) {
	model, _, messageSender := loadTestModelWithSender(t)

	updated, historyCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = executeCmd(t, updated.(Model), historyCmd)
	if model.selectedID != 2 {
		t.Fatalf("browse selected %d, want 2", model.selectedID)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fixture journey")})
	model = updated.(Model)
	updated, sendCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = executeCmd(t, updated.(Model), sendCmd)
	if len(messageSender.targets) != 1 || messageSender.targets[0].ChatID != 2 {
		t.Fatalf("send targets = %#v", messageSender.targets)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	updated, refreshCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = executeCmd(t, updated.(Model), refreshCmd)
	if model.selectedID != 2 {
		t.Fatalf("refresh changed selection to %d", model.selectedID)
	}

	updated, quitCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_ = updated
	if quitCmd == nil {
		t.Fatal("quit command missing")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit command returned %T", quitCmd())
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Thelost77/aspen/internal/logger"
	tea "github.com/charmbracelet/bubbletea"
)

func TestOperationLogsExcludeMessageAndContactContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aspen.log")
	cleanup, err := logger.InitAt(path, true)
	if err != nil {
		t.Fatal(err)
	}

	model, _, _ := loadTestModelWithSender(t)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	privateBody := "PRIVATE-MESSAGE-BODY-7f911"
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(privateBody)})
	model = updated.(Model)
	updated, sendCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = executeCmd(t, updated.(Model), sendCmd)
	_ = model
	cleanup()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{privateBody, "+15550000001", "Alice", "chat-a"} {
		if strings.Contains(string(content), private) {
			t.Fatalf("log leaked %q:\n%s", private, content)
		}
	}
	if !strings.Contains(string(content), "send command completed") {
		t.Fatalf("operation log missing:\n%s", content)
	}
}

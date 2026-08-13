package app

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/logger"
	"github.com/Thelost77/aspen/internal/messages"
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

	privateImageName := "PRIVATE-IMAGE-NAME-287dc.png"
	privateImagePath := filepath.Join(t.TempDir(), privateImageName)
	file, err := os.Create(privateImagePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	model.imageProtocol = inlineimage.Kitty
	model.threads[1].messages[0].Attachments = []messages.Attachment{{
		ID: 7, Name: privateImageName, Path: privateImagePath, MIMEType: "image/png", IsImage: true,
	}}
	model.syncViewport(false)
	model.viewport.GotoBottom()
	model = executeCmd(t, model, model.refreshInlineImages())
	cleanup()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{privateBody, privateImageName, privateImagePath, "+15550000001", "Alice", "chat-a"} {
		if strings.Contains(string(content), private) {
			t.Fatalf("log leaked %q:\n%s", private, content)
		}
	}
	if !strings.Contains(string(content), "send command completed") || !strings.Contains(string(content), "inline image render completed") {
		t.Fatalf("operation log missing:\n%s", content)
	}
}

package app

import (
	"strings"
	"testing"
	"time"

	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/messages"
	"github.com/Thelost77/aspen/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderMessagesSeparatesAdjacentBubbles(t *testing.T) {
	base := time.Date(2026, 8, 5, 14, 0, 0, 0, time.Local)
	chat := messages.Chat{ID: 1, DisplayName: "Alice"}
	rendered := ansi.Strip(renderMessages([]messages.Message{
		{ID: 1, ChatID: 1, Sender: "+15550000001", Text: "first", SentAt: base},
		{ID: 2, ChatID: 1, Sender: "+15550000001", Text: "second", SentAt: base.Add(time.Minute)},
	}, chat, 60, ui.DefaultStyles()))

	blocks := strings.Split(rendered, "\n\n")
	if len(blocks) != 3 {
		t.Fatalf("rendered %d blocks, want date separator plus two bubbles:\n%s", len(blocks), rendered)
	}
	if !strings.Contains(blocks[1], "first") || !strings.Contains(blocks[2], "second") {
		t.Fatalf("message boundaries are unclear:\n%s", rendered)
	}
	if strings.Contains(rendered, "Alice") {
		t.Fatalf("direct thread redundantly labels sender:\n%s", rendered)
	}
}

func TestRenderMessagePreservesUnicodeSpacing(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"Dziś czytam książkę",
		"Dziś\u00a0czytam książkę",
		"Cena: 10\u202f000 zł",
		"Programistka 👩\u200d💻",
	} {
		t.Run(text, func(t *testing.T) {
			message := messages.Message{ID: 1, ChatID: 1, Text: text}
			for _, outgoing := range []bool{false, true} {
				message.IsFromMe = outgoing
				block := renderMessageInline(message, messages.Chat{ID: 1}, 60, false, ui.DefaultStyles(), nil)
				if rendered := ansi.Strip(block.content); !strings.Contains(rendered, text) {
					t.Fatalf("outgoing=%t: message text %q changed during rendering:\n%s", outgoing, text, rendered)
				}
			}
		})
	}
}

func TestInlineImageIsInsideSingleMessageOutline(t *testing.T) {
	attachment := messages.Attachment{ID: 7, Name: "IMG.heic", IsImage: true}
	message := messages.Message{ID: 1, ChatID: 1, Attachments: []messages.Attachment{attachment}}
	imageState := &inlineImageState{rendered: inlineimage.Rendered{Protocol: inlineimage.Kitty, ID: 42, Columns: 10, Rows: 3}}
	block := renderMessageInline(message, messages.Chat{ID: 1}, 40, false, ui.DefaultStyles(), func(messages.ChatID, messages.Attachment) *inlineImageState {
		return imageState
	})
	plain := strings.Split(ansi.Strip(block.content), "\n")
	if len(block.placements) != 1 {
		t.Fatalf("placements = %d, want 1", len(block.placements))
	}
	placement := block.placements[0]
	if !strings.Contains(ansi.Strip(block.content), "\U0010eeee") {
		t.Fatalf("message bubble lacks Kitty placeholders:\n%s", ansi.Strip(block.content))
	}
	if placement.startLine != 1 || !strings.Contains(plain[0], "╭") || !strings.Contains(plain[placement.startLine+placement.height], "╰") {
		t.Fatalf("image is not enclosed by one rounded outline: placement=%#v\n%s", placement, ansi.Strip(block.content))
	}
}

func TestRenderMessagesLabelsGroupSenderOncePerRun(t *testing.T) {
	base := time.Date(2026, 8, 5, 14, 0, 0, 0, time.Local)
	chat := messages.Chat{ID: 1, DisplayName: "Group", IsGroup: true}
	rendered := ansi.Strip(renderMessages([]messages.Message{
		{ID: 1, ChatID: 1, Sender: "+15550000001", SenderName: "Alice", Text: "first", SentAt: base},
		{ID: 2, ChatID: 1, Sender: "+15550000001", SenderName: "Alice", Text: "second", SentAt: base.Add(time.Minute)},
	}, chat, 60, ui.DefaultStyles()))
	if strings.Count(rendered, "Alice") != 1 {
		t.Fatalf("group sender label count = %d, want 1:\n%s", strings.Count(rendered, "Alice"), rendered)
	}
}

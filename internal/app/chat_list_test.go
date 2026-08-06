package app

import (
	"strings"
	"testing"
	"time"

	"github.com/Thelost77/aspen/internal/messages"
	"github.com/Thelost77/aspen/internal/ui"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/x/ansi"
)

func TestChatListDoesNotRenderUnreadCount(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 38, 0, 0, time.Local)
	item := chatItem{chat: messages.Chat{
		ID:            1,
		DisplayName:   "Alice",
		LastMessageAt: now,
		UnreadCount:   15,
	}}
	delegate := chatDelegate{styles: ui.DefaultStyles(), now: func() time.Time { return now }}
	model := list.New([]list.Item{item}, delegate, 30, 4)

	var rendered strings.Builder
	delegate.Render(&rendered, model, 0, item)
	plain := ansi.Strip(rendered.String())
	if !strings.Contains(plain, "12:38") || strings.Contains(plain, "15") {
		t.Fatalf("chat row contains unread count: %q", plain)
	}
}

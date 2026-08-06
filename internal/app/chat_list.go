package app

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Thelost77/aspen/internal/messages"
	"github.com/Thelost77/aspen/internal/ui"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const chatRowHeight = 2

type chatItem struct {
	chat messages.Chat
}

func (i chatItem) Title() string { return i.chat.Title() }
func (i chatItem) Description() string {
	preview := strings.Join(strings.Fields(i.chat.LastMessageText), " ")
	if i.chat.Service != "" && i.chat.Service != "iMessage" {
		if preview != "" {
			preview += " · "
		}
		preview += i.chat.Service
	}
	return preview
}
func (i chatItem) FilterValue() string {
	parts := []string{i.chat.Title(), i.chat.Identifier}
	for _, participant := range i.chat.Participants {
		parts = append(parts, participant.Name, participant.Handle)
	}
	return strings.Join(parts, " ")
}

type chatDelegate struct {
	styles ui.Styles
	now    func() time.Time
}

func (d chatDelegate) Height() int                         { return chatRowHeight }
func (d chatDelegate) Spacing() int                        { return 0 }
func (d chatDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d chatDelegate) Render(w io.Writer, model list.Model, index int, raw list.Item) {
	item, ok := raw.(chatItem)
	if !ok {
		return
	}
	width := max(4, model.Width())
	selected := index == model.Index()
	prefix := "  "
	if selected {
		prefix = "› "
	}

	right := ui.ConversationTime(item.chat.LastMessageAt, d.now())
	leftWidth := max(1, width-lipgloss.Width(prefix)-lipgloss.Width(right)-1)
	title := ui.Truncate(item.Title(), leftWidth)
	first := prefix + ui.PadRight(title, leftWidth) + " " + right
	first = ui.PadRight(first, width)

	previewWidth := max(1, width-4)
	preview := ui.Truncate(item.Description(), previewWidth)
	second := "  " + ui.PadRight(preview, previewWidth) + "  "
	second = ui.PadRight(second, width)
	if selected {
		first = d.styles.Selected.Width(width).Render(first)
		second = d.styles.Selected.Foreground(d.styles.Muted.GetForeground()).Width(width).Render(second)
	} else {
		second = d.styles.Muted.Render(second)
	}
	_, _ = fmt.Fprint(w, first+"\n"+second)
}

type chatList struct {
	model list.Model
}

func newChatList(styles ui.Styles) chatList {
	delegate := chatDelegate{styles: styles, now: time.Now}
	model := list.New(nil, delegate, 0, 0)
	model.Title = "Conversations"
	model.SetShowStatusBar(false)
	model.SetShowHelp(false)
	model.SetFilteringEnabled(true)
	model.DisableQuitKeybindings()
	model.Styles.Title = styles.Title.PaddingLeft(1)
	model.Styles.FilterPrompt = styles.FilterPrompt
	model.Styles.FilterCursor = styles.Accent
	model.Styles.PaginationStyle = styles.Muted
	model.Styles.NoItems = styles.Muted
	return chatList{model: model}
}

func (l *chatList) SetSize(width, height int) {
	l.model.SetSize(max(1, width), max(1, height))
}

func (l *chatList) SetChats(chats []messages.Chat, selected messages.ChatID) tea.Cmd {
	items := make([]list.Item, len(chats))
	for i := range chats {
		items[i] = chatItem{chat: chats[i]}
	}
	cmd := l.model.SetItems(items)
	l.SelectID(selected)
	return cmd
}

func (l *chatList) SelectID(id messages.ChatID) bool {
	if id == 0 {
		return false
	}
	for i, raw := range l.model.VisibleItems() {
		if item, ok := raw.(chatItem); ok && item.chat.ID == id {
			l.model.Select(i)
			return true
		}
	}
	return false
}

func (l chatList) Selected() (messages.Chat, bool) {
	item, ok := l.model.SelectedItem().(chatItem)
	return item.chat, ok
}

func (l chatList) IsFiltering() bool {
	return l.model.FilterState() == list.Filtering
}

func (l chatList) HasFilter() bool {
	return l.model.FilterValue() != "" || l.model.FilterState() == list.FilterApplied
}

func (l *chatList) ClearFilter() {
	l.model.ResetFilter()
}

func (l *chatList) GoToStart() { l.model.Select(0) }

func (l *chatList) GoToEnd() {
	if visible := l.model.VisibleItems(); len(visible) > 0 {
		l.model.Select(len(visible) - 1)
	}
}

func (l *chatList) PageUp()   { l.model.PrevPage() }
func (l *chatList) PageDown() { l.model.NextPage() }

func (l *chatList) ScrollUp(lines int) {
	for range max(0, lines) {
		l.model.CursorUp()
	}
}

func (l *chatList) ScrollDown(lines int) {
	for range max(0, lines) {
		l.model.CursorDown()
	}
}

func (l *chatList) SelectItemRow(row int) (messages.Chat, bool) {
	if row < 0 {
		return messages.Chat{}, false
	}
	visible := l.model.VisibleItems()
	start, end := l.model.Paginator.GetSliceBounds(len(visible))
	index := start + row/chatRowHeight
	if index < start || index >= end {
		return messages.Chat{}, false
	}
	l.model.Select(index)
	return l.Selected()
}

func (l chatList) View() string { return l.model.View() }

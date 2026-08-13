package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Thelost77/aspen/internal/messages"
	msgsender "github.com/Thelost77/aspen/internal/sender"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type mapResolver map[string]string

func (r mapResolver) Resolve(handle string) string {
	if name := r[handle]; name != "" {
		return name
	}
	return handle
}

type fakeMessageSender struct {
	targets []msgsender.SendTarget
	texts   []string
	err     error
}

func (f *fakeMessageSender) Send(_ context.Context, target msgsender.SendTarget, text string) error {
	f.targets = append(f.targets, target)
	f.texts = append(f.texts, text)
	return f.err
}

type fakeStore struct {
	chats             []messages.Chat
	histories         map[messages.ChatID]messages.HistoryPage
	changeVersion     int64
	chatErr           error
	historyErrs       map[messages.ChatID]error
	changeVersionErr  error
	conversationCalls int
	historyCalls      int
	historyIDs        []messages.ChatID
	calls             []string
	closed            bool
}

func (f *fakeStore) Conversations(context.Context, int) ([]messages.Chat, error) {
	f.conversationCalls++
	f.calls = append(f.calls, "conversations")
	return append([]messages.Chat(nil), f.chats...), f.chatErr
}

func (f *fakeStore) History(_ context.Context, id messages.ChatID, _ int, _ *messages.HistoryCursor) (messages.HistoryPage, error) {
	f.historyCalls++
	f.historyIDs = append(f.historyIDs, id)
	f.calls = append(f.calls, "history")
	return f.histories[id], f.historyErrs[id]
}

func (f *fakeStore) ChangeVersion(context.Context) (int64, error) {
	f.calls = append(f.calls, "change-version")
	return f.changeVersion, f.changeVersionErr
}

func (f *fakeStore) Close() error {
	f.closed = true
	return nil
}

func testChats() []messages.Chat {
	now := time.Date(2026, 8, 5, 14, 30, 0, 0, time.Local)
	return []messages.Chat{
		{ID: 1, GUID: "chat-a", Identifier: "+15550000001", DisplayName: "Alice", Service: "iMessage", Participants: []messages.Participant{{Handle: "+15550000001"}}, LastMessageAt: now, LastMessageText: "Alpha preview"},
		{ID: 2, GUID: "chat-b", Identifier: "+15550000002", DisplayName: "Bob 世界", Service: "SMS", Participants: []messages.Participant{{Handle: "+15550000002"}}, LastMessageAt: now.Add(-time.Minute), LastMessageText: "Bravo preview", UnreadCount: 2},
	}
}

func testHistories() map[messages.ChatID]messages.HistoryPage {
	base := time.Date(2026, 8, 5, 14, 0, 0, 0, time.Local)
	return map[messages.ChatID]messages.HistoryPage{
		1: {Messages: []messages.Message{{ID: 11, ChatID: 1, Text: "alpha body", Sender: "+15550000001", SentAt: base}}},
		2: {Messages: []messages.Message{{ID: 21, ChatID: 2, Text: "bravo body with emoji 🦊", Sender: "+15550000002", SentAt: base.Add(time.Minute)}}},
	}
}

func newTestModel() (Model, *fakeStore) {
	store := &fakeStore{chats: testChats(), histories: testHistories(), changeVersion: 21, historyErrs: make(map[messages.ChatID]error)}
	model := NewWithStore(store)
	model.messagePollInterval = 0
	model.now = func() time.Time { return time.Date(2026, 8, 5, 14, 32, 0, 0, time.Local) }
	result, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return result.(Model), store
}

func executeCmd(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	processed := 0
	for len(queue) > 0 {
		processed++
		if processed > 100 {
			t.Fatal("command queue did not settle")
		}
		current := queue[0]
		queue = queue[1:]
		if current == nil {
			continue
		}
		msg := current()
		typeName := fmt.Sprintf("%T", msg)
		if strings.Contains(typeName, "BlinkMsg") || strings.Contains(typeName, "spinner.TickMsg") {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		updated, next := model.Update(msg)
		model = updated.(Model)
		if next != nil {
			queue = append(queue, next)
		}
	}
	return model
}

func loadTestModel(t *testing.T) (Model, *fakeStore) {
	t.Helper()
	model, store := newTestModel()
	model = executeCmd(t, model, model.Init())
	return model, store
}

func loadTestModelWithSender(t *testing.T) (Model, *fakeStore, *fakeMessageSender) {
	t.Helper()
	store := &fakeStore{chats: testChats(), histories: testHistories(), changeVersion: 21, historyErrs: make(map[messages.ChatID]error)}
	messageSender := &fakeMessageSender{}
	model := NewWithStore(store, messageSender)
	model.messagePollInterval = 0
	model.now = func() time.Time { return time.Date(2026, 8, 5, 14, 32, 0, 0, time.Local) }
	result, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = executeCmd(t, result.(Model), result.(Model).Init())
	return model, store, messageSender
}

func TestMessagePollingRefreshesOnlyChangedData(t *testing.T) {
	model, store := loadTestModel(t)
	if len(store.calls) < 3 || store.calls[0] != "change-version" || store.calls[1] != "change-version" || store.calls[2] != "conversations" {
		t.Fatalf("startup calls = %v, want change versions before conversations", store.calls)
	}

	conversationCalls := store.conversationCalls
	historyCalls := store.historyCalls
	updated, cmd := model.Update(messagePollTickMsg{})
	model = executeCmd(t, updated.(Model), cmd)
	if store.conversationCalls != conversationCalls || store.historyCalls != historyCalls {
		t.Fatalf("unchanged token reloaded data: conversations=%d history=%d", store.conversationCalls, store.historyCalls)
	}

	store.changeVersion = 22
	store.chats[0].LastMessageText = "new preview"
	store.histories[1] = messages.HistoryPage{Messages: append(
		append([]messages.Message(nil), store.histories[1].Messages...),
		messages.Message{ID: 22, ChatID: 1, Text: "new body", SentAt: time.Now()},
	)}
	updated, cmd = model.Update(messagePollTickMsg{})
	model = executeCmd(t, updated.(Model), cmd)

	if store.conversationCalls != conversationCalls+1 || store.historyCalls != historyCalls+1 {
		t.Fatalf("selected-chat change loads: conversations=%d history=%d", store.conversationCalls, store.historyCalls)
	}
	if model.messageChangeVersion != 22 {
		t.Fatalf("change version = %d, want 22", model.messageChangeVersion)
	}
	if got := model.chatByID[1].LastMessageText; got != "new preview" {
		t.Fatalf("preview = %q, want new preview", got)
	}
	if got := model.threads[1].messages[len(model.threads[1].messages)-1].ID; got != 22 {
		t.Fatalf("latest rendered message = %d, want 22", got)
	}
}

func TestMessagePollingPreservesLoadedHistoryAndLoadAll(t *testing.T) {
	model, store := loadTestModel(t)
	state := model.threads[1]
	state.messages = make([]messages.Message, 150)
	for i := range state.messages {
		state.messages[i] = messages.Message{ID: messages.MessageID(i + 1), ChatID: 1, Text: fmt.Sprintf("message %d", i+1)}
	}
	state.older = &messages.HistoryCursor{Date: 1, MessageID: 1}
	state.loadingAll = true
	store.changeVersion = 22
	store.histories[1] = messages.HistoryPage{Messages: []messages.Message{
		{ID: 150, ChatID: 1, Text: "updated 150"},
		{ID: 151, ChatID: 1, Text: "new 151"},
	}}

	updated, cmd := model.Update(messagePollTickMsg{})
	model = executeCmd(t, updated.(Model), cmd)
	state = model.threads[1]
	if len(state.messages) != 151 || state.messages[149].Text != "updated 150" || state.messages[150].ID != 151 {
		t.Fatalf("merged history was not preserved: count=%d tail=%#v", len(state.messages), state.messages[149:])
	}
	if !state.loadingAll || state.older == nil {
		t.Fatalf("background refresh changed load-all state: loadingAll=%v older=%v", state.loadingAll, state.older)
	}
}

func TestMessagePollingDefersMergeUntilCurrentHistoryLoadFinishes(t *testing.T) {
	model, store := loadTestModel(t)
	state := model.threads[1]
	state.loading = true
	store.changeVersion = 22

	updated, checkCmd := model.Update(messagePollTickMsg{})
	model = updated.(Model)
	updated, refreshCmd := model.Update(checkCmd())
	model = updated.(Model)
	if !model.pendingLatestMerges[1] {
		t.Fatal("busy selected history did not queue a latest-page merge")
	}
	_ = refreshCmd

	state.loading = false
	updated, mergeCmd := model.Update(HistoryLoadedMsg{
		ChatID: 1, Generation: state.generation, Page: store.histories[1], LoadedAt: time.Now(),
	})
	model = updated.(Model)
	if mergeCmd == nil || model.pendingLatestMerges[1] {
		t.Fatal("completed history did not start the queued latest-page merge")
	}
	model = executeCmd(t, model, mergeCmd)
	if model.threads[1].loading {
		t.Fatal("queued latest-page merge did not settle")
	}
}

func TestMessagePollingDefersMergeUntilOlderLoadFinishes(t *testing.T) {
	model, store := loadTestModel(t)
	state := model.threads[1]
	state.loadingOlder = true
	store.changeVersion = 22

	updated, checkCmd := model.Update(messagePollTickMsg{})
	model = updated.(Model)
	updated, refreshCmd := model.Update(checkCmd())
	model = updated.(Model)
	if !model.pendingLatestMerges[1] {
		t.Fatal("older load did not queue a latest-page merge")
	}
	_ = refreshCmd

	updated, mergeCmd := model.Update(HistoryLoadedMsg{
		ChatID: 1, Generation: state.generation, Prepend: true, Page: messages.HistoryPage{Messages: []messages.Message{}}, LoadedAt: time.Now(),
	})
	model = updated.(Model)
	if mergeCmd == nil || model.pendingLatestMerges[1] {
		t.Fatal("completed older load did not start the queued latest-page merge")
	}
	model = executeCmd(t, model, mergeCmd)
	if model.threads[1].loading {
		t.Fatal("queued latest-page merge did not settle")
	}
}

func TestFailedLatestMergeRetriesOnNextPoll(t *testing.T) {
	model, _ := loadTestModel(t)
	state := model.threads[1]
	state.generation++
	state.loading = true
	state.err = nil
	updated, _ := model.Update(HistoryLoadedMsg{
		ChatID: 1, Generation: state.generation, MergeLatest: true, Err: fmt.Errorf("busy"), LoadedAt: time.Now(),
	})
	model = updated.(Model)
	if !model.pendingLatestMerges[1] {
		t.Fatal("failed latest-page merge was not retained for retry")
	}

	updated, cmd := model.Update(messagePollTickMsg{})
	model = executeCmd(t, updated.(Model), cmd)
	if model.pendingLatestMerges[1] || model.threads[1].loading {
		t.Fatal("next poll did not settle the retained latest-page merge")
	}
}

func TestMessagePollingIgnoresTransientCursorErrors(t *testing.T) {
	model, store := loadTestModel(t)
	conversationCalls := store.conversationCalls
	historyCalls := store.historyCalls
	store.changeVersionErr = fmt.Errorf("busy")

	updated, cmd := model.Update(messagePollTickMsg{})
	model = executeCmd(t, updated.(Model), cmd)
	if store.conversationCalls != conversationCalls || store.historyCalls != historyCalls {
		t.Fatal("cursor error reloaded data")
	}
	if model.fatalErr != nil || model.bannerErr != nil {
		t.Fatalf("cursor error reached UI: fatal=%v banner=%v", model.fatalErr, model.bannerErr)
	}
}

func TestAutomaticRefreshCompletionOrderDoesNotMatter(t *testing.T) {
	for _, historyFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("historyFirst=%v", historyFirst), func(t *testing.T) {
			model, store := loadTestModel(t)
			store.changeVersion = 22
			updated, checkCmd := model.Update(messagePollTickMsg{})
			model = updated.(Model)
			changeMsg := checkCmd()
			updated, refreshCmd := model.Update(changeMsg)
			model = updated.(Model)
			refreshMsg := refreshCmd()
			batch, ok := refreshMsg.(tea.BatchMsg)
			if !ok || len(batch) != 2 {
				t.Fatalf("refresh command = %T", refreshMsg)
			}
			messagesByType := make(map[string]tea.Msg)
			for _, cmd := range batch {
				msg := cmd()
				messagesByType[fmt.Sprintf("%T", msg)] = msg
			}
			ordered := []tea.Msg{messagesByType["app.ChatsLoadedMsg"], messagesByType["app.HistoryLoadedMsg"]}
			if historyFirst {
				ordered[0], ordered[1] = ordered[1], ordered[0]
			}
			for _, msg := range ordered {
				updated, _ = model.Update(msg)
				model = updated.(Model)
			}
			if model.messageChangeVersion != 22 || model.loadingChats || model.threads[1].loading {
				t.Fatalf("settled state: token=%d loadingChats=%v loadingHistory=%v", model.messageChangeVersion, model.loadingChats, model.threads[1].loading)
			}
		})
	}
}

func TestManualRefreshDoesNotCauseDuplicateAutomaticRefresh(t *testing.T) {
	model, store := loadTestModel(t)
	store.changeVersion = 22

	updated, refreshCmd := model.refresh()
	model = executeCmd(t, updated.(Model), refreshCmd)
	conversationCalls := store.conversationCalls
	historyCalls := store.historyCalls

	updated, pollCmd := model.Update(messagePollTickMsg{})
	_ = executeCmd(t, updated.(Model), pollCmd)
	if store.conversationCalls != conversationCalls || store.historyCalls != historyCalls {
		t.Fatal("poll repeated a completed manual refresh")
	}
}

func TestChangeVersionRollbackTriggersFullRefresh(t *testing.T) {
	model, store := loadTestModel(t)
	model.messageChangeVersion = 50
	store.changeVersion = 1
	conversationCalls := store.conversationCalls
	historyCalls := store.historyCalls

	updated, cmd := model.Update(messagePollTickMsg{})
	model = executeCmd(t, updated.(Model), cmd)
	if store.conversationCalls != conversationCalls+1 || store.historyCalls != historyCalls+1 {
		t.Fatalf("rollback loads: conversations=%d history=%d", store.conversationCalls-conversationCalls, store.historyCalls-historyCalls)
	}
	if model.messageChangeVersion != 1 {
		t.Fatalf("rollback token = %d, want 1", model.messageChangeVersion)
	}
}

func TestInFlightPollDoesNotSupersedeManualRefresh(t *testing.T) {
	model, store := loadTestModel(t)
	store.changeVersion = 22
	updated, checkCmd := model.Update(messagePollTickMsg{})
	model = updated.(Model)

	updated, refreshCmd := model.refresh()
	model = updated.(Model)
	chatGeneration := model.chatGeneration
	historyGeneration := model.threads[model.selectedID].generation
	updated, cmd := model.Update(checkCmd())
	model = updated.(Model)
	if model.chatGeneration != chatGeneration || model.threads[model.selectedID].generation != historyGeneration {
		t.Fatal("in-flight poll superseded manual refresh generations")
	}
	if cmd != nil {
		t.Fatal("in-flight poll started refresh work")
	}
	_ = refreshCmd
}

func TestMessagePollingWaitsForRefreshInProgress(t *testing.T) {
	model, _ := loadTestModel(t)
	updated, refreshCmd := model.refresh()
	model = updated.(Model)
	if refreshCmd == nil || !model.loadingChats {
		t.Fatal("manual refresh did not start")
	}
	chatGeneration := model.chatGeneration
	historyGeneration := model.threads[model.selectedID].generation

	updated, duplicateCmd := model.Update(messagePollTickMsg{})
	model = updated.(Model)
	if duplicateCmd != nil {
		t.Fatal("pending refresh scheduled database work")
	}
	if model.chatGeneration != chatGeneration || model.threads[model.selectedID].generation != historyGeneration {
		t.Fatal("pending refresh advanced load generations")
	}
}

func TestContactsEnrichDisplayOnlyAndDoNotBlockStartup(t *testing.T) {
	store := &fakeStore{chats: testChats(), histories: testHistories(), changeVersion: 21, historyErrs: make(map[messages.ChatID]error)}
	model := NewWithStore(store)
	model.messagePollInterval = 0
	model.SetResolverLoader(func() (NameResolver, error) {
		return mapResolver{
			"+15550000001": "Alice Resolved",
			"+15550000002": "Bob Resolved",
		}, fmt.Errorf("one optional source was unreadable")
	})
	result, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = executeCmd(t, result.(Model), result.(Model).Init())

	chat := model.chatByID[1]
	if chat.DisplayName != "Alice" {
		t.Fatalf("database display name should win, got %q", chat.DisplayName)
	}
	if chat.Participants[0].Name != "Alice Resolved" || chat.Participants[0].Handle != "+15550000001" || chat.GUID != "chat-a" {
		t.Fatalf("contact enrichment changed stable identity: %#v", chat)
	}
	if model.threads[1].messages[0].SenderName != "Alice Resolved" {
		t.Fatalf("sender name = %q", model.threads[1].messages[0].SenderName)
	}
	if model.fatalErr != nil {
		t.Fatalf("optional contact error became fatal: %v", model.fatalErr)
	}
}

func TestStartupLoadsChatsAndFirstHistory(t *testing.T) {
	model, _ := loadTestModel(t)
	if model.selectedID != 1 {
		t.Fatalf("selected chat = %d, want 1", model.selectedID)
	}
	state := model.threads[1]
	if state == nil || !state.loaded || len(state.messages) != 1 {
		t.Fatalf("first history state = %#v", state)
	}
	view := model.View()
	for _, want := range []string{"Alice", "alpha body"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "Aspen › Messages") {
		t.Fatalf("redundant global breadcrumb rendered\n%s", view)
	}
}

func TestLateHistoryCannotRenderUnderAnotherChat(t *testing.T) {
	model, _ := newTestModel()

	changeVersionMsg := model.Init()()
	updated, chatsCmd := model.Update(changeVersionMsg)
	model = updated.(Model)
	if chatsCmd == nil {
		t.Fatal("startup did not request conversations")
	}
	updated, historyACmd := model.Update(chatsCmd())
	model = updated.(Model)
	if historyACmd == nil {
		t.Fatal("startup did not request first history")
	}

	updated, historyBCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(Model)
	if model.selectedID != 2 || historyBCmd == nil {
		t.Fatalf("selected=%d cmd=%v", model.selectedID, historyBCmd)
	}

	historyAMsg := historyACmd()
	if batch, ok := historyAMsg.(tea.BatchMsg); ok {
		for _, cmd := range batch {
			if cmd == nil {
				continue
			}
			updated, _ = model.Update(cmd())
			model = updated.(Model)
		}
	} else {
		updated, _ = model.Update(historyAMsg)
		model = updated.(Model)
	}
	if model.selectedID != 2 || strings.Contains(model.viewport.View(), "alpha body") {
		t.Fatalf("late A rendered under B\n%s", model.View())
	}

	model = executeCmd(t, model, historyBCmd)
	if !strings.Contains(model.View(), "bravo body") || strings.Contains(model.viewport.View(), "alpha body") {
		t.Fatalf("B history not isolated\n%s", model.View())
	}
}

func TestConversationReorderPreservesSelectedChatID(t *testing.T) {
	model, _ := loadTestModel(t)
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = executeCmd(t, updated.(Model), cmd)
	if model.selectedID != 2 {
		t.Fatalf("selected chat = %d", model.selectedID)
	}

	model.chatGeneration++
	reordered := []messages.Chat{testChats()[1], testChats()[0]}
	updated, _ = model.Update(ChatsLoadedMsg{Generation: model.chatGeneration, Chats: reordered, LoadedAt: model.now()})
	model = updated.(Model)
	selected, ok := model.chatList.Selected()
	if !ok || model.selectedID != 2 || selected.ID != 2 {
		t.Fatalf("selection after reorder: model=%d list=%#v", model.selectedID, selected)
	}
}

func TestFilterOwnsTextAndClearKeepsStableSelection(t *testing.T) {
	model, _ := loadTestModel(t)
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = executeCmd(t, updated.(Model), cmd)
	if !model.chatList.IsFiltering() {
		t.Fatal("filter did not activate")
	}
	for _, runeValue := range []rune{'q', 'B', 'o', 'b'} {
		updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{runeValue}})
		model = executeCmd(t, updated.(Model), cmd)
	}
	if model.chatList.model.FilterValue() != "qBob" {
		t.Fatalf("filter value = %q", model.chatList.model.FilterValue())
	}
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = executeCmd(t, updated.(Model), cmd)
	if model.chatList.IsFiltering() {
		t.Fatal("filter remained in editing state")
	}
}

func TestMouseClickOpensConversationContext(t *testing.T) {
	model, _ := loadTestModel(t)
	updated, cmd := model.Update(tea.MouseMsg{
		X: 5, Y: 4,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	model = executeCmd(t, updated.(Model), cmd)
	if model.selectedID != 2 || model.focus != FocusViewport {
		t.Fatalf("mouse click selected=%d focus=%v, want chat 2 viewport", model.selectedID, model.focus)
	}

	updated, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	model = updated.(Model)
	model.narrowPane = NarrowList
	model.focus = FocusList
	model.chatList.SelectID(1)
	model.selectedID = 1
	updated, cmd = model.Update(tea.MouseMsg{
		X: 5, Y: 2,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	model = executeCmd(t, updated.(Model), cmd)
	if model.narrowPane != NarrowConversation || model.focus != FocusViewport {
		t.Fatalf("narrow click pane=%v focus=%v", model.narrowPane, model.focus)
	}
}

func TestMouseWheelScrollsConversationList(t *testing.T) {
	model, _ := loadTestModel(t)
	updated, cmd := model.Update(tea.MouseMsg{
		X: 5, Y: 5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	model = executeCmd(t, updated.(Model), cmd)
	if model.selectedID != 2 || model.focus != FocusList {
		t.Fatalf("mouse wheel selected=%d focus=%v, want chat 2 list", model.selectedID, model.focus)
	}

	updated, cmd = model.Update(tea.MouseMsg{
		X: 5, Y: 5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	model = executeCmd(t, updated.(Model), cmd)
	if model.selectedID != 1 {
		t.Fatalf("mouse wheel up selected=%d, want chat 1", model.selectedID)
	}
}

func TestFocusedPaneHasVisibleBorder(t *testing.T) {
	model, _ := loadTestModel(t)
	view := ansi.Strip(model.View())
	if strings.Count(view, "╭") < 2 || strings.Count(view, "╯") < 2 {
		t.Fatalf("split panes do not have visible rounded borders:\n%s", view)
	}
}

func TestHelpSwallowsNavigation(t *testing.T) {
	model, _ := loadTestModel(t)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	model = updated.(Model)
	if !model.help.Visible() {
		t.Fatal("help did not open")
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(Model)
	if cmd != nil || model.selectedID != 1 {
		t.Fatal("navigation leaked through help")
	}
	if !strings.Contains(model.View(), "Keybindings") {
		t.Fatal("help overlay not rendered")
	}
}

func TestResizePreservesSelectionAndScroll(t *testing.T) {
	model, store := loadTestModel(t)
	longMessages := make([]messages.Message, 50)
	for i := range longMessages {
		longMessages[i] = messages.Message{ID: messages.MessageID(100 + i), ChatID: 1, Text: fmt.Sprintf("message %d with enough text to wrap across terminal width", i), IsFromMe: i%2 == 0, SentAt: time.Now().Add(time.Duration(i) * time.Minute)}
	}
	store.histories[1] = messages.HistoryPage{Messages: longMessages}
	updated, cmd := model.refresh()
	model = executeCmd(t, updated.(Model), cmd)
	model.focus = FocusViewport
	model.viewport.SetYOffset(10)
	model.saveViewportOffset()

	for _, size := range []tea.WindowSizeMsg{{Width: 40, Height: 15}, {Width: 120, Height: 40}} {
		updated, _ = model.Update(size)
		model = updated.(Model)
	}
	if model.selectedID != 1 || model.threads[1].offset == 0 {
		t.Fatalf("resize lost state: selected=%d offset=%d", model.selectedID, model.threads[1].offset)
	}
}

func TestUppercaseHLJumpConversationByPage(t *testing.T) {
	model, _ := loadTestModel(t)
	model.focus = FocusViewport
	model.viewport.Height = 6
	model.viewport.SetContent(strings.Repeat("message\n", 40))
	model.viewport.SetYOffset(20)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	model = updated.(Model)
	if model.viewport.YOffset >= 20 {
		t.Fatalf("H offset = %d, want page jump upward", model.viewport.YOffset)
	}
	upOffset := model.viewport.YOffset

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	model = updated.(Model)
	if model.viewport.YOffset <= upOffset {
		t.Fatalf("L offset = %d, want page jump downward", model.viewport.YOffset)
	}
}

func TestRenderStaysInsideTerminal(t *testing.T) {
	model, store := loadTestModel(t)
	store.histories[1] = messages.HistoryPage{Messages: []messages.Message{
		{ID: 1, ChatID: 1, Sender: "发送者", Text: "Combining e\u0301, CJK 世界, emoji 🦊, and a verylongunbrokenurlhttps://example.test/abcdefghijklmnopqrstuvwxyz0123456789", SentAt: time.Now()},
	}}
	updated, cmd := model.refresh()
	model = executeCmd(t, updated.(Model), cmd)

	for _, size := range []tea.WindowSizeMsg{{Width: 40, Height: 15}, {Width: 80, Height: 24}, {Width: 120, Height: 40}} {
		updated, _ = model.Update(size)
		model = updated.(Model)
		lines := strings.Split(model.View(), "\n")
		if len(lines) != size.Height {
			t.Fatalf("%dx%d rendered %d lines", size.Width, size.Height, len(lines))
		}
		for i, line := range lines {
			if width := lipgloss.Width(line); width >= size.Width {
				t.Fatalf("%dx%d line %d width=%d, want <%d: %q", size.Width, size.Height, i, width, size.Width, line)
			}
		}
	}
}

func TestFatalDatabaseErrorSupportsRetry(t *testing.T) {
	model := New(func() (messages.Store, error) { return nil, fmt.Errorf("denied") })
	result, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = result.(Model)
	model = executeCmd(t, model, model.Init())
	if model.fatalErr == nil || !strings.Contains(model.View(), "Messages unavailable") {
		t.Fatalf("fatal error not rendered\n%s", model.View())
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil || updated.(Model).fatalErr != nil {
		t.Fatal("retry did not restart database open")
	}
}

func TestDraftsStayIsolatedByChatID(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft A")})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft B")})
	model = updated.(Model)

	if model.drafts[1].text != "draft A" || model.drafts[2].text != "draft B" {
		t.Fatalf("drafts = A:%q B:%q", model.drafts[1].text, model.drafts[2].text)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	if model.composer.Value() != "draft A" {
		t.Fatalf("restored composer = %q", model.composer.Value())
	}
}

func TestSendUsesCapturedExactChatAndSuppressesDuplicateEnter(t *testing.T) {
	model, _, messageSender := loadTestModelWithSender(t)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello 世界")})
	model = updated.(Model)
	updated, sendCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if sendCmd == nil || len(model.pending) != 1 {
		t.Fatal("send did not start")
	}
	updated, duplicateCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if duplicateCmd != nil || len(messageSender.targets) != 0 {
		t.Fatal("duplicate Enter started another command")
	}
	model = executeCmd(t, model, sendCmd)
	if len(messageSender.targets) != 1 || messageSender.targets[0].GUID != "chat-a" || messageSender.targets[0].ChatID != 1 {
		t.Fatalf("targets = %#v", messageSender.targets)
	}
	if messageSender.texts[0] != "hello 世界" {
		t.Fatalf("text = %q", messageSender.texts[0])
	}
}

func TestFailedSendPreservesDraftAndRefocusesComposer(t *testing.T) {
	model, _, messageSender := loadTestModelWithSender(t)
	messageSender.err = &msgsender.SendError{Kind: msgsender.ErrorPermission, Err: fmt.Errorf("denied")}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("keep me")})
	model = updated.(Model)
	updated, sendCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = executeCmd(t, updated.(Model), sendCmd)

	if model.drafts[1].text != "keep me" || model.composer.Value() != "keep me" || model.focus != FocusComposer {
		t.Fatalf("failed send state: draft=%q composer=%q focus=%v", model.drafts[1].text, model.composer.Value(), model.focus)
	}
	if !strings.Contains(model.View(), "Allow Aspen") {
		t.Fatalf("permission guidance missing\n%s", model.View())
	}
}

func TestLateSendSuccessCannotClearAnotherChatDraft(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("send A")})
	model = updated.(Model)
	updated, sendACmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft B")})
	model = updated.(Model)

	updated, _ = model.Update(sendACmd())
	model = updated.(Model)
	if model.selectedID != 2 || model.drafts[2].text != "draft B" || model.composer.Value() != "draft B" {
		t.Fatalf("B changed after A success: selected=%d draft=%q composer=%q", model.selectedID, model.drafts[2].text, model.composer.Value())
	}
	if model.drafts[1].text != "" {
		t.Fatalf("matching A draft was not cleared: %q", model.drafts[1].text)
	}
}

func TestLateSendSuccessRefreshesCapturedChat(t *testing.T) {
	model, store, _ := loadTestModelWithSender(t)
	model.pending[1] = pendingSend{generation: 1, draftGeneration: 1, text: "send A"}
	model.drafts[1] = &draftState{text: "send A", generation: 1}
	model.selectedID = 2
	model.chatList.SelectID(2)
	historyCalls := store.historyCalls

	updated, cmd := model.Update(SendFinishedMsg{ChatID: 1, Generation: 1, DraftGeneration: 1})
	model = executeCmd(t, updated.(Model), cmd)
	refreshedIDs := store.historyIDs[historyCalls:]
	foundCapturedChat := false
	for _, chatID := range refreshedIDs {
		foundCapturedChat = foundCapturedChat || chatID == 1
	}
	if !foundCapturedChat {
		t.Fatalf("late send history IDs = %v, want captured chat 1", refreshedIDs)
	}
	if !model.threads[1].loaded || model.threads[1].loading {
		t.Fatal("captured send chat did not refresh")
	}
	if model.selectedID != 2 {
		t.Fatalf("late send changed selection to %d", model.selectedID)
	}
}

func TestComposerOwnsQAndSupportsNewline(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = updated.(Model)
	if model.composer.Value() != "q" {
		t.Fatalf("q escaped composer: value=%q cmd=%v", model.composer.Value(), cmd)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(Model)
	if model.composer.Value() != "q\nx" {
		t.Fatalf("multiline composer = %q", model.composer.Value())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(Model)
	if model.composer.Value() != "q\nx\ny" {
		t.Fatalf("Alt+Enter composer = %q", model.composer.Value())
	}
}

func TestGroupComposerRemainsDisabled(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	group := testChats()[0]
	group.ID = 3
	group.GUID = "group-guid"
	group.IsGroup = true
	model.chats = append(model.chats, group)
	model.chatByID[group.ID] = group
	model.selectedID = group.ID
	model.chatList.SetChats(model.chats, group.ID)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	if cmd != nil || model.focus == FocusComposer || !strings.Contains(model.status, "Group sending") {
		t.Fatalf("group composer enabled: focus=%v status=%q", model.focus, model.status)
	}
}

func TestOlderHistoryPrependsWithoutJumpingVisibleAnchor(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	base := time.Date(2026, 8, 5, 12, 0, 0, 0, time.Local)
	current := make([]messages.Message, 20)
	for i := range current {
		current[i] = messages.Message{ID: messages.MessageID(100 + i), ChatID: 1, Sender: "+15550000001", Text: fmt.Sprintf("current %02d", i), SentAt: base.Add(time.Duration(i) * time.Minute)}
	}
	state := model.threads[1]
	state.messages = current
	state.older = &messages.HistoryCursor{Date: 1, MessageID: 100}
	state.loaded = true
	model.syncViewport(false)
	model.viewport.GotoTop()
	state.offset = 0
	model.focus = FocusViewport

	updated, olderCmd := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if olderCmd == nil || !model.threads[1].loadingOlder {
		t.Fatal("page-up at top did not start older history load")
	}
	updated, duplicateCmd := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if duplicateCmd != nil {
		t.Fatal("second older load started while one was pending")
	}
	model = updated.(Model)

	older := []messages.Message{
		{ID: 90, ChatID: 1, Sender: "+15550000001", Text: "older 00", SentAt: base.AddDate(0, 0, -1)},
		{ID: 91, ChatID: 1, Sender: "+15550000001", Text: "older 01", SentAt: base.AddDate(0, 0, -1).Add(time.Minute)},
		{ID: 100, ChatID: 1, Sender: "+15550000001", Text: "duplicate should be ignored", SentAt: base.AddDate(0, 0, -1).Add(2 * time.Minute)},
	}
	generation := model.threads[1].generation
	updated, _ = model.Update(HistoryLoadedMsg{
		ChatID: 1, Generation: generation, Prepend: true,
		Page: messages.HistoryPage{Messages: older}, LoadedAt: model.now(),
	})
	model = updated.(Model)
	state = model.threads[1]
	if len(state.messages) != 22 || state.loadingOlder || state.older != nil {
		t.Fatalf("prepended state: count=%d loading=%v older=%#v", len(state.messages), state.loadingOlder, state.older)
	}
	if state.offset <= 0 || !strings.Contains(model.viewport.View(), "current 00") {
		t.Fatalf("visible anchor jumped: offset=%d\n%s", state.offset, model.viewport.View())
	}
	for _, message := range state.messages {
		if message.Text == "duplicate should be ignored" {
			t.Fatal("duplicate page entry replaced current message")
		}
	}
}

func TestStaleOlderHistoryResultIsDiscarded(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	state := model.threads[1]
	before := len(state.messages)
	updated, _ := model.Update(HistoryLoadedMsg{
		ChatID: 1, Generation: state.generation - 1, Prepend: true,
		Page: messages.HistoryPage{Messages: []messages.Message{{ID: 99, Text: "stale"}}},
	})
	model = updated.(Model)
	if len(model.threads[1].messages) != before {
		t.Fatal("stale older page mutated history")
	}
}

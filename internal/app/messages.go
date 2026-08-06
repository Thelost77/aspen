package app

import (
	"context"
	"time"

	"github.com/Thelost77/aspen/internal/logger"
	"github.com/Thelost77/aspen/internal/messages"
	msgsender "github.com/Thelost77/aspen/internal/sender"
	tea "github.com/charmbracelet/bubbletea"
)

type StoreOpener func() (messages.Store, error)

type StoreOpenedMsg struct {
	Generation uint64
	Store      messages.Store
	Err        error
}

type ChatsLoadedMsg struct {
	Generation uint64
	Chats      []messages.Chat
	LoadedAt   time.Time
	Err        error
}

type HistoryLoadedMsg struct {
	ChatID     messages.ChatID
	Generation uint64
	Page       messages.HistoryPage
	Prepend    bool
	LoadedAt   time.Time
	Err        error
}

type SendFinishedMsg struct {
	ChatID          messages.ChatID
	Generation      uint64
	DraftGeneration uint64
	Err             error
}

func openStoreCmd(opener StoreOpener, generation uint64) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		store, err := opener()
		logger.Debug("Messages store open completed", "duration_ms", time.Since(started).Milliseconds(), "failed", err != nil)
		return StoreOpenedMsg{Generation: generation, Store: store, Err: err}
	}
}

func loadChatsCmd(store messages.Store, generation uint64) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		chats, err := store.Conversations(ctx, 100)
		logger.Debug("conversation load completed", "duration_ms", time.Since(started).Milliseconds(), "count", len(chats), "failed", err != nil)
		return ChatsLoadedMsg{Generation: generation, Chats: chats, LoadedAt: time.Now(), Err: err}
	}
}

func loadHistoryCmd(store messages.Store, chatID messages.ChatID, generation uint64, cursor *messages.HistoryCursor, prepend bool) tea.Cmd {
	cursor = cloneCursor(cursor)
	return func() tea.Msg {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		page, err := store.History(ctx, chatID, 100, cursor)
		logger.Debug("history load completed", "duration_ms", time.Since(started).Milliseconds(), "count", len(page.Messages), "older", prepend, "failed", err != nil)
		return HistoryLoadedMsg{ChatID: chatID, Generation: generation, Page: page, Prepend: prepend, LoadedAt: time.Now(), Err: err}
	}
}

func sendMessageCmd(messageSender msgsender.Sender, target msgsender.SendTarget, text string, generation, draftGeneration uint64) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		err := messageSender.Send(ctx, target, text)
		logger.Info("send command completed", "duration_ms", time.Since(started).Milliseconds(), "accepted", err == nil)
		return SendFinishedMsg{
			ChatID:          target.ChatID,
			Generation:      generation,
			DraftGeneration: draftGeneration,
			Err:             err,
		}
	}
}

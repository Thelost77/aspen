package messages

import (
	"context"
	"time"
)

type ChatID int64
type MessageID int64

type Participant struct {
	Handle string
	Name   string
}

type Chat struct {
	ID              ChatID
	GUID            string
	Identifier      string
	DisplayName     string
	Service         string
	Participants    []Participant
	LastMessageAt   time.Time
	LastMessageText string
	UnreadCount     int
	IsGroup         bool
}

func (c Chat) Title() string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	if len(c.Participants) == 1 && c.Participants[0].Name != "" {
		return c.Participants[0].Name
	}
	if c.Identifier != "" {
		return c.Identifier
	}
	if len(c.Participants) > 0 {
		return c.Participants[0].Handle
	}
	return "Unknown conversation"
}

type Message struct {
	ID          MessageID
	GUID        string
	ChatID      ChatID
	Sender      string
	SenderName  string
	Text        string
	SentAt      time.Time
	IsFromMe    bool
	IsRead      bool
	Service     string
	Attachments []Attachment
	IsSystem    bool
}

type Attachment struct {
	ID       int64
	Name     string
	Path     string
	MIMEType string
	UTI      string
	Size     int64
	IsImage  bool
}

type HistoryCursor struct {
	Date      int64
	MessageID MessageID
}

type HistoryPage struct {
	Messages []Message
	Older    *HistoryCursor
}

type Store interface {
	Conversations(context.Context, int) ([]Chat, error)
	History(context.Context, ChatID, int, *HistoryCursor) (HistoryPage, error)
	Close() error
}

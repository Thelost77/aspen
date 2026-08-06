package app

import (
	"strings"
	"time"

	"github.com/Thelost77/aspen/internal/logger"
	"github.com/Thelost77/aspen/internal/messages"
	tea "github.com/charmbracelet/bubbletea"
)

type NameResolver interface {
	Resolve(string) string
}

type ResolverLoader func() (NameResolver, error)

type ContactsLoadedMsg struct {
	Generation uint64
	Resolver   NameResolver
	Err        error
}

func loadContactsCmd(loader ResolverLoader, generation uint64) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		resolver, err := loader()
		logger.Debug("contact load completed", "duration_ms", time.Since(started).Milliseconds(), "partial_failure", err != nil)
		return ContactsLoadedMsg{Generation: generation, Resolver: resolver, Err: err}
	}
}

func (m *Model) SetResolverLoader(loader ResolverLoader) {
	m.resolverLoader = loader
}

func (m Model) enrichChats(chats []messages.Chat) []messages.Chat {
	enriched := make([]messages.Chat, len(chats))
	for i, chat := range chats {
		enriched[i] = chat
		enriched[i].Participants = append([]messages.Participant(nil), chat.Participants...)
		for p := range enriched[i].Participants {
			handle := enriched[i].Participants[p].Handle
			enriched[i].Participants[p].Name = m.resolveName(handle)
		}
		if enriched[i].DisplayName != "" {
			continue
		}
		if !enriched[i].IsGroup {
			name := m.resolveName(enriched[i].Identifier)
			if name != "" && name != enriched[i].Identifier {
				enriched[i].DisplayName = name
			} else if len(enriched[i].Participants) == 1 && enriched[i].Participants[0].Name != enriched[i].Participants[0].Handle {
				enriched[i].DisplayName = enriched[i].Participants[0].Name
			}
			continue
		}
		var names []string
		for _, participant := range enriched[i].Participants {
			if participant.Name != "" && participant.Name != participant.Handle {
				names = append(names, participant.Name)
			} else if participant.Handle != "" {
				names = append(names, participant.Handle)
			}
		}
		if len(names) > 0 {
			enriched[i].DisplayName = strings.Join(names, ", ")
		}
	}
	return enriched
}

func (m Model) enrichMessages(input []messages.Message) []messages.Message {
	enriched := append([]messages.Message(nil), input...)
	for i := range enriched {
		if !enriched[i].IsFromMe && enriched[i].Sender != "" {
			enriched[i].SenderName = m.resolveName(enriched[i].Sender)
		}
		enriched[i].Attachments = append([]messages.Attachment(nil), enriched[i].Attachments...)
	}
	return enriched
}

func (m Model) resolveName(handle string) string {
	if m.resolver == nil || handle == "" {
		return handle
	}
	return m.resolver.Resolve(handle)
}

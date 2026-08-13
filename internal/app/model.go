package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/messages"
	msgsender "github.com/Thelost77/aspen/internal/sender"
	"github.com/Thelost77/aspen/internal/ui"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	minimumSidebarWidth        = 24
	preferredSidebarWidth      = 30
	minimumConversationWidth   = 48
	defaultMessagePollInterval = 2 * time.Second
)

type Focus int

const (
	FocusList Focus = iota
	FocusViewport
	FocusComposer
)

type Layout int

const (
	LayoutNarrow Layout = iota
	LayoutSplit
)

type NarrowPane int

const (
	NarrowList NarrowPane = iota
	NarrowConversation
)

type threadState struct {
	messages     []messages.Message
	placements   []inlinePlacement
	imageRefs    []inlineImageRef
	older        *messages.HistoryCursor
	generation   uint64
	loading      bool
	loadingOlder bool
	loadingAll   bool
	loaded       bool
	err          error
	offset       int
}

type draftState struct {
	text       string
	generation uint64
}

type pendingSend struct {
	generation      uint64
	draftGeneration uint64
	text            string
}

type Model struct {
	opener         StoreOpener
	store          messages.Store
	messageSender  msgsender.Sender
	resolverLoader ResolverLoader
	resolver       NameResolver

	styles   ui.Styles
	help     ui.HelpOverlay
	chatList chatList
	viewport viewport.Model
	composer textarea.Model
	search   conversationSearchState

	chats                       []messages.Chat
	chatByID                    map[messages.ChatID]messages.Chat
	selectedID                  messages.ChatID
	threads                     map[messages.ChatID]*threadState
	drafts                      map[messages.ChatID]*draftState
	pending                     map[messages.ChatID]pendingSend
	inlineImages                map[inlineImageKey]*inlineImageState
	inlineImageResidents        map[inlineImageKey]bool
	inlineImageITermPositions   map[inlineImageKey]inlineImagePosition
	inlineImageLoader           *inlineImageLoader
	inlineImageOutput           inlineimage.TerminalOutput
	imageProtocol               inlineimage.Protocol
	inlineImageBytes            int
	inlineImageCacheBudget      int
	inlineImageGeneration       uint64
	inlineImageLayoutGeneration uint64
	inlineImageFrameGeneration  uint64
	inlineImageAccess           uint64
	sendGeneration              uint64

	focus                    Focus
	layout                   Layout
	narrowPane               NarrowPane
	width                    int
	height                   int
	bodyWidth                int
	bodyHeight               int
	paneContentHeight        int
	sidebarWidth             int
	sidebarContentWidth      int
	conversationWidth        int
	conversationContentWidth int

	openGeneration    uint64
	chatGeneration    uint64
	contactGeneration uint64
	loadingChats      bool
	fatalErr          error
	bannerErr         error
	status            string
	lastRefreshed     time.Time
	now               func() time.Time

	messagePollInterval  time.Duration
	messagePollLoading   bool
	messageChangeVersion int64
	pendingLatestMerges  map[messages.ChatID]bool
}

func New(opener StoreOpener, senders ...msgsender.Sender) Model {
	return newModel(opener, nil, firstSender(senders))
}

func NewWithStore(store messages.Store, senders ...msgsender.Sender) Model {
	return newModel(nil, store, firstSender(senders))
}

func firstSender(senders []msgsender.Sender) msgsender.Sender {
	if len(senders) == 0 {
		return nil
	}
	return senders[0]
}

func newModel(opener StoreOpener, store messages.Store, messageSender msgsender.Sender) Model {
	styles := ui.DefaultStyles()
	vp := viewport.New(1, 1)
	composer := newComposer(styles)
	vp.MouseWheelEnabled = false
	model := Model{
		opener:                    opener,
		store:                     store,
		messageSender:             messageSender,
		styles:                    styles,
		help:                      ui.NewHelpOverlay(styles),
		chatList:                  newChatList(styles),
		viewport:                  vp,
		composer:                  composer,
		search:                    newConversationSearch(styles),
		chatByID:                  make(map[messages.ChatID]messages.Chat),
		threads:                   make(map[messages.ChatID]*threadState),
		drafts:                    make(map[messages.ChatID]*draftState),
		pending:                   make(map[messages.ChatID]pendingSend),
		inlineImages:              make(map[inlineImageKey]*inlineImageState),
		inlineImageResidents:      make(map[inlineImageKey]bool),
		inlineImageITermPositions: make(map[inlineImageKey]inlineImagePosition),
		inlineImageLoader:         newInlineImageLoader(),
		inlineImageCacheBudget:    defaultInlineImageCacheBudget,
		imageProtocol:             inlineimage.Detect(os.Getenv),
		focus:                     FocusList,
		narrowPane:                NarrowList,
		openGeneration:            1,
		chatGeneration:            1,
		contactGeneration:         1,
		loadingChats:              true,
		status:                    "Loading conversations…",
		now:                       time.Now,
		messagePollInterval:       defaultMessagePollInterval,
		pendingLatestMerges:       make(map[messages.ChatID]bool),
	}
	return model
}

func (m *Model) SetInlineImageProtocol(protocol inlineimage.Protocol) {
	m.imageProtocol = protocol
}

func (m Model) Init() tea.Cmd {
	var commands []tea.Cmd
	if m.store != nil {
		commands = append(commands, loadChangeVersionCmd(m.store, true))
	} else if m.opener != nil {
		commands = append(commands, openStoreCmd(m.opener, m.openGeneration))
	} else {
		commands = append(commands, func() tea.Msg {
			return StoreOpenedMsg{Generation: m.openGeneration, Err: fmt.Errorf("messages store is not configured")}
		})
	}
	if m.resolverLoader != nil {
		commands = append(commands, loadContactsCmd(m.resolverLoader, m.contactGeneration))
	}
	return tea.Batch(commands...)
}

func (m Model) Close() error {
	cleanupErr := m.closeInlineImages()
	if m.store == nil {
		return cleanupErr
	}
	return errors.Join(cleanupErr, m.store.Close())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyRunes && keyMsg.Alt {
		if len(keyMsg.Runes) == 1 && keyMsg.Runes[0] >= 0x80 {
			keyMsg.Alt = false
			msg = keyMsg
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		oldColumns, oldRows := m.inlineImageDimensions()
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetSize(msg.Width, msg.Height)
		m.setSizes()
		newColumns, newRows := m.inlineImageDimensions()
		if oldColumns != newColumns || oldRows != newRows {
			m.invalidateInlineImageLayout()
		} else {
			m.resetInlineImageResidency()
		}
		m.syncViewport(false)
		return m, m.refreshInlineImages()

	case tea.ResumeMsg:
		m.resetInlineImageResidency()
		return m, m.refreshInlineImages()

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case StoreOpenedMsg:
		if msg.Generation != m.openGeneration {
			if msg.Store != nil {
				_ = msg.Store.Close()
			}
			return m, nil
		}
		if msg.Err != nil {
			m.loadingChats = false
			m.fatalErr = msg.Err
			m.status = "Database unavailable"
			return m, nil
		}
		m.store = msg.Store
		m.fatalErr = nil
		m.loadingChats = true
		m.status = "Loading conversations…"
		m.chatGeneration++
		m.messagePollLoading = true
		return m, loadChangeVersionCmd(m.store, true)

	case messagePollTickMsg:
		if m.store == nil || m.messagePollLoading || m.loadingChats {
			return m, scheduleMessagePoll(m.messagePollInterval)
		}
		m.messagePollLoading = true
		return m, tea.Batch(
			loadChangeVersionCmd(m.store, false),
			m.startPendingLatestMerge(m.selectedID),
		)

	case changeVersionLoadedMsg:
		if !msg.Initial && m.loadingChats {
			m.messagePollLoading = false
			return m, scheduleMessagePoll(m.messagePollInterval)
		}
		m.messagePollLoading = false
		if msg.Initial {
			if msg.Err == nil {
				m.messageChangeVersion = msg.Version
			}
			return m, tea.Batch(
				loadChatsCmd(m.store, m.chatGeneration),
				scheduleMessagePoll(m.messagePollInterval),
			)
		}
		nextPoll := scheduleMessagePoll(m.messagePollInterval)
		if msg.Err != nil || msg.Version == m.messageChangeVersion {
			return m, nextPoll
		}
		if msg.Version < m.messageChangeVersion {
			m.messageChangeVersion = msg.Version
			return m, m.refreshChat(m.selectedID, nextPoll)
		}
		cmds := []tea.Cmd{nextPoll}
		m.chatGeneration++
		m.loadingChats = true
		m.status = "Refreshing…"
		cmds = append(cmds, loadChatsCmd(m.store, m.chatGeneration))
		if m.selectedID != 0 {
			cmds = append(cmds, m.startLatestHistoryMerge(m.selectedID))
		}
		return m, tea.Batch(cmds...)

	case ChatsLoadedMsg:
		if msg.Generation != m.chatGeneration {
			return m, nil
		}
		m.loadingChats = false
		if msg.Err != nil {
			if len(m.chats) == 0 {
				m.fatalErr = msg.Err
				m.status = "Database unavailable"
			} else {
				m.bannerErr = msg.Err
				m.status = "Refresh failed"
			}
			return m, nil
		}
		if msg.ChangeVersion > 0 {
			m.messageChangeVersion = msg.ChangeVersion
		}
		m.fatalErr = nil
		m.bannerErr = nil
		m.status = "Refreshed"
		m.lastRefreshed = msg.LoadedAt
		m.chats = m.enrichChats(msg.Chats)
		m.chatByID = make(map[messages.ChatID]messages.Chat, len(m.chats))
		for _, chat := range m.chats {
			m.chatByID[chat.ID] = chat
		}

		selected := m.selectedID
		if _, ok := m.chatByID[selected]; !ok {
			selected = 0
		}
		if selected == 0 && len(m.chats) > 0 {
			selected = m.chats[0].ID
		}
		m.selectedID = selected
		listCmd := m.chatList.SetChats(m.chats, selected)
		if selected == 0 {
			m.composer.Reset()
			m.viewport.SetContent("")
			return m, listCmd
		}
		m.loadSelectedDraft()
		if m.focus != FocusComposer {
			m.composer.Blur()
		}
		if state := m.threads[selected]; state == nil || !state.loaded && !state.loading {
			historyCmd := m.startHistoryLoad(selected)
			return m, tea.Batch(listCmd, historyCmd)
		}
		m.syncViewport(false)
		return m, tea.Batch(listCmd, m.refreshInlineImages())

	case HistoryLoadedMsg:
		state := m.threads[msg.ChatID]
		if state == nil || msg.Generation != state.generation {
			return m, nil
		}
		if msg.Prepend {
			state.loadingOlder = false
			if msg.Err != nil {
				state.loadingAll = false
				m.bannerErr = msg.Err
				m.status = "Older message load failed"
				return m, m.startPendingLatestMerge(msg.ChatID)
			}
			oldOffset := state.offset
			oldLineCount := 0
			if msg.ChatID == m.selectedID {
				oldLineCount = renderedLineCount(m.renderThread(msg.ChatID, state.messages).content)
			}
			state.messages = prependUniqueMessages(m.enrichMessages(msg.Page.Messages), state.messages)
			state.older = cloneCursor(msg.Page.Older)
			state.loaded = true
			state.err = nil
			m.bannerErr = nil
			m.refreshConversationSearch(msg.ChatID)
			var nextPageCmd tea.Cmd
			switch {
			case state.loadingAll && state.older != nil:
				nextPageCmd = m.startOlderHistoryLoadFor(msg.ChatID)
			case state.loadingAll:
				state.loadingAll = false
				m.status = fmt.Sprintf("Full conversation loaded · %d messages", len(state.messages))
			default:
				m.status = "Loaded older messages"
			}
			var imageCmd tea.Cmd
			if msg.ChatID == m.selectedID {
				m.syncViewport(false)
				newLineCount := renderedLineCount(m.renderThread(msg.ChatID, state.messages).content)
				m.viewport.SetYOffset(oldOffset + max(0, newLineCount-oldLineCount))
				state.offset = m.viewport.YOffset
				imageCmd = m.refreshInlineImages()
			}
			return m, tea.Batch(imageCmd, nextPageCmd, m.startPendingLatestMerge(msg.ChatID))
		}

		state.loading = false
		state.loadingOlder = false
		if msg.Err != nil {
			state.err = msg.Err
			m.bannerErr = msg.Err
			m.status = "Message refresh failed"
			if msg.ChatID == m.selectedID {
				m.syncViewport(false)
			}
			if msg.MergeLatest {
				m.pendingLatestMerges[msg.ChatID] = true
				return m, nil
			}
			return m, m.startPendingLatestMerge(msg.ChatID)
		}
		wasLoaded := state.loaded
		wasAtBottom := msg.ChatID == m.selectedID && m.viewport.AtBottom()
		oldOffset := state.offset
		if msg.MergeLatest {
			state.messages = mergeLatestMessages(state.messages, m.enrichMessages(msg.Page.Messages))
		} else {
			state.messages = m.enrichMessages(msg.Page.Messages)
			state.older = cloneCursor(msg.Page.Older)
		}
		state.loaded = true
		state.err = nil
		m.bannerErr = nil
		m.status = "Refreshed"
		m.lastRefreshed = msg.LoadedAt
		m.refreshConversationSearch(msg.ChatID)
		m.pruneInlineImages()
		var imageCmd tea.Cmd
		if msg.ChatID == m.selectedID {
			m.syncViewport(false)
			switch {
			case !wasLoaded || wasAtBottom:
				m.viewport.GotoBottom()
			default:
				m.viewport.SetYOffset(oldOffset)
			}
			state.offset = m.viewport.YOffset
			imageCmd = m.refreshInlineImages()
		}
		return m, tea.Batch(imageCmd, m.startPendingLatestMerge(msg.ChatID))

	case ContactsLoadedMsg:
		if msg.Generation != m.contactGeneration {
			return m, nil
		}
		if msg.Resolver == nil {
			return m, nil
		}
		m.resolver = msg.Resolver
		m.chats = m.enrichChats(m.chats)
		m.chatByID = make(map[messages.ChatID]messages.Chat, len(m.chats))
		for _, chat := range m.chats {
			m.chatByID[chat.ID] = chat
		}
		for _, state := range m.threads {
			state.messages = m.enrichMessages(state.messages)
		}
		listCmd := m.chatList.SetChats(m.chats, m.selectedID)
		m.syncViewport(false)
		return m, tea.Batch(listCmd, m.refreshInlineImages())

	case SendFinishedMsg:
		return m.handleSendFinished(msg)

	case inlineImageLoadedMsg:
		if !m.applyInlineImageLoaded(msg) {
			return m, nil
		}
		if msg.key.chatID == m.selectedID {
			wasAtBottom := m.viewport.AtBottom()
			offset := m.viewport.YOffset
			m.syncViewport(false)
			if wasAtBottom {
				m.viewport.GotoBottom()
			} else {
				m.viewport.SetYOffset(offset)
			}
			state := m.threads[m.selectedID]
			if state != nil {
				state.offset = m.viewport.YOffset
			}
		}
		return m, m.refreshInlineImages()

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.fatalErr != nil {
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "r":
				return m.retryOpen()
			}
			return m, nil
		}
		if m.focus == FocusComposer {
			return m.handleComposerKey(msg)
		}
		if m.search.editing {
			return m.handleConversationSearchKey(msg)
		}
		if m.chatList.IsFiltering() {
			before := m.selectedID
			cmd := m.updateChatList(msg)
			return m.afterListUpdate(before, cmd)
		}
		if msg.String() == "?" {
			m.help.Toggle()
			return m, m.refreshInlineImages()
		}
		if m.help.Visible() {
			if msg.String() == "esc" {
				m.help.Hide()
				return m, m.refreshInlineImages()
			}
			return m, nil
		}
		if m.bannerErr != nil && msg.String() == "esc" {
			m.bannerErr = nil
			return m, nil
		}
		if msg.String() == "q" {
			return m, tea.Quit
		}
		if m.focus == FocusViewport && msg.String() == "/" {
			return m.beginConversationSearch()
		}
		if m.focus == FocusViewport && m.conversationFilterActive(m.selectedID) && msg.String() == "esc" {
			m.clearConversationSearch(true)
			return m, m.refreshInlineImages()
		}
		if msg.String() == "i" {
			return m, m.focusComposer()
		}
		if msg.String() == "r" {
			return m.refresh()
		}
		if msg.String() == "R" {
			return m, m.retryInlineImages()
		}
		if msg.String() == "tab" {
			return m, m.toggleFocus()
		}
		return m.handleNavigationKey(msg)
	}

	if m.focus == FocusList || m.chatList.IsFiltering() {
		before := m.selectedID
		cmd := m.updateChatList(msg)
		return m.afterListUpdate(before, cmd)
	}
	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.fatalErr != nil || m.help.Visible() || m.search.editing || msg.Y < 0 || msg.Y >= m.bodyHeight {
		return m, nil
	}

	listClicked := m.layout == LayoutNarrow && m.narrowPane == NarrowList
	if m.layout == LayoutSplit {
		listClicked = msg.X > 0 && msg.X < m.sidebarWidth-1
	}
	if listClicked {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			before := m.selectedID
			m.chatList.ScrollUp(3)
			return m.afterListUpdate(before, nil)
		case tea.MouseButtonWheelDown:
			before := m.selectedID
			m.chatList.ScrollDown(3)
			return m.afterListUpdate(before, nil)
		case tea.MouseButtonLeft:
			if msg.Action != tea.MouseActionPress {
				return m, nil
			}
			chat, ok := m.chatList.SelectItemRow(msg.Y - 2)
			if !ok {
				return m, nil
			}
			cmd := m.selectChat(chat.ID)
			m.focus = FocusViewport
			m.composer.Blur()
			if m.layout == LayoutNarrow {
				m.narrowPane = NarrowConversation
				m.setSizes()
				cmd = tea.Batch(cmd, m.refreshInlineImages())
			}
			return m, cmd
		}
	}

	conversationClicked := m.layout == LayoutNarrow && m.narrowPane == NarrowConversation
	if m.layout == LayoutSplit {
		conversationClicked = msg.X > m.sidebarWidth+1 && msg.X < m.bodyWidth-1
	}
	if !conversationClicked {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.viewport.ScrollUp(3)
		m.saveViewportOffset()
		return m, m.refreshInlineImages()
	case tea.MouseButtonWheelDown:
		m.viewport.ScrollDown(3)
		m.saveViewportOffset()
		return m, m.refreshInlineImages()
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		composerStart := 3 + m.viewport.Height
		if msg.Y > composerStart {
			return m, m.focusComposer()
		}
		m.saveSelectedDraft()
		m.composer.Blur()
		m.focus = FocusViewport
	}
	return m, nil
}

func (m Model) retryOpen() (tea.Model, tea.Cmd) {
	if m.opener == nil {
		return m, nil
	}
	m.openGeneration++
	m.loadingChats = true
	m.fatalErr = nil
	m.status = "Retrying database…"
	return m, openStoreCmd(m.opener, m.openGeneration)
}

func (m Model) refresh() (tea.Model, tea.Cmd) {
	if m.store == nil {
		return m.retryOpen()
	}
	return m, m.refreshChat(m.selectedID)
}

func (m *Model) refreshChat(chatID messages.ChatID, commands ...tea.Cmd) tea.Cmd {
	m.chatGeneration++
	m.loadingChats = true
	m.status = "Refreshing…"
	commands = append(commands, loadChatsCmd(m.store, m.chatGeneration))
	if chatID != 0 {
		commands = append(commands, m.startHistoryLoad(chatID))
	}
	return tea.Batch(commands...)
}

func (m *Model) startHistoryLoad(chatID messages.ChatID) tea.Cmd {
	delete(m.pendingLatestMerges, chatID)
	m.cancelInlineImageLoads(chatID)
	state := m.threads[chatID]
	if state == nil {
		state = &threadState{}
		m.threads[chatID] = state
	}
	state.generation++
	state.loading = true
	state.loadingOlder = false
	state.loadingAll = false
	state.err = nil
	if chatID == m.selectedID {
		m.syncViewport(false)
	}
	return loadHistoryCmd(m.store, chatID, state.generation, nil, false, false)
}

func (m *Model) startLatestHistoryMerge(chatID messages.ChatID) tea.Cmd {
	state := m.threads[chatID]
	if m.store == nil || state == nil {
		return nil
	}
	if !state.loaded {
		if state.loading {
			m.pendingLatestMerges[chatID] = true
		}
		return nil
	}
	if state.loading || state.loadingOlder {
		m.pendingLatestMerges[chatID] = true
		return nil
	}
	state.generation++
	state.loading = true
	state.err = nil
	return loadHistoryCmd(m.store, chatID, state.generation, nil, false, true)
}

func (m *Model) startPendingLatestMerge(chatID messages.ChatID) tea.Cmd {
	if !m.pendingLatestMerges[chatID] {
		return nil
	}
	state := m.threads[chatID]
	if state == nil || state.loading || state.loadingOlder {
		return nil
	}
	delete(m.pendingLatestMerges, chatID)
	return m.startLatestHistoryMerge(chatID)
}

func (m *Model) startOlderHistoryLoad() tea.Cmd {
	return m.startOlderHistoryLoadFor(m.selectedID)
}

func (m *Model) startOlderHistoryLoadFor(chatID messages.ChatID) tea.Cmd {
	state := m.threads[chatID]
	if m.store == nil || state == nil || !state.loaded || state.older == nil || state.loading || state.loadingOlder {
		return nil
	}
	state.loadingOlder = true
	if state.loadingAll {
		m.status = fmt.Sprintf("Loading full conversation… %d messages", len(state.messages))
	} else {
		m.status = "Loading older messages…"
	}
	return loadHistoryCmd(m.store, chatID, state.generation, state.older, true, false)
}

func (m *Model) toggleAllHistory() tea.Cmd {
	state := m.threads[m.selectedID]
	if state == nil || !state.loaded {
		return nil
	}
	if state.loadingAll {
		state.loadingAll = false
		m.status = "Stopping full conversation load after current page…"
		return nil
	}
	if state.older == nil {
		m.status = "Full conversation already loaded"
		return nil
	}
	state.loadingAll = true
	return m.startOlderHistoryLoadFor(m.selectedID)
}

func (m Model) handleNavigationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.layout == LayoutNarrow && m.narrowPane == NarrowConversation {
		if msg.String() == "esc" || msg.String() == "left" {
			m.saveViewportOffset()
			m.narrowPane = NarrowList
			m.focus = FocusList
			m.setSizes()
			return m, m.refreshInlineImages()
		}
	}

	if m.focus == FocusList {
		switch msg.String() {
		case "enter", "right":
			if chat, ok := m.chatList.Selected(); ok {
				cmd := m.selectChat(chat.ID)
				m.focus = FocusViewport
				if m.layout == LayoutNarrow {
					m.narrowPane = NarrowConversation
					m.setSizes()
					cmd = tea.Batch(cmd, m.refreshInlineImages())
				}
				return m, cmd
			}
		case "g", "home":
			before := m.selectedID
			m.chatList.GoToStart()
			return m.afterListUpdate(before, nil)
		case "G", "end":
			before := m.selectedID
			m.chatList.GoToEnd()
			return m.afterListUpdate(before, nil)
		case "H", "pgup":
			before := m.selectedID
			m.chatList.PageUp()
			return m.afterListUpdate(before, nil)
		case "L", "pgdown":
			before := m.selectedID
			m.chatList.PageDown()
			return m.afterListUpdate(before, nil)
		}
		before := m.selectedID
		cmd := m.updateChatList(msg)
		return m.afterListUpdate(before, cmd)
	}

	loadOlder := false
	switch msg.String() {
	case "A":
		return m, m.toggleAllHistory()
	case "esc", "left":
		m.focus = FocusList
		return m, nil
	case "g", "home":
		m.viewport.GotoTop()
		loadOlder = true
	case "G", "end":
		m.viewport.GotoBottom()
	case "j", "down":
		m.viewport.ScrollDown(1)
	case "k", "up":
		m.viewport.ScrollUp(1)
		loadOlder = true
	case "H", "pgup":
		m.viewport.HalfPageUp()
		loadOlder = true
	case "L", "pgdown":
		m.viewport.HalfPageDown()
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.saveViewportOffset()
		return m, tea.Batch(cmd, m.refreshInlineImages())
	}
	m.saveViewportOffset()
	imageCmd := m.refreshInlineImages()
	if loadOlder && m.viewport.AtTop() {
		return m, tea.Batch(imageCmd, m.startOlderHistoryLoad())
	}
	return m, imageCmd
}

func (m *Model) toggleFocus() tea.Cmd {
	if m.layout == LayoutNarrow {
		switch {
		case m.narrowPane == NarrowList && m.selectedID != 0:
			m.narrowPane = NarrowConversation
			m.focus = FocusViewport
		case m.focus == FocusViewport:
			cmd := m.focusComposer()
			m.setSizes()
			return tea.Batch(cmd, m.refreshInlineImages())
		default:
			m.composer.Blur()
			m.focus = FocusViewport
		}
		m.setSizes()
		return m.refreshInlineImages()
	}
	switch m.focus {
	case FocusList:
		m.focus = FocusViewport
	case FocusViewport:
		return m.focusComposer()
	default:
		m.saveSelectedDraft()
		m.composer.Blur()
		m.focus = FocusList
	}
	return nil
}

func (m *Model) updateChatList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.chatList.model, cmd = m.chatList.model.Update(msg)
	return cmd
}

func (m Model) afterListUpdate(previous messages.ChatID, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	chat, ok := m.chatList.Selected()
	if !ok {
		return m, cmd
	}
	if chat.ID != previous {
		loadCmd := m.selectChat(chat.ID)
		return m, tea.Batch(cmd, loadCmd)
	}
	return m, cmd
}

func (m *Model) selectChat(chatID messages.ChatID) tea.Cmd {
	if chatID == 0 {
		return nil
	}
	if chatID == m.selectedID {
		m.chatList.SelectID(chatID)
		if state := m.threads[chatID]; state != nil && (state.loaded || state.loading) {
			m.syncViewport(false)
			return m.refreshInlineImages()
		}
		return m.startHistoryLoad(chatID)
	}
	m.saveViewportOffset()
	m.saveSelectedDraft()
	if state := m.threads[m.selectedID]; state != nil {
		state.loadingAll = false
	}
	m.clearConversationSearch(false)
	m.selectedID = chatID
	m.chatList.SelectID(chatID)
	m.loadSelectedDraft()
	m.composer.Blur()
	m.syncViewport(true)
	historyCmd := m.startHistoryLoad(chatID)
	return tea.Batch(historyCmd, m.refreshInlineImages())
}

func (m *Model) saveViewportOffset() {
	if state := m.threads[m.selectedID]; state != nil {
		state.offset = m.viewport.YOffset
	}
}

func (m *Model) syncViewport(switched bool) {
	state := m.threads[m.selectedID]
	if state == nil || !state.loaded {
		if state != nil {
			state.placements = nil
			state.imageRefs = nil
		}
		m.viewport.SetContent("")
		m.viewport.GotoTop()
		return
	}
	rendered := m.renderThread(m.selectedID, state.messages)
	state.placements = rendered.placements
	state.imageRefs = rendered.imageRefs
	m.viewport.SetContent(rendered.content)
	if switched {
		m.viewport.SetYOffset(state.offset)
	}
}

func (m Model) renderThread(chatID messages.ChatID, threadMessages []messages.Message) renderedConversation {
	width := max(1, m.conversationContentWidth)
	filtered := m.filteredConversationMessages(chatID, threadMessages)
	return renderMessagesInline(filtered, m.chatByID[chatID], width, m.styles, m.inlineImageFor)
}

func (m *Model) setSizes() {
	m.bodyWidth = ui.UsableWidth(m.width)
	m.bodyHeight = max(1, m.height-1)
	m.paneContentHeight = max(1, m.bodyHeight-2)
	threshold := minimumSidebarWidth + 1 + minimumConversationWidth
	if m.bodyWidth >= threshold {
		m.layout = LayoutSplit
		m.sidebarWidth = min(preferredSidebarWidth, max(minimumSidebarWidth, m.bodyWidth/3))
		m.conversationWidth = max(1, m.bodyWidth-m.sidebarWidth-1)
	} else {
		m.layout = LayoutNarrow
		m.sidebarWidth = m.bodyWidth
		m.conversationWidth = m.bodyWidth
	}
	m.sidebarContentWidth = max(1, m.sidebarWidth-2)
	m.conversationContentWidth = max(1, m.conversationWidth-2)
	m.chatList.SetSize(m.sidebarContentWidth, m.paneContentHeight)

	conversationHeaderHeight := 2
	m.composer.SetWidth(m.conversationContentWidth)
	m.search.input.Width = max(1, m.conversationContentWidth-len(m.search.input.Prompt))
	composerHeight := min(5, max(2, m.composer.LineCount()))
	m.composer.SetHeight(composerHeight)
	composerAreaHeight := composerHeight + 1
	m.viewport.Width = m.conversationContentWidth
	m.viewport.Height = max(1, m.paneContentHeight-conversationHeaderHeight-composerAreaHeight)
}

func cloneCursor(cursor *messages.HistoryCursor) *messages.HistoryCursor {
	if cursor == nil {
		return nil
	}
	copy := *cursor
	return &copy
}

func mergeLatestMessages(current, latest []messages.Message) []messages.Message {
	latestByID := make(map[messages.MessageID]messages.Message, len(latest))
	for _, message := range latest {
		latestByID[message.ID] = message
	}
	merged := make([]messages.Message, 0, len(current)+len(latest))
	for _, message := range current {
		if replacement, ok := latestByID[message.ID]; ok {
			merged = append(merged, replacement)
			delete(latestByID, message.ID)
		} else {
			merged = append(merged, message)
		}
	}
	for _, message := range latest {
		if _, ok := latestByID[message.ID]; !ok {
			continue
		}
		merged = append(merged, message)
		delete(latestByID, message.ID)
	}
	return merged
}

func prependUniqueMessages(older, current []messages.Message) []messages.Message {
	currentIDs := make(map[messages.MessageID]bool, len(current))
	for _, message := range current {
		currentIDs[message.ID] = true
	}
	seenOlder := make(map[messages.MessageID]bool, len(older))
	combined := make([]messages.Message, 0, len(older)+len(current))
	for _, message := range older {
		if currentIDs[message.ID] || seenOlder[message.ID] {
			continue
		}
		seenOlder[message.ID] = true
		combined = append(combined, message)
	}
	return append(combined, current...)
}

func renderedLineCount(content string) int {
	if content == "" {
		return 0
	}
	return len(strings.Split(content, "\n"))
}

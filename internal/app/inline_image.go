package app

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/messages"
	tea "github.com/charmbracelet/bubbletea"
)

type inlineImageKey struct {
	chatID       messages.ChatID
	attachmentID int64
	pathHash     uint32
}

type inlineImageState struct {
	generation    uint64
	path          string
	maxColumns    int
	maxRows       int
	loading       bool
	rendered      inlineimage.Rendered
	transferUntil time.Time
	err           error
}

type inlineImageLoadedMsg struct {
	key        inlineImageKey
	generation uint64
	rendered   inlineimage.Rendered
	err        error
}

type inlinePlacement struct {
	key       inlineImageKey
	startLine int
	left      int
	width     int
	height    int
}

func imageKey(chatID messages.ChatID, attachment messages.Attachment) inlineImageKey {
	key := inlineImageKey{chatID: chatID, attachmentID: attachment.ID}
	if attachment.ID == 0 {
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(attachment.Path))
		key.pathHash = hash.Sum32()
	}
	return key
}

func imageID(key inlineImageKey) uint32 {
	id := uint32(uint64(key.chatID)*2654435761) ^ uint32(key.attachmentID) ^ key.pathHash ^ 0x494d0000
	if id == 0 {
		return 1
	}
	return id
}

func placementID(key inlineImageKey) uint32 {
	id := imageID(key) ^ 0x504c4143
	if id == 0 {
		return 1
	}
	return id
}

func (m *Model) startInlineImageLoads(chatID messages.ChatID) tea.Cmd {
	if m.imageProtocol == inlineimage.Unsupported {
		return nil
	}
	state := m.threads[chatID]
	if state == nil || !state.loaded {
		return nil
	}
	bubbleWidth := min(72, max(12, m.conversationContentWidth*3/4))
	bubbleWidth = min(bubbleWidth, max(1, m.conversationContentWidth-2))
	maxColumns := min(50, max(1, bubbleWidth-4))
	maxRows := min(18, max(1, m.viewport.Height-4))
	var commands []tea.Cmd
	for _, message := range state.messages {
		for _, attachment := range message.Attachments {
			if !attachment.IsImage || attachment.Path == "" {
				continue
			}
			key := imageKey(chatID, attachment)
			current := m.inlineImages[key]
			if current != nil && current.path == attachment.Path && current.maxColumns == maxColumns && current.maxRows == maxRows {
				continue
			}
			generation := uint64(1)
			if current != nil {
				generation = current.generation + 1
			}
			next := &inlineImageState{
				generation: generation,
				path:       attachment.Path,
				maxColumns: maxColumns,
				maxRows:    maxRows,
				loading:    true,
			}
			if current != nil {
				next.rendered = current.rendered
			}
			m.inlineImages[key] = next
			path := attachment.Path
			protocol := m.imageProtocol
			id := imageID(key)
			commands = append(commands, func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				rendered, err := inlineimage.Render(ctx, path, protocol, id, maxColumns, maxRows)
				return inlineImageLoadedMsg{key: key, generation: generation, rendered: rendered, err: err}
			})
		}
	}
	return tea.Batch(commands...)
}

func (m Model) inlineImageFor(chatID messages.ChatID, attachment messages.Attachment) *inlineImageState {
	return m.inlineImages[imageKey(chatID, attachment)]
}

func (m Model) inlineTransfers(chatID messages.ChatID) string {
	if m.imageProtocol != inlineimage.Kitty {
		return ""
	}
	state := m.threads[chatID]
	if state == nil {
		return ""
	}
	var output strings.Builder
	for _, message := range state.messages {
		for _, attachment := range message.Attachments {
			imageState := m.inlineImageFor(chatID, attachment)
			if imageState != nil && !imageState.loading && imageState.err == nil && time.Now().Before(imageState.transferUntil) {
				output.WriteString(imageState.rendered.TransferSequence())
			}
		}
	}
	return output.String()
}

func (m Model) decorateInlineImages(view string, state *threadState) string {
	if m.imageProtocol == inlineimage.Unsupported || state == nil || len(state.placements) == 0 && len(m.inlineImages) == 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		return view
	}

	var controls strings.Builder
	controls.WriteString(m.inlinePlacementCleanup())

	visibleTop := m.viewport.YOffset
	visibleBottom := visibleTop + m.viewport.Height
	for _, placement := range state.placements {
		placementBottom := placement.startLine + placement.height
		visibleStart := max(placement.startLine, visibleTop)
		visibleEnd := min(placementBottom, visibleBottom)
		if visibleStart >= visibleEnd {
			continue
		}
		imageState := m.inlineImages[placement.key]
		if imageState == nil || imageState.loading || imageState.err != nil {
			continue
		}
		clippedTopRows := visibleStart - placement.startLine
		visibleRows := visibleEnd - visibleStart
		display := imageState.rendered.DisplayRegionSequence(placementID(placement.key), clippedTopRows, visibleRows)
		if display == "" {
			continue
		}
		row := visibleStart - visibleTop
		up := len(lines) - 1 - row
		left := m.viewport.Width - placement.left
		controls.WriteString("\x1b7")
		if up > 0 {
			_, _ = fmt.Fprintf(&controls, "\x1b[%dA", up)
		}
		if left > 0 {
			_, _ = fmt.Fprintf(&controls, "\x1b[%dD", left)
		}
		controls.WriteString(display)
		controls.WriteString("\x1b8")
	}
	_, _ = fmt.Fprintf(&controls, "\x1b[38;5;%dm\x1b[39m", 16+m.viewport.YOffset%216)
	lines[len(lines)-1] += controls.String()
	return strings.Join(lines, "\n")
}

func (m Model) sortedInlineImageKeys() []inlineImageKey {
	keys := make([]inlineImageKey, 0, len(m.inlineImages))
	for key := range m.inlineImages {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].chatID != keys[j].chatID {
			return keys[i].chatID < keys[j].chatID
		}
		if keys[i].attachmentID != keys[j].attachmentID {
			return keys[i].attachmentID < keys[j].attachmentID
		}
		return keys[i].pathHash < keys[j].pathHash
	})
	return keys
}

func (m Model) inlinePlacementCleanup() string {
	var output strings.Builder
	for _, key := range m.sortedInlineImageKeys() {
		imageState := m.inlineImages[key]
		if imageState != nil && imageState.rendered.Protocol == inlineimage.Kitty {
			output.WriteString(imageState.rendered.DeletePlacementSequence(placementID(key)))
		}
	}
	return output.String()
}

func (m Model) viewWithoutInlinePlacements(view string) string {
	cleanup := m.inlinePlacementCleanup()
	if cleanup == "" {
		return view
	}
	lines := strings.Split(view, "\n")
	lines[len(lines)-1] += cleanup
	return strings.Join(lines, "\n")
}

func (m Model) inlineImageCleanup() string {
	var output strings.Builder
	for _, key := range m.sortedInlineImageKeys() {
		imageState := m.inlineImages[key]
		if imageState != nil && imageState.rendered.Protocol == inlineimage.Kitty {
			output.WriteString(imageState.rendered.DeleteImageSequence())
		}
	}
	return output.String()
}

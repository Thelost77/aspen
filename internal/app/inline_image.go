package app

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"time"

	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/logger"
	"github.com/Thelost77/aspen/internal/messages"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	defaultInlineImageCacheBudget = 32 * 1024 * 1024
	inlineImageWorkerLimit        = 2
	inlineImageRenderTimeout      = 20 * time.Second
)

var errInlineImageCacheLimit = errors.New("inline image exceeds the memory cache limit")

type inlineImageKey struct {
	chatID       messages.ChatID
	attachmentID int64
	pathHash     uint32
}

type inlineImageState struct {
	generation       uint64
	threadGeneration uint64
	layoutGeneration uint64
	path             string
	sourceSize       int64
	maxColumns       int
	maxRows          int
	loading          bool
	cancel           context.CancelFunc
	rendered         inlineimage.Rendered
	payloadBytes     int
	lastVisible      uint64
	err              error
}

type inlineImageLoadedMsg struct {
	key              inlineImageKey
	generation       uint64
	threadGeneration uint64
	layoutGeneration uint64
	rendered         inlineimage.Rendered
	err              error
}

type inlinePlacement struct {
	key       inlineImageKey
	startLine int
	left      int
	height    int
}

type inlineImageRef struct {
	attachment messages.Attachment
	startLine  int
	height     int
}

type inlineImagePosition struct {
	row    int
	column int
}

type inlineImageRenderFunc func(context.Context, string, inlineimage.Protocol, uint32, int, int) (inlineimage.Rendered, error)

type inlineImageLoader struct {
	slots  chan struct{}
	render inlineImageRenderFunc
}

func newInlineImageLoader() *inlineImageLoader {
	return &inlineImageLoader{
		slots:  make(chan struct{}, inlineImageWorkerLimit),
		render: inlineimage.Render,
	}
}

func (l *inlineImageLoader) renderImage(
	ctx context.Context,
	path string,
	protocol inlineimage.Protocol,
	id uint32,
	maxColumns int,
	maxRows int,
) (inlineimage.Rendered, error) {
	select {
	case l.slots <- struct{}{}:
		defer func() { <-l.slots }()
	case <-ctx.Done():
		return inlineimage.Rendered{}, ctx.Err()
	}
	return l.render(ctx, path, protocol, id, maxColumns, maxRows)
}

type inlineImageCandidate struct {
	key        inlineImageKey
	attachment messages.Attachment
	visible    bool
	distance   int
	startLine  int
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

func (m *Model) SetInlineImageOutput(output inlineimage.TerminalOutput) {
	m.inlineImageOutput = output
}

func (m *Model) inlineImageDimensions() (int, int) {
	bubbleWidth := min(72, max(12, m.conversationContentWidth*3/4))
	bubbleWidth = min(bubbleWidth, max(1, m.conversationContentWidth-2))
	maxColumns := min(50, max(1, bubbleWidth-4))
	maxRows := min(18, max(1, m.viewport.Height-4))
	return maxColumns, maxRows
}

func (m *Model) refreshInlineImages() tea.Cmd {
	command := m.startInlineImageLoads(m.selectedID)
	m.syncInlineImageResidency()
	return command
}

func (m *Model) startInlineImageLoads(chatID messages.ChatID) tea.Cmd {
	wanted, candidates := m.inlineImageLoadCandidates(chatID)
	m.cancelStaleInlineImageLoads(wanted)
	if len(candidates) == 0 {
		return nil
	}

	maxColumns, maxRows := m.inlineImageDimensions()
	active := 0
	for _, imageState := range m.inlineImages {
		if imageState != nil && imageState.loading {
			active++
		}
	}
	available := max(0, inlineImageWorkerLimit-active)
	commands := make([]tea.Cmd, 0, available)
	threadGeneration := m.threads[chatID].generation
	for _, candidate := range candidates {
		if available == 0 {
			break
		}
		current := m.inlineImages[candidate.key]
		if current != nil && (current.path != candidate.attachment.Path || current.sourceSize != candidate.attachment.Size || current.maxColumns != maxColumns || current.maxRows != maxRows || current.layoutGeneration != m.inlineImageLayoutGeneration) {
			m.removeInlineImage(candidate.key)
			current = nil
		}
		if current != nil {
			continue
		}

		m.inlineImageGeneration++
		generation := m.inlineImageGeneration
		ctx, cancel := context.WithCancel(context.Background())
		m.inlineImages[candidate.key] = &inlineImageState{
			generation:       generation,
			threadGeneration: threadGeneration,
			layoutGeneration: m.inlineImageLayoutGeneration,
			path:             candidate.attachment.Path,
			sourceSize:       candidate.attachment.Size,
			maxColumns:       maxColumns,
			maxRows:          maxRows,
			loading:          true,
			cancel:           cancel,
		}
		path := candidate.attachment.Path
		protocol := m.imageProtocol
		id := imageID(candidate.key)
		key := candidate.key
		layoutGeneration := m.inlineImageLayoutGeneration
		loader := m.inlineImageLoader
		commands = append(commands, func() tea.Msg {
			started := time.Now()
			renderContext, timeoutCancel := context.WithTimeout(ctx, inlineImageRenderTimeout)
			defer timeoutCancel()
			rendered, err := loader.renderImage(renderContext, path, protocol, id, maxColumns, maxRows)
			logger.Debug(
				"inline image render completed",
				"protocol", protocol.String(),
				"duration_ms", time.Since(started).Milliseconds(),
				"failed", err != nil,
				"error_kind", inlineimage.ErrorKindOf(err),
			)
			return inlineImageLoadedMsg{
				key:              key,
				generation:       generation,
				threadGeneration: threadGeneration,
				layoutGeneration: layoutGeneration,
				rendered:         rendered,
				err:              err,
			}
		})
		available--
	}
	return tea.Batch(commands...)
}

func (m *Model) inlineImageLoadCandidates(chatID messages.ChatID) (map[inlineImageKey]bool, []inlineImageCandidate) {
	wanted := make(map[inlineImageKey]bool)
	if m.imageProtocol == inlineimage.Unsupported || chatID == 0 || chatID != m.selectedID || !m.conversationImagesVisible() {
		return wanted, nil
	}
	state := m.threads[chatID]
	if state == nil || !state.loaded || state.loading {
		return wanted, nil
	}

	visibleTop := m.viewport.YOffset
	visibleBottom := visibleTop + m.viewport.Height
	nearTop := max(0, visibleTop-m.viewport.Height)
	nearBottom := visibleBottom + m.viewport.Height
	byKey := make(map[inlineImageKey]inlineImageCandidate)
	first := sort.Search(len(state.imageRefs), func(index int) bool {
		ref := state.imageRefs[index]
		return ref.startLine+max(1, ref.height) > nearTop
	})
	for _, ref := range state.imageRefs[first:] {
		if ref.startLine >= nearBottom {
			break
		}
		refBottom := ref.startLine + max(1, ref.height)
		if ref.startLine >= nearBottom || refBottom <= nearTop || ref.attachment.Path == "" {
			continue
		}
		key := imageKey(chatID, ref.attachment)
		visible := ref.startLine < visibleBottom && refBottom > visibleTop
		distance := 0
		switch {
		case refBottom <= visibleTop:
			distance = visibleTop - refBottom
		case ref.startLine >= visibleBottom:
			distance = ref.startLine - visibleBottom
		}
		candidate := inlineImageCandidate{key: key, attachment: ref.attachment, visible: visible, distance: distance, startLine: ref.startLine}
		previous, exists := byKey[key]
		if !exists || candidate.visible && !previous.visible || candidate.visible == previous.visible && candidate.distance < previous.distance {
			byKey[key] = candidate
		}
		wanted[key] = true
	}
	candidates := make([]inlineImageCandidate, 0, len(byKey))
	for _, candidate := range byKey {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].visible != candidates[j].visible {
			return candidates[i].visible
		}
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		if candidates[i].startLine != candidates[j].startLine {
			return candidates[i].startLine < candidates[j].startLine
		}
		return lessInlineImageKey(candidates[i].key, candidates[j].key)
	})
	return wanted, candidates
}

func (m *Model) cancelStaleInlineImageLoads(wanted map[inlineImageKey]bool) {
	for key, imageState := range m.inlineImages {
		if imageState == nil || wanted[key] || !imageState.loading && imageState.err == nil {
			continue
		}
		m.removeInlineImage(key)
	}
}

func (m *Model) cancelInlineImageLoads(chatID messages.ChatID) {
	for key, imageState := range m.inlineImages {
		if key.chatID == chatID && imageState != nil && imageState.loading {
			m.removeInlineImage(key)
		}
	}
}

func (m *Model) applyInlineImageLoaded(msg inlineImageLoadedMsg) bool {
	imageState := m.inlineImages[msg.key]
	thread := m.threads[msg.key.chatID]
	if imageState == nil || imageState.generation != msg.generation || imageState.layoutGeneration != msg.layoutGeneration || thread == nil || thread.generation != msg.threadGeneration {
		return false
	}
	imageState.loading = false
	imageState.cancel = nil
	if msg.err != nil {
		imageState.err = msg.err
		return true
	}

	payloadBytes := msg.rendered.PayloadBytes()
	budget := m.inlineImageCacheBudget
	if budget <= 0 {
		budget = defaultInlineImageCacheBudget
	}
	if payloadBytes > budget || !m.evictInlineImagesFor(payloadBytes, msg.key, budget) {
		imageState.err = errInlineImageCacheLimit
		logger.Debug(
			"inline image cache rejected",
			"protocol", msg.rendered.Protocol.String(),
			"failed", true,
			"error_kind", "cache_limit",
		)
		return true
	}
	imageState.rendered = msg.rendered
	imageState.payloadBytes = payloadBytes
	imageState.err = nil
	m.inlineImageBytes += payloadBytes
	return true
}

func (m *Model) evictInlineImagesFor(additional int, keep inlineImageKey, budget int) bool {
	if m.inlineImageBytes+additional <= budget {
		return true
	}
	visible := m.visibleInlineImageKeys()
	var candidates []inlineImageKey
	for key, imageState := range m.inlineImages {
		if key == keep || visible[key] || imageState == nil || imageState.loading || imageState.payloadBytes == 0 {
			continue
		}
		candidates = append(candidates, key)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := m.inlineImages[candidates[i]]
		right := m.inlineImages[candidates[j]]
		if left.lastVisible != right.lastVisible {
			return left.lastVisible < right.lastVisible
		}
		return lessInlineImageKey(candidates[i], candidates[j])
	})
	for _, key := range candidates {
		m.removeInlineImage(key)
		if m.inlineImageBytes+additional <= budget {
			return true
		}
	}
	return m.inlineImageBytes+additional <= budget
}

func (m *Model) pruneInlineImages() {
	referenced := make(map[inlineImageKey]map[string]bool)
	for chatID, thread := range m.threads {
		if thread == nil || !thread.loaded {
			continue
		}
		for _, message := range thread.messages {
			for _, attachment := range message.Attachments {
				if !attachment.IsImage || attachment.Path == "" {
					continue
				}
				key := imageKey(chatID, attachment)
				if referenced[key] == nil {
					referenced[key] = make(map[string]bool)
				}
				referenced[key][attachment.Path] = true
			}
		}
	}
	for key, imageState := range m.inlineImages {
		if imageState == nil || !referenced[key][imageState.path] {
			m.removeInlineImage(key)
		}
	}
}

func (m *Model) removeInlineImage(key inlineImageKey) {
	imageState := m.inlineImages[key]
	if imageState == nil {
		delete(m.inlineImageResidents, key)
		return
	}
	if imageState.cancel != nil {
		imageState.cancel()
	}
	if m.inlineImageResidents[key] {
		m.freeResidentInlineImage(key, imageState)
	}
	m.inlineImageBytes = max(0, m.inlineImageBytes-imageState.payloadBytes)
	delete(m.inlineImages, key)
}

func (m Model) inlineImageFor(chatID messages.ChatID, attachment messages.Attachment) *inlineImageState {
	imageState := m.inlineImages[imageKey(chatID, attachment)]
	if imageState == nil || (imageState.path != "" && imageState.path != attachment.Path) || imageState.sourceSize != attachment.Size {
		return nil
	}
	return imageState
}

func (m *Model) syncInlineImageResidency() {
	if m.inlineImageOutput == nil {
		return
	}
	desired := m.visibleInlineImagePlacements()
	for key := range m.inlineImageResidents {
		placement, visible := desired[key]
		imageState := m.inlineImages[key]
		if visible && imageState != nil && (imageState.rendered.Protocol != inlineimage.ITerm2 || m.inlineImageITermPositions[key] == m.iterm2PlacementPosition(placement)) {
			continue
		}
		m.freeResidentInlineImage(key, imageState)
	}
	for key, placement := range desired {
		if m.inlineImageResidents[key] {
			continue
		}
		imageState := m.inlineImages[key]
		if imageState == nil || imageState.loading || imageState.err != nil || imageState.payloadBytes == 0 {
			continue
		}
		m.inlineImageAccess++
		imageState.lastVisible = m.inlineImageAccess
		switch imageState.rendered.Protocol {
		case inlineimage.Kitty:
			m.queueInlineImageOperation(true, imageState.rendered.TransferSequence())
		case inlineimage.ITerm2:
			m.queueInlineImageOperation(false, m.iterm2PlacementSequence(placement, imageState.rendered))
			m.inlineImageITermPositions[key] = m.iterm2PlacementPosition(placement)
		default:
			continue
		}
		m.inlineImageResidents[key] = true
	}
}

func (m *Model) visibleInlineImagePlacements() map[inlineImageKey]inlinePlacement {
	visible := make(map[inlineImageKey]inlinePlacement)
	if !m.conversationImagesVisible() {
		return visible
	}
	state := m.threads[m.selectedID]
	if state == nil {
		return visible
	}
	visibleTop := m.viewport.YOffset
	visibleBottom := visibleTop + m.viewport.Height
	first := sort.Search(len(state.placements), func(index int) bool {
		placement := state.placements[index]
		return placement.startLine+placement.height > visibleTop
	})
	for _, placement := range state.placements[first:] {
		if placement.startLine >= visibleBottom {
			break
		}
		placementBottom := placement.startLine + placement.height
		if placement.startLine >= visibleBottom || placementBottom <= visibleTop {
			continue
		}
		imageState := m.inlineImages[placement.key]
		if imageState == nil || imageState.loading || imageState.err != nil {
			continue
		}
		if imageState.rendered.Protocol == inlineimage.ITerm2 && (placement.startLine < visibleTop || placementBottom > visibleBottom) {
			continue
		}
		if _, exists := visible[placement.key]; !exists {
			visible[placement.key] = placement
		}
	}
	return visible
}

func (m *Model) visibleInlineImageKeys() map[inlineImageKey]bool {
	visible := make(map[inlineImageKey]bool)
	for key := range m.visibleInlineImagePlacements() {
		visible[key] = true
	}
	return visible
}

func (m Model) conversationImagesVisible() bool {
	return !m.help.Visible() && m.selectedID != 0 && (m.layout != LayoutNarrow || m.narrowPane == NarrowConversation)
}

func (m *Model) freeResidentInlineImage(key inlineImageKey, imageState *inlineImageState) {
	if imageState != nil {
		switch imageState.rendered.Protocol {
		case inlineimage.Kitty:
			m.queueInlineImageOperation(true, imageState.rendered.DeleteImageSequence())
		case inlineimage.ITerm2:
			if m.inlineImageOutput != nil {
				m.inlineImageOutput.ClearAfterFrame()
				m.inlineImageFrameGeneration++
			}
			for residentKey := range m.inlineImageResidents {
				resident := m.inlineImages[residentKey]
				if resident != nil && resident.rendered.Protocol == inlineimage.ITerm2 {
					delete(m.inlineImageResidents, residentKey)
					delete(m.inlineImageITermPositions, residentKey)
				}
			}
		}
	}
	delete(m.inlineImageResidents, key)
	delete(m.inlineImageITermPositions, key)
}

func (m *Model) resetInlineImageResidency() {
	for key := range m.inlineImageResidents {
		m.freeResidentInlineImage(key, m.inlineImages[key])
	}
}

func (m *Model) retryInlineImages() tea.Cmd {
	for key, imageState := range m.inlineImages {
		if imageState != nil && imageState.err != nil {
			m.removeInlineImage(key)
		}
	}
	m.resetInlineImageResidency()
	m.status = "Retrying visible images…"
	return m.refreshInlineImages()
}

func (m *Model) invalidateInlineImageLayout() {
	m.resetInlineImageResidency()
	m.inlineImageLayoutGeneration++
	for key := range m.inlineImages {
		m.removeInlineImage(key)
	}
}

func (m *Model) queueInlineImageOperation(beforeFrame bool, sequence string) {
	if sequence == "" || m.inlineImageOutput == nil {
		return
	}
	if beforeFrame {
		m.inlineImageOutput.QueueBeforeFrame(sequence)
	} else {
		m.inlineImageOutput.QueueAfterFrame(sequence)
	}
	m.inlineImageFrameGeneration++
}

func (m Model) inlineImageFrameMarker() string {
	if m.inlineImageFrameGeneration == 0 {
		return ""
	}
	generation := m.inlineImageFrameGeneration
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[39m", byte(generation>>16), byte(generation>>8), byte(generation))
}

func (m Model) iterm2PlacementPosition(placement inlinePlacement) inlineImagePosition {
	conversationLeft := 1
	if m.layout == LayoutSplit {
		conversationLeft = m.sidebarWidth + 2
	}
	return inlineImagePosition{
		row:    3 + placement.startLine - m.viewport.YOffset,
		column: conversationLeft + placement.left,
	}
}

func (m Model) iterm2PlacementSequence(placement inlinePlacement, rendered inlineimage.Rendered) string {
	visibleTop := m.viewport.YOffset
	clippedTopRows := max(0, visibleTop-placement.startLine)
	visibleRows := min(placement.startLine+placement.height, visibleTop+m.viewport.Height) - max(placement.startLine, visibleTop)
	display := rendered.ITerm2DisplayRegionSequence(clippedTopRows, visibleRows)
	if display == "" {
		return ""
	}
	position := m.iterm2PlacementPosition(placement)
	return fmt.Sprintf("\x1b7\x1b[%d;%dH%s\x1b8", position.row+1, position.column+1, display)
}

func (m *Model) closeInlineImages() error {
	for _, imageState := range m.inlineImages {
		if imageState != nil && imageState.cancel != nil {
			imageState.cancel()
		}
	}
	m.resetInlineImageResidency()
	if m.inlineImageOutput == nil {
		return nil
	}
	return m.inlineImageOutput.Flush()
}

func lessInlineImageKey(left, right inlineImageKey) bool {
	if left.chatID != right.chatID {
		return left.chatID < right.chatID
	}
	if left.attachmentID != right.attachmentID {
		return left.attachmentID < right.attachmentID
	}
	return left.pathHash < right.pathHash
}

package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/messages"
	tea "github.com/charmbracelet/bubbletea"
)

func TestImageAttachmentRendersInlineWithKittyProtocol(t *testing.T) {
	path := writeInlineImageFixture(t, "IMG_fixture.png", color.NRGBA{R: 40, G: 80, B: 210, A: 255})
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	var captured bytes.Buffer
	output := inlineimage.NewTerminalOutput(&captured)
	model.SetInlineImageOutput(output)
	attachment := messages.Attachment{ID: 7, Name: "IMG_fixture.png", Path: path, MIMEType: "image/png", IsImage: true}
	model.threads[1].messages[0].Attachments = []messages.Attachment{attachment}
	model.syncViewport(false)
	model.viewport.GotoBottom()
	model = executeCmd(t, model, model.refreshInlineImages())

	imageState := model.inlineImageFor(1, attachment)
	if imageState == nil || imageState.loading || imageState.err != nil {
		t.Fatalf("inline image state = %#v", imageState)
	}
	if len(model.threads[1].placements) != 1 {
		t.Fatalf("inline placements = %d, want 1", len(model.threads[1].placements))
	}
	view := model.View()
	if !strings.Contains(view, "\U0010eeee") {
		t.Fatal("conversation lacks Kitty Unicode placeholders")
	}
	if inlineImageContainsTransfer(view) || strings.Contains(view, "a=p,i=") || strings.Contains(view, "▀") {
		t.Fatal("normal view contains an image payload, cursor placement, or block fallback")
	}
	if _, err := output.Write([]byte(view)); err != nil {
		t.Fatal(err)
	}
	terminalBytes := captured.String()
	if !strings.Contains(terminalBytes, "\x1b_Ga=T,f=100") || !strings.Contains(terminalBytes, "U=1") {
		t.Fatalf("terminal output lacks Kitty transfer: %q", terminalBytes)
	}
	if transfer, placeholder := strings.Index(terminalBytes, "\x1b_Ga=T"), strings.Index(terminalBytes, "\U0010eeee"); transfer < 0 || placeholder < 0 || transfer > placeholder {
		t.Fatal("Kitty placeholder was written before its image transfer")
	}

	captured.Reset()
	if _, err := output.Write([]byte(model.View())); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(captured.String(), "\x1b_Ga=T") {
		t.Fatal("steady-state view retransmitted the image payload")
	}
}

func TestKittyFreesImagesOnlyOnResidentToFreedTransition(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	var captured bytes.Buffer
	output := inlineimage.NewTerminalOutput(&captured)
	model.SetInlineImageOutput(output)
	visibleKey := inlineImageKey{chatID: 1, attachmentID: 7}
	hiddenKey := inlineImageKey{chatID: 2, attachmentID: 8}
	model.inlineImages[visibleKey] = &inlineImageState{rendered: inlineimage.Rendered{Protocol: inlineimage.Kitty, ID: 42, Columns: 10, Rows: 3}}
	model.inlineImages[hiddenKey] = &inlineImageState{rendered: inlineimage.Rendered{Protocol: inlineimage.Kitty, ID: 43, Columns: 10, Rows: 3}}
	model.inlineImageResidents[visibleKey] = true
	model.inlineImageResidents[hiddenKey] = true
	model.threads[1].placements = []inlinePlacement{{key: visibleKey, startLine: 0, height: 3}}
	model.viewport.Height = 3
	model.viewport.SetYOffset(0)

	model.syncInlineImageResidency()
	if err := output.Flush(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(captured.String(), "a=d,d=i,i=43") {
		t.Fatalf("hidden resident image was not freed: %q", captured.String())
	}
	if strings.Contains(captured.String(), "a=d,d=i,i=42") {
		t.Fatalf("visible resident image was freed: %q", captured.String())
	}
	captured.Reset()
	model.syncInlineImageResidency()
	if err := output.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(captured.String(), "a=d,d=i") {
		t.Fatalf("freed image was deleted twice: %q", captured.String())
	}
}

func TestImageAttachmentRendersInlineWithITerm2Protocol(t *testing.T) {
	path := writeInlineImageFixture(t, "IMG_fixture.png", color.NRGBA{A: 255})
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.ITerm2
	var captured bytes.Buffer
	output := inlineimage.NewTerminalOutput(&captured)
	model.SetInlineImageOutput(output)
	attachment := messages.Attachment{ID: 8, Name: "IMG_fixture.png", Path: path, MIMEType: "image/png", IsImage: true}
	model.threads[1].messages[0].Attachments = []messages.Attachment{attachment}
	model.syncViewport(false)
	model.viewport.GotoBottom()
	model = executeCmd(t, model, model.refreshInlineImages())

	view := model.View()
	if inlineImageContainsTransfer(view) {
		t.Fatal("normal view contains an iTerm2 image payload")
	}
	if _, err := output.Write([]byte(view)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(captured.String(), "\x1b]1337;File=inline=1") {
		t.Fatal("terminal output lacks iTerm2 inline image sequence")
	}
	if strings.Contains(captured.String(), "▀") {
		t.Fatal("terminal output used a block fallback")
	}
}

func TestHelpHidesAndRestoresKittyImages(t *testing.T) {
	path := writeInlineImageFixture(t, "fixture.png", color.NRGBA{A: 255})
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	var captured bytes.Buffer
	output := inlineimage.NewTerminalOutput(&captured)
	model.SetInlineImageOutput(output)
	attachment := messages.Attachment{ID: 7, Name: "fixture.png", Path: path, IsImage: true}
	model.threads[1].messages[0].Attachments = []messages.Attachment{attachment}
	model.syncViewport(false)
	model.viewport.GotoBottom()
	model = executeCmd(t, model, model.refreshInlineImages())
	if !strings.Contains(model.View(), "\U0010eeee") {
		t.Fatal("conversation lacks Kitty placeholders before help opens")
	}
	if err := output.Flush(); err != nil {
		t.Fatal(err)
	}
	captured.Reset()

	for range 2 {
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		model = executeCmd(t, updated.(Model), cmd)
		if !model.help.Visible() || strings.Contains(model.View(), "\U0010eeee") {
			t.Fatal("help retained Kitty placeholder content")
		}
		if err := output.Flush(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(captured.String(), "a=d,d=i") {
			t.Fatal("help did not free the resident Kitty image")
		}
		captured.Reset()

		updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		model = executeCmd(t, updated.(Model), cmd)
		if model.help.Visible() || !strings.Contains(model.View(), "\U0010eeee") {
			t.Fatal("closing help did not restore Kitty placeholders")
		}
		if err := output.Flush(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(captured.String(), "\x1b_Ga=T") {
			t.Fatal("closing help did not re-upload the visible Kitty image")
		}
		captured.Reset()
	}
}

func TestPartiallyVisibleKittyImageUsesClippedPlaceholderRows(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	rendered := inlineimage.Rendered{Protocol: inlineimage.Kitty, ID: 42, Columns: 10, Rows: 3}
	model.viewport.Width = 20
	model.viewport.Height = 3
	model.viewport.SetContent(strings.Join([]string{
		rendered.PlaceholderRow(0),
		rendered.PlaceholderRow(1),
		rendered.PlaceholderRow(2),
		"after",
	}, "\n"))
	model.viewport.SetYOffset(1)

	view := model.viewport.View()
	if strings.Contains(view, "\x1b_Ga=p") {
		t.Fatalf("Kitty clipping used cursor graphics: %q", view)
	}
	if !strings.Contains(view, rendered.PlaceholderRow(1)) || !strings.Contains(view, rendered.PlaceholderRow(2)) {
		t.Fatalf("visible Kitty source rows were not preserved: %q", view)
	}
}

func TestMultipleImagesLoadThroughBubbleTeaBatchRuntime(t *testing.T) {
	paths := []string{
		writeInlineImageFixture(t, "first.png", color.NRGBA{R: 200, A: 255}),
		writeInlineImageFixture(t, "second.png", color.NRGBA{B: 200, A: 255}),
		writeInlineImageFixture(t, "third.png", color.NRGBA{G: 200, A: 255}),
	}
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	model.height = 60
	model.setSizes()
	var captured synchronizedBuffer
	output := inlineimage.NewTerminalOutput(&captured)
	model.SetInlineImageOutput(output)
	model.focus = FocusViewport
	originalRender := model.inlineImageLoader.render
	var activeRenders atomic.Int32
	var maxActiveRenders atomic.Int32
	model.inlineImageLoader.render = func(ctx context.Context, path string, protocol inlineimage.Protocol, id uint32, columns, rows int) (inlineimage.Rendered, error) {
		active := activeRenders.Add(1)
		for {
			maximum := maxActiveRenders.Load()
			if active <= maximum || maxActiveRenders.CompareAndSwap(maximum, active) {
				break
			}
		}
		defer activeRenders.Add(-1)
		time.Sleep(25 * time.Millisecond)
		return originalRender(ctx, path, protocol, id, columns, rows)
	}
	imageMessage := model.threads[1].messages[0]
	var older []messages.Message
	for i := range 12 {
		older = append(older, messages.Message{ID: messages.MessageID(100 + i), ChatID: 1, Text: strings.Repeat("older synthetic line ", 4), SentAt: imageMessage.SentAt.Add(-time.Duration(12-i) * time.Minute)})
	}
	model.threads[1].messages = append(older, imageMessage)
	for i, path := range paths {
		last := len(model.threads[1].messages) - 1
		model.threads[1].messages[last].Attachments = append(model.threads[1].messages[last].Attachments, messages.Attachment{
			ID: int64(i + 1), Name: filepath.Base(path), Path: path, IsImage: true,
		})
	}
	model.syncViewport(false)
	model.viewport.GotoBottom()

	programModel := &inlineImageProgramModel{model: model}
	program := tea.NewProgram(programModel, tea.WithInput(nil), tea.WithOutput(output), tea.WithAltScreen(), tea.WithFPS(120))
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	waitForTerminalCount(t, &captured, "\x1b_Ga=T", 3)

	before := captured.Count("\x1b_Ga=T")
	program.Send(tea.FocusMsg{})
	time.Sleep(30 * time.Millisecond)
	if got := captured.Count("\x1b_Ga=T"); got != before {
		t.Fatalf("unchanged render retransmitted images: got %d transfers, want %d", got, before)
	}

	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	waitForTerminalCount(t, &captured, "a=d,d=i", 3)
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	waitForTerminalCount(t, &captured, "\x1b_Ga=T", 6)

	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	waitForTerminalCount(t, &captured, "a=d,d=i", 6)
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	waitForTerminalCount(t, &captured, "\x1b_Ga=T", 9)

	program.Send(tea.WindowSizeMsg{Width: 80, Height: 60})
	waitForTerminalCount(t, &captured, "\x1b_Ga=T", 12)
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	waitForTerminalCount(t, &captured, "\x1b_Ga=T", 15)

	program.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Bubble Tea program did not stop")
	}
	if inlineImageContainsTransfer(programModel.model.View()) {
		t.Fatal("model view retained image transfer bytes")
	}
	if maxActiveRenders.Load() != inlineImageWorkerLimit {
		t.Fatalf("maximum concurrent renders = %d, want %d", maxActiveRenders.Load(), inlineImageWorkerLimit)
	}
}

func TestSchedulesOnlyVisibleAndNearVisibleImages(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	model.focus = FocusViewport
	model.viewport.Height = 10
	model.viewport.SetContent(strings.Repeat("line\n", 200))
	model.viewport.SetYOffset(100)
	visible := messages.Attachment{ID: 61, Name: "visible.png", Path: "/synthetic/visible.png", IsImage: true}
	near := messages.Attachment{ID: 62, Name: "near.png", Path: "/synthetic/near.png", IsImage: true}
	far := messages.Attachment{ID: 63, Name: "far.png", Path: "/synthetic/far.png", IsImage: true}
	model.threads[1].imageRefs = []inlineImageRef{
		{attachment: far, startLine: 1, height: 1},
		{attachment: near, startLine: 91, height: 1},
		{attachment: visible, startLine: 102, height: 1},
	}

	command := model.startInlineImageLoads(1)
	if command == nil {
		t.Fatal("visible image loads were not scheduled")
	}
	if model.inlineImages[imageKey(1, visible)] == nil || model.inlineImages[imageKey(1, near)] == nil {
		t.Fatal("visible or near-visible image was not scheduled")
	}
	if model.inlineImages[imageKey(1, far)] != nil {
		t.Fatal("far image was scheduled eagerly")
	}
	model.cancelStaleInlineImageLoads(nil)
}

func TestCacheEvictsLeastRecentlyVisiblePayloadAndReloadsIt(t *testing.T) {
	path := writeInlineImageFixture(t, "cache.png", color.NRGBA{R: 90, G: 30, A: 255})
	rendered, err := inlineimage.Render(context.Background(), path, inlineimage.Kitty, 42, 20, 8)
	if err != nil {
		t.Fatal(err)
	}
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	oldAttachment := messages.Attachment{ID: 71, Name: "old.png", Path: path, IsImage: true}
	newAttachment := messages.Attachment{ID: 72, Name: "new.png", Path: path, IsImage: true}
	oldKey := imageKey(1, oldAttachment)
	newKey := imageKey(1, newAttachment)
	payloadBytes := rendered.PayloadBytes()
	model.inlineImageCacheBudget = payloadBytes
	model.inlineImages[oldKey] = &inlineImageState{path: path, rendered: rendered, payloadBytes: payloadBytes, lastVisible: 1}
	model.inlineImageBytes = payloadBytes
	model.inlineImageGeneration++
	generation := model.inlineImageGeneration
	model.inlineImages[newKey] = &inlineImageState{
		generation: generation, threadGeneration: model.threads[1].generation, layoutGeneration: model.inlineImageLayoutGeneration,
		path: path, loading: true,
	}
	accepted := model.applyInlineImageLoaded(inlineImageLoadedMsg{
		key: newKey, generation: generation, threadGeneration: model.threads[1].generation,
		layoutGeneration: model.inlineImageLayoutGeneration, rendered: rendered,
	})
	if !accepted || model.inlineImages[oldKey] != nil || model.inlineImages[newKey] == nil {
		t.Fatalf("cache eviction failed: entries=%d bytes=%d resident=%d", len(model.inlineImages), model.inlineImageBytes, len(model.inlineImageResidents))
	}
	if model.inlineImageBytes > model.inlineImageCacheBudget {
		t.Fatalf("cache bytes = %d, budget = %d", model.inlineImageBytes, model.inlineImageCacheBudget)
	}

	model.threads[1].messages[0].Attachments = []messages.Attachment{oldAttachment}
	model.syncViewport(false)
	model.viewport.GotoBottom()
	command := model.refreshInlineImages()
	if command == nil || model.inlineImages[oldKey] == nil || !model.inlineImages[oldKey].loading {
		t.Fatal("evicted image did not start loading after it became visible")
	}
}

func TestPruneRemovesPayloadsNoLongerReferencedByLoadedMessages(t *testing.T) {
	path := writeInlineImageFixture(t, "removed.png", color.NRGBA{B: 90, A: 255})
	rendered, err := inlineimage.Render(context.Background(), path, inlineimage.Kitty, 42, 20, 8)
	if err != nil {
		t.Fatal(err)
	}
	model, _, _ := loadTestModelWithSender(t)
	attachment := messages.Attachment{ID: 73, Name: "removed.png", Path: path, IsImage: true}
	key := imageKey(1, attachment)
	model.inlineImages[key] = &inlineImageState{path: path, rendered: rendered, payloadBytes: rendered.PayloadBytes()}
	model.inlineImageBytes = rendered.PayloadBytes()

	model.pruneInlineImages()
	if model.inlineImages[key] != nil || model.inlineImageBytes != 0 {
		t.Fatalf("unreferenced image remained cached: entries=%d bytes=%d", len(model.inlineImages), model.inlineImageBytes)
	}
}

func TestChatSwitchAndResizeCancelStaleInlineImageWork(t *testing.T) {
	for _, test := range []struct {
		name   string
		cancel func(*Model)
	}{
		{name: "chat switch", cancel: func(model *Model) { _ = model.selectChat(2) }},
		{name: "resize", cancel: func(model *Model) {
			updated, _ := model.Update(tea.WindowSizeMsg{Width: 45, Height: 18})
			*model = updated.(Model)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, _, _ := loadTestModelWithSender(t)
			model.imageProtocol = inlineimage.Kitty
			attachment := messages.Attachment{ID: 81, Name: "slow.png", Path: "/synthetic/slow.png", IsImage: true}
			model.threads[1].messages[0].Attachments = []messages.Attachment{attachment}
			model.syncViewport(false)
			model.viewport.GotoBottom()
			started := make(chan struct{})
			model.inlineImageLoader.render = func(ctx context.Context, _ string, _ inlineimage.Protocol, _ uint32, _, _ int) (inlineimage.Rendered, error) {
				close(started)
				<-ctx.Done()
				return inlineimage.Rendered{}, ctx.Err()
			}
			command := model.refreshInlineImages()
			key := imageKey(1, attachment)
			staleGeneration := model.inlineImages[key].generation
			result := make(chan tea.Msg, 1)
			go func() { result <- command() }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("image conversion did not start")
			}
			test.cancel(&model)
			select {
			case message := <-result:
				loaded, ok := message.(inlineImageLoadedMsg)
				if !ok || !errorsIsCanceled(loaded.err) {
					t.Fatalf("canceled command returned %#v", message)
				}
				updated, _ := model.Update(loaded)
				model = updated.(Model)
			case <-time.After(time.Second):
				t.Fatal("stale image conversion was not canceled")
			}
			if imageState := model.inlineImages[key]; imageState != nil && imageState.generation == staleGeneration {
				t.Fatal("stale loading generation survived cancellation")
			}
		})
	}
}

func TestDuplicateAttachmentPlacementsShareOneTransfer(t *testing.T) {
	path := writeInlineImageFixture(t, "shared.png", color.NRGBA{G: 180, A: 255})
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	var captured bytes.Buffer
	output := inlineimage.NewTerminalOutput(&captured)
	model.SetInlineImageOutput(output)
	attachment := messages.Attachment{ID: 91, Name: "shared.png", Path: path, IsImage: true}
	model.threads[1].messages[0].Attachments = []messages.Attachment{attachment, attachment}
	model.syncViewport(false)
	model.viewport.GotoBottom()
	model = executeCmd(t, model, model.refreshInlineImages())
	if len(model.threads[1].placements) != 2 {
		t.Fatalf("placements = %d, want 2", len(model.threads[1].placements))
	}
	if err := output.Flush(); err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(captured.String(), "\x1b_Ga=T"); count != 1 {
		t.Fatalf("shared attachment transfer count = %d, want 1", count)
	}
}

type inlineImageProgramModel struct {
	model Model
}

func (m *inlineImageProgramModel) Init() tea.Cmd {
	return m.model.refreshInlineImages()
}

func (m *inlineImageProgramModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	updated, command := m.model.Update(message)
	m.model = updated.(Model)
	return m, command
}

func (m *inlineImageProgramModel) View() string {
	return m.model.View()
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *synchronizedBuffer) Count(value string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buffer.String(), value)
}

func waitForTerminalCount(t *testing.T, output *synchronizedBuffer, value string, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if output.Count(value) >= count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("terminal output count for %q = %d, want at least %d", value, output.Count(value), count)
}

func writeInlineImageFixture(t *testing.T, name string, fill color.NRGBA) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			pixel := fill
			pixel.R += uint8(x)
			pixel.G += uint8(y)
			fixture.Set(x, y, pixel)
		}
	}
	if err := png.Encode(file, fixture); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func errorsIsCanceled(err error) bool {
	return err != nil && inlineimage.ErrorKindOf(err) == inlineimage.ErrorCanceled
}

func inlineImageContainsTransfer(view string) bool {
	return strings.Contains(view, "\x1b_Ga=T") || strings.Contains(view, "\x1b]1337;File=inline=1")
}

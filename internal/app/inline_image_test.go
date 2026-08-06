package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/messages"
)

func TestImageAttachmentRendersInlineWithKittyProtocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "IMG_fixture.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			fixture.Set(x, y, color.NRGBA{R: uint8(30 * x), G: uint8(50 * y), B: 210, A: 255})
		}
	}
	if err := png.Encode(file, fixture); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	attachment := messages.Attachment{ID: 7, Name: "IMG_fixture.png", Path: path, MIMEType: "image/png", IsImage: true}
	model.threads[1].messages[0].Attachments = []messages.Attachment{attachment}
	imageCmd := model.startInlineImageLoads(1)
	model.syncViewport(false)
	model.viewport.GotoBottom()
	model = executeCmd(t, model, imageCmd)

	imageState := model.inlineImageFor(1, attachment)
	if imageState == nil || imageState.loading || imageState.err != nil {
		t.Fatalf("inline image state = %#v", imageState)
	}
	if len(model.threads[1].placements) != 1 {
		t.Fatalf("inline placements = %d, want 1", len(model.threads[1].placements))
	}
	view := model.View()
	if !strings.Contains(view, "\x1b_Ga=t,f=100") || !strings.Contains(view, "a=p,i=") {
		t.Fatalf("conversation lacks Kitty transfer or placement")
	}
	if strings.Contains(view, "▀") {
		t.Fatal("conversation fell back to low-quality half blocks")
	}
}

func TestImageAttachmentRendersInlineWithITerm2Protocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "IMG_fixture.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewNRGBA(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.ITerm2
	attachment := messages.Attachment{ID: 8, Name: "IMG_fixture.png", Path: path, MIMEType: "image/png", IsImage: true}
	model.threads[1].messages[0].Attachments = []messages.Attachment{attachment}
	model = executeCmd(t, model, model.startInlineImageLoads(1))
	model.viewport.GotoBottom()

	view := model.View()
	if !strings.Contains(view, "\x1b]1337;File=inline=1") {
		t.Fatal("conversation lacks iTerm2 inline image sequence")
	}
	if strings.Contains(view, "▀") {
		t.Fatal("conversation fell back to low-quality half blocks")
	}
}

func TestHelpClearsKittyPlacements(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	key := inlineImageKey{chatID: 1, attachmentID: 7}
	model.inlineImages[key] = &inlineImageState{rendered: inlineimage.Rendered{Protocol: inlineimage.Kitty, ID: 42, Columns: 10, Rows: 3}}
	model.help.Toggle()

	view := model.View()
	if !strings.Contains(view, "a=d,d=i,i=42") {
		t.Fatal("help did not clear Kitty image placement")
	}
	if strings.Contains(view, "a=p,i=42") {
		t.Fatal("help placed image above dialog")
	}
}

func TestPartiallyVisibleKittyImageIsCroppedToViewport(t *testing.T) {
	model, _, _ := loadTestModelWithSender(t)
	model.imageProtocol = inlineimage.Kitty
	key := inlineImageKey{chatID: 1, attachmentID: 7}
	model.inlineImages[key] = &inlineImageState{rendered: inlineimage.Rendered{
		Protocol: inlineimage.Kitty, ID: 42, Columns: 10, Rows: 3, PixelWidth: 100, PixelHeight: 60,
	}}
	state := model.threads[1]
	state.placements = []inlinePlacement{{key: key, startLine: 0, left: 0, width: 10, height: 3}}
	model.viewport.Width = 20
	model.viewport.Height = 3
	model.viewport.SetContent("one\ntwo\nthree\nfour")
	model.viewport.SetYOffset(1)

	decorated := model.decorateInlineImages(model.viewport.View(), state)
	if !strings.Contains(decorated, "a=p,i=42") || !strings.Contains(decorated, "y=20,w=100,h=40,c=10,r=2") {
		t.Fatalf("partially visible image was not cropped correctly: %q", decorated)
	}
	if !strings.Contains(decorated, "a=d,d=i") {
		t.Fatal("previous Kitty placement was not cleared")
	}
}

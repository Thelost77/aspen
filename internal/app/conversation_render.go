package app

import (
	"strings"
	"time"

	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/messages"
	"github.com/Thelost77/aspen/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

type renderedConversation struct {
	content    string
	placements []inlinePlacement
	imageRefs  []inlineImageRef
}

type renderedBlock struct {
	content    string
	placements []inlinePlacement
	imageRefs  []inlineImageRef
}

func renderMessages(all []messages.Message, chat messages.Chat, width int, styles ui.Styles) string {
	return renderMessagesInline(all, chat, width, styles, nil, "").content
}

func renderMessagesInline(
	all []messages.Message,
	chat messages.Chat,
	width int,
	styles ui.Styles,
	imageLookup func(messages.ChatID, messages.Attachment) *inlineImageState,
	imageBlockedLabel string,
) renderedConversation {
	if width <= 0 || len(all) == 0 {
		return renderedConversation{}
	}
	var blocks []renderedBlock
	var previous messages.Message
	for i, message := range all {
		if i == 0 || !ui.SameLocalDay(previous.SentAt, message.SentAt) {
			label := " " + ui.DateLabel(message.SentAt) + " "
			lineWidth := max(0, (width-lipgloss.Width(label))/2)
			separator := strings.Repeat("─", lineWidth) + label + strings.Repeat("─", max(0, width-lineWidth-lipgloss.Width(label)))
			blocks = append(blocks, renderedBlock{content: styles.Muted.Render(separator)})
		}
		grouped := i > 0 && previous.IsFromMe == message.IsFromMe && previous.Sender == message.Sender && ui.SameLocalDay(previous.SentAt, message.SentAt) && message.SentAt.Sub(previous.SentAt) <= 5*time.Minute
		blocks = append(blocks, renderMessageInline(message, chat, width, grouped, styles, imageLookup, imageBlockedLabel))
		previous = message
	}

	var output strings.Builder
	var placements []inlinePlacement
	var imageRefs []inlineImageRef
	lineOffset := 0
	for i, block := range blocks {
		if i > 0 {
			output.WriteString("\n\n")
			lineOffset++
		}
		for _, placement := range block.placements {
			placement.startLine += lineOffset
			placements = append(placements, placement)
		}
		for _, ref := range block.imageRefs {
			ref.startLine += lineOffset
			imageRefs = append(imageRefs, ref)
		}
		output.WriteString(block.content)
		lineOffset += renderedLineCount(block.content)
	}
	return renderedConversation{content: output.String(), placements: placements, imageRefs: imageRefs}
}

func renderMessageInline(
	message messages.Message,
	chat messages.Chat,
	width int,
	grouped bool,
	styles ui.Styles,
	imageLookup func(messages.ChatID, messages.Attachment) *inlineImageState,
	imageBlockedLabel string,
) renderedBlock {
	bubbleOuterWidth := min(72, max(12, width*3/4))
	bubbleOuterWidth = min(bubbleOuterWidth, max(1, width-2))
	contentWidth := max(1, bubbleOuterWidth-4)

	type pendingPlacement struct {
		key       inlineImageKey
		startLine int
		height    int
	}
	type pendingRef struct {
		attachment messages.Attachment
		startLine  int
		height     int
	}

	var contentLines []string
	if text := strings.TrimSpace(message.Text); text != "" {
		contentLines = append(contentLines, strings.Split(ui.Wrap(text, contentWidth), "\n")...)
	}
	var pending []pendingPlacement
	var pendingRefs []pendingRef
	for _, attachment := range message.Attachments {
		if !attachment.IsImage || imageLookup == nil {
			contentLines = append(contentLines, strings.Split(ui.Wrap("📎 "+attachmentLabel(attachment), contentWidth), "\n")...)
			continue
		}
		if imageBlockedLabel != "" {
			contentLines = append(contentLines, strings.Split(ui.Wrap(imageBlockedLabel, contentWidth), "\n")...)
			continue
		}

		imageState := imageLookup(message.ChatID, attachment)
		if imageState == nil || imageState.loading || imageState.err != nil {
			start := len(contentLines)
			label := "Image · " + attachment.Name
			if imageState != nil && imageState.err != nil {
				label = "Image unavailable · " + attachment.Name
			}
			lines := strings.Split(ui.Wrap(label, contentWidth), "\n")
			contentLines = append(contentLines, lines...)
			pendingRefs = append(pendingRefs, pendingRef{attachment: attachment, startLine: start, height: len(lines)})
			continue
		}

		if len(contentLines) > 0 {
			contentLines = append(contentLines, "")
		}
		columns := imageState.rendered.Columns
		rows := max(1, imageState.rendered.Rows)
		start := len(contentLines)
		for row := range rows {
			line := strings.Repeat(" ", columns)
			if imageState.rendered.Protocol == inlineimage.Kitty {
				line = imageState.rendered.PlaceholderRow(row)
			}
			contentLines = append(contentLines, line)
		}
		pending = append(pending, pendingPlacement{
			key:       imageKey(message.ChatID, attachment),
			startLine: start,
			height:    rows,
		})
		pendingRefs = append(pendingRefs, pendingRef{attachment: attachment, startLine: start, height: rows})
	}
	if len(contentLines) == 0 {
		contentLines = []string{"[Unsupported message]"}
	}

	content := strings.Join(contentLines, "\n")
	bubbleWidth := min(bubbleOuterWidth, maxLineWidth(content)+4)
	bubbleLeft := 0
	var lines []string
	if message.IsFromMe {
		bubbleLeft = max(0, width-bubbleWidth)
		bubble := styles.Outgoing.Width(max(1, bubbleWidth-2)).Render(content)
		lines = strings.Split(lipgloss.PlaceHorizontal(width, lipgloss.Right, bubble), "\n")
	} else {
		lines = strings.Split(styles.Incoming.Width(max(1, bubbleWidth-2)).Render(content), "\n")
	}
	placements := make([]inlinePlacement, 0, len(pending))
	for _, placement := range pending {
		placements = append(placements, inlinePlacement{
			key:       placement.key,
			startLine: placement.startLine + 1,
			left:      bubbleLeft + 2,
			height:    placement.height,
		})
	}
	imageRefs := make([]inlineImageRef, 0, len(pendingRefs))
	for _, ref := range pendingRefs {
		imageRefs = append(imageRefs, inlineImageRef{
			attachment: ref.attachment,
			startLine:  ref.startLine + 1,
			height:     ref.height,
		})
	}
	if timestamp := ui.MessageTime(message.SentAt); timestamp != "" {
		timestamp = styles.Timestamp.Render(timestamp)
		if message.IsFromMe {
			timestamp = lipgloss.PlaceHorizontal(width, lipgloss.Right, timestamp+" ")
		} else {
			timestamp = " " + timestamp
		}
		lines = append(lines, timestamp)
	}

	if !message.IsFromMe && !grouped && chat.IsGroup {
		sender := message.SenderName
		if sender == "" {
			sender = message.Sender
		}
		if sender == "" {
			sender = "Unknown sender"
		}
		lines = append([]string{styles.Sender.Render(ui.Truncate(sender, width))}, lines...)
		for i := range placements {
			placements[i].startLine++
		}
		for i := range imageRefs {
			imageRefs[i].startLine++
		}
	}
	return renderedBlock{content: strings.Join(lines, "\n"), placements: placements, imageRefs: imageRefs}
}

func attachmentLabel(attachment messages.Attachment) string {
	label := attachment.Name
	if size := ui.ByteSize(attachment.Size); size != "" {
		label += " · " + size
	}
	return label
}

func maxLineWidth(content string) int {
	width := 0
	for _, line := range strings.Split(content, "\n") {
		width = max(width, lipgloss.Width(line))
	}
	return width
}

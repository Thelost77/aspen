package app

import (
	"strings"
	"time"

	"github.com/Thelost77/aspen/internal/messages"
	"github.com/Thelost77/aspen/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

type renderedConversation struct {
	content    string
	placements []inlinePlacement
}

type renderedBlock struct {
	content    string
	placements []inlinePlacement
}

func renderMessages(all []messages.Message, chat messages.Chat, width int, styles ui.Styles) string {
	return renderMessagesInline(all, chat, width, styles, nil).content
}

func renderMessagesInline(
	all []messages.Message,
	chat messages.Chat,
	width int,
	styles ui.Styles,
	imageLookup func(messages.ChatID, messages.Attachment) *inlineImageState,
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
		blocks = append(blocks, renderMessageInline(message, chat, width, grouped, styles, imageLookup))
		previous = message
	}

	var output strings.Builder
	var placements []inlinePlacement
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
		output.WriteString(block.content)
		lineOffset += renderedLineCount(block.content)
	}
	return renderedConversation{content: output.String(), placements: placements}
}

func renderMessageInline(
	message messages.Message,
	chat messages.Chat,
	width int,
	grouped bool,
	styles ui.Styles,
	imageLookup func(messages.ChatID, messages.Attachment) *inlineImageState,
) renderedBlock {
	bubbleOuterWidth := min(72, max(12, width*3/4))
	bubbleOuterWidth = min(bubbleOuterWidth, max(1, width-2))
	contentWidth := max(1, bubbleOuterWidth-4)

	var textLines []string
	if text := strings.TrimSpace(message.Text); text != "" {
		textLines = append(textLines, strings.Split(ui.Wrap(text, contentWidth), "\n")...)
	}
	var readyImages []struct {
		attachment messages.Attachment
		state      *inlineImageState
	}
	for _, attachment := range message.Attachments {
		if attachment.IsImage && imageLookup != nil {
			imageState := imageLookup(message.ChatID, attachment)
			switch {
			case imageState == nil:
				textLines = append(textLines, strings.Split(ui.Wrap("📎 "+attachmentLabel(attachment), contentWidth), "\n")...)
			case imageState.loading:
				textLines = append(textLines, strings.Split(ui.Wrap("◌ Loading "+attachment.Name+"…", contentWidth), "\n")...)
			case imageState.err != nil:
				textLines = append(textLines, strings.Split(ui.Wrap("Image unavailable · "+attachment.Name, contentWidth), "\n")...)
			default:
				readyImages = append(readyImages, struct {
					attachment messages.Attachment
					state      *inlineImageState
				}{attachment: attachment, state: imageState})
			}
			continue
		}
		textLines = append(textLines, strings.Split(ui.Wrap("📎 "+attachmentLabel(attachment), contentWidth), "\n")...)
	}

	type pendingPlacement struct {
		key       inlineImageKey
		startLine int
		width     int
		height    int
	}
	contentLines := append([]string(nil), textLines...)
	var pending []pendingPlacement
	for _, ready := range readyImages {
		if len(contentLines) > 0 {
			contentLines = append(contentLines, "")
		}
		columns := ready.state.rendered.Columns
		rows := max(1, ready.state.rendered.Rows)
		start := len(contentLines)
		for range rows {
			contentLines = append(contentLines, strings.Repeat(" ", columns))
		}
		pending = append(pending, pendingPlacement{
			key:       imageKey(message.ChatID, ready.attachment),
			startLine: start,
			width:     columns,
			height:    rows,
		})
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
			width:     placement.width,
			height:    placement.height,
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
	}
	return renderedBlock{content: strings.Join(lines, "\n"), placements: placements}
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

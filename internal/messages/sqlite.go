package messages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const conversationsQuery = `
WITH visible AS (
	SELECT
		cmj.chat_id,
		m.ROWID AS message_id,
		m.text,
		m.attributedBody,
		COALESCE(m.date, 0) AS date,
		COALESCE(m.is_from_me, 0) AS is_from_me,
		COALESCE(m.is_read, 0) AS is_read,
		COALESCE(m.item_type, 0) AS item_type,
		COALESCE(m.is_system_message, 0) AS is_system_message,
		COALESCE(m.is_service_message, 0) AS is_service_message,
		COALESCE(m.cache_has_attachments, 0) AS has_attachments,
		ROW_NUMBER() OVER (
			PARTITION BY cmj.chat_id
			ORDER BY COALESCE(m.date, 0) DESC, m.ROWID DESC
		) AS row_number
	FROM chat_message_join cmj
	JOIN message m ON m.ROWID = cmj.message_id
	WHERE COALESCE(m.associated_message_type, 0) = 0
), aggregate_messages AS (
	SELECT
		chat_id,
		MAX(date) AS last_message_date,
		SUM(CASE WHEN is_from_me = 0 AND is_read = 0 THEN 1 ELSE 0 END) AS unread_count
	FROM visible
	GROUP BY chat_id
)
SELECT
	c.ROWID,
	COALESCE(c.guid, ''),
	COALESCE(c.chat_identifier, ''),
	COALESCE(c.display_name, ''),
	COALESCE(c.service_name, ''),
	COALESCE(c.style, 0),
	a.last_message_date,
	COALESCE(a.unread_count, 0),
	COALESCE(v.text, ''),
	v.attributedBody,
	v.item_type,
	v.is_system_message,
	v.is_service_message,
	v.has_attachments
FROM chat c
JOIN aggregate_messages a ON a.chat_id = c.ROWID
JOIN visible v ON v.chat_id = c.ROWID AND v.row_number = 1
ORDER BY a.last_message_date DESC, c.ROWID DESC
LIMIT ?`

func (s *SQLiteStore) ChangeVersion(ctx context.Context) (int64, error) {
	s.changeConnLock.Lock()
	defer s.changeConnLock.Unlock()
	if s.changeConn == nil {
		return 0, classifyDatabaseError("load change version", errors.New("change detector is closed"))
	}
	var version int64
	if err := s.changeConn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&version); err != nil {
		return 0, classifyDatabaseError("load change version", err)
	}
	return version, nil
}

func (s *SQLiteStore) Conversations(ctx context.Context, limit int) ([]Chat, error) {
	if limit <= 0 {
		return []Chat{}, nil
	}
	rows, err := s.db.QueryContext(ctx, conversationsQuery, limit)
	if err != nil {
		return nil, classifyDatabaseError("load conversations", err)
	}
	defer func() { _ = rows.Close() }()

	chats := make([]Chat, 0, limit)
	styles := make(map[ChatID]int)
	for rows.Next() {
		var chat Chat
		var style int
		var appleDate int64
		var latestText string
		var attributedBody []byte
		var itemType, isSystem, isService, hasAttachments int
		if err := rows.Scan(
			&chat.ID,
			&chat.GUID,
			&chat.Identifier,
			&chat.DisplayName,
			&chat.Service,
			&style,
			&appleDate,
			&chat.UnreadCount,
			&latestText,
			&attributedBody,
			&itemType,
			&isSystem,
			&isService,
			&hasAttachments,
		); err != nil {
			return nil, classifyDatabaseError("scan conversations", err)
		}
		chat.LastMessageAt, _ = AppleTime(appleDate)
		chat.LastMessageText = messageDisplayText(latestText, attributedBody, itemType != 0 || isSystem != 0 || isService != 0, hasAttachments != 0)
		styles[chat.ID] = style
		chats = append(chats, chat)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyDatabaseError("load conversations", err)
	}
	if err := s.loadParticipants(ctx, chats); err != nil {
		return nil, err
	}
	for i := range chats {
		chats[i].IsGroup = styles[chats[i].ID] == 43 || len(chats[i].Participants) > 1
	}
	return chats, nil
}

func (s *SQLiteStore) loadParticipants(ctx context.Context, chats []Chat) error {
	if len(chats) == 0 {
		return nil
	}
	placeholders := make([]string, len(chats))
	args := make([]any, len(chats))
	chatIndexes := make(map[ChatID]int, len(chats))
	for i := range chats {
		placeholders[i] = "?"
		args[i] = chats[i].ID
		chatIndexes[chats[i].ID] = i
	}
	query := `
SELECT chj.chat_id, COALESCE(h.id, '')
FROM chat_handle_join chj
JOIN handle h ON h.ROWID = chj.handle_id
WHERE chj.chat_id IN (` + strings.Join(placeholders, ",") + `)
ORDER BY chj.chat_id, h.ROWID`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return classifyDatabaseError("load participants", err)
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[ChatID]map[string]bool)
	for rows.Next() {
		var chatID ChatID
		var handle string
		if err := rows.Scan(&chatID, &handle); err != nil {
			return classifyDatabaseError("scan participants", err)
		}
		index, ok := chatIndexes[chatID]
		if !ok || handle == "" {
			continue
		}
		if seen[chatID] == nil {
			seen[chatID] = make(map[string]bool)
		}
		if !seen[chatID][handle] {
			chats[index].Participants = append(chats[index].Participants, Participant{Handle: handle})
			seen[chatID][handle] = true
		}
	}
	if err := rows.Err(); err != nil {
		return classifyDatabaseError("load participants", err)
	}
	return nil
}

type historyRow struct {
	message        Message
	appleDate      int64
	attributed     []byte
	itemType       int
	isSystem       int
	isService      int
	hasAttachments int
}

func (s *SQLiteStore) History(ctx context.Context, chatID ChatID, limit int, cursor *HistoryCursor) (HistoryPage, error) {
	if chatID <= 0 {
		return HistoryPage{}, fmt.Errorf("load history: invalid chat ID %d", chatID)
	}
	if limit <= 0 {
		return HistoryPage{Messages: []Message{}}, nil
	}

	query := `
SELECT
	m.ROWID,
	COALESCE(m.guid, ''),
	COALESCE(m.text, ''),
	m.attributedBody,
	COALESCE(m.date, 0),
	COALESCE(m.is_from_me, 0),
	COALESCE(m.is_read, 0),
	COALESCE(m.service, ''),
	COALESCE(h.id, ''),
	COALESCE(m.item_type, 0),
	COALESCE(m.is_system_message, 0),
	COALESCE(m.is_service_message, 0),
	COALESCE(m.cache_has_attachments, 0)
FROM chat_message_join cmj
JOIN message m ON m.ROWID = cmj.message_id
LEFT JOIN handle h ON h.ROWID = m.handle_id
WHERE cmj.chat_id = ?
  AND COALESCE(m.associated_message_type, 0) = 0`
	args := []any{chatID}
	if cursor != nil {
		query += `
  AND (COALESCE(m.date, 0) < ? OR (COALESCE(m.date, 0) = ? AND m.ROWID < ?))`
		args = append(args, cursor.Date, cursor.Date, cursor.MessageID)
	}
	query += `
ORDER BY COALESCE(m.date, 0) DESC, m.ROWID DESC
LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return HistoryPage{}, classifyDatabaseError("load history", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]historyRow, 0, limit+1)
	for rows.Next() {
		var row historyRow
		var isFromMe, isRead int
		if err := rows.Scan(
			&row.message.ID,
			&row.message.GUID,
			&row.message.Text,
			&row.attributed,
			&row.appleDate,
			&isFromMe,
			&isRead,
			&row.message.Service,
			&row.message.Sender,
			&row.itemType,
			&row.isSystem,
			&row.isService,
			&row.hasAttachments,
		); err != nil {
			return HistoryPage{}, classifyDatabaseError("scan history", err)
		}
		row.message.ChatID = chatID
		row.message.SentAt, _ = AppleTime(row.appleDate)
		row.message.IsFromMe = isFromMe != 0
		row.message.IsRead = isRead != 0
		row.message.IsSystem = row.itemType != 0 || row.isSystem != 0 || row.isService != 0
		if strings.TrimSpace(row.message.Text) == "" {
			row.message.Text = ExtractAttributedText(row.attributed)
		}
		loaded = append(loaded, row)
	}
	if err := rows.Err(); err != nil {
		return HistoryPage{}, classifyDatabaseError("load history", err)
	}

	hasOlder := len(loaded) > limit
	if hasOlder {
		loaded = loaded[:limit]
	}
	messages := make([]Message, len(loaded))
	for i := range loaded {
		messages[i] = loaded[i].message
	}
	if err := s.loadAttachments(ctx, messages); err != nil {
		return HistoryPage{}, err
	}
	for i := range messages {
		if strings.TrimSpace(messages[i].Text) == "" && len(messages[i].Attachments) == 0 {
			if messages[i].IsSystem {
				messages[i].Text = "[System message]"
			} else {
				messages[i].Text = "[Unsupported message]"
			}
		}
	}

	var older *HistoryCursor
	if hasOlder && len(loaded) > 0 {
		oldest := loaded[len(loaded)-1]
		older = &HistoryCursor{Date: oldest.appleDate, MessageID: oldest.message.ID}
	}
	slices.Reverse(messages)
	return HistoryPage{Messages: messages, Older: older}, nil
}

func (s *SQLiteStore) loadAttachments(ctx context.Context, messages []Message) error {
	if len(messages) == 0 {
		return nil
	}
	placeholders := make([]string, len(messages))
	args := make([]any, len(messages))
	indexes := make(map[MessageID]int, len(messages))
	for i := range messages {
		placeholders[i] = "?"
		args[i] = messages[i].ID
		indexes[messages[i].ID] = i
	}
	query := `
SELECT
	maj.message_id,
	a.ROWID,
	COALESCE(a.filename, ''),
	COALESCE(a.mime_type, ''),
	COALESCE(a.uti, ''),
	COALESCE(a.total_bytes, 0)
FROM message_attachment_join maj
JOIN attachment a ON a.ROWID = maj.attachment_id
WHERE maj.message_id IN (` + strings.Join(placeholders, ",") + `)
ORDER BY maj.message_id, a.ROWID`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return classifyDatabaseError("load attachments", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var messageID MessageID
		var attachment Attachment
		if err := rows.Scan(&messageID, &attachment.ID, &attachment.Path, &attachment.MIMEType, &attachment.UTI, &attachment.Size); err != nil {
			return classifyDatabaseError("scan attachments", err)
		}
		index, ok := indexes[messageID]
		if !ok {
			continue
		}
		attachment.Path = expandAttachmentPath(attachment.Path)
		if attachment.Path != "" {
			attachment.Name = filepath.Base(attachment.Path)
		}
		if attachment.Name == "" || attachment.Name == "." {
			attachment.Name = "Attachment"
		}
		attachment.IsImage = strings.HasPrefix(strings.ToLower(attachment.MIMEType), "image/") || imageUTIs[strings.ToLower(attachment.UTI)]
		messages[index].Attachments = append(messages[index].Attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return classifyDatabaseError("load attachments", err)
	}
	return nil
}

func messageDisplayText(text string, attributed []byte, system, hasAttachments bool) string {
	if strings.TrimSpace(text) != "" {
		return text
	}
	if extracted := ExtractAttributedText(attributed); extracted != "" {
		return extracted
	}
	if hasAttachments {
		return "[Attachment]"
	}
	if system {
		return "[System message]"
	}
	return "[Unsupported message]"
}

func expandAttachmentPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

var imageUTIs = map[string]bool{
	"public.image":       true,
	"public.jpeg":        true,
	"public.png":         true,
	"public.heic":        true,
	"public.heif":        true,
	"public.gif":         true,
	"public.tiff":        true,
	"public.bmp":         true,
	"public.webp":        true,
	"public.svg-image":   true,
	"com.compuserve.gif": true,
}

var _ Store = (*SQLiteStore)(nil)

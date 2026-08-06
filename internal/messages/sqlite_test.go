package messages

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func createFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chat.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close fixture: %v", err)
		}
	}()

	schema := []string{
		`CREATE TABLE chat (
			ROWID INTEGER PRIMARY KEY,
			guid TEXT,
			style INTEGER,
			chat_identifier TEXT,
			service_name TEXT,
			display_name TEXT
		)`,
		`CREATE TABLE handle (
			ROWID INTEGER PRIMARY KEY,
			id TEXT,
			service TEXT
		)`,
		`CREATE TABLE message (
			ROWID INTEGER PRIMARY KEY,
			guid TEXT,
			text TEXT,
			attributedBody BLOB,
			date INTEGER,
			is_from_me INTEGER DEFAULT 0,
			is_read INTEGER DEFAULT 0,
			service TEXT,
			handle_id INTEGER,
			associated_message_type INTEGER DEFAULT 0,
			item_type INTEGER DEFAULT 0,
			is_system_message INTEGER DEFAULT 0,
			is_service_message INTEGER DEFAULT 0,
			cache_has_attachments INTEGER DEFAULT 0
		)`,
		`CREATE TABLE chat_message_join (chat_id INTEGER, message_id INTEGER)`,
		`CREATE TABLE chat_handle_join (chat_id INTEGER, handle_id INTEGER)`,
		`CREATE TABLE attachment (
			ROWID INTEGER PRIMARY KEY,
			filename TEXT,
			mime_type TEXT,
			uti TEXT,
			total_bytes INTEGER
		)`,
		`CREATE TABLE message_attachment_join (message_id INTEGER, attachment_id INTEGER)`,
	}
	for _, statement := range schema {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create fixture schema: %v", err)
		}
	}

	statements := []string{
		`INSERT INTO chat VALUES (1, 'iMessage;+;fixture-direct', 45, '+15550000001', 'iMessage', '')`,
		`INSERT INTO chat VALUES (2, 'SMS;-;fixture-sms', 45, '+15550000002', 'SMS', '')`,
		`INSERT INTO chat VALUES (3, 'iMessage;+;fixture-group', 43, 'fixture-group', 'iMessage', 'Fixture Group')`,
		`INSERT INTO handle VALUES (1, '+15550000001', 'iMessage')`,
		`INSERT INTO handle VALUES (2, '+15550000002', 'SMS')`,
		`INSERT INTO handle VALUES (3, '+15550000003', 'iMessage')`,
		`INSERT INTO handle VALUES (4, '+15550000004', 'iMessage')`,
		`INSERT INTO handle VALUES (5, '+15550000005', 'iMessage')`,
		`INSERT INTO chat_handle_join VALUES (1, 1)`,
		`INSERT INTO chat_handle_join VALUES (2, 2)`,
		`INSERT INTO chat_handle_join VALUES (3, 3)`,
		`INSERT INTO chat_handle_join VALUES (3, 4)`,
		`INSERT INTO chat_handle_join VALUES (3, 5)`,
		`INSERT INTO message VALUES (101, 'msg-101', 'plain', NULL, 700000000000000100, 0, 0, 'iMessage', 1, 0, 0, 0, 0, 0)`,
		`INSERT INTO message VALUES (102, 'msg-102', '', X'73747265616D7479706564204E53537472696E6701556E69636F646520E29C93004E5344696374696F6E617279', 700000000000000200, 1, 1, 'iMessage', NULL, 0, 0, 0, 0, 0)`,
		`INSERT INTO message VALUES (105, 'msg-105', 'same time', NULL, 700000000000000200, 0, 0, 'iMessage', 1, 0, 0, 0, 0, 0)`,
		`INSERT INTO message VALUES (103, 'reaction-103', 'Loved “plain”', NULL, 700000000000000500, 0, 1, 'iMessage', 1, 2000, 0, 0, 0, 0)`,
		`INSERT INTO message VALUES (104, 'msg-104', '', NULL, 700000000000000400, 0, 0, 'iMessage', 1, 0, 0, 0, 0, 1)`,
		`INSERT INTO message VALUES (201, 'msg-201', 'sms', NULL, 700000000000000300, 1, 1, 'SMS', NULL, 0, 0, 0, 0, 0)`,
		`INSERT INTO message VALUES (301, 'msg-301', 'group one\nline two', NULL, 700000000000000300, 0, 0, 'iMessage', 3, 0, 0, 0, 0, 0)`,
		`INSERT INTO message VALUES (302, 'msg-302', '', NULL, 700000000000000250, 0, 0, 'iMessage', 4, 0, 4, 0, 0, 0)`,
		`INSERT INTO chat_message_join VALUES (1, 101)`,
		`INSERT INTO chat_message_join VALUES (1, 102)`,
		`INSERT INTO chat_message_join VALUES (1, 103)`,
		`INSERT INTO chat_message_join VALUES (1, 104)`,
		`INSERT INTO chat_message_join VALUES (1, 105)`,
		`INSERT INTO chat_message_join VALUES (2, 201)`,
		`INSERT INTO chat_message_join VALUES (3, 301)`,
		`INSERT INTO chat_message_join VALUES (3, 302)`,
		`INSERT INTO attachment VALUES (1, '~/Library/Messages/Attachments/fixture/photo.heic', 'image/heic', 'public.heic', 2048)`,
		`INSERT INTO message_attachment_join VALUES (104, 1)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("populate fixture: %v", err)
		}
	}
	return path
}

func TestOpenRejectsMissingAndUnsupportedDatabase(t *testing.T) {
	t.Parallel()

	if _, err := Open(filepath.Join(t.TempDir(), "missing.db")); !IsDatabaseError(err, ErrorNotFound) {
		t.Fatalf("missing database error = %v", err)
	}

	path := createFixture(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE message DROP COLUMN service`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !IsDatabaseError(err, ErrorUnsupported) {
		t.Fatalf("unsupported database error = %v", err)
	}
}

func TestConversations(t *testing.T) {
	t.Parallel()
	store, err := Open(createFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()

	chats, err := store.Conversations(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 3 {
		t.Fatalf("got %d chats, want 3", len(chats))
	}
	if chats[0].ID != 1 || chats[0].LastMessageText != "[Attachment]" {
		t.Fatalf("first chat = %#v", chats[0])
	}
	if chats[0].UnreadCount != 3 {
		t.Fatalf("direct unread = %d, want 3", chats[0].UnreadCount)
	}
	if chats[1].ID != 3 || chats[2].ID != 2 {
		t.Fatalf("tie order IDs = %d, %d; want 3, 2", chats[1].ID, chats[2].ID)
	}
	if !chats[1].IsGroup || len(chats[1].Participants) != 3 || chats[1].UnreadCount != 2 {
		t.Fatalf("group chat = %#v", chats[1])
	}
	if chats[2].Service != "SMS" || chats[2].IsGroup {
		t.Fatalf("SMS chat = %#v", chats[2])
	}
}

func TestHistoryPaginationAndAttachments(t *testing.T) {
	t.Parallel()
	store, err := Open(createFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()

	first, err := store.History(context.Background(), 1, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Older == nil {
		t.Fatal("first page has no older cursor")
	}
	if got := []MessageID{first.Messages[0].ID, first.Messages[1].ID}; got[0] != 105 || got[1] != 104 {
		t.Fatalf("first page IDs = %v, want [105 104]", got)
	}
	attachment := first.Messages[1].Attachments
	if len(attachment) != 1 || attachment[0].Name != "photo.heic" || !attachment[0].IsImage || attachment[0].Size != 2048 {
		t.Fatalf("attachment = %#v", attachment)
	}

	second, err := store.History(context.Background(), 1, 2, first.Older)
	if err != nil {
		t.Fatal(err)
	}
	if second.Older != nil {
		t.Fatalf("second page older cursor = %#v, want nil", second.Older)
	}
	if got := []MessageID{second.Messages[0].ID, second.Messages[1].ID}; got[0] != 101 || got[1] != 102 {
		t.Fatalf("second page IDs = %v, want [101 102]", got)
	}
	if second.Messages[1].Text != "Unicode ✓" {
		t.Fatalf("attributed text = %q", second.Messages[1].Text)
	}
	for _, page := range []HistoryPage{first, second} {
		for _, message := range page.Messages {
			if message.ID == 103 {
				t.Fatal("reaction row was not filtered")
			}
		}
	}
}

func TestHistorySystemAndUnsupportedRows(t *testing.T) {
	t.Parallel()
	store, err := Open(createFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()

	page, err := store.History(context.Background(), 3, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 2 || page.Messages[0].Text != "[System message]" || !page.Messages[0].IsSystem {
		t.Fatalf("group history = %#v", page.Messages)
	}
}

func TestStoreConnectionIsReadOnly(t *testing.T) {
	t.Parallel()
	path := createFixture(t)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO chat (ROWID) VALUES (99)`); err == nil {
		t.Fatal("write succeeded through read-only connection")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() {
		t.Fatalf("database size changed from %d to %d", before.Size(), after.Size())
	}
}

func TestHistoryRejectsInvalidChat(t *testing.T) {
	t.Parallel()
	store, err := Open(createFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()
	_, err = store.History(context.Background(), 0, 10, nil)
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("invalid chat error = %v", err)
	}
}

package messages

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestActualDatabaseReadOnlySmoke(t *testing.T) {
	path := os.Getenv("ASPEN_INTEGRATION_DB")
	if path == "" {
		t.Skip("set ASPEN_INTEGRATION_DB to run the read-only Messages database smoke test")
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	chats, err := store.Conversations(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) > 1 {
		t.Fatalf("conversation limit ignored: got %d", len(chats))
	}
	if len(chats) == 1 {
		page, err := store.History(ctx, chats[0].ID, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Messages) > 1 {
			t.Fatalf("history limit ignored: got %d", len(page.Messages))
		}
	}
}

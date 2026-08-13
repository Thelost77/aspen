package messages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type ErrorKind string

const (
	ErrorNotFound    ErrorKind = "not_found"
	ErrorPermission  ErrorKind = "permission_denied"
	ErrorUnsupported ErrorKind = "unsupported_schema"
	ErrorBusy        ErrorKind = "busy"
	ErrorCorrupt     ErrorKind = "corrupt"
	ErrorUnknown     ErrorKind = "unknown"
)

type DatabaseError struct {
	Kind ErrorKind
	Op   string
	Err  error
}

func (e *DatabaseError) Error() string {
	return fmt.Sprintf("Messages database %s: %v", e.Op, e.Err)
}

func (e *DatabaseError) Unwrap() error { return e.Err }

func IsDatabaseError(err error, kind ErrorKind) bool {
	var dbErr *DatabaseError
	return errors.As(err, &dbErr) && dbErr.Kind == kind
}

type SQLiteStore struct {
	db             *sql.DB
	changeConn     *sql.Conn
	changeConnLock sync.Mutex
}

func DefaultDatabasePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/Library/Messages/chat.db"
	}
	return filepath.Join(home, "Library", "Messages", "chat.db")
}

func Open(path string) (*SQLiteStore, error) {
	expanded, err := expandPath(path)
	if err != nil {
		return nil, &DatabaseError{Kind: ErrorUnknown, Op: "resolve path", Err: err}
	}
	if _, err := os.Stat(expanded); err != nil {
		return nil, classifyDatabaseError("open", err)
	}

	u := &url.URL{Scheme: "file", Path: expanded}
	query := u.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	query.Add("_pragma", "busy_timeout(3000)")
	u.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, classifyDatabaseError("open", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(3)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, classifyDatabaseError("connect", err)
	}

	changeConn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, classifyDatabaseError("open change detector", err)
	}
	store := &SQLiteStore{db: db, changeConn: changeConn}
	if err := store.validate(ctx); err != nil {
		_ = changeConn.Close()
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.changeConnLock.Lock()
	var changeConnErr error
	if s.changeConn != nil {
		changeConnErr = s.changeConn.Close()
		s.changeConn = nil
	}
	s.changeConnLock.Unlock()
	return errors.Join(changeConnErr, s.db.Close())
}

var requiredSchema = map[string][]string{
	"attachment":              {"ROWID", "filename", "mime_type", "uti", "total_bytes"},
	"chat":                    {"ROWID", "guid", "style", "chat_identifier", "service_name", "display_name"},
	"chat_handle_join":        {"chat_id", "handle_id"},
	"chat_message_join":       {"chat_id", "message_id"},
	"handle":                  {"ROWID", "id"},
	"message":                 {"ROWID", "guid", "text", "attributedBody", "date", "is_from_me", "is_read", "service", "handle_id", "associated_message_type", "item_type", "is_system_message", "is_service_message", "cache_has_attachments"},
	"message_attachment_join": {"message_id", "attachment_id"},
}

func (s *SQLiteStore) validate(ctx context.Context) error {
	for table, columns := range requiredSchema {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&exists); err != nil {
			return classifyDatabaseError("validate schema", err)
		}
		if exists == 0 {
			return &DatabaseError{Kind: ErrorUnsupported, Op: "validate schema", Err: fmt.Errorf("required table %q is missing", table)}
		}

		rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			return classifyDatabaseError("validate schema", err)
		}
		present := make(map[string]bool)
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				_ = rows.Close()
				return classifyDatabaseError("validate schema", err)
			}
			present[name] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return classifyDatabaseError("validate schema", err)
		}
		_ = rows.Close()
		for _, column := range columns {
			if !present[column] {
				return &DatabaseError{Kind: ErrorUnsupported, Op: "validate schema", Err: fmt.Errorf("required column %q.%q is missing", table, column)}
			}
		}
	}

	var queryOnly int
	if err := s.db.QueryRowContext(ctx, `PRAGMA query_only`).Scan(&queryOnly); err != nil {
		return classifyDatabaseError("verify read-only mode", err)
	}
	if queryOnly != 1 {
		return &DatabaseError{Kind: ErrorUnknown, Op: "verify read-only mode", Err: errors.New("query_only is disabled")}
	}
	return nil
}

func expandPath(path string) (string, error) {
	if path == "" {
		path = DefaultDatabasePath()
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return filepath.Abs(path)
}

func classifyDatabaseError(op string, err error) error {
	kind := ErrorUnknown
	switch {
	case errors.Is(err, os.ErrNotExist):
		kind = ErrorNotFound
	case errors.Is(err, os.ErrPermission):
		kind = ErrorPermission
	default:
		text := strings.ToLower(err.Error())
		switch {
		case strings.Contains(text, "permission denied"), strings.Contains(text, "authorization denied"), strings.Contains(text, "not authorized"):
			kind = ErrorPermission
		case strings.Contains(text, "database is locked"), strings.Contains(text, "database is busy"):
			kind = ErrorBusy
		case strings.Contains(text, "malformed"), strings.Contains(text, "not a database"), strings.Contains(text, "database disk image is malformed"):
			kind = ErrorCorrupt
		}
	}
	return &DatabaseError{Kind: kind, Op: op, Err: err}
}

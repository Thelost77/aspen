package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesPrivateLogAndHonorsLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "aspen.log")
	cleanup, err := InitAt(path, false)
	if err != nil {
		t.Fatal(err)
	}
	Debug("debug-hidden", "operation", "fixture")
	Info("session-safe", "count", 2)
	cleanup()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "debug-hidden") || !strings.Contains(string(content), "session-safe") {
		t.Fatalf("unexpected log content: %q", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("log mode = %o", info.Mode().Perm())
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if directory.Mode().Perm() != 0o700 {
		t.Fatalf("log directory mode = %o", directory.Mode().Perm())
	}
}

func TestInitRotatesLargeLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aspen", "aspen.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxLogSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	cleanup, err := InitAt(path, true)
	if err != nil {
		t.Fatal(err)
	}
	Debug("new-log")
	cleanup()
	if _, err := os.Stat(path + ".old"); err != nil {
		t.Fatalf("rotated log missing: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "new-log") {
		t.Fatalf("new log content = %q", content)
	}
}

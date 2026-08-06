package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const maxLogSize int64 = 5 * 1024 * 1024

var (
	mutex   sync.RWMutex
	current = slog.New(slog.NewTextHandler(io.Discard, nil))
)

func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "aspen", "aspen.log")
	}
	return filepath.Join(home, ".config", "aspen", "aspen.log")
}

func Init(debug bool) (func(), error) {
	return InitAt(DefaultPath(), debug)
}

func InitAt(path string, debug bool) (func(), error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return func() {}, fmt.Errorf("create log directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return func() {}, fmt.Errorf("secure log directory: %w", err)
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		_ = os.Remove(path + ".old")
		if err := os.Rename(path, path+".old"); err != nil {
			return func() {}, fmt.Errorf("rotate log: %w", err)
		}
		_ = os.Chmod(path+".old", 0o600)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return func() {}, fmt.Errorf("open log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return func() {}, fmt.Errorf("secure log: %w", err)
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	instance := slog.New(slog.NewTextHandler(file, &slog.HandlerOptions{Level: level}))
	mutex.Lock()
	current = instance
	mutex.Unlock()

	return func() {
		_ = file.Sync()
		_ = file.Close()
		mutex.Lock()
		if current == instance {
			current = slog.New(slog.NewTextHandler(io.Discard, nil))
		}
		mutex.Unlock()
	}, nil
}

func Get() *slog.Logger {
	mutex.RLock()
	defer mutex.RUnlock()
	return current
}

func Debug(message string, args ...any) { Get().Debug(message, args...) }
func Info(message string, args ...any)  { Get().Info(message, args...) }
func Warn(message string, args ...any)  { Get().Warn(message, args...) }
func Error(message string, args ...any) { Get().Error(message, args...) }

func Session() {
	Info("aspen session started", "pid", os.Getpid())
}

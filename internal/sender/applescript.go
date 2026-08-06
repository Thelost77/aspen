package sender

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	osascriptPath = "/usr/bin/osascript"
	outputLimit   = 64 * 1024
)

const sendScript = `on run argv
	if (count of argv) is not 2 then error "ASPEN_INVALID_ARGUMENTS"
	set targetChatID to item 1 of argv
	set messageText to item 2 of argv
	tell application "Messages"
		set matchingChats to every chat whose id is targetChatID
		if (count of matchingChats) is 0 then error "ASPEN_CHAT_NOT_FOUND"
		send messageText to item 1 of matchingChats
	end tell
end run
`

type processRunner interface {
	Run(context.Context, string, []string, string) ([]byte, error)
}

type AppleScriptSender struct {
	runner  processRunner
	timeout time.Duration
}

func NewAppleScriptSender() *AppleScriptSender {
	return &AppleScriptSender{runner: execRunner{}, timeout: 20 * time.Second}
}

func (s *AppleScriptSender) Send(ctx context.Context, target SendTarget, text string) error {
	if target.ChatID <= 0 || strings.TrimSpace(target.GUID) == "" {
		return &SendError{Kind: ErrorInvalidTarget, Err: errors.New("selected conversation has no stable Messages chat identifier")}
	}
	if strings.TrimSpace(text) == "" {
		return &SendError{Kind: ErrorEmptyMessage, Err: errors.New("message is empty")}
	}
	if target.IsGroup {
		return &SendError{Kind: ErrorUnsupported, Err: errors.New("group sending is not enabled because exact targeting has not completed manual verification")}
	}
	if !strings.EqualFold(target.Service, "iMessage") && !strings.EqualFold(target.Service, "SMS") {
		return &SendError{Kind: ErrorUnsupported, Err: fmt.Errorf("sending through %s is not verified", safeServiceName(target.Service))}
	}

	timeout := s.timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := s.runner.Run(runCtx, osascriptPath, []string{"-", target.GUID, text}, sendScript)
	if err == nil {
		return nil
	}
	return classifySendError(runCtx, output, err)
}

func classifySendError(ctx context.Context, output []byte, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &SendError{Kind: ErrorTimeout, Err: errors.New("the Messages app did not respond before the send timed out")}
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return &SendError{Kind: ErrorProcess, Err: context.Canceled}
	}

	lower := strings.ToLower(string(output))
	switch {
	case strings.Contains(lower, "aspen_chat_not_found"):
		return &SendError{Kind: ErrorNotFound, Err: errors.New("selected conversation is no longer available in Messages")}
	case strings.Contains(lower, "-1743"), strings.Contains(lower, "not authorized to send apple events"), strings.Contains(lower, "not permitted to send apple events"), strings.Contains(lower, "automation") && strings.Contains(lower, "denied"):
		return &SendError{Kind: ErrorPermission, Err: errors.New("automation permission for Messages is denied")}
	case strings.Contains(lower, "application isn’t running"), strings.Contains(lower, "application isn't running"), strings.Contains(lower, "connection is invalid"), strings.Contains(lower, "messages got an error") && strings.Contains(lower, "not running"):
		return &SendError{Kind: ErrorUnavailable, Err: errors.New("the Messages app is not configured or responding")}
	case strings.Contains(lower, "recipient is unavailable"), strings.Contains(lower, "not registered with imessage"), strings.Contains(lower, "not available to receive messages"):
		return &SendError{Kind: ErrorRecipient, Err: errors.New("the recipient is unavailable in Messages")}
	case len(output) > 0:
		return &SendError{Kind: ErrorScript, Err: errors.New("the Messages app rejected the AppleScript send command")}
	default:
		return &SendError{Kind: ErrorProcess, Err: fmt.Errorf("cannot run osascript: %w", err)}
	}
}

func safeServiceName(service string) string {
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "":
		return "an unknown service"
	case "rcs":
		return "RCS"
	default:
		return "this service"
	}
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, executable string, args []string, stdin string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdin = strings.NewReader(stdin)
	var output limitedBuffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.Bytes(), err
}

type limitedBuffer struct {
	buffer bytes.Buffer
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	remaining := outputLimit - b.buffer.Len()
	if remaining > 0 {
		_, _ = b.buffer.Write(data[:min(len(data), remaining)])
	}
	return len(data), nil
}

func (b *limitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

var _ Sender = (*AppleScriptSender)(nil)

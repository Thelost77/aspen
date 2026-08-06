package sender

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	executable string
	args       []string
	stdin      string
	output     []byte
	err        error
	calls      int
	wait       bool
}

func (r *fakeRunner) Run(ctx context.Context, executable string, args []string, stdin string) ([]byte, error) {
	r.calls++
	r.executable = executable
	r.args = append([]string(nil), args...)
	r.stdin = stdin
	if r.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return r.output, r.err
}

func validTarget() SendTarget {
	return SendTarget{ChatID: 42, GUID: "iMessage;+;fixture-guid", Service: "iMessage"}
}

func TestAppleScriptSenderPassesChatAndTextAsArguments(t *testing.T) {
	runner := &fakeRunner{}
	sender := &AppleScriptSender{runner: runner, timeout: time.Second}
	message := "quotes \" and slash \\ plus\nUnicode 世界"
	target := validTarget()

	if err := sender.Send(context.Background(), target, message); err != nil {
		t.Fatal(err)
	}
	if runner.executable != "/usr/bin/osascript" {
		t.Fatalf("executable = %q", runner.executable)
	}
	wantArgs := []string{"-", target.GUID, message}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", runner.args, wantArgs)
	}
	if strings.Contains(runner.stdin, target.GUID) || strings.Contains(runner.stdin, message) {
		t.Fatal("recipient or message was interpolated into AppleScript source")
	}
	if !strings.Contains(runner.stdin, "every chat whose id is targetChatID") || !strings.Contains(runner.stdin, "send messageText") {
		t.Fatalf("script does not target exact chat by argv variable:\n%s", runner.stdin)
	}
}

func TestAppleScriptSenderRejectsUnsupportedTargetBeforeProcess(t *testing.T) {
	tests := []struct {
		name   string
		target SendTarget
		text   string
		kind   ErrorKind
	}{
		{name: "missing ID", target: SendTarget{GUID: "guid", Service: "iMessage"}, text: "hello", kind: ErrorInvalidTarget},
		{name: "missing GUID", target: SendTarget{ChatID: 1, Service: "iMessage"}, text: "hello", kind: ErrorInvalidTarget},
		{name: "empty message", target: validTarget(), text: " \n ", kind: ErrorEmptyMessage},
		{name: "group", target: SendTarget{ChatID: 1, GUID: "group", Service: "iMessage", IsGroup: true}, text: "hello", kind: ErrorUnsupported},
		{name: "RCS", target: SendTarget{ChatID: 1, GUID: "rcs", Service: "RCS"}, text: "hello", kind: ErrorUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			sender := &AppleScriptSender{runner: runner, timeout: time.Second}
			err := sender.Send(context.Background(), tt.target, tt.text)
			if !IsError(err, tt.kind) {
				t.Fatalf("error = %v, want kind %s", err, tt.kind)
			}
			if runner.calls != 0 {
				t.Fatalf("runner called %d times", runner.calls)
			}
		})
	}
}

func TestAppleScriptSenderClassifiesFailuresWithoutLeakingOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		kind   ErrorKind
	}{
		{name: "permission", output: "execution error: Not authorized to send Apple events to Messages. (-1743)", kind: ErrorPermission},
		{name: "missing chat", output: "execution error: ASPEN_CHAT_NOT_FOUND (-2700)", kind: ErrorNotFound},
		{name: "unavailable", output: "Messages got an error: application isn't running", kind: ErrorUnavailable},
		{name: "recipient", output: "The recipient is unavailable", kind: ErrorRecipient},
		{name: "script", output: "private fixture recipient and private fixture body", kind: ErrorScript},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{output: []byte(tt.output), err: errors.New("exit status 1")}
			sender := &AppleScriptSender{runner: runner, timeout: time.Second}
			err := sender.Send(context.Background(), validTarget(), "hello")
			if !IsError(err, tt.kind) {
				t.Fatalf("error = %v, want kind %s", err, tt.kind)
			}
			if strings.Contains(err.Error(), tt.output) {
				t.Fatalf("error leaked process output: %v", err)
			}
		})
	}
}

func TestAppleScriptSenderTimeout(t *testing.T) {
	runner := &fakeRunner{wait: true}
	sender := &AppleScriptSender{runner: runner, timeout: 5 * time.Millisecond}
	err := sender.Send(context.Background(), validTarget(), "hello")
	if !IsError(err, ErrorTimeout) {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestLimitedBufferCapsCapturedOutput(t *testing.T) {
	var buffer limitedBuffer
	input := []byte(strings.Repeat("x", outputLimit*2))
	written, err := buffer.Write(input)
	if err != nil || written != len(input) {
		t.Fatalf("Write() = %d, %v", written, err)
	}
	if len(buffer.Bytes()) != outputLimit {
		t.Fatalf("captured bytes = %d, want %d", len(buffer.Bytes()), outputLimit)
	}
}

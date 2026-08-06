package sender

import (
	"context"
	"errors"
	"fmt"

	"github.com/Thelost77/aspen/internal/messages"
)

type SendTarget struct {
	ChatID  messages.ChatID
	GUID    string
	Service string
	IsGroup bool
}

type Sender interface {
	Send(context.Context, SendTarget, string) error
}

type ErrorKind string

const (
	ErrorInvalidTarget ErrorKind = "invalid_target"
	ErrorEmptyMessage  ErrorKind = "empty_message"
	ErrorUnsupported   ErrorKind = "unsupported_target"
	ErrorPermission    ErrorKind = "automation_permission"
	ErrorUnavailable   ErrorKind = "messages_unavailable"
	ErrorRecipient     ErrorKind = "recipient_unavailable"
	ErrorNotFound      ErrorKind = "chat_not_found"
	ErrorTimeout       ErrorKind = "timeout"
	ErrorScript        ErrorKind = "script_error"
	ErrorProcess       ErrorKind = "process_error"
)

type SendError struct {
	Kind ErrorKind
	Err  error
}

func (e *SendError) Error() string {
	return fmt.Sprintf("send message: %v", e.Err)
}

func (e *SendError) Unwrap() error { return e.Err }

func IsError(err error, kind ErrorKind) bool {
	var sendErr *SendError
	return errors.As(err, &sendErr) && sendErr.Kind == kind
}

package inlineimage

import (
	"io"
	"sync"
)

// TerminalOutput serializes image protocol operations with Bubble Tea renderer
// writes. Operations queued before a frame upload image data before placeholders
// are drawn. Operations queued after a frame place cursor-addressed images over
// the completed text frame.
type TerminalOutput interface {
	io.Writer
	QueueBeforeFrame(string)
	QueueAfterFrame(string)
	ClearAfterFrame()
	Flush() error
}

type terminalOutput struct {
	mu     sync.Mutex
	writer io.Writer
	before []string
	after  []string
}

type terminalFileOutput struct {
	*terminalOutput
	file interface {
		io.ReadWriteCloser
		Fd() uintptr
	}
}

func NewTerminalOutput(writer io.Writer) TerminalOutput {
	output := &terminalOutput{writer: writer}
	if file, ok := writer.(interface {
		io.ReadWriteCloser
		Fd() uintptr
	}); ok {
		return &terminalFileOutput{terminalOutput: output, file: file}
	}
	return output
}

func (o *terminalOutput) QueueBeforeFrame(sequence string) {
	if sequence == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.before = append(o.before, sequence)
}

func (o *terminalOutput) QueueAfterFrame(sequence string) {
	if sequence == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.after = append(o.after, sequence)
}

func (o *terminalOutput) ClearAfterFrame() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.after = nil
}

func (o *terminalOutput) Write(frame []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := writeSequences(o.writer, o.before); err != nil {
		return 0, err
	}
	o.before = nil

	written, err := o.writer.Write(frame)
	if err != nil {
		return written, err
	}
	if written != len(frame) {
		return written, io.ErrShortWrite
	}
	if err := writeSequences(o.writer, o.after); err != nil {
		return written, err
	}
	o.after = nil
	return written, nil
}

func (o *terminalOutput) Flush() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := writeSequences(o.writer, o.before); err != nil {
		return err
	}
	o.before = nil
	if err := writeSequences(o.writer, o.after); err != nil {
		return err
	}
	o.after = nil
	return nil
}

func writeSequences(writer io.Writer, sequences []string) error {
	for _, sequence := range sequences {
		written, err := io.WriteString(writer, sequence)
		if err != nil {
			return err
		}
		if written != len(sequence) {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (o *terminalFileOutput) Read(buffer []byte) (int, error) {
	return o.file.Read(buffer)
}

func (o *terminalFileOutput) Close() error {
	return nil
}

func (o *terminalFileOutput) Fd() uintptr {
	return o.file.Fd()
}

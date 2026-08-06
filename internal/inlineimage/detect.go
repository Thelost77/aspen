package inlineimage

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

const terminalProbeTimeout = 500 * time.Millisecond

func SelectProtocol(value string, detector func() Protocol) (Protocol, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "":
		return detector(), nil
	case "kitty", "ghostty":
		return Kitty, nil
	case "iterm2", "iterm":
		return ITerm2, nil
	case "off", "none":
		return Unsupported, nil
	default:
		return Unsupported, fmt.Errorf("unknown graphics protocol %q (use auto, kitty, iterm2, or off)", value)
	}
}

func (p Protocol) String() string {
	switch p {
	case Kitty:
		return "kitty"
	case ITerm2:
		return "iterm2"
	default:
		return "off"
	}
}

func DetectTerminal() Protocol {
	if protocol := Detect(os.Getenv); protocol != Unsupported {
		return protocol
	}
	return protocolOrSSHFallback(probeTerminal("/dev/tty", terminalProbeTimeout), os.Getenv)
}

func protocolOrSSHFallback(detected Protocol, getenv func(string) string) Protocol {
	if detected != Unsupported {
		return detected
	}
	if getenv("SSH_TTY") != "" {
		return Kitty
	}
	return Unsupported
}

func probeTerminal(path string, timeout time.Duration) Protocol {
	tty, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return Unsupported
	}
	defer func() { _ = tty.Close() }()
	if !term.IsTerminal(tty.Fd()) {
		return Unsupported
	}
	oldState, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return Unsupported
	}
	defer func() { _ = term.Restore(tty.Fd(), oldState) }()

	queries := []struct {
		protocol Protocol
		request  string
	}{
		{protocol: Kitty, request: "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"},
		{protocol: ITerm2, request: "\x1b]1337;ReportCellSize\x07"},
	}
	for _, query := range queries {
		if _, err := tty.WriteString(query.request); err != nil {
			return Unsupported
		}
		response, err := readTerminalResponse(tty, timeout)
		if err == nil && responseMatches(query.protocol, response) {
			return query.protocol
		}
	}
	return Unsupported
}

func readTerminalResponse(tty *os.File, timeout time.Duration) (string, error) {
	poll := []unix.PollFd{{Fd: int32(tty.Fd()), Events: unix.POLLIN}}
	ready, err := unix.Poll(poll, int(timeout.Milliseconds()))
	if err != nil {
		return "", err
	}
	if ready == 0 || poll[0].Revents&unix.POLLIN == 0 {
		return "", errors.New("terminal capability query timed out")
	}
	buffer := make([]byte, 1024)
	count, err := tty.Read(buffer)
	if err != nil {
		return "", err
	}
	return string(buffer[:count]), nil
}

func responseMatches(protocol Protocol, response string) bool {
	switch protocol {
	case Kitty:
		return strings.Contains(response, "i=31") && strings.Contains(response, "OK")
	case ITerm2:
		return strings.Contains(response, "1337") && strings.Contains(response, "ReportCellSize=")
	default:
		return false
	}
}

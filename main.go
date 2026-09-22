package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/Thelost77/aspen/internal/app"
	"github.com/Thelost77/aspen/internal/contacts"
	"github.com/Thelost77/aspen/internal/inlineimage"
	"github.com/Thelost77/aspen/internal/logger"
	"github.com/Thelost77/aspen/internal/messages"
	"github.com/Thelost77/aspen/internal/sender"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "aspen:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("macOS is required")
	}

	flags := flag.NewFlagSet("aspen", flag.ContinueOnError)
	databasePath := flags.String("db", messages.DefaultDatabasePath(), "Messages chat.db path")
	graphics := flags.String("graphics", "auto", "inline graphics: auto, kitty, iterm2, or off")
	debugLogging := flags.Bool("debug", false, "enable debug operation logging")
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *showVersion {
		_, _ = fmt.Fprintln(os.Stdout, version())
		return nil
	}

	selectionSource := "explicit"
	protocol, err := inlineimage.SelectProtocol(*graphics, func() inlineimage.Protocol {
		detected, source := inlineimage.DetectTerminalWithSource()
		selectionSource = source
		return detected
	})
	if err != nil {
		return err
	}
	if inlineimage.DetectMosh(os.Getenv) {
		protocol = inlineimage.Unsupported
		selectionSource = "mosh"
	}
	cleanupLogger, err := logger.Init(*debugLogging)
	if err != nil {
		return err
	}
	defer cleanupLogger()
	logger.Session()

	model := app.New(func() (messages.Store, error) {
		return messages.Open(*databasePath)
	}, sender.NewAppleScriptSender())
	model.SetResolverLoader(func() (app.NameResolver, error) {
		resolver, diagnostics := contacts.LoadDefault()
		return resolver, errors.Join(diagnostics...)
	})
	model.SetInlineImageProtocol(protocol)
	if selectionSource == "mosh" {
		model.SetGraphicsUnavailableReason("mosh")
	}
	terminalOutput := inlineimage.NewTerminalOutput(os.Stdout)
	model.SetInlineImageOutput(terminalOutput)
	logger.Info("inline image protocol selected", "protocol", protocol.String(), "source", selectionSource)
	program := tea.NewProgram(model, tea.WithOutput(terminalOutput), tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := program.Run()
	if finalModel, ok := final.(app.Model); ok {
		if closeErr := finalModel.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}

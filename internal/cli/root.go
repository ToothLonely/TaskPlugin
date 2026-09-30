// Package cli parses requests and presents results through explicit streams.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Streams holds the command's input, result output, and diagnostics.
type Streams struct {
	In           io.Reader
	Out          io.Writer
	Err          io.Writer
	OpenDialogue func() (Dialogue, error)
	RunEditor    func(context.Context, string, string, string) error
}

type usageError struct{ message string }

func (e *usageError) Error() string { return e.message }

// Run executes a command and returns the process exit code. It never exits the
// process, so the caller can release resources before calling os.Exit.
func Run(ctx context.Context, args []string, version string, streams Streams) int {
	return RunWithPlans(ctx, args, version, streams, nil)
}

// RunWithPlans connects repository operations while preserving lazy discovery.
func RunWithPlans(ctx context.Context, args []string, version string, streams Streams, open OpenPlans) int {
	if len(args) > 0 && args[0] == "_hook" {
		runHook(ctx, args[1:], streams, open)
		return 0
	}
	err := execute(ctx, args, version, streams, open)
	if err == nil {
		return 0
	}
	code := 1
	var usage *usageError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = 130
		err = fmt.Errorf("операция отменена: %w", err)
	case errors.As(err, &usage):
		code = 2
	}
	// A failed diagnostic write cannot be reported to that same stream. Keep the
	// original nonzero status so a broken pipe cannot turn failure into success.
	fmt.Fprintf(streams.Err, "Ошибка: %v\n", err)
	return code
}

func execute(ctx context.Context, args []string, version string, streams Streams, open OpenPlans) error {
	out := streams.Out
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 {
		return writeHelp(out, "")
	}
	command := args[0]
	switch command {
	case "--help":
		command = "help"
	case "--version":
		command = "version"
	case "--":
		if len(args) == 1 {
			return writeHelp(out, "")
		}
		command = args[1]
		args = args[1:]
	}
	if command != "help" && command != "version" {
		if command == "edit" || command == "move" || command == "pause" || command == "resume" || command == "archive" {
			return runLifecycle(ctx, command, args[1:], streams, open)
		}
		if command == "hooks" {
			return runHooks(ctx, args[1:], streams)
		}
		if command == "complete" {
			return runComplete(ctx, args[1:], streams, open)
		}
		if command == "import" || command == "export" {
			return runTransfer(ctx, command, args[1:], streams, open)
		}
		if command == "start" || command == "attach" {
			return runStart(ctx, command, args[1:], streams, open)
		}
		if command == "init" || command == "add" || command == "status" || command == "sync" {
			return runPlan(ctx, command, args[1:], streams, open)
		}
		if planned(command) {
			return fmt.Errorf("команда %q ещё не реализована; доступные команды: git task help", command)
		}
		return &usageError{fmt.Sprintf("неизвестная команда %q; используйте git task help", command)}
	}
	positionals, help, err := parseInfoArgs(command, args[1:])
	if err != nil {
		return err
	}
	if help && (args[0] == "--help" || args[0] == "--version") {
		return &usageError{"повтор или конфликт флагов --help/--version"}
	}
	if command == "version" {
		if len(positionals) != 0 {
			return &usageError{"version не принимает позиционные аргументы"}
		}
		if help {
			return writeHelp(out, command)
		}
		_, err := fmt.Fprintf(out, "git-task %s\n", version)
		return err
	}
	if len(positionals) > 1 {
		return &usageError{"help принимает не более одного имени команды"}
	}
	if help {
		return writeHelp(out, "help")
	}
	topic := ""
	if len(positionals) == 1 {
		topic = positionals[0]
	}
	return writeHelp(out, topic)
}

// The information commands have only a boolean --help flag. Parse each flag
// with the command's FlagSet while retaining interspersed positional arguments.
// Value-bearing flags belong to their future command parsers.
func parseInfoArgs(command string, args []string) ([]string, bool, error) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var help bool
	fs.BoolVar(&help, "help", false, "показать справку")
	var positionals []string
	seenHelp := false
	for i, arg := range args {
		if arg == "--" {
			return append(positionals, args[i+1:]...), help, nil
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		if arg != "--help" {
			return nil, false, &usageError{fmt.Sprintf("неизвестный флаг %q", arg)}
		}
		if seenHelp {
			return nil, false, &usageError{"флаг --help указан повторно"}
		}
		seenHelp = true
		if err := fs.Parse([]string{arg}); err != nil {
			return nil, false, &usageError{err.Error()}
		}
	}
	return positionals, help, nil
}

func planned(command string) bool {
	switch command {
	case "init", "add", "start", "attach", "edit", "move", "pause", "resume",
		"complete", "archive", "status", "show", "sync", "import", "export", "hooks", "doctor":
		return true
	}
	return false
}

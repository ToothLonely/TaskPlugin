package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
)

func runExchange(ctx context.Context, command string, args []string, streams Streams, open OpenPlans) error {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var help, ff, merge, rebase bool
	fs.BoolVar(&help, "help", false, "")
	if command == "pull" {
		fs.BoolVar(&ff, "ff-only", false, "")
		fs.BoolVar(&merge, "merge", false, "")
		fs.BoolVar(&rebase, "rebase", false, "")
	}
	seen := map[string]bool{}
	mode := "ff-only"
	modes := 0
	for _, arg := range args {
		if arg != "--help" && !(command == "pull" && (arg == "--ff-only" || arg == "--merge" || arg == "--rebase")) {
			return &usageError{"неподдерживаемый аргумент " + command + ": " + arg}
		}
		if seen[arg] {
			return &usageError{"повтор флага " + arg}
		}
		seen[arg] = true
		if arg != "--help" {
			modes++
			mode = arg[2:]
		}
		if err := fs.Parse([]string{arg}); err != nil {
			return &usageError{err.Error()}
		}
	}
	if modes > 1 {
		return &usageError{"выберите только один режим pull: --ff-only, --merge или --rebase"}
	}
	if help {
		return writeHelp(streams.Out, command)
	}
	if open == nil {
		return fmt.Errorf("операции плана не подключены")
	}
	p, err := open(ctx)
	if err != nil {
		return err
	}
	message := "Код отправлен, общий план опубликован."
	if command == "push" {
		err = p.Push(ctx)
	} else {
		err = p.Pull(ctx, mode)
		message = "Код обновлён, общий план получен и сверен; локальная очередь не публиковалась."
	}
	if err != nil {
		return err
	}
	for _, n := range p.TakeNotices() {
		if _, err := fmt.Fprintf(streams.Err, "Уведомление [%s]: %s\n", n.Code, n.Message); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(streams.Out, message)
	return err
}

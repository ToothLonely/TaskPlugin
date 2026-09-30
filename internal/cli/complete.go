package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

func runComplete(ctx context.Context, args []string, streams Streams, open OpenPlans) error {
	var id, commit string
	var yes, help bool
	fs := flag.NewFlagSet("complete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&id, "id", "", "ID задачи")
	fs.StringVar(&commit, "commit", "", "commit завершения")
	fs.BoolVar(&yes, "yes", false, "подтвердить завершение")
	fs.BoolVar(&help, "help", false, "показать справку")
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			if i+1 != len(args) {
				return &usageError{"complete не принимает позиционные аргументы"}
			}
			break
		}
		name, value, valued := strings.Cut(args[i], "=")
		if seen[name] {
			return &usageError{fmt.Sprintf("флаг %s указан повторно", name)}
		}
		seen[name] = true
		switch name {
		case "--yes", "--help":
			if valued {
				return &usageError{fmt.Sprintf("флаг %s не принимает значение", name)}
			}
			if err := fs.Parse([]string{name}); err != nil {
				return &usageError{err.Error()}
			}
		case "--id", "--commit":
			if !valued {
				i++
				if i == len(args) || strings.HasPrefix(args[i], "--") {
					return &usageError{fmt.Sprintf("флаг %s требует значение", name)}
				}
				value = args[i]
			}
			if strings.TrimSpace(value) == "" {
				return &usageError{fmt.Sprintf("пустое значение %s", name)}
			}
			if err := fs.Parse([]string{name + "=" + value}); err != nil {
				return &usageError{err.Error()}
			}
		default:
			return &usageError{fmt.Sprintf("неверный аргумент complete: %q", args[i])}
		}
	}
	if help {
		return writeHelp(streams.Out, "complete")
	}
	if id == "" {
		return &usageError{"complete требует --id"}
	}
	if open == nil {
		return fmt.Errorf("операции плана не подключены")
	}
	plans, err := open(ctx)
	if err != nil {
		return err
	}
	preview, err := plans.PrepareComplete(ctx, id, commit)
	if err != nil {
		return err
	}
	if !preview.NoChange {
		if _, err := fmt.Fprintf(streams.Err, "Завершение manual: %s [%s] %q; commit: %s\n", preview.Task.Number, preview.Task.ID, preview.Task.Title, preview.Commit); err != nil {
			return err
		}
		if !yes {
			confirmed, err := confirmComplete(ctx, streams)
			if err != nil {
				return err
			}
			if !confirmed {
				return context.Canceled
			}
		}
	}
	item, err := plans.ApplyComplete(ctx, preview)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(streams.Out, "%s [%s] done — %s\n", item.Number, item.ID, item.Title); err != nil {
		return fmt.Errorf("задача уже завершена; ошибка вывода: %w", err)
	}
	return nil
}

func confirmComplete(ctx context.Context, streams Streams) (confirmed bool, err error) {
	if streams.OpenDialogue == nil {
		return false, fmt.Errorf("подтверждение complete требует терминал; используйте --yes")
	}
	input, err := streams.OpenDialogue()
	if err != nil {
		return false, fmt.Errorf("подтверждение complete требует терминал; используйте --yes: %w", err)
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	line, err := promptLine(ctx, input, streams.Err, "Подтвердить связь с задачей и завершить? [да/НЕТ]: ")
	if errors.Is(err, io.EOF) {
		return false, context.Canceled
	}
	return line == "да" || line == "yes" || line == "y", err
}

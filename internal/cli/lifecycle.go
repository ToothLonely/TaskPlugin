package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"git-task/internal/task"
)

type lifecycleArgs struct {
	id        string
	attemptID string
	edit      task.EditOptions
	position  task.Position
	help      bool
}

func parseLifecycleArgs(command string, args []string) (lifecycleArgs, error) {
	var result lifecycleArgs
	var title, description string
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&result.id, "id", "", "ID задачи")
	if command == "pause" || command == "resume" {
		fs.StringVar(&result.attemptID, "attempt", "", "ID подхода")
	}
	fs.BoolVar(&result.help, "help", false, "показать справку")
	if command == "edit" {
		fs.StringVar(&title, "title", "", "название")
		fs.StringVar(&description, "description", "", "описание")
	}
	if command == "move" {
		fs.StringVar(&result.position.After, "after", "", "после ID")
		fs.StringVar(&result.position.Before, "before", "", "перед ID")
		fs.BoolVar(&result.position.End, "end", false, "в конец")
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" && i == len(args)-1 {
			break
		}
		name, value, hasValue := strings.Cut(arg, "=")
		boolean := name == "--help" || command == "move" && name == "--end"
		valued := name == "--id" || (command == "pause" || command == "resume") && name == "--attempt" || command == "edit" && (name == "--title" || name == "--description") || command == "move" && (name == "--after" || name == "--before")
		if !boolean && !valued {
			return result, &usageError{fmt.Sprintf("%s: неизвестный флаг или лишний аргумент %q", command, arg)}
		}
		if seen[name] {
			return result, &usageError{fmt.Sprintf("флаг %s указан повторно", name)}
		}
		seen[name] = true
		if boolean {
			if hasValue {
				return result, &usageError{fmt.Sprintf("флаг %s не принимает значение", name)}
			}
		} else {
			if !hasValue {
				i++
				if i == len(args) || strings.HasPrefix(args[i], "--") {
					return result, &usageError{fmt.Sprintf("флаг %s требует значение", name)}
				}
				value = args[i]
			}
			if name != "--description" && strings.TrimSpace(value) == "" {
				return result, &usageError{fmt.Sprintf("пустое значение %s", name)}
			}
			arg = name + "=" + value
		}
		if err := fs.Parse([]string{arg}); err != nil {
			return result, &usageError{err.Error()}
		}
	}
	if seen["--title"] {
		result.edit.Title = &title
	}
	if seen["--description"] {
		result.edit.Description = &description
	}
	positions := 0
	for _, name := range []string{"--after", "--before", "--end"} {
		if seen[name] {
			positions++
		}
	}
	if positions > 1 || command == "move" && !result.help && positions != 1 {
		return result, &usageError{"move требует ровно один из --after, --before, --end"}
	}
	if result.id == "" && !result.help {
		return result, &usageError{command + " требует --id <id>"}
	}
	return result, nil
}

func runLifecycle(ctx context.Context, command string, args []string, streams Streams, open OpenPlans) error {
	parsed, err := parseLifecycleArgs(command, args)
	if err != nil {
		return err
	}
	if parsed.help {
		return writeHelp(streams.Out, command)
	}
	if open == nil {
		return fmt.Errorf("операции плана не подключены")
	}
	plans, err := open(ctx)
	if err != nil {
		return err
	}
	var item task.Task
	changed := true
	switch command {
	case "edit":
		preview, prepareErr := plans.PrepareEdit(ctx, parsed.id)
		if prepareErr != nil {
			return prepareErr
		}
		if parsed.edit == (task.EditOptions{}) {
			item, changed, err = editInEditor(ctx, plans, preview, streams)
		} else {
			item, changed, err = plans.ApplyEdit(ctx, preview, parsed.edit)
		}
	case "move":
		item, changed, err = plans.Move(ctx, parsed.id, parsed.position)
	case "pause":
		item, changed, err = plans.Pause(ctx, parsed.id, parsed.attemptID)
	case "archive":
		item, changed, err = plans.Archive(ctx, parsed.id)
	case "resume":
		item, err = plans.Resume(ctx, parsed.id, parsed.attemptID)
	}
	if err != nil {
		return err
	}
	action := "Изменений нет"
	if changed {
		action = map[string]string{"edit": "Задача отредактирована", "move": "Задача перемещена", "pause": "Задача приостановлена", "archive": "Задача архивирована", "resume": "Работа продолжена"}[command]
	}
	if _, err = fmt.Fprintf(streams.Out, "%s: %s [%s] %s — %s\n", action, item.Number, item.ID, item.Status, item.Title); err != nil {
		return fmt.Errorf("операция %s выполнена для %s; ошибка вывода: %w", command, item.ID, err)
	}
	return nil
}

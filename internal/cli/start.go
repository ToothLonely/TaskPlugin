package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"git-task/internal/app"
	"git-task/internal/task"
)

type startArgs struct {
	options                  app.StartOptions
	help, selectMenu, rebind bool
}

func parseStartArgs(command string, args []string) (startArgs, error) {
	var result startArgs
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&result.help, "help", false, "")
	fs.StringVar(&result.options.ID, "id", "", "")
	if command == "start" {
		fs.StringVar(&result.options.Title, "title", "", "")
		fs.StringVar(&result.options.New, "new", "", "")
		fs.StringVar(&result.options.From, "from", "", "")
		fs.BoolVar(&result.options.Again, "again", false, "")
		fs.BoolVar(&result.selectMenu, "select", false, "")
	} else {
		fs.BoolVar(&result.rebind, "rebind", false, "")
	}
	seen := map[string]bool{}
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		if !strings.HasPrefix(name, "--") || fs.Lookup(strings.TrimPrefix(name, "--")) == nil {
			return result, &usageError{fmt.Sprintf("неизвестный флаг %q", name)}
		}
		if seen[name] {
			return result, &usageError{fmt.Sprintf("флаг %s указан повторно", name)}
		}
		seen[name] = true
		boolean := name == "--help" || name == "--again" || name == "--select" || name == "--rebind"
		if boolean && hasValue {
			return result, &usageError{fmt.Sprintf("флаг %s не принимает значение", name)}
		}
		if !boolean {
			if !hasValue {
				i++
				if i == len(args) || strings.HasPrefix(args[i], "--") {
					return result, &usageError{fmt.Sprintf("флаг %s требует значение", name)}
				}
				value = args[i]
			}
			if strings.TrimSpace(value) == "" {
				return result, &usageError{fmt.Sprintf("пустое значение %s", name)}
			}
			name += "=" + value
		}
		if err := fs.Parse([]string{name}); err != nil {
			return result, &usageError{err.Error()}
		}
	}
	selectors := 0
	for _, name := range []string{"--id", "--title", "--new", "--select"} {
		if seen[name] {
			selectors++
		}
	}
	if selectors > 1 {
		return result, &usageError{"--id, --title, --new и --select взаимоисключающие"}
	}
	if len(positionals) > 1 || !result.help && len(positionals) != 1 {
		return result, &usageError{command + ": требуется ровно одно имя ветки"}
	}
	if len(positionals) == 1 {
		result.options.Branch = positionals[0]
		if strings.TrimSpace(positionals[0]) == "" || strings.HasPrefix(positionals[0], "-") {
			return result, &usageError{"неверное имя ветки"}
		}
	}
	if result.options.Again && (!seen["--id"] && !seen["--title"]) {
		return result, &usageError{"--again требует --id или --title"}
	}
	if command == "attach" && !result.help && !seen["--id"] {
		return result, &usageError{"attach требует --id"}
	}
	return result, nil
}

func runStart(ctx context.Context, command string, args []string, streams Streams, open OpenPlans) error {
	parsed, err := parseStartArgs(command, args)
	if err != nil {
		return err
	}
	if parsed.help {
		return writeHelp(streams.Out, command)
	}
	if parsed.selectMenu {
		if streams.OpenDialogue == nil {
			return fmt.Errorf("--select требует терминал ввода и диагностики; используйте --id, --title или --new")
		}
	}
	if open == nil {
		return fmt.Errorf("операции плана не подключены")
	}
	plans, err := open(ctx)
	if err != nil {
		return err
	}
	var result task.Task
	if command == "start" {
		if parsed.selectMenu {
			parsed.options, err = chooseStart(ctx, plans, parsed.options, streams)
			if err != nil {
				return err
			}
		}
		result, err = plans.Start(ctx, parsed.options)
	} else {
		result, err = plans.Attach(ctx, parsed.options.Branch, parsed.options.ID, parsed.rebind)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(streams.Out, "Задача %s [%s] active: %s\n", result.Number, result.ID, result.ActiveAttempt.Branch)
	if err != nil {
		return fmt.Errorf("операция выполнена; не удалось вывести результат: %w", err)
	}
	return nil
}

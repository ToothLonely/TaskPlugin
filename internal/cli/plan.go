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

// OpenPlans wires repository discovery at the application boundary. Help and
// argument errors never call it, so they work outside repositories.
type OpenPlans func(context.Context) (*app.Plans, error)

type planArgs struct {
	target      string
	title       string
	description string
	position    task.Position
	help        bool
}

func parsePlanArgs(command string, args []string) (planArgs, error) {
	result := planArgs{target: "main"}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&result.help, "help", false, "показать справку")
	if command == "init" {
		fs.StringVar(&result.target, "target", "main", "целевая ветка")
	}
	if command == "add" {
		fs.StringVar(&result.description, "description", "", "описание")
		fs.StringVar(&result.position.After, "after", "", "после ID")
		fs.StringVar(&result.position.Before, "before", "", "перед ID")
		fs.BoolVar(&result.position.End, "end", false, "в конец плана")
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
		boolean := name == "--help" || command == "add" && name == "--end"
		valued := command == "init" && name == "--target" || command == "add" && (name == "--description" || name == "--after" || name == "--before")
		if !boolean && !valued {
			return result, &usageError{fmt.Sprintf("неизвестный флаг %q", name)}
		}
		if seen[name] {
			return result, &usageError{fmt.Sprintf("флаг %s указан повторно", name)}
		}
		seen[name] = true
		if boolean {
			if hasValue {
				return result, &usageError{fmt.Sprintf("флаг %s не принимает значение", name)}
			}
		} else if !hasValue {
			i++
			if i == len(args) || strings.HasPrefix(args[i], "--") {
				return result, &usageError{fmt.Sprintf("флаг %s требует значение", name)}
			}
			value = args[i]
		}
		parsedFlag := name
		if valued {
			parsedFlag += "=" + value
		}
		if err := fs.Parse([]string{parsedFlag}); err != nil {
			return result, &usageError{err.Error()}
		}
		if valued && name != "--description" && strings.TrimSpace(value) == "" {
			return result, &usageError{fmt.Sprintf("пустое значение %s", name)}
		}
	}
	count := 0
	for _, name := range []string{"--after", "--before", "--end"} {
		if seen[name] {
			count++
		}
	}
	if count > 1 {
		return result, &usageError{"--after, --before и --end взаимоисключающие"}
	}
	expected := 0
	if command == "add" {
		expected = 1
	}
	if len(positionals) > expected || !result.help && len(positionals) != expected {
		return result, &usageError{fmt.Sprintf("%s: требуется позиционных аргументов: %d", command, expected)}
	}
	if len(positionals) == 1 {
		result.title = positionals[0]
		if strings.TrimSpace(result.title) == "" {
			return result, &usageError{"title не должен быть пустым"}
		}
	}
	return result, nil
}

func runPlan(ctx context.Context, command string, args []string, streams Streams, open OpenPlans) error {
	parsed, err := parsePlanArgs(command, args)
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
	switch command {
	case "init":
		created, unborn, err := plans.Init(ctx, parsed.target)
		if err != nil {
			return err
		}
		if unborn {
			if _, err = fmt.Fprintln(streams.Err, "Предупреждение: в репозитории ещё нет commit; запуск задач потребует существующего основания."); err != nil {
				return fmt.Errorf("инициализация выполнена; не удалось вывести предупреждение: %w", err)
			}
		}
		message := "План уже инициализирован."
		if created {
			message = "План создан."
		}
		_, err = fmt.Fprintln(streams.Out, message)
		if err != nil {
			return fmt.Errorf("инициализация выполнена; не удалось вывести результат: %w", err)
		}
		return err
	case "add":
		added, err := plans.Add(ctx, parsed.title, parsed.description, parsed.position)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(streams.Out, "Добавлена %s [%s]: %s\n", added.Number, added.ID, added.Title)
		if err != nil {
			return fmt.Errorf("задача %s уже добавлена; не удалось вывести результат: %w", added.ID, err)
		}
		return err
	case "status":
		plan, pending, err := plans.StatusState(ctx)
		if err != nil {
			return err
		}
		if pending {
			if _, err = fmt.Fprintln(streams.Err, "Предупреждение: незавершённая операция в operation.json; изменения заблокированы до явного восстановления."); err != nil {
				return err
			}
		}
		return writeStatus(streams.Out, plan)
	}
	return &usageError{"неизвестная команда плана"}
}

func writeStatus(out io.Writer, plan task.Plan) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Целевая ветка: %s\n", plan.TargetBranch)
	if len(plan.Order) == 0 {
		text.WriteString("План пуст.\n")
	}
	for _, id := range plan.Order {
		t, err := plan.FindID(id)
		if err != nil {
			return err
		}
		fmt.Fprintf(&text, "%s [%s] %s — %s\n", t.Number, t.ID, t.Status, t.Title)
	}
	_, err := io.WriteString(out, text.String())
	return err
}

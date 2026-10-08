package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"git-task/internal/app"
	"git-task/internal/storage"
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
	apply       bool
	targetSet   bool
}

func parsePlanArgs(command string, args []string) (planArgs, error) {
	result := planArgs{target: "main"}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&result.help, "help", false, "показать справку")
	if command == "init" {
		fs.StringVar(&result.target, "target", "main", "целевая ветка")
		fs.BoolVar(&result.apply, "apply", false, "применить заполненный шаблон")
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
		boolean := name == "--help" || command == "add" && name == "--end" || command == "init" && name == "--apply"
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
	result.targetSet = seen["--target"]
	if result.apply && result.targetSet {
		return result, &usageError{"--apply и --target взаимоисключающие; укажите target_branch в шаблоне"}
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
	case "sync":
		plan, changed, err := plans.Sync(ctx)
		if err != nil {
			return err
		}
		if err = writeWarnings(streams.Err, plan); err != nil {
			return fmt.Errorf("сверка выполнена; ошибка вывода: %w", err)
		}
		message := "План согласован с Git; изменений нет."
		if changed {
			message = "План обновлён по локальным данным Git."
		}
		if _, err = fmt.Fprintln(streams.Out, message); err != nil {
			return fmt.Errorf("сверка выполнена; ошибка вывода: %w", err)
		}
		return nil
	case "init":
		var created, unborn bool
		message := "План уже инициализирован."
		if parsed.targetSet {
			created, unborn, err = plans.Init(ctx, parsed.target)
			if created {
				message = "План создан."
			}
		} else {
			var result storage.TemplateResult
			result, unborn, err = plans.InitTemplate(ctx, parsed.apply)
			if result.Draft {
				message = "Шаблон .git-task/plan.json: заполните target_branch и tasks, затем выполните git task init --apply."
			} else if result.Applied {
				message = "Шаблон применён; ID, order и служебные поля созданы."
			}
		}
		if err != nil {
			return err
		}
		if unborn {
			if _, err = fmt.Fprintln(streams.Err, "Предупреждение: в репозитории ещё нет commit; запуск задач потребует существующего основания."); err != nil {
				return fmt.Errorf("инициализация выполнена; не удалось вывести предупреждение: %w", err)
			}
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
		_, err = fmt.Fprintf(streams.Out, "Добавлена [%s]: %s\n", added.ID, added.Title)
		if err != nil {
			return fmt.Errorf("задача %s уже добавлена; не удалось вывести результат: %w", added.ID, err)
		}
		return err
	}
	return &usageError{"неизвестная команда плана"}
}

func writeWarnings(out io.Writer, plan task.Plan) error {
	for _, id := range plan.Order {
		t, _ := plan.FindID(id)
		for _, warning := range t.Warnings {
			if _, err := fmt.Fprintf(out, "Предупреждение [%s] %s: %s\n", warning.Code, id, warning.Message); err != nil {
				return err
			}
		}
	}
	return nil
}

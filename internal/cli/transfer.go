package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"git-task/internal/app"
)

type transferArgs struct {
	path   string
	format string
	yes    bool
	help   bool
}

func parseTransferArgs(command string, args []string) (transferArgs, error) {
	result := transferArgs{format: "markdown"}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&result.help, "help", false, "показать справку")
	fs.StringVar(&result.format, "format", "markdown", "формат")
	if command == "import" {
		fs.BoolVar(&result.yes, "yes", false, "подтвердить импорт")
	} else {
		fs.StringVar(&result.path, "output", "", "путь экспорта")
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
		boolean := name == "--help" || command == "import" && name == "--yes"
		valued := name == "--format" || command == "export" && name == "--output"
		if !boolean && !valued {
			return result, &usageError{fmt.Sprintf("неизвестный флаг %q", name)}
		}
		if seen[name] {
			return result, &usageError{fmt.Sprintf("флаг %s указан повторно", name)}
		}
		seen[name] = true
		if boolean && hasValue {
			return result, &usageError{fmt.Sprintf("флаг %s не принимает значение", name)}
		}
		if valued {
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
			arg = name + "=" + value
		}
		if err := fs.Parse([]string{arg}); err != nil {
			return result, &usageError{err.Error()}
		}
	}
	if result.format != "markdown" && result.format != "json" {
		return result, &usageError{"--format: допустимы markdown и json"}
	}
	expected := 0
	if command == "import" {
		expected = 1
	}
	if len(positionals) > expected || !result.help && len(positionals) != expected {
		return result, &usageError{fmt.Sprintf("%s: требуется позиционных аргументов: %d", command, expected)}
	}
	if len(positionals) == 1 {
		result.path = positionals[0]
		if strings.TrimSpace(result.path) == "" {
			return result, &usageError{"путь импорта не должен быть пустым"}
		}
	}
	return result, nil
}

func runTransfer(ctx context.Context, command string, args []string, streams Streams, open OpenPlans) error {
	parsed, err := parseTransferArgs(command, args)
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
	if command == "export" {
		data, err := plans.Export(ctx, parsed.format)
		if err != nil {
			return err
		}
		if parsed.format == "markdown" {
			if _, err = fmt.Fprintln(streams.Err, "Предупреждение: Markdown сохраняет только порядок, названия и отметки done; ID, описания, связи, история и остальные состояния теряются. Для полного снимка используйте --format json."); err != nil {
				return err
			}
		}
		if parsed.path != "" {
			return plans.ExportFile(ctx, parsed.path, data)
		}
		_, err = streams.Out.Write(data)
		return err
	}
	preview, err := plans.PrepareImport(ctx, parsed.path, parsed.format)
	if err != nil {
		return err
	}
	if err = writeImportPreview(streams.Err, preview, parsed.format); err != nil {
		return err
	}
	if !parsed.yes {
		confirmed, err := confirmImport(ctx, streams)
		if err != nil {
			return err
		}
		if !confirmed {
			return context.Canceled
		}
	}
	if err = plans.ApplyImport(ctx, preview); err != nil {
		return err
	}
	if _, err = fmt.Fprintf(streams.Out, "Импортировано задач: %d.\n", len(preview.Plan.Tasks)); err != nil {
		return fmt.Errorf("импорт уже сохранён; ошибка вывода результата: %w", err)
	}
	return nil
}

func writeImportPreview(out io.Writer, preview *app.ImportPreview, format string) error {
	if _, err := fmt.Fprintf(out, "Импорт: формат %s\nНазначение: %s\nЗадач: %d\nНастройки: target_branch=%q\n", format, preview.Destination, len(preview.Plan.Tasks), preview.Plan.TargetBranch); err != nil {
		return err
	}
	for i, id := range preview.Plan.Order {
		item, err := preview.Plan.FindID(id)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(out, "%d. [%s] %s: %q\n", i+1, item.ID, item.Status, item.Title); err != nil {
			return err
		}
	}
	return nil
}

func confirmImport(ctx context.Context, streams Streams) (confirmed bool, err error) {
	if streams.OpenDialogue == nil {
		return false, fmt.Errorf("подтверждение импорта требует терминал; используйте --yes")
	}
	input, err := streams.OpenDialogue()
	if err != nil {
		return false, fmt.Errorf("подтверждение импорта требует терминал; используйте --yes: %w", err)
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	line, err := promptLine(ctx, input, streams.Err, "Применить импорт? [да/НЕТ]: ")
	if err != nil {
		return false, err
	}
	return line == "да" || line == "yes" || line == "y", nil
}

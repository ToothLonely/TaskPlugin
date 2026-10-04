package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"git-task/internal/git"
	"git-task/internal/hooks"
)

func parseDoctorArgs(args []string) (action string, yes, help bool, err error) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&action, "repair", "", "явное восстановление")
	fs.BoolVar(&yes, "yes", false, "подтвердить восстановление")
	fs.BoolVar(&help, "help", false, "справка")
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" && i == len(args)-1 {
			break
		}
		name, value, hasValue := strings.Cut(arg, "=")
		if name != "--repair" && name != "--yes" && name != "--help" {
			return "", false, false, &usageError{fmt.Sprintf("неизвестный аргумент doctor %q", arg)}
		}
		if seen[name] {
			return "", false, false, &usageError{fmt.Sprintf("флаг %s указан повторно", name)}
		}
		seen[name] = true
		if name == "--repair" {
			if !hasValue {
				i++
				if i == len(args) || strings.HasPrefix(args[i], "-") {
					return "", false, false, &usageError{"--repair требует действие"}
				}
				value = args[i]
			}
			arg = name + "=" + value
		} else if hasValue {
			return "", false, false, &usageError{fmt.Sprintf("%s не принимает значение", name)}
		}
		if parseErr := fs.Parse([]string{arg}); parseErr != nil {
			return "", false, false, &usageError{parseErr.Error()}
		}
	}
	if seen["--repair"] && action != "recover-start" && action != "restore-backup" && action != "unlock" {
		return "", false, false, &usageError{"--repair: recover-start, restore-backup или unlock"}
	}
	if yes && action == "" {
		return "", false, false, &usageError{"--yes требует --repair"}
	}
	return action, yes, help, nil
}

func runDoctor(ctx context.Context, args []string, streams Streams, open OpenPlans) error {
	action, yes, help, err := parseDoctorArgs(args)
	if err != nil {
		return err
	}
	if help {
		return writeHelp(streams.Out, "doctor")
	}
	if open == nil {
		return fmt.Errorf("операции плана не подключены")
	}
	p, err := open(ctx)
	if err != nil {
		return fmt.Errorf("doctor: %w; выберите обычный рабочий репозиторий и проверьте наличие Git", err)
	}
	if action == "" {
		d, err := p.Doctor(ctx, diagnoseDoctorHooks)
		if err != nil {
			return err
		}
		for _, f := range d.Findings {
			mark := "OK"
			if f.Problem {
				mark = "ПРОБЛЕМА"
			}
			if _, err := fmt.Fprintf(streams.Out, "[%s] %s: %s\n", mark, f.Name, f.Detail); err != nil {
				return err
			}
			if f.Next != "" {
				if _, err := fmt.Fprintf(streams.Out, "  Следующее действие: %s\n", f.Next); err != nil {
					return err
				}
			}
		}
		if d.HasProblems() {
			return fmt.Errorf("doctor обнаружил проблемы; данные не изменены")
		}
		return nil
	}
	preview, err := p.PrepareRepair(ctx, action)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(streams.Out, preview.Storage.Description); err != nil {
		return err
	}
	if !preview.Storage.NoChange && !yes {
		confirmed, err := confirmRepair(ctx, streams)
		if err != nil {
			return err
		}
		if !confirmed {
			return context.Canceled
		}
	}
	path, err := p.ApplyRepair(ctx, preview)
	if err != nil {
		return err
	}
	if preview.Storage.NoChange {
		return nil
	}
	if _, err := fmt.Fprintf(streams.Out, "Восстановление выполнено: %s. Сохранённый оригинал: %s\n", action, path); err != nil {
		return fmt.Errorf("восстановление уже выполнено; ошибка вывода: %w", err)
	}
	return nil
}

func diagnoseDoctorHooks(ctx context.Context, root string) ([]string, error) {
	client, err := git.New(root)
	if err != nil {
		return nil, err
	}
	return (hooks.Installer{Git: client}).Diagnose(ctx)
}

func confirmRepair(ctx context.Context, streams Streams) (confirmed bool, err error) {
	if streams.OpenDialogue == nil {
		return false, fmt.Errorf("repair требует терминал; используйте --yes")
	}
	d, err := streams.OpenDialogue()
	if err != nil {
		return false, fmt.Errorf("repair требует терминал; используйте --yes: %w", err)
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	line, err := promptLine(ctx, d, streams.Err, "Применить показанное восстановление? [да/НЕТ]: ")
	if errors.Is(err, io.EOF) {
		return false, context.Canceled
	}
	return line == "да" || line == "yes" || line == "y", err
}

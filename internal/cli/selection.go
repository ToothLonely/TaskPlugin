package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"git-task/internal/app"
	"git-task/internal/task"
)

type Dialogue interface {
	ReadLine(context.Context) (string, error)
	Close() error
}

func chooseStart(ctx context.Context, plans *app.Plans, options app.StartOptions, streams Streams) (result app.StartOptions, err error) {
	input, err := streams.OpenDialogue()
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	selection, err := plans.PrepareSelection(ctx, options)
	if err != nil {
		return result, err
	}
	base := selection.Plan.TargetBranch
	if options.From != "" {
		base = options.From
	}
	if _, err := fmt.Fprintf(streams.Err, "Ветка: %s\nОснование: %s (%s)\nЦель: %s\n", options.Branch, base, selection.Base, selection.Plan.TargetBranch); err != nil {
		return result, err
	}
	var available []task.Task
	for _, item := range selection.Plan.Tasks {
		if item.Status == task.Todo {
			available = append(available, item)
			_, err = fmt.Fprintf(streams.Err, "%d. [%s] todo: %s\n", len(available), item.ID, strconv.Quote(item.Title))
		} else {
			reason := map[task.Status]string{
				task.Active: "уже начата", task.Paused: "требуется resume",
				task.Done: "требуется явный --id или --title с --again", task.Archived: "в архиве",
			}[item.Status]
			_, err = fmt.Fprintf(streams.Err, "- [%s] %s: %s — %s\n", item.ID, item.Status, strconv.Quote(item.Title), reason)
		}
		if err != nil {
			return result, err
		}
	}
	if _, err := fmt.Fprintln(streams.Err, "n. Новая задача\n0. Отмена (или q)"); err != nil {
		return result, err
	}
	options.Selection = selection
	for {
		line, err := promptLine(ctx, input, streams.Err, "Выберите номер, n или 0: ")
		if err != nil {
			return result, err
		}
		switch line {
		case "0", "q":
			return result, context.Canceled
		case "n":
			for {
				title, err := promptLine(ctx, input, streams.Err, "Название новой задачи (0 или q — отмена): ")
				if err != nil {
					return result, err
				}
				if title == "0" || title == "q" {
					return result, context.Canceled
				}
				if title != "" {
					options.New = title
					return options, nil
				}
				if _, err := fmt.Fprintln(streams.Err, "Название не должно быть пустым."); err != nil {
					return result, err
				}
			}
		default:
			index, err := strconv.Atoi(line)
			if err == nil && index > 0 && index <= len(available) {
				options.ID = available[index-1].ID
				return options, nil
			}
			if _, err := fmt.Fprintln(streams.Err, "Неверный выбор; введите номер доступной задачи, n или 0."); err != nil {
				return result, err
			}
		}
	}
}

func promptLine(ctx context.Context, input Dialogue, out io.Writer, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := io.WriteString(out, prompt); err != nil {
		return "", err
	}
	line, err := input.ReadLine(ctx)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, io.EOF) || strings.ContainsAny(line, "\x03\x04\x1a") {
		return "", context.Canceled
	}
	return strings.TrimSpace(line), err
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
	"unicode"
	"unicode/utf8"

	"git-task/internal/app"
	"git-task/internal/task"
)

type editDocument struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

func parseEditDocument(data []byte) (task.EditOptions, error) {
	if !utf8.Valid(data) {
		return task.EditOptions{}, fmt.Errorf("документ редактора должен быть UTF-8")
	}
	var document editDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return task.EditOptions{}, fmt.Errorf("документ редактора должен быть JSON-объектом")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return task.EditOptions{}, fmt.Errorf("невалидный документ редактора: %w", err)
		}
		key, ok := token.(string)
		if !ok || seen[key] || key != "title" && key != "description" {
			return task.EditOptions{}, fmt.Errorf("неизвестное или повторное поле редактора: %v", token)
		}
		seen[key] = true
		field := &document.Title
		if key == "description" {
			field = &document.Description
		}
		if err := decoder.Decode(field); err != nil {
			return task.EditOptions{}, fmt.Errorf("невалидное поле %s: %w", key, err)
		}
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return task.EditOptions{}, fmt.Errorf("невалидный документ редактора")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return task.EditOptions{}, fmt.Errorf("лишние данные в документе редактора")
	}
	if document.Title == nil || document.Description == nil {
		return task.EditOptions{}, fmt.Errorf("документ требует строковые title и description")
	}
	return task.EditOptions{Title: document.Title, Description: document.Description}, nil
}

func editInEditor(ctx context.Context, plans *app.Plans, preview *app.EditPreview, streams Streams) (item task.Task, changed bool, err error) {
	command, err := plans.Editor(ctx)
	if err != nil {
		return item, false, err
	}
	if _, err = splitEditor(command); err != nil {
		return item, false, err
	}
	data, err := json.MarshalIndent(editDocument{Title: &preview.Task.Title, Description: &preview.Task.Description}, "", "  ")
	if err != nil {
		return item, false, err
	}
	f, err := os.CreateTemp("", "git-task-edit-*.json")
	if err != nil {
		return item, false, err
	}
	path := f.Name()
	keep := false
	defer func() {
		if keep {
			err = fmt.Errorf("результат редактора сохранён: %s: %w", path, err)
			return
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("удаление временного документа %s: %w", path, removeErr))
		}
	}()
	if err = errors.Join(writeEditDocument(f, data), f.Close()); err != nil {
		return item, false, err
	}
	run := streams.RunEditor
	if run == nil {
		run = func(ctx context.Context, command, path, dir string) error {
			return launchEditor(ctx, command, path, dir, streams)
		}
	}
	if err = run(ctx, command, path, preview.Dir); err != nil {
		return item, false, err
	}
	if err = ctx.Err(); err != nil {
		return item, false, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return item, false, err
	}
	if !info.Mode().IsRegular() {
		return item, false, fmt.Errorf("редактор заменил документ небезопасным путём")
	}
	f, err = os.Open(path)
	if err != nil {
		return item, false, err
	}
	data, readErr := io.ReadAll(f)
	if err = errors.Join(readErr, f.Close()); err != nil {
		return item, false, err
	}
	keep = true
	options, err := parseEditDocument(data)
	if err != nil {
		return item, false, err
	}
	item, changed, err = plans.ApplyEdit(ctx, preview, options)
	if err == nil {
		keep = false
	}
	return item, changed, err
}

func writeEditDocument(f *os.File, data []byte) error {
	_, err := f.Write(append(data, '\n'))
	return errors.Join(err, f.Sync())
}

func launchEditor(ctx context.Context, command, path, dir string, streams Streams) error {
	args, err := splitEditor(command)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, args[0], append(args[1:], path)...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = streams.In, streams.Err, streams.Err
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("редактирование отменено редактором (код %d): %w", exit.ExitCode(), context.Canceled)
		}
		return fmt.Errorf("не удалось запустить редактор; используйте --title/--description: %w", err)
	}
	return nil
}

func splitEditor(command string) ([]string, error) {
	runes := []rune(command)
	var args []string
	var word []rune
	var quote rune
	started := false
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if ch == 0 {
			return nil, fmt.Errorf("NUL в настройке редактора")
		}
		if ch == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			if quote == '"' && next == '"' || quote == 0 && (unicode.IsSpace(next) || next == '"' || next == '\'') {
				word = append(word, next)
				i++
				started = true
				continue
			}
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				word = append(word, ch)
			}
			continue
		}
		switch {
		case ch == '"' || ch == '\'':
			quote = ch
			started = true
		case unicode.IsSpace(ch):
			if started {
				args = append(args, string(word))
				word, started = nil, false
			}
		default:
			word = append(word, ch)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("незакрытая кавычка в настройке редактора")
	}
	if started {
		args = append(args, string(word))
	}
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("пустая команда редактора")
	}
	return args, nil
}

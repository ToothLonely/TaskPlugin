package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"git-task/internal/app"
	"git-task/internal/task"
)

func parseStatusArgs(args []string) (machine, help bool, err error) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&machine, "json", false, "структурированный JSON")
	fs.BoolVar(&help, "help", false, "показать справку")
	seen := map[string]bool{}
	for i, arg := range args {
		if arg == "--" && i == len(args)-1 {
			break
		}
		if arg != "--json" && arg != "--help" {
			return false, false, &usageError{fmt.Sprintf("status: неизвестный флаг или лишний аргумент %q", arg)}
		}
		if seen[arg] {
			return false, false, &usageError{fmt.Sprintf("флаг %s указан повторно", arg)}
		}
		seen[arg] = true
		if err := fs.Parse([]string{arg}); err != nil {
			return false, false, &usageError{err.Error()}
		}
	}
	return machine, help, nil
}

func runStatus(ctx context.Context, args []string, streams Streams, open OpenPlans) error {
	machine, help, err := parseStatusArgs(args)
	if err != nil {
		return err
	}
	if help {
		return writeHelp(streams.Out, "status")
	}
	if open == nil {
		return fmt.Errorf("операции плана не подключены")
	}
	plans, err := open(ctx)
	if err != nil {
		return err
	}
	r, err := plans.StatusReport(ctx)
	if err != nil {
		return err
	}
	if err := writeStatusWarnings(streams.Err, r.Warnings); err != nil {
		return err
	}
	if machine {
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		n, err := streams.Out.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		return err
	}
	return writeStatusReport(streams.Out, r)
}

func writeStatusWarnings(out io.Writer, warnings []app.StatusWarning) error {
	for _, w := range warnings {
		if _, err := fmt.Fprintf(out, "Предупреждение [%s] %s: %s\n", w.Code, w.TaskID, lineText(w.Message)); err != nil {
			return err
		}
	}
	return nil
}

func writeStatusReport(out io.Writer, r app.StatusReport) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Целевая ветка: %s\n", r.Repository.TargetBranch)
	branch := "detached HEAD"
	if r.Repository.CurrentBranch != nil {
		branch = *r.Repository.CurrentBranch
	}
	fmt.Fprintf(&text, "Текущая ветка: %s", branch)
	if r.Repository.Unborn {
		text.WriteString(" (ещё нет Git-коммита)")
	}
	text.WriteByte('\n')
	if r.Repository.HeadCommit != nil {
		fmt.Fprintf(&text, "Git-коммит HEAD: %s; время Git-коммита: %s (не время последней работы человека)\n", *r.Repository.HeadCommit, knownTime(r.Repository.HeadCommittedAt))
	}
	if r.CurrentTaskID == nil {
		text.WriteString("Текущая задача: нет связи с active/paused.\n")
	} else {
		fmt.Fprintf(&text, "Текущая задача: %s\n", *r.CurrentTaskID)
	}
	fmt.Fprintf(&text, "Прогресс: %d из %d", r.Progress.Done, r.Progress.Total)
	if r.Progress.Percent == nil {
		text.WriteString(", процент не определён\n")
	} else {
		fmt.Fprintf(&text, " (%.1f%%)\n", *r.Progress.Percent)
	}
	text.WriteString("Активные и приостановленные задачи:\n")
	count := 0
	for _, t := range r.Tasks {
		if t.Status != task.Active && t.Status != task.Paused {
			continue
		}
		count++
		fmt.Fprintf(&text, "  %s [%s] %s — %s\n", t.Number, t.ID, t.Status, lineText(t.Title))
		for i, a := range t.OrderedAttempts() {
			fmt.Fprintf(&text, "    Подход %d [%s] %s; автор: %s; ветка: %s; цель: %s\n", i+1, a.ID, a.Status, lineText(known(a.Author)), a.Branch, a.TargetBranch)
		}
		if t.Description != "" {
			fmt.Fprintf(&text, "    Контекст: %s\n", lineText(t.Description))
		}
	}
	if count == 0 {
		text.WriteString("  нет.\n")
	}
	if r.LastCompletion == nil {
		text.WriteString("Последнее зарегистрированное завершение: нет.\n")
	} else {
		last := r.LastCompletion
		fmt.Fprintf(&text, "Последнее зарегистрированное завершение: %s, подход %s; текущий статус %s; источник %s; событие %d\n", last.TaskID, last.AttemptID, last.TaskStatus, last.Completion.Source, last.Completion.Event)
		writeCompletion(&text, last.Completion)
	}
	if r.NextTaskID == nil {
		text.WriteString("Следующая задача (первый todo): нет.\n")
	} else {
		for _, t := range r.Tasks {
			if t.ID == *r.NextTaskID {
				fmt.Fprintf(&text, "Следующая задача (первый todo): %s [%s] — %s\n", t.Number, t.ID, lineText(t.Title))
			}
		}
	}
	text.WriteString("План:\n")
	if len(r.Tasks) == 0 {
		text.WriteString("  План пуст.\n")
	}
	for _, t := range r.Tasks {
		fmt.Fprintf(&text, "  %s [%s] %s — %s\n", t.Number, t.ID, t.Status, lineText(t.Title))
	}
	text.WriteString("Предупреждения:\n")
	if len(r.Warnings) == 0 {
		text.WriteString("  нет.\n")
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&text, "  [%s] %s: %s\n", w.Code, w.TaskID, lineText(w.Message))
	}
	_, err := io.WriteString(out, text.String())
	return err
}

func lineText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r >= 128 && r <= 159 {
			return ' '
		}
		return r
	}, s)
}

func known(s string) string {
	if s == "" {
		return "нет данных"
	}
	return s
}

package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"git-task/internal/app"
	"git-task/internal/task"
)

func knownTime(t *time.Time) string {
	if t == nil {
		return "неизвестно"
	}
	return t.Format(time.RFC3339Nano)
}

func writeAttempt(text *strings.Builder, a task.Attempt) {
	fmt.Fprintf(text, "  ID подхода: %s\n  Ветка: %s; исходная ветка: %s; цель: %s\n  Commit основания: %s\n  Начало подхода: %s\n", a.ID, known(a.Branch), known(a.OriginalBranch), known(a.TargetBranch), known(a.BaseCommit), knownTime(a.StartedAt))
	if a.Observation != nil {
		o := a.Observation
		fmt.Fprintf(text, "  Наблюдённые Git-коммиты: tip %s; цель %s; собственная работа %s\n", known(o.Tip), known(o.TargetCommit), known(o.WorkCommit))
	}
	for _, b := range a.Rebindings {
		fmt.Fprintf(text, "  Перепривязка: %s -> %s; основание %s; зарегистрирована %s\n", b.From, b.To, b.BaseCommit, b.ObservedAt.Format(time.RFC3339Nano))
	}
	if a.Completion != nil {
		fmt.Fprintf(text, "  Завершение: источник %s; событие %d; вид слияния %s\n", a.Completion.Source, a.Completion.Event, known(string(a.Completion.MergeKind)))
		writeCompletion(text, *a.Completion)
	}
}

func writeCompletion(text *strings.Builder, c task.Completion) {
	fmt.Fprintf(text, "  Цель завершения: %s; commit интеграции/подтверждения: %s; commit работы: %s\n  Известное время завершения: %s; зарегистрировано наблюдение: %s\n", c.TargetBranch, known(c.Commit), known(c.WorkCommit), knownTime(c.CompletedAt), knownTime(c.ObservedAt))
}

func runShow(ctx context.Context, args []string, streams Streams, open OpenPlans) error {
	parsed, err := parseLifecycleArgs("show", args)
	if err != nil {
		return err
	}
	if parsed.help {
		return writeHelp(streams.Out, "show")
	}
	if open == nil {
		return fmt.Errorf("операции плана не подключены")
	}
	plans, err := open(ctx)
	if err != nil {
		return err
	}
	item, pending, err := plans.Show(ctx, parsed.id)
	if err != nil {
		return err
	}
	if pending {
		if err := writeStatusWarnings(streams.Err, []app.StatusWarning{{Code: "operation_pending", Message: "Незавершённая операция в operation.json; показан сохранённый план без восстановления."}}); err != nil {
			return err
		}
	}
	var text strings.Builder
	fmt.Fprintf(&text, "%s [%s] %s — %s\nОписание: %s\n", item.Number, item.ID, item.Status, lineText(item.Title), lineText(known(item.Description)))
	text.WriteString("Подходы:\n")
	if len(item.Attempts) == 0 {
		text.WriteString("  нет.\n")
	}
	for i, a := range item.OrderedAttempts() {
		fmt.Fprintf(&text, "Подход %d; статус %s; автор %s:\n", i+1, a.Status, lineText(known(a.Author)))
		writeAttempt(&text, a)
	}
	text.WriteString("Предупреждения (сохранённые; show не выполняет сверку):\n")
	if len(item.Warnings) == 0 {
		text.WriteString("  нет.\n")
	}
	for _, w := range item.Warnings {
		fmt.Fprintf(&text, "  [%s] %s\n", w.Code, lineText(w.Message))
	}
	_, err = io.WriteString(streams.Out, text.String())
	return err
}

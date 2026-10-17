package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"git-task/internal/diagnostic"
	"git-task/internal/task"
)

type Finding struct {
	Name    string
	Detail  error
	Next    string
	Problem bool
}

type Inspection struct {
	Plan     *task.Plan
	Findings []Finding
}

func (s *Store) Inspect(ctx context.Context) (Inspection, error) {
	var result Inspection
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := inspectPath(s.dir, true); err != nil {
		return result, err
	}
	exclusion, excludeErr := readFile(s.excludePath)
	excluded := false
	if excludeErr == nil {
		for _, line := range strings.Split(string(exclusion), "\n") {
			if strings.TrimSuffix(line, "\r") == "/.git-task/" {
				excluded = true
			}
		}
	}
	if !excluded {
		detail := diagnostic.Errorf("нет собственного правила /.git-task/ в %s", s.excludePath)
		if excludeErr != nil {
			detail = diagnostic.Errorf("%v; %w", detail, excludeErr)
		}
		result.Findings = append(result.Findings, Finding{Name: "info/exclude", Detail: detail, Next: "git task init; чужие правила и существующий план сохраняются", Problem: true})
	}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		result.Findings = append(result.Findings, Finding{"plan.json", diagnostic.New("план отсутствует"), "git task init", true})
		return result, nil
	}
	if err != nil {
		return result, err
	}
	for _, e := range entries {
		name := e.Name()
		data, readErr := readFile(filepath.Join(s.dir, name))
		f := Finding{Name: name, Detail: diagnostic.Errorf("%d байт", len(data))}
		if readErr != nil {
			f.Detail, f.Next, f.Problem = readErr, "сохраните исходники; разберите небезопасный путь вручную", true
			if errors.Is(readErr, os.ErrNotExist) {
				f.Detail, f.Next = diagnostic.Errorf("файл исчез во время диагностики: %s", name), "повторите git task doctor; данные не менялись"
			}
			result.Findings = append(result.Findings, f)
			continue
		}
		switch name {
		case "plan.template.json":
			if _, err := task.ReadTemplate(data); err != nil {
				f.Detail, f.Next, f.Problem = err, "сохраните исходный шаблон; ручной разбор", true
			} else {
				f.Detail = diagnostic.New("исходный шаблон до init --apply; история рабочего плана хранится отдельно")
			}
		case "plan.json", "plan.backup.json":
			if name == "plan.json" && isTemplate(data) {
				if _, err := task.ReadTemplate(data); err != nil {
					f.Detail, f.Next, f.Problem = err, "исправьте шаблон, затем git task init --apply", true
				} else {
					f.Detail, f.Next, f.Problem = diagnostic.New("начальный шаблон ещё не применён"), "заполните target_branch и tasks; git task init --apply", true
				}
				break
			}
			plan, err := decode(data)
			if err != nil {
				f.Detail, f.Next, f.Problem = err, "сохраните исходники; проверьте backup через doctor --repair restore-backup", true
				if version := schemaVersion(data); version != 0 && version != task.SchemaVersion {
					f.Next = "поддерживается только схема 3; сохраните исходник, автоматического перехода нет"
				}
			} else {
				f.Detail = diagnostic.Errorf("схема %d, revision %d, задач %d", schemaVersion(data), plan.Revision, len(plan.Tasks))
				if name == "plan.json" {
					result.Plan = &plan
				}
			}
		case "operation.json":
			f = s.inspectJournal(data)
		case "write.lock":
			f.Detail = diagnostic.Errorf("межпроцессная блокировка: %s", strings.TrimSpace(string(data)))
			f.Next, f.Problem = "дождитесь владельца; doctor --repair unlock проверяет отсутствие PID", true
			if _, operationErr := os.Lstat(filepath.Join(s.dir, "operation.json")); operationErr == nil {
				f.Next = "дождитесь владельца; doctor --repair recover-start проверит журнал и отсутствие владельца; unlock поверх operation запрещён"
			}
		default:
			f.Next, f.Problem = "сохраните файл; разберите прерванную запись или конфликт вручную", true
			if strings.HasPrefix(name, "conflict-apply-") && strings.HasSuffix(name, ".json") {
				f.Detail = diagnostic.New("исходная ручная редакция до init --apply; не является валидным backup")
				f.Next, f.Problem = "сохраните для сравнения или ручного восстановления текста", false
			}
		}
		result.Findings = append(result.Findings, f)
	}
	if result.Plan == nil {
		found := false
		for _, f := range result.Findings {
			found = found || f.Name == "plan.json"
		}
		if !found {
			result.Findings = append(result.Findings, Finding{"plan.json", diagnostic.New("отсутствует"), "сохраните служебные данные; ручной разбор", true})
		}
	}
	return result, nil
}

func (s *Store) inspectJournal(data []byte) Finding {
	f := Finding{Name: "operation.json", Next: "git task doctor --repair recover-start", Problem: true}
	j, err := decodeJournal(data)
	if err != nil {
		f.Detail, f.Next = err, "сохраните operation.json; разберите повреждение вручную"
		return f
	}
	installed, err := (&Operation{store: s}).State(j)
	f.Detail = diagnostic.Errorf("намерение %s; результат уже установлен: %t", j.Intent, installed)
	if err != nil {
		f.Detail = diagnostic.Errorf("%v; состояние изменилось во время диагностики: %w", f.Detail, err)
		f.Next = "повторите git task doctor; если конфликт сохраняется, сохраните исходники для ручного разбора"
	}
	return f
}

package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git-task/internal/task"
)

type Finding struct {
	Name    string
	Detail  string
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
		detail := "нет собственного правила /.git-task/ в " + s.excludePath
		if excludeErr != nil {
			detail += "; " + excludeErr.Error()
		}
		result.Findings = append(result.Findings, Finding{Name: "info/exclude", Detail: detail, Next: "git task init с прежним --target; чужие правила сохраняются", Problem: true})
	}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		result.Findings = append(result.Findings, Finding{"plan.json", "план отсутствует", "git task init", true})
		return result, nil
	}
	if err != nil {
		return result, err
	}
	for _, e := range entries {
		name := e.Name()
		data, readErr := readFile(filepath.Join(s.dir, name))
		f := Finding{Name: name, Detail: fmt.Sprintf("%d байт", len(data))}
		if readErr != nil {
			f.Detail, f.Next, f.Problem = readErr.Error(), "сохраните исходники; разберите небезопасный путь вручную", true
			if errors.Is(readErr, os.ErrNotExist) {
				f.Detail, f.Next = "файл исчез во время диагностики: "+name, "повторите git task doctor; данные не менялись"
			}
			result.Findings = append(result.Findings, f)
			continue
		}
		switch name {
		case "plan.json", "plan.backup.json":
			plan, err := decode(data)
			if err != nil {
				f.Detail, f.Next, f.Problem = err.Error(), "сохраните исходники; проверьте backup через doctor --repair restore-backup", true
				if version := schemaVersion(data); version != 0 && version != task.SchemaVersion {
					f.Next = "сохраните исходник; используйте совместимую версию CLI, не восстанавливайте поверх неизвестной схемы"
				}
			} else {
				f.Detail = fmt.Sprintf("схема %d, revision %d, задач %d", schemaVersion(data), plan.Revision, len(plan.Tasks))
				if name == "plan.json" {
					result.Plan = &plan
					if schemaVersion(data) == 1 {
						f.Next, f.Problem = "git task migrate", true
					}
				}
			}
		case "operation.json":
			f = s.inspectJournal(data)
		case "write.lock":
			f.Detail = "межпроцессная блокировка: " + strings.TrimSpace(string(data))
			f.Next, f.Problem = "дождитесь владельца; doctor --repair unlock проверяет отсутствие PID", true
			if _, operationErr := os.Lstat(filepath.Join(s.dir, "operation.json")); operationErr == nil {
				f.Next = "дождитесь владельца; doctor --repair recover-start проверит журнал и отсутствие владельца; unlock поверх operation запрещён"
			}
		case "plan.schema-1.json":
			_, err := task.MigrateV1(data)
			if err != nil {
				f.Detail, f.Next, f.Problem = err.Error(), "сохраните исходник миграции; ручной разбор", true
			}
		default:
			f.Next, f.Problem = "сохраните файл; разберите прерванную запись или конфликт вручную", true
		}
		result.Findings = append(result.Findings, f)
	}
	if result.Plan == nil {
		found := false
		for _, f := range result.Findings {
			found = found || f.Name == "plan.json"
		}
		if !found {
			result.Findings = append(result.Findings, Finding{"plan.json", "отсутствует", "сохраните служебные данные; ручной разбор", true})
		}
	}
	return result, nil
}

func (s *Store) inspectJournal(data []byte) Finding {
	f := Finding{Name: "operation.json", Next: "git task doctor --repair recover-start", Problem: true}
	j, err := decodeJournal(data)
	if err != nil {
		f.Detail, f.Next = err.Error(), "сохраните operation.json; разберите повреждение вручную"
		return f
	}
	installed, err := (&Operation{store: s}).State(j)
	f.Detail = fmt.Sprintf("намерение %s; результат уже установлен: %t", j.Intent, installed)
	if err != nil {
		f.Detail += "; состояние изменилось во время диагностики: " + err.Error()
		f.Next = "повторите git task doctor; если конфликт сохраняется, сохраните исходники для ручного разбора"
	}
	return f
}

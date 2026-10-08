package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"git-task/internal/task"
)

var ErrTemplate = errors.New("план ещё является шаблоном: заполните target_branch и tasks в .git-task/plan.json, затем выполните git task init --apply")

type TemplateResult struct {
	Created bool
	Draft   bool
	Applied bool
}

func validateBackup(name string, data []byte) error {
	if name == "plan.template.json" {
		_, err := task.ReadTemplate(data)
		return err
	}
	_, err := decode(data)
	return err
}

func isTemplate(data []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return false
	}
	_, exists := fields["format"]
	return !exists
}

func (s *Store) templateSource() ([]byte, bool, error) {
	data, err := readFile(filepath.Join(s.dir, "plan.json"))
	if err != nil {
		return nil, false, err
	}
	if isTemplate(data) {
		if _, err = task.ReadTemplate(data); err != nil {
			return nil, false, err
		}
		for _, name := range []string{"plan.backup.json", "plan.template.json"} {
			if _, err = os.Lstat(filepath.Join(s.dir, name)); err == nil {
				return nil, false, fmt.Errorf("шаблон рядом с историей плана %s; сохраните файлы и разберите замену вручную", name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, false, err
			}
		}
		return data, true, nil
	}
	_, err = decode(data)
	return data, false, err
}

func (s *Store) InitTemplate(ctx context.Context, apply bool) (result TemplateResult, err error) {
	if err = s.check(ctx, false); err != nil {
		return result, err
	}
	if err = inspectPath(s.excludePath, false); err != nil {
		return result, err
	}
	original, draft, err := s.templateSource()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if apply && errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("шаблон отсутствует; сначала выполните git task init: %w", err)
	}
	if err = os.MkdirAll(s.dir, 0700); err != nil {
		return result, err
	}
	unlock, err := s.lock()
	if err != nil {
		return result, err
	}
	defer func() {
		releaseErr := unlock()
		if releaseErr != nil && (result.Created || result.Applied) {
			releaseErr = fmt.Errorf("файл уже сохранён; ошибка снятия блокировки: %w", releaseErr)
		}
		err = errors.Join(err, releaseErr)
	}()
	if err = s.check(ctx, true); err != nil {
		return result, err
	}
	if err = s.noOperation(); err != nil {
		return result, err
	}
	if err = s.compare(original); err != nil {
		return result, err
	}
	result.Draft = draft
	var next []byte
	if original == nil {
		next = []byte("{\n  \"target_branch\": \"\",\n  \"tasks\": []\n}\n")
		result.Draft = true
	} else if apply && draft {
		template, parseErr := task.ReadTemplate(original)
		if parseErr != nil {
			return result, parseErr
		}
		plan, planErr := template.Plan()
		if planErr != nil {
			return result, planErr
		}
		next, err = encode(plan)
		if err != nil {
			return result, err
		}
	}
	if err = s.exclude(ctx); err != nil {
		return result, err
	}
	if next == nil {
		return result, nil
	}
	if err = s.installWithBackup(ctx, original, next, "plan.template.json"); err != nil {
		return result, err
	}
	result.Created = original == nil
	result.Applied = apply && draft
	result.Draft = !result.Applied
	return result, nil
}

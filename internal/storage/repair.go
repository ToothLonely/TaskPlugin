package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"git-task/internal/task"
)

type Repair struct {
	Action      string
	Description string
	NoChange    bool
	Plan        *task.Plan
	files       map[string][]byte
	lockInfo    os.FileInfo
}

func (s *Store) repairFiles() (map[string][]byte, error) {
	if err := inspectPath(s.dir, true); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte)
	for _, e := range entries {
		if e.Name() == "recovery.lock" {
			continue
		}
		data, err := readFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, err
		}
		files[e.Name()] = data
	}
	return files, nil
}

func (s *Store) PrepareRepair(ctx context.Context, action string) (*Repair, error) {
	if err := s.guard.CheckStorage(ctx); err != nil {
		return nil, err
	}
	if err := inspectPath(filepath.Join(s.dir, "recovery.lock"), false); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(filepath.Join(s.dir, "recovery.lock")); err == nil {
		return nil, ErrLocked
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	files, err := s.repairFiles()
	if err != nil {
		return nil, err
	}
	r := &Repair{Action: action, files: files}
	if action == "recover-start" {
		j, err := (&Operation{store: s}).Journal()
		if err != nil {
			return nil, err
		}
		if j == nil {
			if files["write.lock"] != nil {
				return nil, ErrLocked
			}
			r.NoChange = true
			r.Description = "Нет незавершённой операции"
			return r, nil
		}
		installed, err := (&Operation{store: s}).State(j)
		if err != nil {
			return nil, err
		}
		if files["write.lock"] != nil {
			if err := absentOwner(files["write.lock"]); err != nil {
				return nil, err
			}
			r.lockInfo, err = os.Lstat(filepath.Join(s.dir, "write.lock"))
			if err != nil {
				return nil, err
			}
		}
		r.Description = fmt.Sprintf("operation.json: %s; результат уже установлен: %t", j.Intent, installed)
		if files["write.lock"] != nil {
			r.Description += "; распознанный владелец отсутствует: " + strings.TrimSpace(string(files["write.lock"]))
		}
		return r, nil
	}
	if action != "restore-backup" && action != "unlock" {
		return nil, fmt.Errorf("неизвестное восстановление %q", action)
	}
	if files["operation.json"] != nil {
		return nil, ErrOperation
	}
	for name := range files {
		if strings.HasPrefix(name, "pending-") {
			return nil, ErrInterrupted
		}
		if name != "plan.json" && name != "plan.backup.json" && name != "plan.template.json" && name != "write.lock" && !(strings.HasPrefix(name, "conflict-") && strings.HasSuffix(name, ".json")) {
			return nil, fmt.Errorf("чужой файл: %s", name)
		}
	}
	if action == "unlock" {
		if files["write.lock"] == nil {
			r.NoChange = true
			r.Description = "Нет блокировки"
			return r, nil
		}
		if err := absentOwner(files["write.lock"]); err != nil {
			return nil, err
		}
		r.lockInfo, err = os.Lstat(filepath.Join(s.dir, "write.lock"))
		r.Description = "Владелец отсутствует: " + string(files["write.lock"])
		return r, err
	}
	if files["write.lock"] != nil {
		return nil, ErrLocked
	}
	if files["plan.json"] == nil {
		return nil, fmt.Errorf("нет оригинала; требуется ручной разбор")
	}
	if _, err := decode(files["plan.json"]); err == nil {
		return nil, fmt.Errorf("валидный план не заменяется backup")
	}
	var header struct {
		Format  string `json:"format"`
		Version int    `json:"schema_version"`
	}
	if json.Unmarshal(files["plan.json"], &header) == nil && (header.Format != task.Format || header.Version != task.SchemaVersion) {
		return nil, fmt.Errorf("чужой формат или несовместимая схема не заменяются")
	}
	backup, err := decode(files["plan.backup.json"])
	if err != nil {
		return nil, fmt.Errorf("backup текущей схемы невалиден: %w", err)
	}
	info, err := os.Lstat(filepath.Join(s.dir, "plan.backup.json"))
	if err != nil {
		return nil, err
	}
	pending := 0
	if backup.Team != nil {
		pending = len(backup.Team.Pending)
	}
	r.Plan = &backup
	r.Description = fmt.Sprintf("Оригинал: %d байт, невалиден. Backup: %d байт, revision %d, задач %d, неопубликованных действий %d, mtime %s. Байты различаются: %t; предметное сравнение невозможно из-за повреждения. Вы выбираете старый снимок: более свежая неопубликованная очередь может остаться только в сохранённом оригинале.", len(files["plan.json"]), len(files["plan.backup.json"]), backup.Revision, len(backup.Tasks), pending, info.ModTime().UTC().Format(time.RFC3339), !bytes.Equal(files["plan.json"], files["plan.backup.json"]))
	return r, nil
}

func (s *Store) CheckRepair(r *Repair, holdingLock bool) error {
	actual, err := s.repairFiles()
	if err != nil {
		return err
	}
	if holdingLock {
		delete(actual, "write.lock")
	}
	expectedCount := len(r.files)
	if holdingLock && r.files["write.lock"] != nil {
		expectedCount--
	}
	if len(actual) != expectedCount {
		return ErrConflict
	}
	for name, expected := range r.files {
		if holdingLock && name == "write.lock" {
			continue
		}
		if !bytes.Equal(actual[name], expected) {
			return ErrConflict
		}
	}
	return nil
}

func (s *Store) WithRepairOperation(ctx context.Context, r *Repair, run func(*Operation) error) (err error) {
	if r == nil || r.Action != "recover-start" {
		return fmt.Errorf("нет предпросмотра recover-start")
	}
	if r.files["write.lock"] == nil {
		return s.WithOperation(ctx, run)
	}
	if err = s.guard.CheckStorage(ctx); err != nil {
		return err
	}
	gate := filepath.Join(s.dir, "recovery.lock")
	f, err := os.OpenFile(gate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	info, statErr := f.Stat()
	if err = errors.Join(statErr, f.Close()); err != nil {
		return err
	}
	defer func() {
		current, releaseErr := os.Lstat(gate)
		if releaseErr == nil && os.SameFile(info, current) {
			releaseErr = os.Remove(gate)
		} else if releaseErr == nil {
			releaseErr = ErrConflict
		}
		err = errors.Join(err, releaseErr)
	}()
	if err = s.CheckRepair(r, false); err != nil {
		return err
	}
	j, journalErr := (&Operation{store: s}).Journal()
	if journalErr != nil {
		return journalErr
	}
	if j == nil {
		return ErrConflict
	}
	if _, err = (&Operation{store: s}).State(j); err != nil {
		return err
	}
	if err = absentOwner(r.files["write.lock"]); err != nil {
		return err
	}
	current, err := os.Lstat(filepath.Join(s.dir, "write.lock"))
	if err != nil {
		return err
	}
	if !os.SameFile(current, r.lockInfo) {
		return ErrConflict
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(s.dir, "write.lock")); err != nil {
		return err
	}
	unlock, err := s.lockFile()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if err = s.check(ctx, true); err != nil {
		return err
	}
	return run(&Operation{store: s})
}

func (s *Store) ApplyRepair(ctx context.Context, r *Repair) (saved string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r == nil || (r.Action != "unlock" && r.Action != "restore-backup") {
		return "", fmt.Errorf("неверный repair")
	}
	if r.NoChange {
		return "", s.CheckRepair(r, false)
	}
	if err = s.guard.CheckStorage(ctx); err != nil {
		return "", err
	}
	gate := filepath.Join(s.dir, "recovery.lock")
	f, err := os.OpenFile(gate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	gateInfo, statErr := f.Stat()
	if err = errors.Join(statErr, f.Close()); err != nil {
		return "", err
	}
	defer func() {
		info, checkErr := os.Lstat(gate)
		if checkErr == nil && os.SameFile(gateInfo, info) {
			checkErr = os.Remove(gate)
		} else if checkErr == nil {
			checkErr = ErrConflict
		}
		err = errors.Join(err, checkErr)
	}()
	if err = s.CheckRepair(r, false); err != nil {
		return "", err
	}
	if r.Action == "unlock" {
		if err = absentOwner(r.files["write.lock"]); err != nil {
			return "", err
		}
		info, err := os.Lstat(filepath.Join(s.dir, "write.lock"))
		if err != nil {
			return "", err
		}
		if !os.SameFile(info, r.lockInfo) {
			return "", ErrConflict
		}
		if err = ctx.Err(); err != nil {
			return "", err
		}
		return "", os.Remove(filepath.Join(s.dir, "write.lock"))
	}
	unlock, err := s.lockFile()
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if err = s.CheckRepair(r, true); err != nil {
		return "", err
	}
	if err = s.guard.CheckStorage(ctx); err != nil {
		return "", err
	}
	f, err = os.CreateTemp(s.dir, "conflict-original-*.json")
	if err != nil {
		return "", err
	}
	saved = f.Name()
	_, writeErr := f.Write(r.files["plan.json"])
	if err = errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return saved, err
	}
	candidate, err := s.writePending(r.files["plan.backup.json"])
	if err != nil {
		return saved, err
	}
	if err = s.point(ctx, "restore-candidate"); err != nil {
		return saved, err
	}
	if err = s.compare(r.files["plan.json"]); err != nil {
		return saved, err
	}
	backup, err := readFile(filepath.Join(s.dir, "plan.backup.json"))
	if err != nil {
		return saved, err
	}
	if !bytes.Equal(backup, r.files["plan.backup.json"]) {
		return saved, ErrConflict
	}
	if err = s.noOperation(); err != nil {
		return saved, err
	}
	if err = os.Rename(candidate, filepath.Join(s.dir, "plan.json")); err != nil {
		return saved, err
	}
	return saved, s.point(ctx, "restore-installed")
}

func absentOwner(data []byte) error {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 || lines[0] != "git-task write lock" || !strings.HasPrefix(lines[1], "pid=") || !strings.HasPrefix(lines[2], "created=") || !strings.HasPrefix(lines[3], "host=") {
		return fmt.Errorf("владельца lock нельзя проверить: неизвестный формат")
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(lines[1], "pid="))
	if err != nil || pid <= 0 {
		return fmt.Errorf("неверный PID lock")
	}
	if _, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(lines[2], "created=")); err != nil {
		return err
	}
	host, err := os.Hostname()
	if err != nil {
		return err
	}
	if host != strings.TrimPrefix(lines[3], "host=") {
		return fmt.Errorf("lock другого компьютера не снимается")
	}
	return processAbsent(pid)
}

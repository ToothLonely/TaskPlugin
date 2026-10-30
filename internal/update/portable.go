package update

import (
	"context"
	"encoding/json"
	"errors"
	"git-task/internal/diagnostic"
	"os"
	"path/filepath"
)

type transaction struct {
	Target          string `json:"target"`
	Backup          string `json:"backup"`
	Candidate       string `json:"candidate"`
	PreviousHash    string `json:"previous_hash"`
	NextHash        string `json:"next_hash"`
	PreviousVersion string `json:"previous_version"`
	NextVersion     string `json:"next_version"`
}

func writeNew(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	return errors.Join(writeErr, syncErr, file.Close())
}

func replacePortable(ctx context.Context, i Installation, binary []byte, version, hash string) error {
	return applyPortable(ctx, i, binary, version, hash, Inspect, verifyVersion)
}

func applyPortable(ctx context.Context, i Installation, binary []byte, version, hash string, inspect func(context.Context, string, string) (Installation, error), verify func(context.Context, string, string) error) error {
	return applyPortableSteps(ctx, i, binary, version, hash, inspect, verify, func(string) {})
}

func applyPortableSteps(ctx context.Context, i Installation, binary []byte, version, hash string, inspect func(context.Context, string, string) (Installation, error), verify func(context.Context, string, string) error, checkpoint func(string)) error {
	journalPath := i.Path + ".update.json"
	backup := i.Path + ".update-backup"
	for _, path := range []string{journalPath, backup, filepath.Join(filepath.Dir(i.Path), "git-task-portable.json.update-backup")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return diagnostic.Errorf("предыдущее обновление требует восстановления; сохраните %s и следуйте DOCUMENTATION.md", path)
		}
	}
	candidate, err := os.CreateTemp(filepath.Dir(i.Path), ".git-task-next-*.exe")
	if err != nil {
		return err
	}
	staged := candidate.Name()
	if err := candidate.Close(); err != nil {
		return err
	}
	defer os.Remove(staged)
	if err := os.WriteFile(staged, binary, i.Mode); err != nil {
		return err
	}
	if err := os.Chmod(staged, i.Mode); err != nil {
		return err
	}
	file, err := os.OpenFile(staged, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = errors.Join(file.Sync(), file.Close())
	if err != nil {
		return err
	}
	if err := verify(ctx, staged, version); err != nil {
		return err
	}
	current, err := inspect(ctx, i.Path, i.Version)
	if err != nil {
		return err
	}
	if current.Owner != "portable" || current.Hash != i.Hash {
		return diagnostic.Errorf("portable-бинарник изменился перед заменой")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx := transaction{Target: i.Path, Backup: backup, Candidate: staged, PreviousHash: i.Hash, NextHash: hash, PreviousVersion: i.Version, NextVersion: version}
	data, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	if err := writeNew(journalPath, data, 0600); err != nil {
		return err
	}
	checkpoint("journal")
	if err := os.Rename(i.Path, backup); err != nil {
		return diagnostic.Errorf("не удалось сохранить работающий бинарник; оригинал сохранён, журнал %s: %w", journalPath, err)
	}
	checkpoint("backup")
	if err := os.Rename(staged, i.Path); err != nil {
		return errors.Join(err, os.Rename(backup, i.Path))
	}
	checkpoint("binary")
	if err := verify(ctx, i.Path, version); err != nil {
		if moveErr := os.Rename(i.Path, staged); moveErr != nil {
			return errors.Join(err, moveErr)
		}
		return errors.Join(err, os.Rename(backup, i.Path))
	}
	installedHash, _, err := fileHash(i.Path)
	if err != nil {
		return err
	}
	if installedHash != hash {
		return diagnostic.New("контрольная сумма бинарника в архиве не совпадает")
	}
	receipt := portableReceipt{Owner: "portable", Version: version, SHA256: hash}
	receiptData, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(filepath.Dir(i.Path), "git-task-portable.json")
	if err := os.Rename(receiptPath, receiptPath+".update-backup"); err != nil {
		return diagnostic.Errorf("бинарник заменён, но квитанция требует восстановления: %w", err)
	}
	checkpoint("receipt-backup")
	if err := writeNew(receiptPath, receiptData, 0600); err != nil {
		return diagnostic.Errorf("бинарник заменён, но квитанция требует восстановления: %w", err)
	}
	checkpoint("receipt")
	return nil
}

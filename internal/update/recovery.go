package update

import (
	"encoding/json"
	"errors"
	"git-task/internal/diagnostic"
	"os"
	"path/filepath"
)

func recoverPortable(path, version string) error {
	journal := path + ".update.json"
	info, err := os.Lstat(journal)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 8192 {
		return diagnostic.Errorf("небезопасный журнал обновления")
	}
	data, err := os.ReadFile(journal)
	if err != nil {
		return err
	}
	var tx transaction
	if json.Unmarshal(data, &tx) != nil || tx.Target != path || tx.Backup != path+".update-backup" || filepath.Dir(tx.Candidate) != filepath.Dir(path) || !validHash(tx.PreviousHash) || !validHash(tx.NextHash) {
		return diagnostic.Errorf("повреждён журнал обновления; требуется ручное восстановление: %s", journal)
	}
	current, _, err := fileHash(path)
	if err != nil {
		return err
	}
	if current != tx.NextHash || version != tx.NextVersion {
		return diagnostic.Errorf("обновление прервано; сохраните журнал и backup, восстановите согласованные бинарник и квитанцию перед удалением журнала: %s", journal)
	}
	receiptPath := filepath.Join(filepath.Dir(path), "git-task-portable.json")
	receipt, err := json.Marshal(portableReceipt{Owner: "portable", Version: version, SHA256: current})
	if err != nil {
		return err
	}
	_, receiptErr := os.Lstat(receiptPath)
	missingReceipt := errors.Is(receiptErr, os.ErrNotExist)
	if receiptErr != nil && !missingReceipt {
		return receiptErr
	}
	if !missingReceipt {
		data, err := os.ReadFile(receiptPath)
		if err != nil {
			return err
		}
		var got portableReceipt
		if json.Unmarshal(data, &got) != nil || got.Owner != "portable" || got.Version != version || got.SHA256 != current {
			return diagnostic.Errorf("квитанция не совпадает при восстановлении обновления")
		}
	}
	backupPresent := false
	if _, err := os.Lstat(tx.Backup); err == nil {
		hash, _, err := fileHash(tx.Backup)
		if err != nil {
			return err
		}
		if hash != tx.PreviousHash {
			return diagnostic.Errorf("резервная копия обновления изменена")
		}
		backupPresent = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	oldReceipt := receiptPath + ".update-backup"
	oldReceiptPresent := false
	if data, err := os.ReadFile(oldReceipt); err == nil {
		var r portableReceipt
		if json.Unmarshal(data, &r) != nil || r.Owner != "portable" || r.Version != tx.PreviousVersion || r.SHA256 != tx.PreviousHash {
			return diagnostic.Errorf("резервная квитанция изменена")
		}
		oldReceiptPresent = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if missingReceipt {
		if err := writeNew(receiptPath, receipt, 0600); err != nil {
			return err
		}
	}
	if backupPresent {
		if err := os.Remove(tx.Backup); err != nil {
			return err
		}
	}
	if oldReceiptPresent {
		if err := os.Remove(oldReceipt); err != nil {
			return err
		}
	}
	return os.Remove(journal)
}

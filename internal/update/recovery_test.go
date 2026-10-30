package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryRefusesChangedBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-task")
	old, next := sha256.Sum256([]byte("old")), sha256.Sum256([]byte("next"))
	tx := transaction{Target: path, Backup: path + ".update-backup", Candidate: filepath.Join(dir, ".git-task-next.exe"), PreviousHash: hex.EncodeToString(old[:]), NextHash: hex.EncodeToString(next[:]), PreviousVersion: "1.0.0", NextVersion: "2.0.0"}
	journal, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := json.Marshal(portableReceipt{Owner: "portable", Version: "2.0.0", SHA256: tx.NextHash})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{path: []byte("next"), tx.Backup: []byte("external edit"), path + ".update.json": journal, filepath.Join(dir, "git-task-portable.json"): receipt} {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := recoverPortable(path, "2.0.0"); err == nil {
		t.Fatal("accepted changed backup")
	}
	data, err := os.ReadFile(tx.Backup)
	if err != nil || string(data) != "external edit" {
		t.Fatalf("backup changed: %q %v", data, err)
	}
	if _, err := os.Stat(path + ".update.json"); err != nil {
		t.Fatal("journal lost")
	}
}

func TestRecoveryPreservesBackupWhenReceiptChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-task")
	old, next := []byte("old"), []byte("next")
	oldSum, nextSum := sha256.Sum256(old), sha256.Sum256(next)
	tx := transaction{Target: path, Backup: path + ".update-backup", Candidate: filepath.Join(dir, ".git-task-next.exe"), PreviousHash: hex.EncodeToString(oldSum[:]), NextHash: hex.EncodeToString(nextSum[:]), PreviousVersion: "1.0.0", NextVersion: "2.0.0"}
	journal, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := json.Marshal(portableReceipt{Owner: "portable", Version: tx.NextVersion, SHA256: tx.NextHash})
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(dir, "git-task-portable.json")
	for name, data := range map[string][]byte{path: next, tx.Backup: old, path + ".update.json": journal, receiptPath: receipt, receiptPath + ".update-backup": []byte("external edit")} {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := recoverPortable(path, tx.NextVersion); err == nil {
		t.Fatal("accepted changed backup receipt")
	}
	for name, want := range map[string][]byte{tx.Backup: old, receiptPath + ".update-backup": []byte("external edit"), path + ".update.json": journal} {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != string(want) {
			t.Errorf("recovery changed evidence %s: got %q, error %v", name, got, err)
		}
	}
}

func TestRecoveryDoesNotCreateReceiptBeforeValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-task")
	old, next := sha256.Sum256([]byte("old")), sha256.Sum256([]byte("next"))
	tx := transaction{Target: path, Backup: path + ".update-backup", Candidate: filepath.Join(dir, "candidate"), PreviousHash: hex.EncodeToString(old[:]), NextHash: hex.EncodeToString(next[:]), PreviousVersion: "1.0.0", NextVersion: "2.0.0"}
	journal, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(dir, "git-task-portable.json")
	files := map[string][]byte{path: []byte("next"), tx.Backup: []byte("old"), path + ".update.json": journal, receipt + ".update-backup": []byte("external edit")}
	for name, data := range files {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := recoverPortable(path, tx.NextVersion); err == nil {
		t.Fatal("accepted changed receipt")
	}
	for name, want := range files {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != string(want) {
			t.Fatalf("changed %s: %q %v", name, got, err)
		}
	}
	if _, err := os.Lstat(receipt); !os.IsNotExist(err) {
		t.Fatal("created receipt before validating evidence")
	}
}

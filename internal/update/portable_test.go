package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPortableTransaction(t *testing.T) {
	for _, scenario := range []string{"success", "changed", "cancel", "post-verification-failure", "existing-journal"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "git-task.exe")
			old, next := []byte("old executable"), []byte("new executable")
			oldSum, nextSum := sha256.Sum256(old), sha256.Sum256(next)
			i := Installation{Path: target, Owner: "portable", Version: "1.0.0", Hash: hex.EncodeToString(oldSum[:]), Mode: 0755}
			nextHash := hex.EncodeToString(nextSum[:])
			if err := os.WriteFile(target, old, 0755); err != nil {
				t.Fatal(err)
			}
			receipt, _ := json.Marshal(portableReceipt{Owner: "portable", Version: i.Version, SHA256: i.Hash})
			if err := os.WriteFile(filepath.Join(dir, "git-task-portable.json"), receipt, 0600); err != nil {
				t.Fatal(err)
			}
			inspect := func(context.Context, string, string) (Installation, error) {
				result := i
				if scenario == "changed" {
					result.Hash = "changed"
				}
				return result, nil
			}
			verify := func(_ context.Context, path, version string) error {
				if scenario == "post-verification-failure" && path == target {
					return errors.New("injected version failure")
				}
				return nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancel" {
				cancel()
			}
			if scenario == "existing-journal" {
				if err := os.WriteFile(target+".update.json", []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := applyPortable(ctx, i, next, "2.0.0", nextHash, inspect, verify)
			if scenario == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if err := recoverPortable(target, "2.0.0"); err != nil {
					t.Fatal(err)
				}
				if err := recoverPortable(target, "2.0.0"); err != nil {
					t.Fatalf("repeated recovery: %v", err)
				}
				got, _, err := fileHash(target)
				if err != nil || got != nextHash {
					t.Fatalf("new binary: %s %v", got, err)
				}
				if _, err := os.Stat(target + ".update-backup"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("backup was not cleaned")
				}
			} else {
				if err == nil {
					t.Fatal("expected refusal")
				}
				got, _, err := fileHash(target)
				if err != nil || got != i.Hash {
					t.Fatalf("old binary not preserved: %s %v", got, err)
				}
			}
		})
	}
}

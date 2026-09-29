package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-task/internal/task"
)

func TestOperationJournalGuardsWritersAndExactResult(t *testing.T) {
	s, _ := initialized(t)
	ctx := context.Background()
	var journal *Journal
	err := s.WithOperation(ctx, func(op *Operation) error {
		base, err := op.Load()
		if err != nil {
			return err
		}
		next := base.Plan
		if _, err = next.Add("prepared", "", task.Position{}); err != nil {
			return err
		}
		journal, err = op.Prepare(ctx, base, next, map[string]string{"id": "operation"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load(ctx)
	if err != nil || !snap.PendingOperation {
		t.Fatalf("load: %+v %v", snap, err)
	}
	if _, err = s.Init(ctx, "main"); !errors.Is(err, ErrOperation) {
		t.Fatalf("init: %v", err)
	}
	if _, err = s.Save(ctx, snap, snap.Plan); !errors.Is(err, ErrOperation) {
		t.Fatalf("save: %v", err)
	}
	err = s.WithOperation(ctx, func(op *Operation) error {
		read, err := op.Journal()
		if err != nil {
			return err
		}
		if !bytes.Equal(read.raw, journal.raw) {
			t.Fatal("journal changed")
		}
		if err = op.Commit(ctx, read); err != nil {
			return err
		}
		backup, err := os.ReadFile(filepath.Join(s.dir, "plan.backup.json"))
		if err != nil {
			return err
		}
		if err = op.Commit(ctx, read); err != nil {
			return err
		}
		after, err := os.ReadFile(filepath.Join(s.dir, "plan.backup.json"))
		if err != nil {
			return err
		}
		if !bytes.Equal(backup, after) {
			t.Fatal("duplicate commit changed backup")
		}
		// Even a semantically unchanged, externally replaced journal must not close.
		if err = os.WriteFile(filepath.Join(s.dir, "operation.json"), append(read.raw, '\n'), 0600); err != nil {
			return err
		}
		if err = op.Close(read); !errors.Is(err, ErrConflict) {
			t.Fatalf("close modified journal: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOperationJournalRejectsCorruption(t *testing.T) {
	for _, kind := range []string{"digest", "trailing", "unknown-version", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := initialized(t)
			ctx := context.Background()
			err := s.WithOperation(ctx, func(op *Operation) error {
				base, err := op.Load()
				if err != nil {
					return err
				}
				next := base.Plan
				if _, err = next.Add("prepared", "", task.Position{}); err != nil {
					return err
				}
				j, err := op.Prepare(ctx, base, next, map[string]string{"id": "operation"})
				if err != nil {
					return err
				}
				data := append([]byte{}, j.raw...)
				switch kind {
				case "digest":
					data = bytes.Replace(data, []byte(`"id": "operation"`), []byte(`"id": "changed"`), 1)
				case "trailing":
					data = append(data, []byte(` {}`)...)
				case "unknown-version":
					data = bytes.Replace(data, []byte(`"version": 1`), []byte(`"version": 2`), 1)
				case "truncated":
					data = data[:len(data)/2]
				}
				if err = os.WriteFile(filepath.Join(s.dir, "operation.json"), data, 0600); err != nil {
					return err
				}
				if _, err = op.Journal(); err == nil {
					t.Fatal("corrupt journal accepted")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

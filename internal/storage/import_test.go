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

func TestImportPreservesSnapshotRevisionOnlyInEmptyPlan(t *testing.T) {
	t.Parallel()
	s, c := initialized(t)
	ctx := context.Background()
	base := snapshot(t, s)
	next, err := task.NewPlan("release")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(ctx, base, next); err == nil {
		t.Fatal("ordinary save accepted changed settings with unchanged revision")
	}
	if changed, err := s.Import(ctx, base, next); err != nil || !changed {
		t.Fatalf("empty import: %v %v", changed, err)
	}
	base = snapshot(t, s)
	if base.Plan.Revision != 0 || base.Plan.TargetBranch != "release" {
		t.Fatalf("snapshot altered: %+v", base.Plan)
	}
	next = added(t, base, "Retained")
	if _, err = s.Save(ctx, base, next); err != nil {
		t.Fatal(err)
	}
	base = snapshot(t, s)
	before := read(t, filepath.Join(c.Dir, ".git-task", "plan.json"))
	if _, err = s.Import(ctx, base, base.Plan); err == nil {
		t.Fatal("import accepted nonempty destination")
	}
	if !bytes.Equal(before, read(t, filepath.Join(c.Dir, ".git-task", "plan.json"))) {
		t.Fatal("nonempty destination changed")
	}
}

func TestImportRechecksVersionUnderLock(t *testing.T) {
	t.Parallel()
	s, c := initialized(t)
	base := snapshot(t, s)
	next := added(t, base, "Import")
	path := filepath.Join(c.Dir, ".git-task", "plan.json")
	original := read(t, path)
	foreign := append(append([]byte(nil), original...), '\n')
	s.checkpoint = func(name string) error {
		if name == "candidate" {
			put(t, path, foreign)
		}
		return nil
	}
	if _, err := s.Import(context.Background(), base, next); !errors.Is(err, ErrConflict) {
		t.Fatalf("external conflict: %v", err)
	}
	if !bytes.Equal(foreign, read(t, path)) {
		t.Fatal("foreign edit overwritten")
	}
}

func TestImportInterruptedBeforeReplacementPreservesPlan(t *testing.T) {
	t.Parallel()
	s, c := initialized(t)
	base := snapshot(t, s)
	next := added(t, base, "Import")
	path := filepath.Join(c.Dir, ".git-task", "plan.json")
	before := read(t, path)
	injected := errors.New("interrupted before replace")
	s.checkpoint = func(name string) error {
		if name == "backup" {
			return injected
		}
		return nil
	}
	if _, err := s.Import(context.Background(), base, next); !errors.Is(err, injected) {
		t.Fatalf("injection: %v", err)
	}
	if !bytes.Equal(before, read(t, path)) || !bytes.Equal(before, read(t, filepath.Join(c.Dir, ".git-task", "plan.backup.json"))) {
		t.Fatal("interrupted import partially installed")
	}
	if _, err := os.Stat(filepath.Join(c.Dir, ".git-task", "write.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock not released: %v", err)
	}
	if _, err := s.Load(context.Background()); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("interruption not diagnosed: %v", err)
	}
}

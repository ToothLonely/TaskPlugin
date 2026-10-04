package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestPlanChangesPreserveDirtyCodeAndIdentity(t *testing.T) {
	p, c := startFixture(t)
	ctx := context.Background()
	started, err := p.Start(ctx, StartOptions{Branch: "work"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, "file.txt")
	if err := os.WriteFile(path, []byte("staged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, c, "add", "file.txt")
	if err := os.WriteFile(path, []byte("unstaged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	head := testrepo.Run(t, c, "rev-parse", "HEAD")
	refs := testrepo.Run(t, c, "show-ref")
	index := testrepo.Run(t, c, "ls-files", "--stage", "-z")
	status := testrepo.Run(t, c, "status", "--porcelain=v1", "-z")
	preview, err := p.PrepareEdit(ctx, started.ID)
	if err != nil {
		t.Fatal(err)
	}
	title, description := "Новое название 🙂", ""
	edited, changed, err := p.ApplyEdit(ctx, preview, task.EditOptions{Title: &title, Description: &description})
	if err != nil || !changed || edited.ID != started.ID || edited.Number != started.Number || !reflect.DeepEqual(edited.ActiveAttempt, started.ActiveAttempt) {
		t.Fatalf("edit lost identity: %+v %v %v", edited, changed, err)
	}
	if _, changed, err := p.Move(ctx, started.ID, task.Position{End: true}); err != nil || !changed {
		t.Fatalf("move: %v %v", changed, err)
	}
	paused, changed, err := p.Pause(ctx, started.ID)
	if err != nil || !changed || paused.Status != task.Paused || paused.ActiveAttempt.Status != task.Paused || !sameAttemptBinding(paused.ActiveAttempt, started.ActiveAttempt) {
		t.Fatalf("pause: %+v %v", paused, err)
	}
	before := planBytes(t, c)
	backup, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := p.Pause(ctx, started.ID); err != nil || changed || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatalf("repeat pause: %v %v", changed, err)
	}
	if _, err := p.Resume(ctx, started.ID); err == nil || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatalf("dirty resume: %v", err)
	}
	backupAfter, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
	if err != nil || !bytes.Equal(backup, backupAfter) {
		t.Fatalf("no-op changed backup: %v", err)
	}
	archived, changed, err := p.Archive(ctx, started.ID)
	if err != nil || !changed || archived.Status != task.Archived || !reflect.DeepEqual(archived.ActiveAttempt, paused.ActiveAttempt) {
		t.Fatalf("archive lost attempt: %+v %v", archived, err)
	}
	before = planBytes(t, c)
	if _, changed, err := p.Archive(ctx, started.ID); err != nil || changed || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatalf("repeat archive: %v %v", changed, err)
	}
	if !bytes.Equal(head, testrepo.Run(t, c, "rev-parse", "HEAD")) || !bytes.Equal(refs, testrepo.Run(t, c, "show-ref")) || !bytes.Equal(index, testrepo.Run(t, c, "ls-files", "--stage", "-z")) || !bytes.Equal(status, testrepo.Run(t, c, "status", "--porcelain=v1", "-z")) {
		t.Fatal("plan operation changed Git or dirty code")
	}
	if _, err := p.Attach(ctx, "work", "task-002", false); err != nil {
		t.Fatalf("archived branch was not released: %v", err)
	}
	if _, _, err := p.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := savedTask(t, p, started.ID); !reflect.DeepEqual(got.ActiveAttempt, archived.ActiveAttempt) || got.Status != task.Archived {
		t.Fatalf("sync altered archive: %+v", got)
	}
}

func TestEditSnapshotConflictsAndInvalidChanges(t *testing.T) {
	p, c := startFixture(t)
	ctx := context.Background()
	preview, err := p.PrepareEdit(ctx, "task-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Move(ctx, "task-002", task.Position{End: true}); err != nil {
		t.Fatal(err)
	}
	before := planBytes(t, c)
	title := "Не потерять"
	if _, _, err := p.ApplyEdit(ctx, preview, task.EditOptions{Title: &title}); !errors.Is(err, storage.ErrConflict) || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatalf("stale editor overwrote plan: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(c.Dir, ".git-task", "pending-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("conflict evidence missing: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil || !bytes.Contains(data, []byte(title)) {
		t.Fatalf("prepared edit lost: %v", err)
	}
	p2, c2 := startFixture(t)
	before = planBytes(t, c2)
	preview, err = p2.PrepareEdit(ctx, "task-001")
	if err != nil {
		t.Fatal(err)
	}
	blank := " \t"
	if _, _, err := p2.ApplyEdit(ctx, preview, task.EditOptions{Title: &blank}); !errors.Is(err, task.ErrInvalid) || !bytes.Equal(before, planBytes(t, c2)) {
		t.Fatalf("invalid edit mutated plan: %v", err)
	}
	for _, pos := range []task.Position{{}, {After: "task-001"}, {Before: "missing"}, {End: true, After: "task-002"}} {
		if _, _, err := p2.Move(ctx, "task-001", pos); err == nil || !bytes.Equal(before, planBytes(t, c2)) {
			t.Fatalf("invalid move mutated plan: %+v %v", pos, err)
		}
	}
	if _, _, err := p2.Pause(ctx, "task-001"); !errors.Is(err, task.ErrTransition) || !bytes.Equal(before, planBytes(t, c2)) {
		t.Fatalf("pause todo: %v", err)
	}
	if _, _, err := p2.ApplyEdit(ctx, preview, task.EditOptions{}); err != nil || !bytes.Equal(before, planBytes(t, c2)) {
		t.Fatalf("edit no-op: %v", err)
	}
}

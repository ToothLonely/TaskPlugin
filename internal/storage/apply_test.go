package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func editedPlan(t *testing.T, s *Store) ([]byte, []byte) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.Init(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	base := snapshot(t, s)
	next := base.Plan
	if _, err := next.Add("Existing", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, base, next); err != nil {
		t.Fatal(err)
	}
	backup := read(t, filepath.Join(s.dir, "plan.backup.json"))
	input := []byte(`{"format":"git-task","schema_version":3,"revision":1,"last_event":0,"target_branch":"main","tasks":[{"title":"Edited"},{"id":"fixed","title":"Fixed"}]}`)
	put(t, filepath.Join(s.dir, "plan.json"), input)
	return input, backup
}

func TestApplyEditedPlanPreservesInputBackupAndRepeat(t *testing.T) {
	s, _ := fixture(t)
	input, backup := editedPlan(t, s)
	ctx := context.Background()
	if _, err := s.InitTemplate(ctx, false); err == nil {
		t.Fatal("plain init filled incomplete plan")
	}
	if _, err := s.Load(ctx); err == nil {
		t.Fatal("Load filled incomplete plan")
	}
	result, err := s.InitTemplate(ctx, true)
	if err != nil || !result.Applied {
		t.Fatalf("apply: %+v %v", result, err)
	}
	archives, err := filepath.Glob(filepath.Join(s.dir, "conflict-apply-*.json"))
	if err != nil || len(archives) != 1 || !bytes.Equal(input, read(t, archives[0])) {
		t.Fatalf("archive: %v %v", archives, err)
	}
	if !bytes.Equal(backup, read(t, filepath.Join(s.dir, "plan.backup.json"))) {
		t.Fatal("valid backup replaced with partial input")
	}
	ready := read(t, filepath.Join(s.dir, "plan.json"))
	if result, err := s.InitTemplate(ctx, true); err != nil || result != (TemplateResult{}) {
		t.Fatalf("repeat: %+v %v", result, err)
	}
	if !bytes.Equal(ready, read(t, filepath.Join(s.dir, "plan.json"))) || !bytes.Equal(backup, read(t, filepath.Join(s.dir, "plan.backup.json"))) {
		t.Fatal("repeat wrote files")
	}
	inspection, err := s.Inspect(ctx)
	if err != nil || inspection.Plan == nil {
		t.Fatalf("doctor: %+v %v", inspection, err)
	}
	for _, finding := range inspection.Findings {
		if finding.Problem {
			t.Fatalf("doctor: %+v", finding)
		}
	}
	put(t, filepath.Join(s.dir, "plan.json"), []byte(`{"format":"git-task","schema_version":3,"broken":true}`))
	repair, err := s.PrepareRepair(ctx, "restore-backup")
	if err != nil || repair.Plan == nil {
		t.Fatalf("repair: %+v %v", repair, err)
	}
	if _, err := s.ApplyRepair(ctx, repair); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, read(t, filepath.Join(s.dir, "plan.json"))) || !bytes.Equal(input, read(t, archives[0])) {
		t.Fatal("repair lost the valid backup or manual input")
	}
}

func TestApplyEditedPlanWithoutBackup(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	if _, err := s.Init(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(s.dir, "plan.json"), []byte(`{"format":"git-task","schema_version":3,"target_branch":"main","tasks":[{"title":"New"}]}`))
	if _, err := s.InitTemplate(ctx, true); err != nil {
		t.Fatal(err)
	}
	if len(snapshot(t, s).Plan.Tasks) != 1 {
		t.Fatal("task not created")
	}
}

func TestApplyEditedPlanSafetyAndInterruption(t *testing.T) {
	for _, kind := range []string{"candidate", "backup", "installed", "external", "locked", "operation", "tracked", "foreign", "invalid", "unsafe"} {
		t.Run(kind, func(t *testing.T) {
			s, c := fixture(t)
			input, backup := editedPlan(t, s)
			path := filepath.Join(s.dir, "plan.json")
			external := append([]byte(" \n"), input...)
			switch kind {
			case "locked":
				put(t, filepath.Join(s.dir, "write.lock"), []byte("owner"))
			case "operation":
				put(t, filepath.Join(s.dir, "operation.json"), []byte("pending operation"))
			case "tracked":
				testrepo.Run(t, c, "add", "-f", ".git-task/plan.json")
			case "foreign":
				put(t, filepath.Join(s.dir, "foreign.txt"), []byte("keep"))
			case "invalid":
				input = bytes.Replace(input, []byte(`"title":"Edited"`), []byte(`"title":"Edited","id":null`), 1)
				put(t, path, input)
			case "unsafe":
				if err := os.Mkdir(filepath.Join(s.dir, "foreign-directory"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			s.checkpoint = func(point string) error {
				if kind == "external" && point == "backup" {
					put(t, path, external)
				}
				if point == kind {
					return errors.New("interrupted")
				}
				return nil
			}
			_, err := s.InitTemplate(context.Background(), true)
			if err == nil {
				t.Fatal("unsafe/interrupted apply accepted")
			}
			if !bytes.Equal(backup, read(t, filepath.Join(s.dir, "plan.backup.json"))) {
				t.Fatal("backup changed")
			}
			switch kind {
			case "external":
				if !errors.Is(err, ErrConflict) || !bytes.Equal(external, read(t, path)) {
					t.Fatalf("external edit lost: %v", err)
				}
			case "installed":
				s.checkpoint = nil
				ready := read(t, path)
				if _, err := s.InitTemplate(context.Background(), true); err != nil || !bytes.Equal(ready, read(t, path)) {
					t.Fatalf("retry: %v", err)
				}
			default:
				if !bytes.Equal(input, read(t, path)) {
					t.Fatal("input changed")
				}
			}
		})
	}
}

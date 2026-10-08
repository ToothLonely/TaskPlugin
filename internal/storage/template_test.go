package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestTemplateCreationApplyAndBackup(t *testing.T) {
	ctx := context.Background()
	s, c := fixture(t)
	result, err := s.InitTemplate(ctx, false)
	if err != nil || !result.Created || !result.Draft || result.Applied {
		t.Fatalf("init: %+v %v", result, err)
	}
	path := filepath.Join(s.dir, "plan.json")
	initial := read(t, path)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(initial, &fields); err != nil || len(fields) != 2 || string(fields["target_branch"]) != `""` || string(fields["tasks"]) != `[]` {
		t.Fatalf("unexpected initial fields: %s %v", initial, err)
	}
	if _, err := s.Load(ctx); !errors.Is(err, ErrTemplate) {
		t.Fatalf("draft loaded as ready: %v", err)
	}
	if _, err := s.InitTemplate(ctx, true); err == nil || !bytes.Equal(initial, read(t, path)) {
		t.Fatalf("unfinished draft changed: %v", err)
	}
	filled := []byte("{\n\"target_branch\":\"main\",\"tasks\":[{\"title\":\"First\",\"id\":\"001\"},{\"title\":\"Second\"}]}\n")
	put(t, path, filled)
	if result, err := s.InitTemplate(ctx, false); err != nil || !result.Draft || result.Created || !bytes.Equal(filled, read(t, path)) {
		t.Fatalf("repeat init changed template: %+v %v", result, err)
	}
	result, err = s.InitTemplate(ctx, true)
	if err != nil || !result.Applied || result.Draft || result.Created {
		t.Fatalf("apply: %+v %v", result, err)
	}
	plan := snapshot(t, s).Plan
	if len(plan.Tasks) != 2 || plan.Tasks[0].ID != "001" || !reflect.DeepEqual(plan.Order, []string{"001", plan.Tasks[1].ID}) || len(plan.Tasks[1].ID) != 32 {
		t.Fatalf("invalid applied plan: %+v", plan)
	}
	templateBackup := filepath.Join(s.dir, "plan.template.json")
	if !bytes.Equal(filled, read(t, templateBackup)) {
		t.Fatal("original template not preserved")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "plan.backup.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("draft entered ready-plan backup: %v", err)
	}
	inspection, err := s.Inspect(ctx)
	if err != nil || inspection.Plan == nil {
		t.Fatalf("inspection: %+v %v", inspection, err)
	}
	for _, f := range inspection.Findings {
		if f.Problem {
			t.Fatalf("valid storage diagnosed as damaged: %+v", f)
		}
	}
	base := snapshot(t, s)
	next := base.Plan
	added, err := next.Add("Third", "", task.Position{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, base, next); err != nil {
		t.Fatal(err)
	}
	if next.Order[len(next.Order)-1] != added.ID {
		t.Fatalf("addition reordered template tasks: %v", next.Order)
	}
	ready, backup := read(t, path), read(t, filepath.Join(s.dir, "plan.backup.json"))
	for _, apply := range []bool{false, true} {
		result, err := s.InitTemplate(ctx, apply)
		if err != nil || result != (TemplateResult{}) || !bytes.Equal(ready, read(t, path)) || !bytes.Equal(backup, read(t, filepath.Join(s.dir, "plan.backup.json"))) || !bytes.Equal(filled, read(t, templateBackup)) {
			t.Fatalf("ready plan changed: %+v %v", result, err)
		}
	}
	if status := testrepo.Run(t, c, "status", "--porcelain"); len(status) != 0 {
		t.Fatalf("metadata visible in code status: %s", status)
	}
}

func TestTemplateInvalidInputsPreserveFiles(t *testing.T) {
	for _, input := range []string{
		`{"tasks":[]}`, `{"target_branch":"main"}`,
		`{"target_branch":"","tasks":[]}`,
		`{"target_branch":"main","tasks":[{"title":""}]}`,
		`{"target_branch":"main","tasks":[{"title":"a","status":"done"}]}`,
		`{"target_branch":"main","tasks":[],"order":[]}`,
		`{"format":"foreign","target_branch":"main","tasks":[]}`,
		`{"target_branch":"main","tasks":[],"tasks":[]}`,
		`{"format":"git-task","schema_version":3,"target_branch":"main","tasks":[]}`,
		`not JSON`,
	} {
		t.Run(input, func(t *testing.T) {
			s, _ := fixture(t)
			ctx := context.Background()
			if _, err := s.InitTemplate(ctx, false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.dir, "plan.json")
			put(t, path, []byte(input))
			exclude := read(t, s.excludePath)
			if _, err := s.InitTemplate(ctx, true); err == nil {
				t.Fatal("invalid draft applied")
			}
			if !bytes.Equal(read(t, path), []byte(input)) || !bytes.Equal(exclude, read(t, s.excludePath)) {
				t.Fatal("invalid input changed")
			}
			for _, name := range []string{"write.lock", "plan.backup.json", "plan.template.json"} {
				if _, err := os.Stat(filepath.Join(s.dir, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unexpected artifact %s: %v", name, err)
				}
			}
		})
	}
}

func TestTemplateApplyConflictAndInterruption(t *testing.T) {
	for _, point := range []string{"candidate", "backup", "installed", "external"} {
		t.Run(point, func(t *testing.T) {
			s, _ := fixture(t)
			ctx := context.Background()
			if _, err := s.InitTemplate(ctx, false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.dir, "plan.json")
			original := []byte(`{"target_branch":"main","tasks":[{"title":"Original"}]}`)
			external := []byte(`{"target_branch":"main","tasks":[{"title":"External"}]}`)
			put(t, path, original)
			s.checkpoint = func(name string) error {
				if point == "external" && name == "backup" {
					put(t, path, external)
				}
				if point == name {
					return errors.New("interrupted")
				}
				return nil
			}
			_, err := s.InitTemplate(ctx, true)
			if err == nil {
				t.Fatal("injected failure ignored")
			}
			switch point {
			case "external":
				if !errors.Is(err, ErrConflict) || !bytes.Equal(external, read(t, path)) {
					t.Fatalf("external edit lost: %v", err)
				}
			case "installed":
				if _, err := decode(read(t, path)); err != nil {
					t.Fatalf("installed plan incomplete: %v", err)
				}
			default:
				if !bytes.Equal(original, read(t, path)) {
					t.Fatal("original changed before publication")
				}
			}
			if point != "candidate" && !bytes.Equal(original, read(t, filepath.Join(s.dir, "plan.template.json"))) {
				t.Fatal("template backup lost")
			}
			if _, err := os.Stat(filepath.Join(s.dir, "write.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("lock remained: %v", err)
			}
		})
	}
}

func TestTemplateRejectsTrackedPathsAndHistory(t *testing.T) {
	for _, kind := range []string{"tracked", "history", "locked"} {
		t.Run(kind, func(t *testing.T) {
			s, c := fixture(t)
			ctx := context.Background()
			if _, err := s.InitTemplate(ctx, false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.dir, "plan.json")
			input := []byte(`{"target_branch":"main","tasks":[{"title":"Draft"}]}`)
			put(t, path, input)
			switch kind {
			case "tracked":
				testrepo.Run(t, c, "add", "-f", ".git-task/plan.json")
			case "history":
				plan, err := task.NewPlan("main")
				if err != nil {
					t.Fatal(err)
				}
				data, err := encode(plan)
				if err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(s.dir, "plan.backup.json"), data)
			case "locked":
				put(t, filepath.Join(s.dir, "write.lock"), []byte("keep owner"))
			}
			exclude := read(t, s.excludePath)
			if _, err := s.InitTemplate(ctx, true); err == nil || !bytes.Equal(input, read(t, path)) || !bytes.Equal(exclude, read(t, s.excludePath)) {
				t.Fatalf("unsafe apply changed files: %v", err)
			}
		})
	}
}

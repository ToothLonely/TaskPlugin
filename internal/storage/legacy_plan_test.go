package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git-task/internal/task"
)

func TestLegacyStorageNoOpMutationAndRepair(t *testing.T) {
	for _, done := range []bool{false, true} {
		for _, tail := range []string{"absent", `""`, `"existing"`} {
			t.Run(tail+"/"+map[bool]string{false: "todo", true: "done"}[done], func(t *testing.T) {
				s, _ := fixture(t)
				ctx := context.Background()
				if _, err := s.Init(ctx, "main"); err != nil {
					t.Fatal(err)
				}
				plan, err := task.NewPlan("main")
				if err != nil {
					t.Fatal(err)
				}
				plan.Tasks = []task.Task{{ID: "existing", Title: "Existing", Status: task.Todo, Revision: 1}}
				plan.Order = []string{"existing"}
				if done {
					if _, err := plan.CompleteManual("existing", "manual-attempt", "", time.Now().UTC()); err != nil {
						t.Fatal(err)
					}
				}
				data, err := encode(plan)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(data, &fields); err != nil {
					t.Fatal(err)
				}
				if tail != "absent" {
					fields["insertion_tail"] = json.RawMessage(tail)
				}
				original, err := json.MarshalIndent(fields, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				path, backup := filepath.Join(s.dir, "plan.json"), filepath.Join(s.dir, "plan.backup.json")
				put(t, path, original)
				put(t, backup, original)
				if created, err := s.Init(ctx, "main"); err != nil || created {
					t.Fatalf("legacy init --target: %v %v", created, err)
				}
				for _, apply := range []bool{false, true} {
					if result, err := s.InitTemplate(ctx, apply); err != nil || result.Applied {
						t.Fatalf("legacy init/apply: %+v %v", result, err)
					}
				}
				base := snapshot(t, s)
				if changed, err := s.Save(ctx, base, base.Plan); err != nil || changed || !bytes.Equal(original, read(t, path)) || !bytes.Equal(original, read(t, backup)) {
					t.Fatalf("legacy no-op: %v %v", changed, err)
				}
				next := base.Plan
				added, err := next.Add("New", "", task.Position{})
				if err != nil || next.Order[len(next.Order)-1] != added.ID {
					t.Fatalf("legacy add: %v", err)
				}
				if changed, err := s.Save(ctx, base, next); err != nil || !changed {
					t.Fatalf("legacy save: %v %v", changed, err)
				}
				if bytes.Contains(read(t, path), []byte("insertion_tail")) || !bytes.Equal(original, read(t, backup)) {
					t.Fatal("new plan emitted cursor or lost exact legacy backup")
				}
				put(t, path, []byte(`{"format":"git-task","schema_version":3,"tasks":`))
				repair, err := s.PrepareRepair(ctx, "restore-backup")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.ApplyRepair(ctx, repair); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(original, read(t, path)) || !bytes.Equal(original, read(t, backup)) {
					t.Fatal("repair changed legacy bytes")
				}
				if _, err := s.Load(ctx); err != nil {
					t.Fatal(err)
				}
				inspection, err := s.Inspect(ctx)
				if err != nil || inspection.Plan == nil {
					t.Fatalf("legacy doctor: %+v %v", inspection, err)
				}
				if _, err := os.Stat(backup); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

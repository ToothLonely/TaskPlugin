package storage

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestMigrationInterruptionPreservesSourceAndDiagnosesRetry(t *testing.T) {
	for _, boundary := range []string{"candidate", "backup", "installed"} {
		t.Run(boundary, func(t *testing.T) {
			s, _ := initialized(t)
			ctx := context.Background()
			legacy := []byte(`{"format":"git-task","schema_version":1,"revision":3,"target_branch":"main","order":["old-task"],"tasks":[{"id":"old-task","number":"T-003","title":"Migrated work","revision":2,"status":"done","attempts":[{"id":"old-attempt","completion":{"event":1,"source":"manual","target_branch":"main"}}]}],"last_event":1,"insertion_tail":"old-task"}`)
			put(t, filepath.Join(s.dir, "plan.json"), legacy)
			interrupted := errors.New("migration interrupted")
			s.checkpoint = func(point string) error {
				if point == boundary {
					return interrupted
				}
				return nil
			}
			if _, err := s.Migrate(ctx); !errors.Is(err, interrupted) {
				t.Fatalf("missing interruption: %v", err)
			}
			if !bytes.Equal(read(t, filepath.Join(s.dir, "plan.schema-1.json")), legacy) {
				t.Fatal("migration source changed")
			}
			current := read(t, filepath.Join(s.dir, "plan.json"))
			s.checkpoint = nil
			if boundary != "installed" {
				if !bytes.Equal(current, legacy) {
					t.Fatal("uninstalled plan changed")
				}
				if _, err := s.Migrate(ctx); !errors.Is(err, ErrInterrupted) {
					t.Fatalf("pending candidate silently retried: %v", err)
				}
			} else {
				snapshot, err := s.Load(ctx)
				if err != nil {
					t.Fatal(err)
				}
				item, err := snapshot.Plan.FindID("old-task")
				if err != nil || len(item.Attempts) != 1 || item.Attempts[0].ID != "old-attempt" || item.Attempts[0].Author != "" {
					t.Fatalf("migration lost identity/history: %+v %v", item, err)
				}
				if changed, err := s.Migrate(ctx); changed || err != nil {
					t.Fatalf("installed migration replay: %t %v", changed, err)
				}
			}
			if !bytes.Equal(current, read(t, filepath.Join(s.dir, "plan.json"))) {
				t.Fatal("retry rewrote plan")
			}
			if boundary != "candidate" && !bytes.Equal(read(t, filepath.Join(s.dir, "plan.backup.json")), legacy) {
				t.Fatal("legacy backup changed")
			}
		})
	}
}

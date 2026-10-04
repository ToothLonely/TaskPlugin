package storage

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-task/internal/task"
)

func TestCompletionWriteFailuresKeepWholeAttempt(t *testing.T) {
	for _, point := range []string{"candidate", "backup", "installed"} {
		t.Run(point, func(t *testing.T) {
			t.Parallel()
			s, _ := initialized(t)
			ctx := context.Background()
			base := snapshot(t, s)
			plan := added(t, base, "work")
			now := time.Now().UTC()
			oid := strings.Repeat("a", 40)
			if _, err := plan.Start("task-001", task.Attempt{ID: "attempt", Branch: "work", OriginalBranch: "work", TargetBranch: "main", BaseCommit: oid, StartedAt: &now}, false); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Save(ctx, base, plan); err != nil {
				t.Fatal(err)
			}
			base = snapshot(t, s)
			before := read(t, filepath.Join(s.dir, "plan.json"))
			next := base.Plan
			if _, err := next.Complete("task-001", "attempt", task.Completion{Source: task.Merge, TargetBranch: "main", MergeKind: task.FastForward, Commit: oid, WorkCommit: oid, ObservedAt: &now}); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("injected write failure")
			s.checkpoint = func(actual string) error {
				if actual == point {
					return failure
				}
				return nil
			}
			if _, err := s.Save(ctx, base, next); !errors.Is(err, failure) {
				t.Fatalf("save: %v", err)
			}
			data := read(t, filepath.Join(s.dir, "plan.json"))
			actual, err := decode(data)
			if err != nil {
				t.Fatal(err)
			}
			item := actual.Tasks[0]
			if point == "installed" {
				if item.Status != task.Done || item.ActiveAttempt != nil || len(item.Attempts) != 1 || item.Attempts[0].ID != "attempt" {
					t.Fatalf("partial installed result: %+v", item)
				}
				s.checkpoint = nil
				changed, err := s.Save(ctx, snapshot(t, s), actual)
				if changed || err != nil || !bytes.Equal(data, read(t, filepath.Join(s.dir, "plan.json"))) {
					t.Fatalf("installed retry: %v %v", changed, err)
				}
			} else if item.Status != task.Active || item.ActiveAttempt == nil || len(item.Attempts) != 1 || !bytes.Equal(before, data) {
				t.Fatalf("partial pre-install result: %+v", item)
			}
		})
	}
}

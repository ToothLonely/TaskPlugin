package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"git-task/internal/testrepo"
)

func TestA04SafetyChangesAfterLoadRefuseSave(t *testing.T) {
	for _, scenario := range []string{"tracked-index", "tracked-head-staged-deletion", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			s, c := initialized(t)
			base := snapshot(t, s)
			next := added(t, base, "must not be saved")
			path := filepath.Join(s.dir, "plan.json")
			before := read(t, path)
			switch scenario {
			case "tracked-index", "tracked-head-staged-deletion":
				testrepo.Run(t, c, "add", "--force", "--", ".git-task/plan.json")
				if scenario == "tracked-head-staged-deletion" {
					testrepo.Commit(t, c)
					testrepo.Run(t, c, "rm", "--cached", "--", ".git-task/plan.json")
				}
			case "partial":
				testrepo.Run(t, c, "config", "--local", "remote.origin.promisor", "true")
			}
			if _, err := s.Save(context.Background(), base, next); err == nil {
				t.Fatal("save accepted changed repository safety conditions")
			}
			if !bytes.Equal(before, read(t, path)) {
				t.Fatal("rejected save changed the plan")
			}
			if _, err := os.Stat(filepath.Join(s.dir, "plan.backup.json")); !os.IsNotExist(err) {
				t.Fatalf("rejected save created a backup: %v", err)
			}
		})
	}
}

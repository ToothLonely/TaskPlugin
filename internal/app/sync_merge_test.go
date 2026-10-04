package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"git-task/internal/git"
	"git-task/internal/task"
)

func postMergeFixture(t *testing.T) (*Plans, *git.Client, task.Task, string) {
	t.Helper()
	p, c := startFixture(t)
	ctx := context.Background()
	if _, err := p.Start(ctx, StartOptions{Branch: "work"}); err != nil {
		t.Fatal(err)
	}
	commitFile(t, c, "work.txt", "own work\n")
	active := syncTask(t, p, "task-001")
	mergeBranch(t, c, "work", false)
	repo, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.GitDir, "MERGE_HEAD"), []byte(active.ActiveAttempt.Observation.Tip+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return p, c, active, repo.GitDir
}

func TestSyncAfterMergeUsesCommonTracking(t *testing.T) {
	t.Parallel()
	p, c, active, gitDir := postMergeFixture(t)
	ctx := context.Background()
	for _, sync := range []func(context.Context) (task.Plan, error){
		p.Status,
		func(ctx context.Context) (task.Plan, error) {
			plan, _, err := p.Sync(ctx)
			return plan, err
		},
	} {
		plan, err := sync(ctx)
		if err != nil {
			t.Fatal(err)
		}
		item, err := plan.FindID(active.ID)
		if err != nil {
			t.Fatal(err)
		}
		if item.Status != task.Active || !reflect.DeepEqual(item.ActiveAttempt, active.ActiveAttempt) || len(item.Attempts) != 1 || len(item.Warnings) != 1 || item.Warnings[0].Code != "git_in_progress" {
			t.Fatalf("ordinary sync bypassed guard: %+v", item)
		}
	}
	plan, changed, err := p.SyncAfterMerge(ctx)
	if err != nil || !changed {
		t.Fatalf("post-merge sync: changed=%v err=%v", changed, err)
	}
	done, err := plan.FindID(active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != task.Done || done.ActiveAttempt != nil || len(done.Attempts) != 1 || done.Attempts[0].ID != active.ActiveAttempt.ID || done.Attempts[0].Completion.MergeKind != task.MergeCommit || len(done.Warnings) != 0 {
		t.Fatalf("completion: %+v", done)
	}
	marker, err := os.ReadFile(filepath.Join(gitDir, "MERGE_HEAD"))
	if err != nil || string(marker) != active.ActiveAttempt.Observation.Tip+"\n" {
		t.Fatalf("Git state changed: %q %v", marker, err)
	}
	before := planBytes(t, c)
	if _, changed, err := p.SyncAfterMerge(ctx); err != nil || changed || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatalf("duplicate completion: changed=%v err=%v", changed, err)
	}
}

func TestSyncAfterMergeRechecksGitState(t *testing.T) {
	for _, scenario := range []string{"operation-started", "merge-proof-changed", "index-changed"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			p, c, active, gitDir := postMergeFixture(t)
			ctx := context.Background()
			before := planBytes(t, c)
			p.checkpoint = func(point string) error {
				if point != "sync-observed" {
					return nil
				}
				switch scenario {
				case "operation-started":
					return os.Mkdir(filepath.Join(gitDir, "rebase-merge"), 0700)
				case "merge-proof-changed":
					head, err := c.HeadState(ctx)
					if err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(gitDir, "ORIG_HEAD"), []byte(head.Commit+"\n"), 0600)
				case "index-changed":
					if err := os.WriteFile(filepath.Join(c.Dir, "staged.txt"), []byte("new change\n"), 0600); err != nil {
						return err
					}
					_, err := c.Run(ctx, "add", "--", "staged.txt")
					return err
				}
				return nil
			}
			if _, _, err := p.SyncAfterMerge(ctx); !errors.Is(err, git.ErrInProgress) {
				t.Fatalf("Git change accepted: %v", err)
			}
			if !bytes.Equal(before, planBytes(t, c)) || !reflect.DeepEqual(savedTask(t, p, active.ID), active) {
				t.Fatal("partial completion saved after Git changed")
			}
		})
	}
}

package git

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPostMergeProof(t *testing.T) {
	c := isolatedClient(t)
	ctx := context.Background()
	mustRun(t, c, "commit", "--allow-empty", "-m", "base")
	mustRun(t, c, "checkout", "-b", "work")
	mustRun(t, c, "commit", "--allow-empty", "-m", "work")
	tip, err := c.BranchCommit(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}
	mustRun(t, c, "checkout", "main")
	mustRun(t, c, "merge", "--no-ff", "-m", "integrate", "work")
	head, err := c.HeadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mergePath := filepath.Join(repo.GitDir, "MERGE_HEAD")
	originalPath := filepath.Join(repo.GitDir, "ORIG_HEAD")
	original, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	write := func(t *testing.T, path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mergeHeads := []byte(tip + "\n")
	write(t, mergePath, mergeHeads)
	t.Run("completed-merge", func(t *testing.T) {
		if err := c.CheckPostMerge(ctx); err != nil {
			t.Fatal(err)
		}
		if err := c.CheckStart(ctx, false); !errors.Is(err, ErrInProgress) {
			t.Fatalf("ordinary guard weakened: %v", err)
		}
		data, err := os.ReadFile(mergePath)
		if err != nil || !bytes.Equal(data, mergeHeads) {
			t.Fatalf("Git marker changed: %q %v", data, err)
		}
	})
	for _, name := range []string{"CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer", "BISECT_START", "index.lock"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(repo.GitDir, name)
			write(t, path, []byte("in progress\n"))
			t.Cleanup(func() {
				if err := os.Remove(path); err != nil {
					t.Error(err)
				}
			})
			if err := c.CheckPostMerge(ctx); !errors.Is(err, ErrInProgress) {
				t.Fatalf("unfinished operation accepted: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name, path string
		data       []byte
	}{
		{"wrong-merge-parent", mergePath, original},
		{"invalid-merge-head", mergePath, []byte("invalid\n")},
		{"missing-merge-newline", mergePath, []byte(tip)},
		{"oversized-merge-head", mergePath, bytes.Repeat([]byte(tip+"\n"), 6500)},
		{"invalid-original", originalPath, []byte("invalid\n")},
		{"old-merge-during-new-operation", originalPath, []byte(head.Commit + "\n")},
		{"missing-original", originalPath, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() {
				write(t, mergePath, mergeHeads)
				write(t, originalPath, original)
			})
			if tc.data == nil {
				if err := os.Remove(tc.path); err != nil {
					t.Fatal(err)
				}
			} else {
				write(t, tc.path, tc.data)
			}
			if err := c.CheckPostMerge(ctx); !errors.Is(err, ErrInProgress) {
				t.Fatalf("unproven merge accepted: %v", err)
			}
		})
	}
	t.Run("index-differs", func(t *testing.T) {
		write(t, filepath.Join(c.Dir, "staged.txt"), []byte("new change\n"))
		mustRun(t, c, "add", "--", "staged.txt")
		t.Cleanup(func() { mustRun(t, c, "update-index", "--force-remove", "--", "staged.txt") })
		if err := c.CheckPostMerge(ctx); !errors.Is(err, ErrInProgress) {
			t.Fatalf("unfinished index accepted: %v", err)
		}
	})
}

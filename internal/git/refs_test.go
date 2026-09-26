package git

import (
	"context"
	"errors"
	"testing"
)

func TestBranchRef(t *testing.T) {
	c := isolatedClient(t)
	for _, name := range []string{"feature", "feature/subtask", "задача", "feature;echo-marker", "feature&marker"} {
		ref, err := c.BranchRef(context.Background(), name)
		if err != nil || ref != "refs/heads/"+name {
			t.Fatalf("BranchRef(%q)=%q, %v", name, ref, err)
		}
	}
	for _, name := range []string{"", "-b", "--help", "refs/heads/topic", "@{-1}", "HEAD", "a b", "a..b", "a.lock", "a\nline", "a\x00b"} {
		ref, err := c.BranchRef(context.Background(), name)
		if ref != "" || !errors.Is(err, ErrInvalidBranch) {
			t.Fatalf("BranchRef(%q)=%q, %v", name, ref, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.BranchRef(ctx, "feature"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

package git

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestTrackingGitQueriesAndMissingObjects(t *testing.T) {
	t.Parallel()
	c := isolatedClient(t)
	ctx := context.Background()
	mustRun(t, c, "commit", "--allow-empty", "-m", "base")
	base, err := c.BranchCommit(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	mustRun(t, c, "checkout", "-b", "work")
	mustRun(t, c, "commit", "--allow-empty", "-m", "work")
	tip, _ := c.BranchCommit(ctx, "work")
	for _, tc := range []struct {
		a, b string
		want bool
	}{{base, tip, true}, {tip, base, false}} {
		got, err := c.IsAncestor(ctx, tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Fatalf("ancestry: %v %v", got, err)
		}
	}
	if found, err := c.HasObject(ctx, strings.Repeat("f", 40)); err != nil || found {
		t.Fatalf("missing object: %v %v", found, err)
	}
	if _, err := c.IsAncestor(ctx, "--all", tip); err == nil {
		t.Fatal("option accepted as commit")
	}
	if _, err := c.IsAncestor(ctx, strings.Repeat("f", 40), tip); err == nil {
		t.Fatal("Git error became negative ancestry")
	}
	work, err := c.OwnWork(ctx, tip, base, base)
	if err != nil || work != tip {
		t.Fatalf("work: %q %v", work, err)
	}
	log, err := c.BranchLog(ctx, "work")
	if err != nil || len(log.Entries) != 2 || log.Entries[0].New != base || log.Entries[1].Old != base || log.Entries[1].New != tip || len(log.Digest()) != 64 {
		t.Fatalf("log: %+v %v", log, err)
	}
	missing, err := c.BranchLog(ctx, "absent")
	if err != nil || missing.Digest() != "" {
		t.Fatalf("missing log: %+v %v", missing, err)
	}
	r := mustRun(t, c, "rev-parse", "--path-format=absolute", "--git-path", "logs/refs/heads/work")
	if err := os.WriteFile(outputLine(r.Stdout), []byte("broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BranchLog(ctx, "work"); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("damaged log: %v", err)
	}
	if err := os.WriteFile(outputLine(r.Stdout), []byte(strings.Repeat("x", 4*1024*1024+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BranchLog(ctx, "work"); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("budget: %v", err)
	}
}

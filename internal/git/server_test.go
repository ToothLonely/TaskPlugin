package git

import (
	"context"
	"strings"
	"testing"
)

func TestServerWorkRejectsEmptyOldAndSquashedTips(t *testing.T) {
	c := isolatedClient(t)
	runTest := func(t *testing.T, c *Client, args ...string) []byte { return mustRun(t, c, args...).Stdout }
	commitTest := func(t *testing.T, c *Client) { mustRun(t, c, "commit", "--allow-empty", "-m", "work") }
	ctx := context.Background()
	commitTest(t, c)
	base := strings.TrimSpace(string(runTest(t, c, "rev-parse", "HEAD")))
	runTest(t, c, "checkout", "-b", "work")
	commitTest(t, c)
	tip := strings.TrimSpace(string(runTest(t, c, "rev-parse", "HEAD")))
	runTest(t, c, "checkout", "main")
	mustRun(t, c, "commit", "--allow-empty", "-m", "distinct target work")
	before := strings.TrimSpace(string(runTest(t, c, "rev-parse", "HEAD")))
	if before == tip {
		t.Fatal("independent branch tips must differ")
	}
	if included, err := c.IsAncestor(ctx, tip, before); err != nil || included {
		t.Fatalf("fixture work is already integrated: %v", err)
	}
	if work, err := c.ServerWork(ctx, base, base, base, before); err != nil || work != "" {
		t.Fatalf("empty: %s %v", work, err)
	}
	if work, err := c.ServerWork(ctx, base, tip, base, before); err != nil || work != "" {
		t.Fatalf("unmerged/squash: %s %v", work, err)
	}
	runTest(t, c, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "merge", "--no-ff", "work", "-m", "merge")
	after := strings.TrimSpace(string(runTest(t, c, "rev-parse", "HEAD")))
	if work, err := c.ServerWork(ctx, base, tip, before, after); err != nil || work == "" {
		t.Fatalf("merge proof: %s %v", work, err)
	}
	if work, err := c.ServerWork(ctx, base, tip, after, after); err != nil || work != "" {
		t.Fatalf("old reachable: %s %v", work, err)
	}
}

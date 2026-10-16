package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRebaseStorageChecksBoundaryParent(t *testing.T) {
	c := isolatedClient(t)
	if err := os.Mkdir(filepath.Join(c.Dir, ".git-task"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), []byte("historical plan"), 0600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, c, "add", ".git-task/plan.json")
	mustRun(t, c, "commit", "-m", "tracked storage")
	tracked := outputLine(mustRun(t, c, "rev-parse", "HEAD").Stdout)
	mustRun(t, c, "rm", "--cached", ".git-task/plan.json")
	mustRun(t, c, "commit", "-m", "local removal")
	local := outputLine(mustRun(t, c, "rev-parse", "HEAD").Stdout)
	tree := outputLine(mustRun(t, c, "rev-parse", "HEAD^{tree}").Stdout)
	incoming := outputLine(mustRun(t, c, "commit-tree", tree, "-p", tracked, "-m", "remote removal").Stdout)
	ctx := context.Background()
	if err := c.CheckTreeStorage(ctx, local); err != nil {
		t.Fatal(err)
	}
	if err := c.CheckTreeStorage(ctx, incoming); err != nil {
		t.Fatal(err)
	}
	if err := c.checkRebaseStorage(ctx, local, incoming); err == nil {
		t.Fatal("deletion from a tracked boundary parent was accepted")
	}
	safeLocal := outputLine(mustRun(t, c, "commit-tree", tree, "-p", local, "-m", "safe local").Stdout)
	safeIncoming := outputLine(mustRun(t, c, "commit-tree", tree, "-p", local, "-m", "safe incoming").Stdout)
	if err := c.checkRebaseStorage(ctx, safeLocal, safeIncoming); err != nil {
		t.Fatalf("older shared storage history should not block safe replay: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := c.checkRebaseStorage(canceled, safeLocal, safeIncoming); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
}

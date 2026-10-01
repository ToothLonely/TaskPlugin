package git_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git-task/internal/testrepo"
)

func TestStatusHeadUsesCommitterDateAndPreservesHEAD(t *testing.T) {
	t.Parallel()
	c := testrepo.New(t)
	ctx := context.Background()
	head, err := c.StatusHead(ctx)
	if err != nil || !head.Unborn || head.Detached || head.Branch == nil || *head.Branch != "main" || head.Commit != nil || head.CommittedAt != nil {
		t.Fatalf("unborn HEAD: %+v, %v", head, err)
	}
	c.Env = append(c.Env, "GIT_AUTHOR_DATE=2020-01-02T03:04:05+02:00", "GIT_COMMITTER_DATE=2024-06-07T08:09:10-03:30")
	testrepo.Commit(t, c)
	oid := strings.TrimSpace(string(testrepo.Run(t, c, "rev-parse", "HEAD")))
	for _, detached := range []bool{false, true} {
		if detached {
			testrepo.Run(t, c, "checkout", "--detach")
		}
		before := testrepo.Run(t, c, "rev-parse", "HEAD")
		head, err = c.StatusHead(ctx)
		if err != nil || head.Unborn || head.Detached != detached || head.Commit == nil || *head.Commit != oid || head.CommittedAt == nil || head.CommittedAt.Format(time.RFC3339) != "2024-06-07T08:09:10-03:30" {
			t.Fatalf("committer date and HEAD (detached=%v): %+v, %v", detached, head, err)
		}
		if detached && head.Branch != nil || !detached && (head.Branch == nil || *head.Branch != "main") {
			t.Fatalf("branch (detached=%v): %+v", detached, head)
		}
		if string(before) != string(testrepo.Run(t, c, "rev-parse", "HEAD")) {
			t.Fatal("StatusHead changed HEAD")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.StatusHead(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not preserved: %v", err)
	}
}

package git

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type StatusHead struct {
	Branch      *string
	Commit      *string
	CommittedAt *time.Time
	Detached    bool
	Unborn      bool
}

func (c *Client) StatusHead(ctx context.Context) (StatusHead, error) {
	var head StatusHead
	r, err := c.Run(ctx, "symbolic-ref", "--quiet", "HEAD")
	if exitCode(err, 1) {
		head.Detached = true
	} else if err != nil {
		return head, err
	} else {
		ref := outputLine(r.Stdout)
		if !strings.HasPrefix(ref, "refs/heads/") {
			return head, fmt.Errorf("HEAD ссылается вне локальных веток: %q", ref)
		}
		branch := strings.TrimPrefix(ref, "refs/heads/")
		head.Branch = &branch
	}
	if head.Branch != nil {
		commit, err := c.BranchCommit(ctx, *head.Branch)
		if err != nil {
			return head, err
		}
		if commit == "" {
			head.Unborn = true
		} else {
			head.Commit = &commit
		}
	} else {
		r, err := c.Run(ctx, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil {
			return head, err
		}
		commit := outputLine(r.Stdout)
		head.Commit = &commit
	}
	if head.Commit != nil {
		r, err := c.Run(ctx, "show", "--no-patch", "--format=%cI", *head.Commit, "--")
		if err != nil {
			return head, err
		}
		committedAt, err := time.Parse(time.RFC3339, outputLine(r.Stdout))
		if err != nil {
			return head, fmt.Errorf("неверное время Git-коммита: %w", err)
		}
		head.CommittedAt = &committedAt
	}
	return head, nil
}

package git

import (
	"context"
	"fmt"
	"strings"
)

func (c *Client) HeadLogEntry(ctx context.Context) (string, error) {
	r, err := c.Run(ctx, "reflog", "show", "--max-count=1", "--format=%H%x09%gs", "HEAD")
	return strings.TrimSpace(string(r.Stdout)), err
}

func (c *Client) CheckoutResume(ctx context.Context, branch, operation string) error {
	if _, err := c.BranchRef(ctx, branch); err != nil {
		return err
	}
	if !hexID(operation) {
		return fmt.Errorf("неверная идентичность операции resume")
	}
	child := c.forOperation(operation)
	var env []string
	for _, entry := range child.Env {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "GIT_REFLOG_ACTION") {
			env = append(env, entry)
		}
	}
	child.Env = append(env, "GIT_REFLOG_ACTION=git-task resume "+operation)
	_, err := child.Run(ctx, "checkout", "--no-overwrite-ignore", branch, "--")
	return err
}

func (c *Client) OwnsResume(ctx context.Context, branch, commit, operation string) (bool, error) {
	if _, err := c.BranchRef(ctx, branch); err != nil {
		return false, err
	}
	if !hexID(commit) || !hexID(operation) {
		return false, fmt.Errorf("неверная идентичность операции resume")
	}
	head, err := c.HeadState(ctx)
	if err != nil || head != (Head{Ref: "refs/heads/" + branch, Commit: commit}) {
		return false, err
	}
	entry, err := c.HeadLogEntry(ctx)
	return entry == commit+"\tgit-task resume "+operation, err
}

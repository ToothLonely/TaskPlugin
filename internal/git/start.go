package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrInProgress = errors.New("незавершённая Git-операция")

// Head records both symbolic identity and commit, including detached HEAD.
type Head struct {
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
}

// HeadState captures the starting point without resolving it as a branch name.
func (c *Client) HeadState(ctx context.Context) (Head, error) {
	r, err := c.Run(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Head{}, err
	}
	h := Head{Commit: outputLine(r.Stdout)}
	r, err = c.Run(ctx, "symbolic-ref", "--quiet", "HEAD")
	if exitCode(err, 1) {
		return h, nil
	}
	if err != nil {
		return Head{}, err
	}
	h.Ref = outputLine(r.Stdout)
	return h, nil
}

// BranchCommit returns an empty commit only when the literal branch is absent.
func (c *Client) BranchCommit(ctx context.Context, branch string) (string, error) {
	ref, err := c.BranchRef(ctx, branch)
	if err != nil {
		return "", err
	}
	_, err = c.Run(ctx, "show-ref", "--verify", "--quiet", ref)
	if exitCode(err, 1) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	_, err = c.Run(ctx, "symbolic-ref", "--quiet", ref)
	if err == nil {
		return "", fmt.Errorf("символическая ветка не поддерживается: %s", branch)
	}
	if !exitCode(err, 1) {
		return "", err
	}
	r, err := c.Run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return outputLine(r.Stdout), err
}

// ResolveBase accepts literal refs/tags/local branches and unambiguous hex IDs,
// not revision expressions. Ref enumeration avoids Git's DWIM precedence.
func (c *Client) ResolveBase(ctx context.Context, input string) (commit, ref string, err error) {
	if input == "" || strings.HasPrefix(input, "-") || strings.ContainsRune(input, '\x00') {
		return "", "", fmt.Errorf("неверное основание %q", input)
	}
	r, err := c.Run(ctx, "for-each-ref", "--format=%(refname)")
	if err != nil {
		return "", "", err
	}
	var matches []string
	for _, name := range strings.Split(string(r.Stdout), "\n") {
		if name != "" && (name == input || name == "refs/heads/"+input || name == "refs/tags/"+input) {
			matches = append(matches, name)
		}
	}
	if hexID(input) {
		r, err = c.Run(ctx, "rev-parse", "--disambiguate="+input)
		if err != nil {
			return "", "", err
		}
		for _, oid := range strings.Fields(string(r.Stdout)) {
			matches = append(matches, oid)
		}
	}
	if len(matches) != 1 {
		return "", "", fmt.Errorf("основание %q отсутствует или неоднозначно", input)
	}
	resolved := matches[0]
	r, err = c.Run(ctx, "rev-parse", "--verify", "--end-of-options", resolved+"^{commit}")
	if err != nil {
		return "", "", err
	}
	if strings.HasPrefix(resolved, "refs/") {
		ref = resolved
	}
	return outputLine(r.Stdout), ref, nil
}

func hexID(s string) bool {
	if len(s) < 4 || len(s) > 64 {
		return false
	}
	for _, b := range s {
		if !(b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F') {
			return false
		}
	}
	return true
}

// CheckStart rejects unfinished Git operations and, for switching, dirty trees.
func (c *Client) CheckStart(ctx context.Context, switching bool) error {
	repo, err := c.Discover(ctx)
	if err != nil {
		return err
	}
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer", "BISECT_START", "index.lock"} {
		_, err := os.Lstat(filepath.Join(repo.GitDir, name))
		if err == nil {
			return fmt.Errorf("%w: %s", ErrInProgress, name)
		}
		if !os.IsNotExist(err) {
			return err
		}
	}
	unmerged, err := c.Run(ctx, "ls-files", "--unmerged", "-z")
	if err != nil {
		return err
	}
	if len(unmerged.Stdout) != 0 {
		return fmt.Errorf("%w: в индексе остались неразрешённые конфликты", ErrInProgress)
	}
	if switching {
		r, err := c.Run(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none")
		if err != nil {
			return err
		}
		if len(r.Stdout) != 0 {
			return fmt.Errorf("start требует чистого индекса и рабочего дерева")
		}
	}
	return nil
}

// CheckTreeStorage guards the destination tree before checkout, including case
// aliases on the actual filesystem (not the core.ignoreCase setting).
func (c *Client) CheckTreeStorage(ctx context.Context, commit string) error {
	if !hexID(commit) {
		return fmt.Errorf("ожидается commit ID")
	}
	r, err := c.Run(ctx, "ls-tree", "--full-tree", "--name-only", "-z", commit)
	if err != nil {
		return err
	}
	tracked, err := trackedStoragePath(c.Dir, r.Stdout)
	if err != nil {
		return err
	}
	if tracked {
		return fmt.Errorf("целевое дерево отслеживает .git-task")
	}
	return nil
}

// CreateBranch uses compare-and-create and a unique reflog marker. A racing
// creator cannot silently become this operation's branch. No rollback deletes it.
func (c *Client) CreateBranch(ctx context.Context, branch, commit, operation string) error {
	ref, err := c.BranchRef(ctx, branch)
	if err != nil {
		return err
	}
	if !hexID(commit) || !hexID(operation) {
		return fmt.Errorf("неверная идентичность операции")
	}
	child := c.forOperation(operation)
	_, err = child.Run(ctx, "update-ref", "--no-deref", "--create-reflog", "-m", "git-task start "+operation, ref, commit, strings.Repeat("0", len(commit)))
	return err
}

// OwnsBranch requires the unchanged creation entry, not merely a matching tip.
func (c *Client) OwnsBranch(ctx context.Context, branch, commit, operation string) (bool, error) {
	actual, err := c.BranchCommit(ctx, branch)
	if err != nil || actual != commit {
		return false, err
	}
	ref, err := c.BranchRef(ctx, branch)
	if err != nil {
		return false, err
	}
	r, err := c.Run(ctx, "reflog", "show", "--format=%H%x09%gs", ref)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(r.Stdout)) == commit+"\tgit-task start "+operation, nil
}

// Checkout protects ignored user files and passes a recursion marker only to
// this child process. Third-party hooks are not disabled.
func (c *Client) Checkout(ctx context.Context, branch, operation string) error {
	if _, err := c.BranchRef(ctx, branch); err != nil {
		return err
	}
	child := c.forOperation(operation)
	_, err := child.Run(ctx, "checkout", "--no-overwrite-ignore", branch, "--")
	return err
}

func (c *Client) forOperation(operation string) Client {
	child := *c
	child.Env = nil
	for _, entry := range c.environment() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "GIT_TASK_OPERATION") {
			child.Env = append(child.Env, entry)
		}
	}
	child.Env = append(child.Env, "GIT_TASK_OPERATION="+operation)
	return child
}

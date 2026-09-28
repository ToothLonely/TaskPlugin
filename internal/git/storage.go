package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckStorage rejects unsupported repositories and metadata tracked in either
// the index or HEAD. A staged deletion does not make the HEAD path safe.
func (c *Client) CheckStorage(ctx context.Context) error {
	repo, err := c.Discover(ctx)
	if err != nil {
		return err
	}
	if repo.GitDir != repo.CommonDir {
		return fmt.Errorf("дополнительные worktree не поддерживаются")
	}
	r, err := c.Run(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	worktrees := 0
	for _, record := range strings.Split(string(r.Stdout), "\x00") {
		if strings.HasPrefix(record, "worktree ") {
			worktrees++
		}
	}
	if worktrees != 1 {
		return fmt.Errorf("дополнительные worktree не поддерживаются")
	}
	r, err = c.Run(ctx, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return err
	}
	if outputLine(r.Stdout) != "false" {
		return fmt.Errorf("shallow-репозиторий не поддерживается")
	}
	r, err = c.Run(ctx, "config", "--get", "extensions.partialClone")
	if err != nil && !exitCode(err, 1) {
		return err
	}
	if len(r.Stdout) != 0 {
		return fmt.Errorf("partial clone не поддерживается")
	}
	r, err = c.Run(ctx, "config", "--type=bool", "--get-regexp", `^remote\..*\.promisor$`)
	if err != nil && !exitCode(err, 1) {
		return err
	}
	for _, line := range strings.Split(string(r.Stdout), "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), " true") {
			return fmt.Errorf("partial clone не поддерживается")
		}
	}
	r, err = c.Run(ctx, "ls-files", "-z", "--", ":(top,icase,literal).git-task")
	if err != nil {
		return err
	}
	tracked, err := trackedStoragePath(repo.Root, r.Stdout)
	if err != nil {
		return err
	}
	if tracked {
		return fmt.Errorf(".git-task уже отслеживается в индексе")
	}
	exists, err := c.HasCommit(ctx)
	if err != nil {
		return err
	}
	if exists {
		// ls-tree does not support the icase pathspec magic. Read root entry
		// names only, then apply the same physical-path check as for the index.
		r, err = c.Run(ctx, "ls-tree", "--full-tree", "--name-only", "-z", "HEAD")
		if err != nil {
			return err
		}
		tracked, err = trackedStoragePath(repo.Root, r.Stdout)
		if err != nil {
			return err
		}
		if tracked {
			return fmt.Errorf(".git-task уже отслеживается в HEAD")
		}
	}
	return nil
}

// trackedStoragePath interprets NUL-separated Git paths using filesystem
// identity, not core.ignoreCase or an OS-wide assumption. Windows directories
// can be case-sensitive; macOS volumes can be case-insensitive.
func trackedStoragePath(root string, paths []byte) (bool, error) {
	seen := map[string]bool{}
	for _, path := range strings.Split(string(paths), "\x00") {
		name, _, _ := strings.Cut(path, "/")
		if name == ".git-task" {
			return true, nil
		}
		if !strings.EqualFold(name, ".git-task") || seen[name] {
			continue
		}
		seen[name] = true
		canonical, canonicalErr := os.Stat(filepath.Join(root, ".git-task"))
		candidate, candidateErr := os.Stat(filepath.Join(root, name))
		for _, err := range []error{canonicalErr, candidateErr} {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
		if canonicalErr == nil && candidateErr == nil {
			if os.SameFile(canonical, candidate) {
				return true, nil
			}
			continue
		}
		if canonicalErr == nil || candidateErr == nil {
			continue
		}
		// A tracked directory may be deleted from the working tree. Probe the
		// lookup semantics of this same parent without creating probe files.
		aliases, err := caseInsensitiveDirectory(root)
		if err != nil {
			return false, err
		}
		if aliases {
			return true, nil
		}
	}
	return false, nil
}

func caseInsensitiveDirectory(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	for _, entry := range entries {
		name := entry.Name()
		for i := 0; i < len(name); i++ {
			b := name[i]
			if b >= 'a' && b <= 'z' {
				b -= 'a' - 'A'
			} else if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			} else {
				continue
			}
			other := name[:i] + string(b) + name[i+1:]
			if names[other] {
				continue
			}
			original, err := os.Lstat(filepath.Join(root, name))
			if err != nil {
				return false, err
			}
			alias, err := os.Lstat(filepath.Join(root, other))
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return os.SameFile(original, alias), nil
		}
	}
	return false, fmt.Errorf("нельзя определить совпадение регистров служебного пути без изменения каталога %s", root)
}

// HasCommit distinguishes an unborn branch from an unexpected Git failure.
func (c *Client) HasCommit(ctx context.Context) (bool, error) {
	_, err := c.Run(ctx, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if exitCode(err, 1) {
		return false, nil
	}
	return err == nil, err
}

// StorageIgnored checks the directory itself, so a higher-priority negation
// cannot selectively expose future lock, backup or temporary files.
func (c *Client) StorageIgnored(ctx context.Context) (bool, error) {
	_, err := c.Run(ctx, "check-ignore", "--quiet", "--no-index", "--", ".git-task/")
	if exitCode(err, 1) {
		return false, nil
	}
	return err == nil, err
}

func exitCode(err error, code int) bool {
	var command *CommandError
	return errors.As(err, &command) && command.Result.ExitCode == code
}

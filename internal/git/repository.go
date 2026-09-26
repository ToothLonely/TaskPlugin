package git

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrNotWorkTree indicates a repository without a usable working tree.
var ErrNotWorkTree = errors.New("требуется рабочее дерево Git; bare-репозиторий не поддерживается")

// Repository contains absolute paths resolved by Git, not guessed from .git.
// Discovery alone does not establish support for plan operations (for example,
// linked worktrees can be inspected but are outside the product's first version).
type Repository struct {
	Root        string
	GitDir      string
	CommonDir   string
	ExcludePath string
}

// Discover finds repository paths from any directory within a working tree,
// including an unborn repository. It does not change repository state.
func (c *Client) Discover(ctx context.Context) (Repository, error) {
	inside, err := c.Run(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return Repository{}, fmt.Errorf("не удалось обнаружить рабочий репозиторий Git: %w", err)
	}
	if outputLine(inside.Stdout) != "true" {
		return Repository{}, ErrNotWorkTree
	}
	var repo Repository
	queries := []struct {
		args []string
		dest *string
	}{
		{[]string{"--show-toplevel"}, &repo.Root},
		{[]string{"--absolute-git-dir"}, &repo.GitDir},
		{[]string{"--git-common-dir"}, &repo.CommonDir},
		{[]string{"--git-path", "info/exclude"}, &repo.ExcludePath},
	}
	for _, query := range queries {
		args := append([]string{"rev-parse", "--path-format=absolute"}, query.args...)
		result, err := c.Run(ctx, args...)
		if err != nil {
			return Repository{}, fmt.Errorf("не удалось определить пути репозитория: %w", err)
		}
		path := outputLine(result.Stdout)
		if !filepath.IsAbs(path) {
			return Repository{}, fmt.Errorf("Git вернул не абсолютный путь: %q", path)
		}
		*query.dest = filepath.Clean(path)
	}
	return repo, nil
}

// Remove only Git's line terminator: surrounding spaces can belong to a path.
func outputLine(output []byte) string {
	return strings.TrimSuffix(strings.TrimSuffix(string(output), "\n"), "\r")
}

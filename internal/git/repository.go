package git

import (
	"context"
	"path/filepath"
	"strings"

	"git-task/internal/diagnostic"
)

// ErrNotWorkTree indicates a repository without a usable working tree.
var ErrNotWorkTree = diagnostic.New("требуется рабочее дерево Git; bare-репозиторий не поддерживается")

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
		return Repository{}, diagnostic.Errorf("не удалось обнаружить рабочий репозиторий Git: %w", err)
	}
	if outputLine(inside.Stdout) != "true" {
		return Repository{}, ErrNotWorkTree
	}
	result, err := c.Run(ctx, "rev-parse", "--path-format=absolute", "--show-toplevel", "--absolute-git-dir", "--git-common-dir", "--git-path", "info/exclude")
	if err != nil {
		return Repository{}, diagnostic.Errorf("не удалось определить пути репозитория: %w", err)
	}
	paths := strings.Split(outputLine(result.Stdout), "\n")
	if len(paths) != 4 {
		paths = nil
		for _, args := range [][]string{{"--show-toplevel"}, {"--absolute-git-dir"}, {"--git-common-dir"}, {"--git-path", "info/exclude"}} {
			result, err := c.Run(ctx, append([]string{"rev-parse", "--path-format=absolute"}, args...)...)
			if err != nil {
				return Repository{}, diagnostic.Errorf("не удалось определить пути репозитория: %w", err)
			}
			paths = append(paths, outputLine(result.Stdout))
		}
	}
	var repo Repository
	for i, dest := range []*string{&repo.Root, &repo.GitDir, &repo.CommonDir, &repo.ExcludePath} {
		path := paths[i]
		if !filepath.IsAbs(path) {
			return Repository{}, diagnostic.Errorf("Git вернул не абсолютный путь: %q", path)
		}
		*dest = filepath.Clean(path)
	}
	return repo, nil
}

// Remove only Git's line terminator: surrounding spaces can belong to a path.
func outputLine(output []byte) string {
	return strings.TrimSuffix(strings.TrimSuffix(string(output), "\n"), "\r")
}

package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func (c *Client) HooksDirectory(ctx context.Context) (string, error) {
	repo, err := c.Discover(ctx)
	if err != nil {
		return "", err
	}
	if repo.GitDir != repo.CommonDir {
		return "", fmt.Errorf("дополнительные worktree не поддерживаются")
	}
	r, err := c.Run(ctx, "config", "--show-scope", "--get", "core.hooksPath")
	if err != nil && !exitCode(err, 1) {
		return "", err
	}
	if err == nil {
		scope, _, ok := strings.Cut(string(r.Stdout), "\t")
		if !ok || scope != "local" {
			return "", fmt.Errorf("core.hooksPath задан вне локальной конфигурации; используйте ручное подключение из README.md, общий каталог не изменён")
		}
	}
	r, err = c.Run(ctx, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	if err != nil {
		return "", err
	}
	path := outputLine(r.Stdout)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("Git вернул не абсолютный путь hooks: %q", path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(repo.GitDir, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("core.hooksPath находится вне каталога этого Git-репозитория; используйте ручное подключение из README.md, каталог не изменён")
	}
	return path, nil
}

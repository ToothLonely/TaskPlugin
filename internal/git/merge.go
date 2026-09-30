package git

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (c *Client) CheckPostMerge(ctx context.Context) error {
	return c.checkState(ctx, false, true)
}

func (c *Client) completedMerge(ctx context.Context, repo Repository) (bool, error) {
	mergeHeads, err := readMergeRefs(filepath.Join(repo.GitDir, "MERGE_HEAD"))
	if err != nil || len(mergeHeads) == 0 {
		return false, err
	}
	original, err := readMergeRefs(filepath.Join(repo.GitDir, "ORIG_HEAD"))
	if err != nil || len(original) != 1 {
		return false, err
	}
	head, err := c.HeadState(ctx)
	if err != nil {
		return false, err
	}
	parents, err := c.Parents(ctx, head.Commit)
	if err != nil {
		return false, err
	}
	if len(parents) < 2 || parents[0] != original[0] || !slices.Equal(parents[1:], mergeHeads) {
		return false, nil
	}
	r, err := c.Run(ctx, "diff-index", "--cached", "--quiet", "--no-ext-diff", "--no-textconv", head.Commit, "--")
	if exitCode(err, 1) {
		return false, nil
	}
	return r.ExitCode == 0, err
}

func readMergeRefs(path string) ([]string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	const limit = 4096 * 65
	data, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	if err := errors.Join(readErr, f.Close()); err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > limit || data[len(data)-1] != '\n' {
		return nil, nil
	}
	refs := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for _, ref := range refs {
		if !fullOID(ref, false) {
			return nil, nil
		}
	}
	return refs, nil
}

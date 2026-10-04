package git

import (
	"context"
	"fmt"
	"strings"
)

func (c *Client) FetchCodeRef(ctx context.Context, remote, branch string) (string, error) {
	tip, err := c.RemoteCodeTip(ctx, remote, branch)
	if err != nil || tip == "" {
		return tip, err
	}
	ref, err := c.BranchRef(ctx, branch)
	if err != nil {
		return "", err
	}
	_, err = c.run(ctx, nil, true, "fetch", "--no-tags", "--no-recurse-submodules", "--", remote, ref)
	if err != nil {
		return "", err
	}
	r, err := c.Run(ctx, "rev-parse", "--verify", "--end-of-options", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(r.Stdout)) != tip {
		return "", fmt.Errorf("ссылка кода изменилась во время fetch")
	}
	return tip, nil
}

func (c *Client) RemoteCodeTip(ctx context.Context, remote, branch string) (string, error) {
	if err := remoteName(remote); err != nil {
		return "", err
	}
	ref, err := c.BranchRef(ctx, branch)
	if err != nil {
		return "", err
	}
	r, err := c.run(ctx, nil, true, "ls-remote", "--refs", "--", remote, ref)
	if err != nil {
		return "", err
	}
	if len(r.Stdout) == 0 {
		return "", nil
	}
	fields := strings.Fields(string(r.Stdout))
	if len(fields) != 2 || fields[1] != ref || !objectID(fields[0]) {
		return "", fmt.Errorf("неверная ссылка кода")
	}
	return fields[0], nil
}

func (c *Client) ServerWork(ctx context.Context, base, tip, before, after string) (string, error) {
	for _, oid := range []string{base, tip, before, after} {
		if !objectID(oid) {
			return "", fmt.Errorf("неверный commit серверного события")
		}
	}
	for _, pair := range [][2]string{{before, after}, {base, tip}, {tip, after}} {
		included, err := c.IsAncestor(ctx, pair[0], pair[1])
		if err != nil {
			return "", err
		}
		if !included {
			return "", nil
		}
	}
	if tip == base {
		return "", nil
	}
	included, err := c.IsAncestor(ctx, tip, before)
	if err != nil || included {
		return "", err
	}
	r, err := c.Run(ctx, "rev-list", "--first-parent", "--no-merges", "--max-count=1", tip, "--not", before, base, "--")
	if err != nil {
		return "", err
	}
	work := strings.TrimSpace(string(r.Stdout))
	if work != "" && !objectID(work) {
		return "", fmt.Errorf("неверное доказательство работы")
	}
	return work, nil
}

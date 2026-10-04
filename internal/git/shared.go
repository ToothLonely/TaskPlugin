package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const PlanBranch = "git-task-plan"

var ErrPushRace = errors.New("служебная ветка изменена конкурентным push")

func remoteName(name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("неверное имя remote")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return fmt.Errorf("неверное имя remote")
		}
	}
	return nil
}

func (c *Client) CheckRemote(ctx context.Context, name string) error {
	if err := remoteName(name); err != nil {
		return err
	}
	_, err := c.Run(ctx, "remote", "get-url", "--", name)
	return err
}

func (c *Client) RemotePlanTip(ctx context.Context, name string) (string, error) {
	if err := remoteName(name); err != nil {
		return "", err
	}
	r, err := c.run(ctx, nil, true, "ls-remote", "--exit-code", "--refs", "--", name, "refs/heads/"+PlanBranch)
	var command *CommandError
	if errors.As(err, &command) && command.Result.ExitCode == 2 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(r.Stdout))
	if len(fields) != 2 || fields[1] != "refs/heads/"+PlanBranch || !objectID(fields[0]) {
		return "", fmt.Errorf("неверный ответ ls-remote")
	}
	return fields[0], nil
}

func objectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return strings.Trim(s, "0") != ""
}

func (c *Client) FetchPlan(ctx context.Context, name string) (string, error) {
	tip, err := c.RemotePlanTip(ctx, name)
	if err != nil || tip == "" {
		return tip, err
	}
	_, err = c.run(ctx, nil, true, "fetch", "--no-tags", "--no-recurse-submodules", "--", name, "refs/heads/"+PlanBranch+":refs/remotes/"+name+"/"+PlanBranch)
	if err != nil {
		return "", err
	}
	return c.CachedPlanTip(ctx, name)
}

func (c *Client) CachedPlanTip(ctx context.Context, name string) (string, error) {
	if err := remoteName(name); err != nil {
		return "", err
	}
	ref := "refs/remotes/" + name + "/" + PlanBranch
	_, err := c.Run(ctx, "show-ref", "--verify", "--quiet", ref)
	var command *CommandError
	if errors.As(err, &command) && command.Result.ExitCode == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	r, err := c.Run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(r.Stdout))
	if !objectID(value) {
		return "", fmt.Errorf("неверная служебная ссылка")
	}
	return value, nil
}

func (c *Client) ReadPlanCommit(ctx context.Context, tip string) ([]byte, error) {
	if !objectID(tip) {
		return nil, fmt.Errorf("неверный commit плана")
	}
	r, err := c.Run(ctx, "ls-tree", "-z", tip)
	if err != nil {
		return nil, err
	}
	entry := strings.TrimSuffix(string(r.Stdout), "\x00")
	meta, path, ok := strings.Cut(entry, "\t")
	fields := strings.Fields(meta)
	if !ok || path != "plan.json" || len(fields) != 3 || fields[0] != "100644" || fields[1] != "blob" || !objectID(fields[2]) {
		return nil, fmt.Errorf("чужая служебная ветка: ожидается только plan.json")
	}
	r, err = c.Run(ctx, "cat-file", "blob", fields[2])
	return r.Stdout, err
}

func (c *Client) PlanCommit(ctx context.Context, parent string, data []byte) (string, error) {
	if parent != "" && !objectID(parent) {
		return "", fmt.Errorf("неверный родитель")
	}
	r, err := c.run(ctx, data, false, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	blob := strings.TrimSpace(string(r.Stdout))
	if !objectID(blob) {
		return "", fmt.Errorf("неверный blob")
	}
	r, err = c.run(ctx, []byte("100644 blob "+blob+"\tplan.json\n"), false, "mktree")
	if err != nil {
		return "", err
	}
	tree := strings.TrimSpace(string(r.Stdout))
	if !objectID(tree) {
		return "", fmt.Errorf("неверное дерево")
	}
	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	r, err = c.run(ctx, []byte("git-task: publish plan actions\n"), false, args...)
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(r.Stdout))
	if !objectID(commit) {
		return "", fmt.Errorf("неверный commit")
	}
	return commit, nil
}

func (c *Client) PushPlan(ctx context.Context, name, parent, commit string) error {
	if err := remoteName(name); err != nil {
		return err
	}
	if !objectID(commit) {
		return fmt.Errorf("неверный commit")
	}
	_, err := c.run(ctx, nil, true, "push", "--porcelain", "--no-recurse-submodules", "--", name, commit+":refs/heads/"+PlanBranch)
	if err == nil {
		return nil
	}
	var command *CommandError
	if !errors.As(err, &command) {
		return err
	}
	for _, line := range strings.Split(string(command.Result.Stdout), "\n") {
		if strings.HasPrefix(line, "!\t") && (strings.HasSuffix(line, "[rejected] (fetch first)") || strings.HasSuffix(line, "[rejected] (non-fast-forward)")) {
			return errors.Join(ErrPushRace, err)
		}
	}
	return err
}

func (c *Client) UpdatePlanBranch(ctx context.Context, old, next string) error {
	if !objectID(next) || old != "" && !objectID(old) {
		return fmt.Errorf("неверный commit")
	}
	if old == "" {
		old = strings.Repeat("0", len(next))
	}
	_, err := c.run(ctx, nil, true, "update-ref", "refs/heads/"+PlanBranch, next, old)
	return err
}

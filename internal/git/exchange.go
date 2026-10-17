package git

import (
	"context"
	"strings"

	"git-task/internal/diagnostic"
)

type CodeUpstream struct {
	Head   Head
	Remote string
	Ref    string
}

func (c *Client) configValue(ctx context.Context, key string) (string, error) {
	r, err := c.Run(ctx, "config", "--null", "--get-all", key)
	if exitCode(err, 1) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	values := strings.Split(strings.TrimSuffix(string(r.Stdout), "\x00"), "\x00")
	if len(values) != 1 {
		return "", diagnostic.Errorf("ожидается одно значение %s", key)
	}
	return values[0], nil
}

func (c *Client) CodeUpstream(ctx context.Context, remote string, pulling bool) (CodeUpstream, error) {
	var u CodeUpstream
	if err := c.CheckRemote(ctx, remote); err != nil {
		return u, err
	}
	h, err := c.HeadState(ctx)
	if err != nil {
		return u, err
	}
	branch, ok := strings.CutPrefix(h.Ref, "refs/heads/")
	if !ok || branch == PlanBranch {
		return u, diagnostic.Errorf("нужна обычная текущая ветка кода, не detached HEAD или git-task-plan")
	}
	configured, err := c.configValue(ctx, "branch."+branch+".remote")
	if err != nil {
		return u, err
	}
	if configured != remote {
		return u, diagnostic.Errorf("remote upstream текущей ветки должен совпадать с remote плана %s; настройте upstream явно", remote)
	}
	ref, err := c.configValue(ctx, "branch."+branch+".merge")
	if err != nil {
		return u, err
	}
	target, ok := strings.CutPrefix(ref, "refs/heads/")
	if !ok || target == PlanBranch {
		return u, diagnostic.Errorf("нужен единственный upstream ветки кода; настройте upstream явно")
	}
	if _, err := c.BranchRef(ctx, target); err != nil {
		return u, err
	}
	for _, key := range []string{"branch." + branch + ".pushRemote", "remote.pushDefault"} {
		value, err := c.configValue(ctx, key)
		if err != nil {
			return u, err
		}
		if value != "" && value != remote {
			return u, diagnostic.Errorf("%s указывает на другой remote; обмен остановлен", key)
		}
	}
	r, err := c.Run(ctx, "config", "--bool", "--get", "remote."+remote+".mirror")
	if err != nil && !exitCode(err, 1) {
		return u, err
	}
	if outputLine(r.Stdout) == "true" {
		return u, diagnostic.Errorf("mirror remote не поддерживается")
	}
	var urls []string
	for _, push := range []bool{false, true} {
		args := []string{"remote", "get-url", "--all"}
		if push {
			args = append(args, "--push")
		}
		r, err := c.Run(ctx, append(args, "--", remote)...)
		if err != nil {
			return u, err
		}
		url := outputLine(r.Stdout)
		if url == "" || strings.ContainsAny(url, "\r\n\x00") {
			return u, diagnostic.Errorf("обмен требует единственный fetch и push URL")
		}
		urls = append(urls, url)
	}
	if urls[0] != urls[1] {
		return u, diagnostic.Errorf("fetch и push URL различаются; обмен остановлен")
	}
	if err := c.CheckStart(ctx, pulling); err != nil {
		return u, err
	}
	return CodeUpstream{Head: h, Remote: remote, Ref: ref}, nil
}

func (c *Client) PushCode(ctx context.Context, u CodeUpstream) error {
	r, err := c.RunNetwork(ctx, "-c", "push.followTags=false", "push", "--porcelain", "--no-force", "--no-recurse-submodules", "--", u.Remote, u.Head.Ref+":"+u.Ref)
	if err != nil {
		for _, line := range strings.Split(string(r.Stdout), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) == 3 && fields[1] == u.Head.Ref+":"+u.Ref && (fields[0] == " " || fields[0] == "*" || fields[0] == "=") {
				return diagnostic.Errorf("Git подтвердил отправку ветки кода, но завершился ошибкой: %w", err)
			}
		}
	}
	return err
}

func (c *Client) PullCode(ctx context.Context, u CodeUpstream, mode string) error {
	if mode != "ff-only" && mode != "merge" && mode != "rebase" {
		return diagnostic.Errorf("неподдерживаемый режим pull")
	}
	if _, err := c.RunNetwork(ctx, "fetch", "--no-tags", "--no-recurse-submodules", "--", u.Remote, u.Ref); err != nil {
		return err
	}
	r, err := c.Run(ctx, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return err
	}
	tip := outputLine(r.Stdout)
	if err := c.CheckTreeStorage(ctx, tip); err != nil {
		return err
	}
	if mode == "rebase" {
		if err := c.checkRebaseStorage(ctx, u.Head.Commit, tip); err != nil {
			return err
		}
	}
	head, err := c.HeadState(ctx)
	if err != nil {
		return err
	}
	if head != u.Head {
		return diagnostic.Errorf("HEAD изменился во время получения кода; интеграция не начата")
	}
	if err := c.CheckStart(ctx, true); err != nil {
		return err
	}
	args := []string{"-c", "pull.ff=true", "-c", "pull.rebase=false", "-c", "rebase.autoStash=false", "-c", "merge.autoStash=false", "-c", "branch." + strings.TrimPrefix(u.Head.Ref, "refs/heads/") + ".mergeOptions=", "pull", "--no-edit", "--no-autostash", "--no-recurse-submodules", "--no-tags", "--no-squash"}
	switch mode {
	case "ff-only":
		args = append(args, "--no-rebase", "--ff-only")
	case "merge":
		args = append(args, "--no-rebase", "--ff")
	case "rebase":
		args = append(args, "--rebase=true", "--ff")
	default:
		return diagnostic.Errorf("неподдерживаемый режим pull")
	}
	_, err = c.RunNetwork(ctx, append(args, "--", ".", tip)...)
	return err
}

func (c *Client) checkRebaseStorage(ctx context.Context, head, tip string) error {
	if !objectID(head) || !objectID(tip) {
		return diagnostic.Errorf("ожидаются commit ID для проверки rebase")
	}
	r, err := c.Run(ctx, "rev-list", "--parents", head, "^"+tip, "--")
	if err != nil {
		return diagnostic.Errorf("не удалось проверить историю перед rebase: %w", err)
	}
	checked := make(map[string]bool)
	for _, commit := range strings.Fields(string(r.Stdout)) {
		if checked[commit] {
			continue
		}
		if err := c.CheckTreeStorage(ctx, commit); err != nil {
			return diagnostic.Errorf("rebase не начат: дерево локального коммита или его родителя %s небезопасно для .git-task: %w", commit, err)
		}
		checked[commit] = true
	}
	return nil
}

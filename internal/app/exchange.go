package app

import (
	"context"
	"errors"
	"time"

	"git-task/internal/diagnostic"
	"git-task/internal/git"
	"git-task/internal/storage"
)

func (p *Plans) prepareExchange(ctx context.Context, pulling bool) (git.CodeUpstream, error) {
	base, err := p.store.Load(ctx)
	if err != nil {
		return git.CodeUpstream{}, err
	}
	if base.PendingOperation {
		return git.CodeUpstream{}, storage.ErrOperation
	}
	if err := checkQueue(base.Plan); err != nil {
		return git.CodeUpstream{}, err
	}
	if _, err := p.localPlanBranch(ctx); err != nil {
		return git.CodeUpstream{}, err
	}
	return p.git.CodeUpstream(ctx, base.Plan.Team.Remote, pulling)
}

func (p *Plans) Push(ctx context.Context) error {
	u, err := p.prepareExchange(ctx, false)
	if err != nil {
		return err
	}
	if err := p.git.PushCode(ctx, u); err != nil {
		return diagnostic.Errorf("push кода завершился ошибкой (удалённая ветка могла измениться); публикация плана не запускалась, очередь сохранена: %w", err)
	}
	if err := p.point(ctx, "exchange-code-pushed"); err != nil {
		return diagnostic.Errorf("код отправлен, сверка и публикация плана не выполнены; git task sync, затем git task team publish: %w", err)
	}
	if _, _, err := p.Sync(ctx); err != nil {
		return diagnostic.Errorf("код отправлен, локальная сверка плана не завершена; git task sync, затем git task team publish: %w", err)
	}
	if err := p.Publish(ctx); err != nil {
		return diagnostic.Errorf("код отправлен, публикация плана не подтверждена; локальная очередь сохранена, git task team publish: %w", err)
	}
	return nil
}

func (p *Plans) Pull(ctx context.Context, mode string) error {
	if mode == "" {
		mode = "ff-only"
	}
	if mode != "ff-only" && mode != "merge" && mode != "rebase" {
		return diagnostic.Errorf("неподдерживаемый режим pull")
	}
	u, err := p.prepareExchange(ctx, true)
	if err != nil {
		return err
	}
	if err := p.git.PullCode(ctx, u, mode); err != nil {
		return p.pullFailure(ctx, u.Head, err)
	}
	if err := p.point(ctx, "exchange-code-pulled"); err != nil {
		return diagnostic.Errorf("код обновлён, получение и сверка плана не выполнены; git task team fetch, затем git task sync: %w", err)
	}
	if err := p.FetchTeam(ctx); err != nil {
		return diagnostic.Errorf("код обновлён, общий план не получен или не согласован; локальная очередь сохранена, git task team fetch, затем git task sync: %w", err)
	}
	if _, _, err := p.Sync(ctx); err != nil {
		return diagnostic.Errorf("код обновлён, общий план получен, локальная сверка не завершена; git task sync: %w", err)
	}
	return nil
}

func (p *Plans) pullFailure(ctx context.Context, before git.Head, cause error) error {
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	after, headErr := p.git.HeadState(check)
	stateErr := p.git.CheckStart(check, false)
	state := diagnostic.New("HEAD не изменился")
	if headErr != nil {
		state = diagnostic.New("состояние HEAD не подтверждено")
	} else if after != before {
		state = diagnostic.New("HEAD уже изменился")
	}
	if errors.Is(stateErr, git.ErrInProgress) {
		state = diagnostic.Errorf("%v; интеграция не завершена, разрешите конфликт/операцию Git", state)
	} else if stateErr != nil {
		state = diagnostic.Errorf("%v; состояние интеграции не подтверждено", state)
	}
	return diagnostic.Errorf("pull кода завершился ошибкой: %s; последующие получение и сверка плана не запускались (hooks могли применить уже полученные данные). После завершения Git: git task team fetch, затем git task sync: %w", state, errors.Join(cause, headErr, stateErr))
}

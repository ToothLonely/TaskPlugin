package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
)

func (p *Plans) Migrate(ctx context.Context) (bool, error) { return p.store.Migrate(ctx) }

func (p *Plans) Connect(ctx context.Context, remote string) error {
	if err := validateRemoteName(remote); err != nil {
		return err
	}
	if err := p.git.CheckRemote(ctx, remote); err != nil {
		return err
	}
	base, err := p.store.Load(ctx)
	if err != nil {
		return err
	}
	if base.PendingOperation {
		return storage.ErrOperation
	}
	if base.Plan.TargetBranch == git.PlanBranch {
		return fmt.Errorf("служебная ветка не может быть target_branch")
	}
	if base.Plan.Team != nil {
		if base.Plan.Team.Remote != remote {
			return fmt.Errorf("план уже подключён к remote %s", base.Plan.Team.Remote)
		}
		return p.Publish(ctx)
	}
	if _, err := p.localPlanBranch(ctx); err != nil {
		return err
	}
	empty, err := task.NewPlan(base.Plan.TargetBranch)
	if err != nil {
		return err
	}
	data, err := task.SharedBytes(empty)
	if err != nil {
		return err
	}
	next := base.Plan
	next.Team = &task.TeamState{Remote: remote, Base: data}
	for _, t := range next.Tasks {
		for _, a := range t.Attempts {
			if a.Observation != nil {
				next.Team.LocalAttempts = append(next.Team.LocalAttempts, a.ID)
			}
		}
	}
	before := empty
	before.Team = next.Team
	next, err = task.Queue(before, next)
	if err != nil {
		return err
	}
	next.Revision++
	if _, err = p.store.SaveShared(ctx, base, next); err != nil {
		return err
	}
	return p.Publish(ctx)
}

func (p *Plans) localPlanBranch(ctx context.Context) (string, error) {
	head, err := p.git.HeadState(ctx)
	if err != nil {
		return "", err
	}
	if head.Ref == "refs/heads/"+git.PlanBranch {
		return "", fmt.Errorf("переключитесь с ветки git-task-plan на ветку кода")
	}
	tip, err := p.git.BranchCommit(ctx, git.PlanBranch)
	if err != nil || tip == "" {
		return tip, err
	}
	data, err := p.git.ReadPlanCommit(ctx, tip)
	if err != nil {
		return "", err
	}
	if _, err := task.ValidateShared(data); err != nil {
		return "", err
	}
	return tip, nil
}

func decodeBase(plan task.Plan) (task.Plan, error) {
	if plan.Team == nil {
		return task.Plan{}, fmt.Errorf("командный режим не подключён; git task team connect --remote <name>")
	}
	return task.ValidateShared(plan.Team.Base)
}

func material(plan task.Plan) task.Plan {
	plan = task.Shared(plan)
	plan.Actions = nil
	plan.Revision = 0
	for i := range plan.Tasks {
		plan.Tasks[i].Revision = 0
		plan.Tasks[i].ActiveAttempt = nil
	}
	return plan
}

func checkQueue(plan task.Plan) error {
	base, err := decodeBase(plan)
	if err != nil {
		return err
	}
	expected, err := task.Replay(base, plan.Team.Pending)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(material(expected), material(plan)) {
		return fmt.Errorf("%w: локальная правка вне очереди; сохраните JSON экспортом", task.ErrSharedConflict)
	}
	return nil
}

func (p *Plans) sharedAt(ctx context.Context, tip string, fallback task.Plan) (task.Plan, []byte, error) {
	if tip == "" {
		data, err := task.SharedBytes(fallback)
		return fallback, data, err
	}
	data, err := p.git.ReadPlanCommit(ctx, tip)
	if err != nil {
		return task.Plan{}, data, err
	}
	plan, err := task.ValidateShared(data)
	return plan, data, err
}

func (p *Plans) FetchTeam(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	base, err := p.store.Load(ctx)
	if err != nil {
		return err
	}
	if base.PendingOperation {
		return storage.ErrOperation
	}
	if err := checkQueue(base.Plan); err != nil {
		return err
	}
	tip, err := p.git.FetchPlan(ctx, base.Plan.Team.Remote)
	if err != nil {
		return err
	}
	return p.applyReceived(ctx, base, tip, true)
}

func (p *Plans) applyReceived(ctx context.Context, base storage.Snapshot, tip string, ack bool) error {
	old, err := decodeBase(base.Plan)
	if err != nil {
		return err
	}
	remote, data, err := p.sharedAt(ctx, tip, old)
	if err != nil {
		return p.store.PreserveShared(ctx, data, err)
	}
	if tip == "" && base.Plan.Team.BaseCommit != "" {
		return fmt.Errorf("общая ветка исчезла; локальный план и очередь сохранены")
	}
	if err := task.ValidateSharedAdvance(old, remote); err != nil {
		return p.store.PreserveShared(ctx, data, err)
	}
	merged, err := task.Replay(remote, base.Plan.Team.Pending)
	if err != nil {
		return p.store.PreserveShared(ctx, data, err)
	}
	next := task.ApplyLocal(merged, base.Plan)
	team := *base.Plan.Team
	team.Base = data
	team.BaseCommit = tip
	team.Pending = slices.Clone(team.Pending)
	if ack {
		pending := team.Pending[:0]
		for _, a := range team.Pending {
			found := false
			for _, r := range remote.Actions {
				if r.ID == a.ID && r.Digest == a.Digest() {
					found = true
					break
				}
			}
			if !found {
				pending = append(pending, a)
			}
		}
		team.Pending = pending
	}
	next.Team = &team
	if reflect.DeepEqual(material(next), material(base.Plan)) && reflect.DeepEqual(next.Team, base.Plan.Team) {
		return nil
	}
	_, err = p.store.SaveShared(ctx, base, next)
	if err == nil {
		for _, t := range next.Tasks {
			old, e := base.Plan.FindID(t.ID)
			oldCount := 0
			if e == nil {
				oldCount = len(old.Attempts)
			}
			if len(t.Attempts) > oldCount && len(t.Attempts) > 1 {
				p.notices = append(p.notices, task.Warning{Code: "parallel_work", Message: fmt.Sprintf("Задача %s: получены независимые подходы, ни один не потерян.", t.ID)})
			}
			if e == nil && old.Status == task.Done && t.Status == task.Active {
				p.notices = append(p.notices, task.Warning{Code: "late_attempt", Message: fmt.Sprintf("Задача %s снова active: получен поздний подход.", t.ID)})
			}
		}
	}
	return err
}

func (p *Plans) TakeNotices() []task.Warning {
	notices := slices.Clone(p.notices)
	p.notices = nil
	return notices
}

func (p *Plans) ReceiveCached(ctx context.Context) error {
	base, err := p.store.Load(ctx)
	if err != nil {
		return err
	}
	if base.Plan.Team == nil || base.PendingOperation {
		return nil
	}
	if err := checkQueue(base.Plan); err != nil {
		return err
	}
	tip, err := p.git.CachedPlanTip(ctx, base.Plan.Team.Remote)
	if err != nil || tip == "" {
		return err
	}
	if base.Plan.Team.BaseCommit != "" {
		if tip == base.Plan.Team.BaseCommit {
			return nil
		}
		older, err := p.git.IsAncestor(ctx, tip, base.Plan.Team.BaseCommit)
		if err != nil {
			return err
		}
		if older {
			return nil
		}
	}
	remote, data, err := p.sharedAt(ctx, tip, task.Plan{})
	if err != nil {
		return p.store.PreserveShared(ctx, data, err)
	}
	_ = remote
	if reflect.DeepEqual(base.Plan.Team.Base, json.RawMessage(data)) {
		return nil
	}
	return p.applyReceived(ctx, base, tip, true)
}

func (p *Plans) Publish(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		base, err := p.store.Load(ctx)
		if err != nil {
			return err
		}
		if base.PendingOperation {
			return storage.ErrOperation
		}
		if err := checkQueue(base.Plan); err != nil {
			return err
		}
		localTip, err := p.localPlanBranch(ctx)
		if err != nil {
			return err
		}
		old, err := decodeBase(base.Plan)
		if err != nil {
			return err
		}
		tip, err := p.git.FetchPlan(ctx, base.Plan.Team.Remote)
		if err != nil {
			return err
		}
		remote, data, err := p.sharedAt(ctx, tip, old)
		if err != nil {
			return p.store.PreserveShared(ctx, data, err)
		}
		if tip == "" && base.Plan.Team.BaseCommit != "" {
			return fmt.Errorf("общая ветка исчезла; публикация остановлена")
		}
		if err := task.ValidateSharedAdvance(old, remote); err != nil {
			return p.store.PreserveShared(ctx, data, err)
		}
		merged, err := task.Replay(remote, base.Plan.Team.Pending)
		if err != nil {
			return p.store.PreserveShared(ctx, data, err)
		}
		candidate, err := task.SharedBytes(merged)
		if err != nil {
			return err
		}
		current, err := p.store.Load(ctx)
		if err != nil {
			return err
		}
		if !base.SameVersion(current) {
			return storage.ErrConflict
		}
		if tip != "" && reflect.DeepEqual(material(merged), material(remote)) && reflect.DeepEqual(merged.Actions, remote.Actions) {
			if err := p.git.UpdatePlanBranch(ctx, localTip, tip); err != nil {
				return err
			}
			return p.applyReceived(ctx, base, tip, true)
		}
		commit, err := p.git.PlanCommit(ctx, tip, candidate)
		if err != nil {
			return err
		}
		if err := p.point(ctx, "publish-before-push"); err != nil {
			return err
		}
		if err := p.git.PushPlan(ctx, base.Plan.Team.Remote, tip, commit); err != nil {
			if errors.Is(err, git.ErrPushRace) {
				continue
			}
			return err
		}
		if err := p.point(ctx, "publish-after-push"); err != nil {
			return err
		}
		if err := p.git.UpdatePlanBranch(ctx, localTip, commit); err != nil {
			return err
		}
		return p.applyReceived(ctx, base, commit, true)
	}
	return fmt.Errorf("%w: исчерпаны три попытки; очередь сохранена, git task team publish", git.ErrPushRace)
}

func (p *Plans) PublishIfConnected(ctx context.Context) error {
	base, err := p.store.Load(ctx)
	if err != nil {
		return err
	}
	if base.Plan.Team == nil {
		return nil
	}
	return p.Publish(ctx)
}

package app

import (
	"context"
	"fmt"
	"slices"
	"time"

	"git-task/internal/task"
)

func (p *Plans) ReconcileTeam(ctx context.Context, commits ...string) error {
	if len(commits) != 2 {
		return fmt.Errorf("team reconcile требует --before <oid> --after <oid> из post-receive")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	initial, err := p.store.Load(ctx)
	if err != nil {
		return err
	}
	if initial.Plan.Team == nil {
		return fmt.Errorf("сначала подключите командный режим")
	}
	if commits[0] != "" || commits[1] != "" {
		event := task.ServerEvent{Before: commits[0], After: commits[1]}
		if !slices.Contains(initial.Plan.ServerEvents, event) {
			next := initial.Plan
			next.ServerEvents = append(slices.Clone(next.ServerEvents), event)
			next.Revision++
			if _, err := p.store.SaveShared(ctx, initial, next); err != nil {
				return err
			}
		}
	}
	if err := p.FetchTeam(ctx); err != nil {
		return err
	}
	base, err := p.store.Load(ctx)
	if err != nil {
		return err
	}
	remote := base.Plan.Team.Remote
	target, err := p.git.FetchCodeRef(ctx, remote, base.Plan.TargetBranch)
	if err != nil {
		return err
	}
	next := base.Plan
	type refCheck struct{ branch, tip string }
	checks := []refCheck{{branch: base.Plan.TargetBranch, tip: target}}
	now := time.Now().UTC()
	for _, event := range base.Plan.ServerEvents {
		included, err := p.git.IsAncestor(ctx, event.After, target)
		if err != nil {
			return err
		}
		if !included {
			return fmt.Errorf("история target_branch переписана; серверное событие сохранено")
		}
		commits = []string{event.Before, event.After}
		for _, t := range base.Plan.Tasks {
			if t.Status == task.Archived {
				continue
			}
			for _, a := range t.Attempts {
				if updated, e := next.FindID(t.ID); e == nil {
					if current, e := updated.Attempt(a.ID); e == nil && current.Status == task.Done {
						continue
					}
				}
				if a.Status == task.Done {
					continue
				}
				tip, err := p.git.FetchCodeRef(ctx, remote, a.Branch)
				if err != nil {
					return err
				}
				if tip == "" {
					continue
				}
				checks = append(checks, refCheck{branch: a.Branch, tip: tip})
				work, err := p.git.ServerWork(ctx, a.BaseCommit, tip, commits[0], commits[1])
				if err != nil {
					return err
				}
				if work == "" {
					continue
				}
				completion := task.Completion{Source: task.Merge, TargetBranch: a.TargetBranch, Commit: event.After, WorkCommit: work, MergeKind: task.FastForward, ObservedAt: &now}
				completion.TargetBefore = event.Before
				parents, err := p.git.Parents(ctx, event.After)
				if err != nil {
					return err
				}
				if len(parents) > 1 {
					completion.MergeKind = task.MergeCommit
				}
				if _, err := next.Complete(t.ID, a.ID, completion); err != nil {
					return err
				}
			}
		}
	}
	if len(next.ServerEvents) > 0 {
		next.ServerEvents = nil
		next.Revision++
	}
	if next.Revision != base.Plan.Revision {
		for _, check := range checks {
			current, err := p.git.RemoteCodeTip(ctx, remote, check.branch)
			if err != nil {
				return err
			}
			if current != check.tip {
				return fmt.Errorf("ссылка кода изменилась во время серверной сверки; событие сохранено")
			}
		}
		if _, err := p.save(ctx, base, next); err != nil {
			return err
		}
	}
	return p.Publish(ctx)
}

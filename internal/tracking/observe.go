package tracking

import (
	"context"
	"errors"
	"fmt"
	"time"

	"git-task/internal/git"
	"git-task/internal/task"
)

type Reader interface {
	BranchCommit(context.Context, string) (string, error)
	BranchLog(context.Context, string) (git.RefLog, error)
	HasObject(context.Context, string) (bool, error)
	IsAncestor(context.Context, string, string) (bool, error)
	OwnWork(context.Context, string, string, string) (string, error)
	FirstParentRange(context.Context, string, string) ([]string, error)
	Parents(context.Context, string) ([]string, error)
}

type Result struct {
	Observation *task.Observation
	Warnings    []task.Warning
	Completion  *task.Completion
	tip         string
	target      string
	branchLog   string
	targetLog   string
}

func warning(o *task.Observation, code, message string) Result {
	return Result{Observation: o, Warnings: []task.Warning{{Code: code, Message: message}}}
}

func Observe(ctx context.Context, reader Reader, a task.Attempt, now time.Time) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	r, err := observe(ctx, reader, a, now)
	if errors.Is(err, git.ErrHistoryUnavailable) {
		return warning(a.Observation, "history_unavailable", "Недостаточно локальной истории или превышен бюджет сверки; требуется явный разбор."), nil
	}
	return r, err
}

func observe(ctx context.Context, reader Reader, a task.Attempt, now time.Time) (Result, error) {
	tip, err := reader.BranchCommit(ctx, a.Branch)
	if err != nil {
		return Result{}, err
	}
	if tip == "" {
		return warning(a.Observation, "branch_missing", "Ветка подхода отсутствует; используйте явный attach --rebind."), nil
	}
	target, err := reader.BranchCommit(ctx, a.TargetBranch)
	if err != nil {
		return Result{}, err
	}
	if target == "" {
		return warning(a.Observation, "target_missing", "Целевая ветка отсутствует; завершение не доказано."), nil
	}
	branchLog, err := reader.BranchLog(ctx, a.Branch)
	if err != nil {
		return Result{}, err
	}
	targetLog, err := reader.BranchLog(ctx, a.TargetBranch)
	if err != nil {
		return Result{}, err
	}
	o := task.Observation{Tip: a.BaseCommit, TargetCommit: target}
	if a.Observation != nil {
		o = *a.Observation
	}
	if o.BranchLog == "" {
		o.BranchLog = bindingDigest(a, branchLog)
	}
	if o.TargetLog == "" && a.Observation == nil {
		o.TargetLog = targetLog.Digest()
	}
	branchChanges, ok := suffix(branchLog, o.BranchLog, o.Tip)
	if !ok {
		return warning(a.Observation, "binding_uncertain", "Непрерывность ветки не подтверждена; журнал потерян или ветка пересоздана. Нужен явный rebind."), nil
	}
	targetChanges, ok := suffix(targetLog, o.TargetLog, o.TargetCommit)
	if !ok {
		return warning(a.Observation, "target_uncertain", "Непрерывность целевой истории не подтверждена; завершение не доказано."), nil
	}
	if branchLog.Entries[len(branchLog.Entries)-1].New != tip || targetLog.Entries[len(targetLog.Entries)-1].New != target {
		return warning(a.Observation, "ref_log_mismatch", "Ref не соответствует последней записи reflog; завершение не доказано."), nil
	}
	for _, oid := range []string{a.BaseCommit, o.Tip, o.TargetCommit, tip, target} {
		exists, err := reader.HasObject(ctx, oid)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return warning(a.Observation, "history_unavailable", "Отсутствует необходимый Git-объект; завершение не доказано."), nil
		}
	}
	for _, entry := range targetChanges {
		forward, err := reader.IsAncestor(ctx, entry.Old, entry.New)
		if err != nil {
			return Result{}, err
		}
		if !forward {
			return warning(a.Observation, "target_rewritten", "Целевая история переписана после наблюдения; завершение не доказано."), nil
		}
	}
	rewritten := false
	for _, entry := range branchChanges {
		forward, err := reader.IsAncestor(ctx, entry.Old, entry.New)
		if err != nil {
			return Result{}, err
		}
		rewritten = rewritten || !forward
	}
	inTarget, err := reader.IsAncestor(ctx, tip, target)
	if err != nil {
		return Result{}, err
	}
	if rewritten {
		o.WorkCommit = ""
		if inTarget {
			return warning(a.Observation, "work_rewritten", "Ветка переписана после наблюдения; старая работа не доказывает завершение нового tip."), nil
		}
	}
	if !inTarget {
		work, err := reader.OwnWork(ctx, tip, target, a.BaseCommit)
		if err != nil {
			return Result{}, err
		}
		if work != "" {
			o.WorkCommit = work
		}
		if work != "" || o.WorkCommit == "" {
			o.Tip, o.TargetCommit = tip, target
			o.BranchLog, o.TargetLog = branchLog.Digest(), targetLog.Digest()
		}
		r := Result{Observation: &o}
		if rewritten {
			r.Warnings = []task.Warning{{Code: "work_rewritten", Message: "Ветка переписана; сохранено новое наблюдение вне цели, прежнее доказательство сброшено."}}
		}
		return withCheck(r, tip, target, branchLog, targetLog), nil
	}
	if o.WorkCommit == "" {
		if tip == a.BaseCommit {
			return withCheck(Result{Observation: a.Observation}, tip, target, branchLog, targetLog), nil
		}
		return warning(a.Observation, "unobserved_work", "Tip уже входит в цель, но работа вне цели не наблюдалась; автоматическое завершение не доказано."), nil
	}
	outside, err := reader.IsAncestor(ctx, o.WorkCommit, o.TargetCommit)
	if err != nil {
		return Result{}, err
	}
	inTip, err := reader.IsAncestor(ctx, o.WorkCommit, tip)
	if err != nil {
		return Result{}, err
	}
	if outside || !inTip {
		return warning(a.Observation, "work_uncertain", "Сохранённое наблюдение работы противоречит текущему графу; завершение не доказано."), nil
	}
	completion, err := integration(ctx, reader, tip, target, o, now)
	if err != nil {
		return Result{}, err
	}
	if completion == nil {
		return warning(a.Observation, "integration_uncertain", "Обычный merge или fast-forward актуального tip не подтверждён."), nil
	}
	completion.TargetBranch = a.TargetBranch
	return withCheck(Result{Observation: &o, Completion: completion}, tip, target, branchLog, targetLog), nil
}

func BindingContinuous(a task.Attempt, log git.RefLog, tip string) bool {
	if len(log.Entries) == 0 || log.Entries[len(log.Entries)-1].New != tip {
		return false
	}
	baseline := a.BaseCommit
	if a.Observation != nil {
		baseline = a.Observation.Tip
	}
	_, ok := suffix(log, bindingDigest(a, log), baseline)
	return ok
}

func bindingDigest(a task.Attempt, log git.RefLog) string {
	if a.Observation != nil && a.Observation.BranchLog != "" {
		return a.Observation.BranchLog
	}
	if len(a.Rebindings) == 0 {
		for _, entry := range log.Entries {
			if entry.Message == "git-task start "+a.ID && entry.New == a.BaseCommit {
				return entry.Digest
			}
		}
	}
	return ""
}

func suffix(log git.RefLog, digest, tip string) ([]git.RefUpdate, bool) {
	if digest == "" {
		return nil, false
	}
	for i, entry := range log.Entries {
		if entry.Digest != digest {
			continue
		}
		if entry.New != tip {
			return nil, false
		}
		changes := log.Entries[i+1:]
		for _, change := range changes {
			if change.Old != tip {
				return nil, false
			}
			tip = change.New
		}
		return changes, true
	}
	return nil, false
}

func withCheck(r Result, tip, target string, branchLog, targetLog git.RefLog) Result {
	r.tip, r.target = tip, target
	r.branchLog, r.targetLog = branchLog.Digest(), targetLog.Digest()
	return r
}

func (r Result) Recheck(ctx context.Context, reader Reader, a task.Attempt) error {
	if r.tip == "" {
		return nil
	}
	tip, err := reader.BranchCommit(ctx, a.Branch)
	if err != nil {
		return err
	}
	target, err := reader.BranchCommit(ctx, a.TargetBranch)
	if err != nil {
		return err
	}
	b, err := reader.BranchLog(ctx, a.Branch)
	if err != nil {
		return err
	}
	t, err := reader.BranchLog(ctx, a.TargetBranch)
	if err != nil {
		return err
	}
	if tip != r.tip || target != r.target || b.Digest() != r.branchLog || t.Digest() != r.targetLog {
		return fmt.Errorf("Git изменён во время сверки; повторите sync")
	}
	return nil
}

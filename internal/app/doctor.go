package app

import (
	"context"
	"slices"
	"strings"

	"git-task/internal/diagnostic"
	"git-task/internal/storage"
	"git-task/internal/tracking"
)

type Diagnosis struct {
	Findings []storage.Finding
}

func (d Diagnosis) HasProblems() bool {
	for _, f := range d.Findings {
		if f.Problem {
			return true
		}
	}
	return false
}

func (p *Plans) Doctor(ctx context.Context, diagnoseHooks func(context.Context, string) ([]string, error)) (Diagnosis, error) {
	d := Diagnosis{}
	add := func(name string, detail error, next string, err error) {
		if err != nil {
			if detail != nil {
				detail = diagnostic.Errorf("%v; %w", detail, err)
			} else {
				detail = err
			}
		}
		d.Findings = append(d.Findings, storage.Finding{Name: name, Detail: detail, Next: next, Problem: err != nil})
	}
	v, err := p.git.Run(ctx, "--version")
	add("Git", diagnostic.New(strings.TrimSpace(string(v.Stdout))), "установите поддерживаемый Git", err)
	repo, err := p.git.Discover(ctx)
	if err != nil {
		add("репозиторий", nil, "выберите обычный репозиторий с рабочим деревом", err)
		return d, ctx.Err()
	}
	add("репозиторий", diagnostic.Errorf("root=%s; git=%s; common=%s", repo.Root, repo.GitDir, repo.CommonDir), "", nil)
	err = p.git.CheckStorage(ctx)
	add("окружение", diagnostic.New("обычный репозиторий, локальное хранение разрешено"), "устраните ограничение bare/shallow/partial/worktree или tracked-путь вручную; исходники не менялись", err)
	if err != nil {
		return d, ctx.Err()
	}
	inspection, err := p.store.Inspect(ctx)
	if err != nil {
		add("хранилище", nil, "сохраните исходники; ручной разбор пути", err)
		return d, ctx.Err()
	}
	d.Findings = append(d.Findings, inspection.Findings...)
	ignored, err := p.git.StorageIgnored(ctx)
	if err == nil && !ignored {
		err = diagnostic.Errorf("/.git-task/ не исключён")
	}
	add("exclude", diagnostic.Errorf("%s; ignored=%t", repo.ExcludePath, ignored), "git task init с прежним --target", err)
	var installed []string
	if diagnoseHooks == nil {
		err = diagnostic.Errorf("диагностика hooks не подключена")
	} else {
		installed, err = diagnoseHooks(ctx, repo.Root)
	}
	add("hooks", diagnostic.Errorf("установлены: %v; без hooks доступен git task sync", installed), "разберите hooks по README.md; чужие обработчики сохраняются", err)
	hasCommit, err := p.git.HasCommit(ctx)
	if err == nil && hasCommit {
		head, headErr := p.git.HeadState(ctx)
		add("HEAD", diagnostic.Errorf("%s %s", head.Ref, head.Commit), "проверьте фактическую ветку/HEAD", headErr)
	} else {
		add("HEAD", diagnostic.New("нет commit; start/resume недоступны"), "создайте первый commit кода перед start", err)
	}
	if hasCommit {
		add("Git-операция", diagnostic.New("состояние для локальных связей"), "завершите текущую Git-операцию вручную; repair не переключает ветки", p.git.CheckStart(ctx, false))
	}
	plan := inspection.Plan
	if plan == nil {
		return d, ctx.Err()
	}
	target, err := p.git.BranchCommit(ctx, plan.TargetBranch)
	if err == nil && target == "" && hasCommit {
		err = diagnostic.Errorf("цель %q отсутствует", plan.TargetBranch)
	}
	add("target", diagnostic.Errorf("%s %s", plan.TargetBranch, target), "явно восстановите целевую ветку; sync не доказывает done без цели", err)
	for _, item := range plan.Tasks {
		for _, a := range item.Attempts {
			local := plan.Team == nil || slices.Contains(plan.Team.LocalAttempts, a.ID)
			pub := diagnostic.New("личный режим")
			if plan.Team != nil {
				pub = diagnostic.New("публикация текущего состояния не подтверждена")
				base, baseErr := decodeBase(*plan)
				if baseErr == nil && plan.Team.BaseCommit != "" {
					if old, findErr := base.FindID(item.ID); findErr == nil {
						if prior, attemptErr := old.Attempt(a.ID); attemptErr == nil && prior.Status == a.Status && prior.Branch == a.Branch {
							pub = diagnostic.New("подход присутствует в подтверждённой базе; смотрите очередь изменений")
						}
					}
				}
			}
			tip := "нет локальной ветки"
			var branchErr error
			if a.Branch != "" {
				tip, branchErr = p.git.BranchCommit(ctx, a.Branch)
				if tip == "" {
					tip = "нет локальной ветки"
				}
			}
			if local && a.Branch != "" && tip == "нет локальной ветки" && a.Completion == nil && branchErr == nil {
				branchErr = diagnostic.Errorf("локальная связь %s потеряна", a.Branch)
			}
			add("подход", diagnostic.Errorf("task=%s status=%s attempt=%s author=%q status=%s local=%t branch=%q tip=%s; %s", item.ID, item.Status, a.ID, a.Author, a.Status, local, a.Branch, diagnostic.New(tip), pub), "для местной потерянной связи: attach --rebind; чужой подход сохраняется и не требует местной ветки", branchErr)
			if local && a.Completion == nil && a.Branch != "" && branchErr == nil {
				log, err := p.git.BranchLog(ctx, a.Branch)
				if err != nil {
					add("журнал связи", diagnostic.Errorf("%s", a.ID), "сохраните локальные свидетельства; явный разбор", err)
				} else if !tracking.BindingContinuous(a, log, tip) {
					add("неопределённость связи", diagnostic.Errorf("%s: непрерывность местной ветки не подтверждена", a.ID), "явный attach --rebind после разбора; done не доказан", nil)
				}
			}
		}
	}
	if plan.Team != nil {
		team := plan.Team
		add("общая база", diagnostic.Errorf("remote=%s commit=%s действий=%d", team.Remote, team.BaseCommit, len(team.Pending)), "сохраните plan.json; повреждённая база/очередь требуют разбора", checkQueue(*plan))
		for _, action := range team.Pending {
			add("неопубликованное действие", diagnostic.Errorf("%s", action.ID), "git task team publish; прежние ID, свежая база, без повторного start/complete; последняя ошибка сети не хранится", nil)
		}
		tip, err := p.git.CachedPlanTip(ctx, team.Remote)
		if err == nil && tip != "" {
			_, _, err = p.sharedAt(ctx, tip, *plan)
		}
		add("полученная служебная версия", diagnostic.New(tip), "при повреждении сохраните исходники; при новой версии: git task team publish/fetch; doctor не применяет её", err)
		_, err = p.localPlanBranch(ctx)
		add("локальная служебная ветка", diagnostic.New("git-task-plan"), "сохраните служебную историю; ручной разбор, без force push", err)
	}
	return d, ctx.Err()
}

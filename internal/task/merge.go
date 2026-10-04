package task

import (
	"fmt"
	"reflect"
	"slices"
)

func mergeField[T comparable](name string, before, local, remote T) (T, error) {
	if local == before || local == remote {
		return remote, nil
	}
	if remote == before {
		return local, nil
	}
	return remote, fmt.Errorf("%w: %s", ErrSharedConflict, name)
}

func mergeValue[T any](name string, before, local, remote T) (T, error) {
	if reflect.DeepEqual(local, before) || reflect.DeepEqual(local, remote) {
		return remote, nil
	}
	if reflect.DeepEqual(remote, before) {
		return local, nil
	}
	return remote, fmt.Errorf("%w: %s", ErrSharedConflict, name)
}

func MergeShared(before, local, remote Plan) (Plan, error) {
	before, local, remote = Shared(before), Shared(local), Shared(remote)
	if err := before.Validate(); err != nil {
		return Plan{}, err
	}
	if err := local.Validate(); err != nil {
		return Plan{}, err
	}
	if err := remote.Validate(); err != nil {
		return Plan{}, err
	}
	next := remote
	var err error
	next.TargetBranch, err = mergeField("target_branch", before.TargetBranch, local.TargetBranch, remote.TargetBranch)
	if err != nil {
		return Plan{}, err
	}
	next.Tasks = slices.Clone(remote.Tasks)
	for _, lt := range local.Tasks {
		bt, be := before.FindID(lt.ID)
		rt, re := remote.FindID(lt.ID)
		if be != nil {
			if re == nil {
				rt.ActiveAttempt = nil
				if !reflect.DeepEqual(lt, rt) {
					return Plan{}, fmt.Errorf("%w: коллизия задачи %s", ErrSharedConflict, lt.ID)
				}
			} else {
				next.Tasks = append(next.Tasks, lt.clone())
			}
			continue
		}
		if re != nil {
			return Plan{}, fmt.Errorf("%w: задача удалена %s", ErrSharedConflict, lt.ID)
		}
		mt, err := mergeTask(bt, lt, rt)
		if err != nil {
			return Plan{}, fmt.Errorf("задача %s: %w", lt.ID, err)
		}
		for i := range next.Tasks {
			if next.Tasks[i].ID == lt.ID {
				next.Tasks[i] = mt
			}
		}
	}
	for _, bt := range before.Tasks {
		if _, err := local.FindID(bt.ID); err != nil {
			return Plan{}, fmt.Errorf("%w: удаление задачи", ErrSharedConflict)
		}
	}
	next.Order, err = mergeOrder(before.Order, local.Order, remote.Order)
	if err != nil {
		return Plan{}, err
	}
	next.InsertionTail, err = mergeField("insertion_tail", before.InsertionTail, local.InsertionTail, remote.InsertionTail)
	if err != nil {
		if slices.Equal(existingOrder(before.Order, local.Order), before.Order) && slices.Equal(existingOrder(before.Order, remote.Order), before.Order) {
			next.InsertionTail = remote.InsertionTail
		} else {
			return Plan{}, err
		}
	}
	next.Revision = max(local.Revision, remote.Revision) + 1
	if next.Revision == 0 {
		return Plan{}, invalid("исчерпана revision")
	}
	if err := normalizeShared(&next, remote); err != nil {
		return Plan{}, err
	}
	return next, next.Validate()
}

func mergeTask(b, l, r Task) (Task, error) {
	next := r.clone()
	var err error
	next.Title, err = mergeField("title", b.Title, l.Title, r.Title)
	if err != nil {
		return Task{}, err
	}
	next.Description, err = mergeField("description", b.Description, l.Description, r.Description)
	if err != nil {
		return Task{}, err
	}
	if l.Number != b.Number && l.Number != r.Number {
		return Task{}, fmt.Errorf("%w: number", ErrSharedConflict)
	}
	ba, la, ra := b.Status == Archived, l.Status == Archived, r.Status == Archived
	archived, err := mergeField("archive", ba, la, ra)
	if err != nil {
		return Task{}, err
	}
	if (la != ba && !reflect.DeepEqual(b.Attempts, r.Attempts)) || (ra != ba && !reflect.DeepEqual(b.Attempts, l.Attempts)) {
		return Task{}, fmt.Errorf("%w: archive и изменение подходов", ErrSharedConflict)
	}
	next.Attempts = slices.Clone(r.Attempts)
	for _, a := range l.Attempts {
		old, be := b.Attempt(a.ID)
		other, re := r.Attempt(a.ID)
		if be != nil {
			if re == nil && !reflect.DeepEqual(a, other) {
				return Task{}, fmt.Errorf("%w: коллизия подхода %s", ErrSharedConflict, a.ID)
			}
			if re != nil {
				next.Attempts = append(next.Attempts, a.clone())
			}
			continue
		}
		if re != nil {
			return Task{}, fmt.Errorf("%w: удалён подход %s", ErrSharedConflict, a.ID)
		}
		merged, err := mergeAttempt(old, a, other)
		if err != nil {
			return Task{}, err
		}
		for i := range next.Attempts {
			if next.Attempts[i].ID == a.ID {
				next.Attempts[i] = merged
			}
		}
	}
	for _, a := range b.Attempts {
		if _, err := l.Attempt(a.ID); err != nil {
			return Task{}, fmt.Errorf("%w: удаление подхода", ErrSharedConflict)
		}
	}
	if archived {
		next.Status = Archived
	} else {
		next.Status = Todo
		next.Status = next.AggregateStatus()
	}
	next.Revision = max(l.Revision, r.Revision) + 1
	if next.Revision == 0 {
		return Task{}, invalid("исчерпана revision")
	}
	next.project("")
	return next, nil
}

func mergeAttempt(b, l, r Attempt) (Attempt, error) {
	for _, a := range []Attempt{l, r} {
		if a.Author != b.Author || a.OriginalBranch != b.OriginalBranch || a.TargetBranch != b.TargetBranch || !reflect.DeepEqual(a.StartedAt, b.StartedAt) {
			return Attempt{}, fmt.Errorf("%w: неизменные поля подхода %s", ErrSharedConflict, b.ID)
		}
	}
	next := r.clone()
	var err error
	next.Rebindings, err = mergeValue("rebindings", b.Rebindings, l.Rebindings, r.Rebindings)
	if err != nil {
		return Attempt{}, err
	}
	next.Branch, err = mergeField("branch", b.Branch, l.Branch, r.Branch)
	if err != nil {
		return Attempt{}, err
	}
	next.BaseCommit, err = mergeField("base_commit", b.BaseCommit, l.BaseCommit, r.BaseCommit)
	if err != nil {
		return Attempt{}, err
	}
	bc, lc, rc := b.Completion, l.Completion, r.Completion
	stripEvent := func(c *Completion) *Completion {
		if c == nil {
			return nil
		}
		n := *c
		n.Event = 0
		return &n
	}
	completion, err := mergeValue("completion", stripEvent(bc), stripEvent(lc), stripEvent(rc))
	if err != nil {
		return Attempt{}, err
	}
	if completion != nil {
		if rc != nil {
			next.Completion = rc
		} else {
			next.Completion = lc
		}
		next.Status = Done
	} else {
		next.Status, err = mergeField("attempt status", b.Status, l.Status, r.Status)
		if err != nil {
			return Attempt{}, err
		}
	}
	return next, nil
}

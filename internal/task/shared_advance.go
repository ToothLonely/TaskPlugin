package task

import (
	"fmt"
	"reflect"
	"slices"
)

func ValidateSharedAdvance(base, received Plan) error {
	base, received = Shared(base), Shared(received)
	for _, old := range base.Tasks {
		next, err := received.FindID(old.ID)
		if err != nil || old.Status == Archived && next.Status != Archived {
			return fmt.Errorf("%w: исчезла или изменена известная задача %s", ErrSharedConflict, old.ID)
		}
		for _, a := range old.Attempts {
			b, err := next.Attempt(a.ID)
			if err != nil || a.Author != b.Author || a.OriginalBranch != b.OriginalBranch || a.TargetBranch != b.TargetBranch || !reflect.DeepEqual(a.StartedAt, b.StartedAt) {
				return fmt.Errorf("%w: исчезли или изменены неизменные данные подхода %s", ErrSharedConflict, a.ID)
			}
			if len(b.Rebindings) < len(a.Rebindings) || len(a.Rebindings) > 0 && !reflect.DeepEqual(a.Rebindings, b.Rebindings[:len(a.Rebindings)]) {
				return fmt.Errorf("%w: утрачена история связей подхода %s", ErrSharedConflict, a.ID)
			}
			if len(a.Rebindings) == len(b.Rebindings) && (a.Branch != b.Branch || a.BaseCommit != b.BaseCommit) {
				return fmt.Errorf("%w: изменена связь без новой перепривязки %s", ErrSharedConflict, a.ID)
			}
			if a.Completion != nil && !reflect.DeepEqual(a, b) {
				return fmt.Errorf("%w: изменено опубликованное завершение подхода %s", ErrSharedConflict, a.ID)
			}
		}
	}
	for _, old := range base.Actions {
		if !slices.Contains(received.Actions, old) {
			return fmt.Errorf("%w: исчезла или изменена квитанция %s", ErrSharedConflict, old.ID)
		}
	}
	return nil
}

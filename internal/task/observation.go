package task

import "slices"

func (p *Plan) Observe(id, attemptID string, observation *Observation, warnings []Warning) (bool, error) {
	return p.change(id, func(_ *Plan, t *Task) (bool, error) {
		if t.Status != Active && t.Status != Paused {
			return false, ErrTransition
		}
		a, err := t.selected([]string{attemptID})
		if err != nil {
			return false, err
		}
		old := a.Observation
		combined := []Warning{}
		for _, w := range t.Warnings {
			if w.AttemptID != attemptID {
				combined = append(combined, w)
			}
		}
		for _, w := range warnings {
			w.AttemptID = attemptID
			combined = append(combined, w)
		}
		if len(combined) == 0 {
			combined = nil
		}
		same := old == nil && observation == nil || old != nil && observation != nil && *old == *observation
		if same && slices.Equal(t.Warnings, combined) {
			return false, nil
		}
		a.Observation = nil
		if observation != nil {
			copy := *observation
			a.Observation = &copy
		}
		t.Warnings = combined
		return true, nil
	})
}

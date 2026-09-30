package task

import "slices"

func (p *Plan) Observe(id, attemptID string, observation *Observation, warnings []Warning) (bool, error) {
	return p.change(id, func(_ *Plan, t *Task) (bool, error) {
		if t.Status != Active && t.Status != Paused || t.ActiveAttempt.ID != attemptID {
			return false, ErrTransition
		}
		old := t.ActiveAttempt.Observation
		same := old == nil && observation == nil || old != nil && observation != nil && *old == *observation
		if same && slices.Equal(t.Warnings, warnings) {
			return false, nil
		}
		t.ActiveAttempt.Observation = nil
		if observation != nil {
			copy := *observation
			t.ActiveAttempt.Observation = &copy
		}
		t.Warnings = append([]Warning(nil), warnings...)
		return true, nil
	})
}

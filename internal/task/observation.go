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
		combined := t.observedWarnings(attemptID, warnings)
		if sameObservation(a.Observation, observation) && slices.Equal(t.Warnings, combined) {
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

func (t Task) ObservationMatches(attemptID string, observation *Observation, warnings []Warning) bool {
	for _, a := range t.Attempts {
		if a.ID != attemptID {
			continue
		}
		return sameObservation(a.Observation, observation) && slices.Equal(t.Warnings, t.observedWarnings(attemptID, warnings))
	}
	return false
}

func sameObservation(old, next *Observation) bool {
	return old == nil && next == nil || old != nil && next != nil && *old == *next
}

func (t Task) observedWarnings(attemptID string, warnings []Warning) []Warning {
	var combined []Warning
	for _, w := range t.Warnings {
		if w.AttemptID != attemptID {
			combined = append(combined, w)
		}
	}
	for _, w := range warnings {
		w.AttemptID = attemptID
		combined = append(combined, w)
	}
	return combined
}

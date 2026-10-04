package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
)

var ErrSharedConflict = errors.New("несовместимые изменения общего плана; обе версии сохранены")

type Receipt struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

func Shared(p Plan) Plan {
	p.Team = nil
	p.ServerEvents = nil
	p.Tasks = slices.Clone(p.Tasks)
	for i := range p.Tasks {
		t := &p.Tasks[i]
		*t = t.clone()
		t.ActiveAttempt = nil
		t.Warnings = nil
		for j := range t.Attempts {
			t.Attempts[j].Observation = nil
		}
	}
	return p
}

func SharedBytes(p Plan) ([]byte, error) { return json.Marshal(Shared(p)) }

func ValidateShared(data []byte) (Plan, error) {
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return Plan{}, err
	}
	canonical, err := SharedBytes(p)
	if err != nil {
		return Plan{}, err
	}
	if !bytes.Equal(canonicalActionJSON(canonical), canonicalActionJSON(data)) {
		return Plan{}, invalid("локальные поля в общем плане")
	}
	return p, nil
}

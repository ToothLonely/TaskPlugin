package task

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"

	"git-task/internal/diagnostic"
)

func Queue(before, after Plan) (Plan, error) {
	if before.Team == nil {
		return after, nil
	}
	team := *before.Team
	team.Pending = slices.Clone(team.Pending)
	if after.Team != nil {
		team.LocalAttempts = slices.Clone(after.Team.LocalAttempts)
	}
	b, err := SharedBytes(before)
	if err != nil {
		return Plan{}, err
	}
	a, err := SharedBytes(after)
	if err != nil {
		return Plan{}, err
	}
	var bp, ap Plan
	if err := json.Unmarshal(b, &bp); err != nil {
		return Plan{}, err
	}
	if err := json.Unmarshal(a, &ap); err != nil {
		return Plan{}, err
	}
	bp.Revision, ap.Revision = 0, 0
	for i := range bp.Tasks {
		bp.Tasks[i].Revision = 0
	}
	for i := range ap.Tasks {
		ap.Tasks[i].Revision = 0
	}
	if !reflect.DeepEqual(bp, ap) {
		id, err := NewID()
		if err != nil {
			return Plan{}, err
		}
		team.Pending = append(team.Pending, Action{ID: id, Before: b, After: a})
	}
	after.Team = &team
	return after, nil
}

func (a Action) Digest() string {
	h := sha256.New()
	h.Write(canonicalActionJSON(a.Before))
	h.Write([]byte{0})
	h.Write(canonicalActionJSON(a.After))
	return hex.EncodeToString(h.Sum(nil))
}

func canonicalActionJSON(data []byte) []byte {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return data
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return data
	}
	return encoded
}

func Replay(remote Plan, actions []Action) (Plan, error) {
	remote = Shared(remote)
	if err := remote.Validate(); err != nil {
		return Plan{}, err
	}
	receipts := make(map[string]string, len(remote.Actions)+len(actions))
	for _, r := range remote.Actions {
		receipts[r.ID] = r.Digest
	}
	snapshots := map[string]Plan{}
	canonical := map[string][]byte{}
	read := func(data []byte) (Plan, error) {
		key := string(data)
		if p, ok := snapshots[key]; ok {
			return p, nil
		}
		var p Plan
		if err := json.Unmarshal(data, &p); err != nil {
			return Plan{}, err
		}
		snapshots[key] = p
		return p, nil
	}
	digest := func(a Action) string {
		h := sha256.New()
		for i, data := range []json.RawMessage{a.Before, a.After} {
			if i != 0 {
				h.Write([]byte{0})
			}
			key := string(data)
			value, ok := canonical[key]
			if !ok {
				value = canonicalActionJSON(data)
				canonical[key] = value
			}
			h.Write(value)
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	for _, action := range actions {
		digest := digest(action)
		if previous, found := receipts[action.ID]; found {
			if previous != digest {
				return Plan{}, diagnostic.Errorf("%w: коллизия действия %s", ErrSharedConflict, action.ID)
			}
			continue
		}
		before, err := read(action.Before)
		if err != nil {
			return Plan{}, err
		}
		after, err := read(action.After)
		if err != nil {
			return Plan{}, err
		}
		merged, err := MergeShared(before, after, remote)
		if err != nil {
			return Plan{}, err
		}
		merged.Actions = append(slices.Clone(remote.Actions), Receipt{ID: action.ID, Digest: digest})
		remote = merged
		receipts[action.ID] = digest
	}
	return remote, remote.Validate()
}

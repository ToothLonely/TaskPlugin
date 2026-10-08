package transfer

import (
	"encoding/json"

	"git-task/internal/task"
)

func DecodeJSON(data []byte) (task.Plan, error) {
	var plan task.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return task.Plan{}, err
	}
	return task.Shared(plan), nil
}

func EncodeJSON(plan task.Plan) ([]byte, error) {
	plan = task.Shared(plan)
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

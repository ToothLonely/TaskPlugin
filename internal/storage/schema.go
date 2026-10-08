package storage

import (
	"encoding/json"
	"errors"
	"fmt"

	"git-task/internal/task"
)

var ErrUnsupportedSchema = errors.New("неподдерживаемая версия схемы")

func schemaVersion(data []byte) int {
	var header struct {
		Version int `json:"schema_version"`
	}
	if json.Unmarshal(data, &header) != nil {
		return 0
	}
	return header.Version
}

func decode(data []byte) (task.Plan, error) {
	var header struct {
		Format  string `json:"format"`
		Version int    `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return task.Plan{}, fmt.Errorf("повреждённый JSON: %w", err)
	}
	if header.Format != task.Format {
		return task.Plan{}, fmt.Errorf("чужой формат плана %q", header.Format)
	}
	if header.Version != task.SchemaVersion {
		return task.Plan{}, fmt.Errorf("%w %d; поддерживается только схема %d", ErrUnsupportedSchema, header.Version, task.SchemaVersion)
	}
	var plan task.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return task.Plan{}, err
	}
	return plan, nil
}

func encode(plan task.Plan) ([]byte, error) {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

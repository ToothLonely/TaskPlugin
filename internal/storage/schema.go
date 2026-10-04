package storage

import (
	"encoding/json"
	"fmt"

	"git-task/internal/task"
)

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
	if header.Version == 1 {
		return task.MigrateV1(data)
	}
	// There are no released earlier schemas to migrate. Never guess at a future
	// schema or rewrite it using a decoder that could lose fields.
	if header.Version != task.SchemaVersion {
		return task.Plan{}, fmt.Errorf("неподдерживаемая версия схемы %d", header.Version)
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

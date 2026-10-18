package storage

import (
	"encoding/json"

	"git-task/internal/diagnostic"
	"git-task/internal/task"
)

var ErrUnsupportedSchema = diagnostic.New("неподдерживаемая версия схемы")

func schemaVersion(data []byte) int {
	var header struct {
		Version int `json:"schema_version"`
	}
	if json.Unmarshal(data, &header) != nil {
		return 0
	}
	return header.Version
}

type documentHeader struct {
	Format  json.RawMessage `json:"format"`
	Version int             `json:"schema_version"`
}

func readHeader(data []byte) (documentHeader, error) {
	var header documentHeader
	err := json.Unmarshal(data, &header)
	return header, err
}

func decode(data []byte) (task.Plan, error) {
	header, err := readHeader(data)
	if err != nil {
		return task.Plan{}, diagnostic.Errorf("повреждённый JSON: %w", err)
	}
	return decodePlan(data, header)
}

func decodePlan(data []byte, header documentHeader) (task.Plan, error) {
	var format string
	if err := json.Unmarshal(header.Format, &format); err != nil {
		return task.Plan{}, diagnostic.Errorf("повреждённый JSON: %w", err)
	}
	if format != task.Format {
		return task.Plan{}, diagnostic.Errorf("чужой формат плана %q", format)
	}
	if header.Version != task.SchemaVersion {
		return task.Plan{}, diagnostic.Errorf("%w %d; поддерживается только схема %d", ErrUnsupportedSchema, header.Version, task.SchemaVersion)
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

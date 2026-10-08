package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestPlanRejectsUnsupportedSchemas(t *testing.T) {
	plan, err := NewPlan("main")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{1, 2, 99} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			old := bytes.Replace(data, []byte(`"schema_version":3`), []byte(fmt.Sprintf(`"schema_version":%d`, version)), 1)
			value := plan
			if err := json.Unmarshal(old, &value); !errors.Is(err, ErrInvalid) {
				t.Fatalf("unsupported schema accepted: %v", err)
			}
			preserved, err := json.Marshal(value)
			if err != nil || !bytes.Equal(data, preserved) {
				t.Fatalf("receiver replaced on invalid schema: %v", err)
			}
		})
	}
}

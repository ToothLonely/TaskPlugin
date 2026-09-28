package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestActiveStatusJSONContract(t *testing.T) {
	p := planForTest(t)
	changed, err := p.Start("task-a", attempt("attempt-active", "feature"), false)
	requireChange(t, changed, err)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"status":"active"`)) || p.Tasks[0].Status != Active {
		t.Fatalf("active state: %s", data)
	}
	var restored Plan
	if err := json.Unmarshal(data, &restored); err != nil || restored.Tasks[0].Status != Active {
		t.Fatalf("roundtrip: %v", err)
	}
	// The former spelling remains here only as a rejected input regression.
	for _, invalid := range []string{"in_progress", "Active", "running"} {
		input := bytes.Replace(data, []byte(`"status":"active"`), []byte(`"status":"`+invalid+`"`), 1)
		if err := json.Unmarshal(input, &restored); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted noncanonical state %q: %v", invalid, err)
		}
	}
}

package transfer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"git-task/internal/task"
)

func TestJSONRoundTripPreservesFullState(t *testing.T) {
	plan, err := decodeMarkdownFixture(t, []byte("- [x] История\n- [ ] Пауза\n- [ ] Архив\n- [ ] Не начато"), "main")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 30, 1, 123, time.UTC)
	for i, id := range []string{"task-001", "task-002", "task-003"} {
		branch := "feature/" + id
		a := task.Attempt{ID: "active-" + id, Branch: branch, OriginalBranch: branch, TargetBranch: "main", BaseCommit: strings.Repeat("a", 40), StartedAt: &now}
		if _, err = plan.Start(id, a, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = plan.Pause("task-002"); err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Archive("task-003"); err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Rebind("task-001", "replacement", strings.Repeat("b", 40), now); err != nil {
		t.Fatal(err)
	}
	description := "Описание\nUnicode 🙂"
	if _, err = plan.Edit("task-001", task.EditOptions{Description: &description}); err != nil {
		t.Fatal(err)
	}
	data, err := EncodeJSON(plan)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeJSON(data)
	if err != nil || !reflect.DeepEqual(got, task.Shared(plan)) {
		t.Fatalf("round-trip: %v\n%+v\n%+v", err, got, plan)
	}
	var wire struct {
		Tasks []map[string]json.RawMessage `json:"tasks"`
	}
	if err = json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Tasks[0]["attempts"] == nil || wire.Tasks[0]["active_attempt"] != nil || wire.Tasks[3]["attempts"] != nil {
		t.Fatalf("history separation: %s", data)
	}
}

func TestJSONImportRejectsUnsupportedSchemas(t *testing.T) {
	plan, err := task.NewPlan("main")
	if err != nil {
		t.Fatal(err)
	}
	data, err := EncodeJSON(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{1, 2, 99} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			old := bytes.Replace(data, []byte(`"schema_version": 3`), []byte(fmt.Sprintf(`"schema_version": %d`, version)), 1)
			if value, err := DecodeJSON(old); err == nil || len(value.Tasks) != 0 {
				t.Fatalf("unsupported schema imported: %+v %v", value, err)
			}
		})
	}
}

func TestJSONRejectsDamagedVersionIdentitiesAndInvariants(t *testing.T) {
	plan, err := decodeMarkdownFixture(t, []byte("- [ ] One\n- [x] Two"), "main")
	if err != nil {
		t.Fatal(err)
	}
	data, err := EncodeJSON(plan)
	if err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string][]byte{
		"syntax":       data[:len(data)/2],
		"version":      bytes.Replace(data, []byte(`"schema_version": 3`), []byte(`"schema_version": 99`), 1),
		"duplicate ID": bytes.ReplaceAll(data, []byte("task-002"), []byte("task-001")),

		"missing order ref": bytes.Replace(data, []byte(`"task-002"`), []byte(`"missing"`), 1),
		"unknown status":    bytes.Replace(data, []byte(`"todo"`), []byte(`"unknown"`), 1),
		"source":            bytes.Replace(data, []byte(`"imported"`), []byte(`"unknown"`), 1),
		"empty history":     bytes.Replace(data, []byte(`"done"`), []byte(`"active"`), 1),
		"unknown field":     bytes.Replace(data, []byte(`"last_event":`), []byte(`"cache": 1, "last_event":`), 1),
		"duplicate key":     bytes.Replace(data, []byte(`"last_event":`), []byte(`"last_event": 2, "last_event":`), 1),
		"null":              []byte("null"), "trailing": append(append([]byte(nil), data...), []byte("{}")...),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeJSON(invalid)
			if err == nil || len(got.Tasks) != 0 {
				t.Fatalf("accepted partial=%+v err=%v", got, err)
			}
		})
	}
}

package task

import (
	"encoding/json"
	"strings"
	"testing"
)

func legacyMigrationData() []byte {
	return []byte(`{"format":"git-task","schema_version":1,"revision":7,"target_branch":"main","order":["legacy-task"],"tasks":[{"id":"legacy-task","number":"T-007","title":"Legacy","revision":5,"status":"paused","active_attempt":{"id":"active-id","branch":"feature","original_branch":"feature","target_branch":"main","base_commit":"` + strings.Repeat("a", 40) + `","started_at":"2026-01-01T00:00:00Z"},"attempts":[{"id":"historic-id","completion":{"event":1,"source":"manual","target_branch":"main"}}]}],"last_event":1,"insertion_tail":"legacy-task"}`)
}

func TestMigrationPreservesIDsHistoryAndUnknownAuthor(t *testing.T) {
	p, err := MigrateV1(legacyMigrationData())
	if err != nil {
		t.Fatal(err)
	}
	item, err := p.FindID("legacy-task")
	if err != nil || p.SchemaVersion != 2 || p.Revision != 7 || item.Revision != 5 || len(item.Attempts) != 2 || item.Status != Paused {
		t.Fatalf("migration: %+v %v", p, err)
	}
	if item.Attempts[0].ID != "historic-id" || item.Attempts[0].Completion.Source != Manual || item.Attempts[1].ID != "active-id" || item.Attempts[1].Status != Paused {
		t.Fatal("IDs/history lost")
	}
	for _, a := range item.Attempts {
		if a.Author != "" {
			t.Fatal("author invented")
		}
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "active_attempt") || strings.Contains(string(data), "author") {
		t.Fatalf("wrong migrated JSON: %s", data)
	}
	var decoded Plan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationStrictlyRejectsDamagedAndFutureSchemas(t *testing.T) {
	valid := string(legacyMigrationData())
	for _, data := range []string{
		strings.Replace(valid, `"schema_version":1`, `"schema_version":99`, 1),
		strings.Replace(valid, `"status":"paused"`, `"status":"done"`, 1),
		strings.Replace(valid, `"status":"paused"`, `"status":"paused","status":"active"`, 1),
		strings.Replace(valid, `"active_attempt":`, `"unknown_attempt":`, 1),
		strings.Replace(valid, `"id":"active-id"`, `"id":"historic-id"`, 1),
		strings.Replace(valid, `"id":"active-id"`, `"id":"active-id","author":"fabricated"`, 1),
	} {
		if _, err := MigrateV1([]byte(data)); err == nil {
			t.Fatalf("accepted invalid legacy: %s", data)
		}
	}
}

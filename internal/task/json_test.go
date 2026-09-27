package task

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func snapshot(t *testing.T, p Plan) string {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestJSONLifecycleRoundTrip(t *testing.T) {
	p := planForTest(t)
	check := func(history, active bool) {
		t.Helper()
		data := snapshot(t, p)
		var raw struct {
			Tasks []map[string]json.RawMessage `json:"tasks"`
		}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			t.Fatal(err)
		}
		_, hasHistory := raw.Tasks[0]["attempts"]
		_, hasActive := raw.Tasks[0]["active_attempt"]
		if hasHistory != history || hasActive != active {
			t.Fatalf("unexpected JSON fields: %s", data)
		}
		var decoded Plan
		if err := json.Unmarshal([]byte(data), &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p, decoded) {
			t.Fatalf("round-trip lost information:\n%+v\n%+v", p, decoded)
		}
		if snapshot(t, decoded) != data {
			t.Fatal("unstable JSON encoding")
		}
	}
	check(false, false)
	if !strings.Contains(snapshot(t, p), `"status":"todo"`) {
		t.Fatal("noncanonical todo")
	}
	changed, err := p.Start("task-a", attempt("first", "feature"), false)
	requireChange(t, changed, err)
	check(false, true)
	changed, err = p.Pause("task-a")
	requireChange(t, changed, err)
	check(false, true)
	changed, err = p.Rebind("task-a", "replacement", testOID, testTime)
	requireChange(t, changed, err)
	check(false, true)
	changed, err = p.CompleteManual("task-a", "ignored", "", testTime)
	requireChange(t, changed, err)
	check(true, false)
	changed, err = p.Start("task-a", attempt("second", "next"), true)
	requireChange(t, changed, err)
	check(true, true)
	changed, err = p.SetWarnings("task-a", []Warning{{Code: "uncertain", Message: "Недостаточно данных"}})
	requireChange(t, changed, err)
	check(true, true)
	changed, err = p.Archive("task-a")
	requireChange(t, changed, err)
	check(true, true)
}

func TestJSONImportedAndManualOmitUnknownFacts(t *testing.T) {
	for _, source := range []Source{Imported, Manual} {
		t.Run(string(source), func(t *testing.T) {
			p := planForTest(t)
			changed, err := p.Complete("task-a", "no-git", Completion{Source: source, TargetBranch: "main"})
			requireChange(t, changed, err)
			data := snapshot(t, p)
			for _, key := range []string{"branch", "base_commit", "started_at", "completed_at", "observed_at", "commit", "merge_kind", "work_commit"} {
				if strings.Contains(data, `"`+key+`":`) {
					t.Fatalf("invented %s: %s", key, data)
				}
			}
			var decoded Plan
			if err := json.Unmarshal([]byte(data), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Tasks[0].Attempts[0].Completion.Source != source {
				t.Fatal("source lost")
			}
		})
	}
}

func TestJSONRejectsMalformedWithoutReplacingReceiver(t *testing.T) {
	base := snapshot(t, planForTest(t))
	cases := map[string]string{
		"unknown status":    strings.Replace(base, `"status":"todo"`, `"status":"pending"`, 1),
		"null status":       strings.Replace(base, `"status":"todo"`, `"status":null`, 1),
		"unknown schema":    strings.Replace(base, `"schema_version":1`, `"schema_version":2`, 1),
		"foreign format":    strings.Replace(base, `"format":"git-task"`, `"format":"other"`, 1),
		"duplicate key":     strings.Replace(base, `"status":"todo"`, `"status":"done","status":"todo"`, 1),
		"unknown field":     strings.Replace(base, `"status":"todo"`, `"status":"todo","typo":true`, 1),
		"empty history":     strings.Replace(base, `"status":"todo"`, `"status":"todo","attempts":[]`, 1),
		"null history":      strings.Replace(base, `"status":"todo"`, `"status":"todo","attempts":null`, 1),
		"null active":       strings.Replace(base, `"status":"todo"`, `"status":"todo","active_attempt":null`, 1),
		"empty warnings":    strings.Replace(base, `"status":"todo"`, `"status":"todo","warnings":[]`, 1),
		"case alias":        strings.Replace(base, `"status":"todo"`, `"Status":"todo"`, 1),
		"missing counter":   strings.Replace(base, `,"last_event":0`, ``, 1),
		"truncated":         base[:len(base)-1],
		"trailing JSON":     base + ` {}`,
		"overflow":          strings.Replace(base, `"revision":0`, `"revision":18446744073709551616`, 1),
		"negative revision": strings.Replace(base, `"revision":0`, `"revision":-1`, 1),
		"fraction revision": strings.Replace(base, `"revision":0`, `"revision":1.5`, 1),
		"invalid UTF8":      strings.Replace(base, `"todo"`, "\"\xff\"", 1),
		"null plan":         `null`,
		"array plan":        `[]`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			p := planInState(t, Done)
			before := snapshot(t, p)
			err := json.Unmarshal([]byte(data), &p)
			if err == nil || snapshot(t, p) != before {
				t.Fatalf("accepted invalid JSON or changed receiver: %v", err)
			}
		})
	}
}

func TestValidateInvalidSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Plan)
	}{
		{"duplicate ID", func(p *Plan) { p.Tasks[1].ID = p.Tasks[0].ID }},
		{"duplicate number", func(p *Plan) { p.Tasks[1].Number = p.Tasks[0].Number }},
		{"blank title", func(p *Plan) { p.Tasks[0].Title = " \t" }},
		{"blank ID", func(p *Plan) { p.Tasks[0].ID = "" }},
		{"missing order", func(p *Plan) { p.Order = p.Order[:1] }},
		{"duplicate order", func(p *Plan) { p.Order[1] = p.Order[0] }},
		{"unknown order", func(p *Plan) { p.Order[1] = "unknown" }},
		{"unknown tail", func(p *Plan) { p.InsertionTail = "unknown" }},
		{"bad status", func(p *Plan) { p.Tasks[0].Status = "TODO" }},
		{"done without history", func(p *Plan) { p.Tasks[0].Status = Done; p.Tasks[0].ActiveAttempt = nil }},
		{"active without attempt", func(p *Plan) { p.Tasks[0].ActiveAttempt = nil }},
		{"todo with attempt", func(p *Plan) { p.Tasks[0].Status = Todo }},
		{"history without completion", func(p *Plan) { p.Tasks[0].Attempts = []Attempt{attempt("old", "old-branch")} }},
		{"target mismatch", func(p *Plan) { p.TargetBranch = "other" }},
		{"invalid OID", func(p *Plan) { p.Tasks[0].ActiveAttempt.BaseCommit = "abc" }},
		{"missing start date", func(p *Plan) { p.Tasks[0].ActiveAttempt.StartedAt = nil }},
		{"invalid original branch", func(p *Plan) { p.Tasks[0].ActiveAttempt.OriginalBranch = "other" }},
		{"unknown event", func(p *Plan) { p.LastEvent = 1 }},
		{"empty warning", func(p *Plan) { p.Tasks[0].Warnings = []Warning{{Code: "missing"}} }},
		{"active completed", func(p *Plan) {
			p.Tasks[0].ActiveAttempt.Completion = &Completion{Event: 1, Source: Manual, TargetBranch: "main"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := planInState(t, InProgress)
			tc.mutate(&p)
			if err := p.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate: %v", err)
			}
			if _, err := json.Marshal(p); err == nil {
				t.Fatal("serialized invalid plan")
			}
		})
	}
}

func TestInvalidCompletionIsAtomic(t *testing.T) {
	cases := []Completion{
		{Source: Merge, TargetBranch: "main"},
		{Source: Merge, TargetBranch: "main", Commit: testOID, WorkCommit: testOID, MergeKind: "squash", ObservedAt: &testTime},
		{Source: Manual, TargetBranch: "main", MergeKind: FastForward},
		{Source: Imported, TargetBranch: "main"},
		{Source: "unknown", TargetBranch: "main"},
		{Source: Manual, TargetBranch: "other"},
		{Source: Manual, TargetBranch: "main", Commit: "bad"},
		{Source: Manual, TargetBranch: "main", Event: 100},
	}
	for i, c := range cases {
		p := planInState(t, Paused)
		before := snapshot(t, p)
		changed, err := p.Complete("task-a", "active", c)
		if changed || err == nil || before != snapshot(t, p) {
			t.Fatalf("case %d: changed=%v err=%v", i, changed, err)
		}
	}
}

func FuzzPlanJSON(f *testing.F) {
	const todo = `{"format":"git-task","schema_version":1,"revision":0,"target_branch":"main","order":["task-a"],"tasks":[{"id":"task-a","number":"T-001","title":"original","revision":0,"status":"todo"}],"last_event":0}`
	for _, title := range []string{`\ud800`, `\udc00`, `\ud800X`, `\ud83d\ude80`, `\ufffd`, `\\ud800`} {
		f.Add([]byte(strings.Replace(todo, `"original"`, `"`+title+`"`, 1)))
	}
	for _, offset := range []string{"+24:00", "+03:60", "+03:30", "-23:59"} {
		data := strings.Replace(todo, `"status":"todo"`, `"status":"done","attempts":[{"id":"imported","completion":{"event":1,"source":"imported","target_branch":"main","observed_at":"2026-09-27T10:00:00`+offset+`"}}]`, 1)
		data = strings.Replace(data, `"last_event":0`, `"last_event":1,"insertion_tail":"task-a"`, 1)
		f.Add([]byte(data))
	}
	f.Add([]byte(`{"format":"git-task","schema_version":1,"revision":0,"target_branch":"main","order":[],"tasks":[],"last_event":0}`))
	f.Add([]byte(`{"tasks":[{"attempts":null}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var p Plan
		if err := json.Unmarshal(data, &p); err != nil {
			return
		}
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var again Plan
		if err := json.Unmarshal(encoded, &again); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p, again) {
			t.Fatal("valid input loses information in round-trip")
		}
	})
}

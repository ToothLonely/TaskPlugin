package task

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestJSONRejectsUnpairedSurrogates(t *testing.T) {
	for _, escaped := range []string{`\ud800`, `\udc00`, `\ud800X`, `\ud800\u0041`, `\ud800\ud800`, `\udc00\ud800`, `\ud83d\ude80\udfff`} {
		for _, field := range []string{"title", "description", "id", "number"} {
			t.Run(field+"/"+escaped, func(t *testing.T) {
				p := planForTest(t)
				p.Tasks[0].Description = "original"
				before := snapshot(t, p)
				value := map[string]string{"title": "Профиль", "description": "original", "id": "task-a", "number": "T-001"}[field]
				data := strings.Replace(before, `"`+field+`":"`+value+`"`, `"`+field+`":"`+escaped+`"`, 1)
				if field == "id" {
					// Keep order consistent so this cannot pass merely because the
					// replacement character produces a missing task reference.
					data = strings.ReplaceAll(data, `"task-a"`, `"`+escaped+`"`)
				}
				err := json.Unmarshal([]byte(data), &p)
				if !errors.Is(err, ErrInvalid) || snapshot(t, p) != before {
					t.Fatalf("malformed escape accepted or receiver changed: %v", err)
				}
			})
		}
	}
}

func TestJSONValidUnicodeEscapes(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`\ud83d\ude80`, "🚀"}, {`\uD83D\uDE80`, "🚀"},
		{`\ud800\udc00`, "𐀀"}, {`\udbff\udfff`, "\U0010ffff"},
		{`\ufffd`, "�"}, {`�`, "�"}, {`Профиль`, "Профиль"},
		{`\\ud800`, `\ud800`}, {`\"\\udc00`, `"\udc00`},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			p := planForTest(t)
			data := strings.Replace(snapshot(t, p), `"title":"Профиль"`, `"title":"`+tc.raw+`"`, 1)
			if err := json.Unmarshal([]byte(data), &p); err != nil {
				t.Fatal(err)
			}
			if p.Tasks[0].Title != tc.want {
				t.Fatalf("title=%q, want %q", p.Tasks[0].Title, tc.want)
			}
			var again Plan
			if err := json.Unmarshal([]byte(snapshot(t, p)), &again); err != nil {
				t.Fatal(err)
			}
			if again.Tasks[0].Title != tc.want {
				t.Fatal("Unicode changed during round-trip")
			}
		})
	}
}

func TestJSONTimestampOffsets(t *testing.T) {
	for _, field := range []string{"started_at", "completed_at", "observed_at", "rebind_observed_at"} {
		for _, offset := range []string{"+24:00", "-24:00", "+03:60", "-03:60", "+23:59", "-23:59", "+03:30", "-04:00", "Z"} {
			t.Run(field+"/"+offset, func(t *testing.T) {
				p := planInState(t, InProgress)
				if field == "rebind_observed_at" {
					changed, err := p.Rebind("task-a", "replacement", testOID, testTime)
					requireChange(t, changed, err)
				} else if field != "started_at" {
					changed, err := p.CompleteManual("task-a", "ignored", "", testTime)
					requireChange(t, changed, err)
				}
				before := snapshot(t, p)
				key := strings.TrimPrefix(field, "rebind_")
				stamp := "2026-09-27T10:00:00" + offset
				data := strings.Replace(before, `"`+key+`":"`+testTime.Format(time.RFC3339Nano)+`"`, `"`+key+`":"`+stamp+`"`, 1)
				err := json.Unmarshal([]byte(data), &p)
				bad := strings.Contains(offset, "24:") || strings.HasSuffix(offset, ":60")
				if bad {
					if !errors.Is(err, ErrInvalid) || snapshot(t, p) != before {
						t.Fatalf("invalid offset accepted or receiver changed: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(snapshot(t, p), `"`+key+`":"`+stamp+`"`) {
					t.Fatal("valid offset changed during round-trip")
				}
			})
		}
	}
}

func TestInvalidTimeTransitionsAreAtomic(t *testing.T) {
	for _, offset := range []int{86400, -86400, 86460, -86460, 1, -1, 86399, -86399} {
		at := time.Date(2026, 9, 27, 10, 0, 0, 0, time.FixedZone("test", offset))
		for _, action := range []string{"start", "rebind", "complete", "observed"} {
			t.Run(at.String()+"/"+action, func(t *testing.T) {
				p := planForTest(t)
				if action == "rebind" {
					p = planInState(t, InProgress)
				}
				before := snapshot(t, p)
				var changed bool
				var err error
				switch action {
				case "start":
					a := attempt("new", "feature")
					a.StartedAt = &at
					changed, err = p.Start("task-a", a, false)
				case "rebind":
					changed, err = p.Rebind("task-a", "replacement", testOID, at)
				case "complete":
					changed, err = p.CompleteManual("task-a", "manual", "", at)
				case "observed":
					changed, err = p.Complete("task-a", "imported", Completion{Source: Imported, TargetBranch: "main", ObservedAt: &at})
				}
				if changed || !errors.Is(err, ErrInvalid) || snapshot(t, p) != before {
					t.Fatalf("invalid time committed: changed=%v, err=%v", changed, err)
				}
			})
		}
	}
}

func TestValidTimeOffsetTransition(t *testing.T) {
	for _, offset := range []int{0, 12600, -14400, 86340, -86340} {
		at := time.Date(2026, 9, 27, 10, 0, 0, 123, time.FixedZone("test", offset))
		p := planForTest(t)
		changed, err := p.CompleteManual("task-a", "manual", "", at)
		requireChange(t, changed, err)
		var again Plan
		if err := json.Unmarshal([]byte(snapshot(t, p)), &again); err != nil {
			t.Fatal(err)
		}
		got := again.Tasks[0].Attempts[0].Completion.CompletedAt
		_, gotOffset := got.Zone()
		if !got.Equal(at) || gotOffset != offset {
			t.Fatalf("date changed: got %s, want %s", got, at)
		}
	}
}

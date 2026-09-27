package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// orderedPlan models the domain result of importing unchecked rows. Parsing
// Markdown and confirming the import belong to the transfer/application layers.
func orderedPlan(t *testing.T, ids ...string) Plan {
	t.Helper()
	p, err := NewPlan("main")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		p.Tasks = append(p.Tasks, Task{ID: id, Number: "display-" + id, Title: id, Status: Todo})
		p.Order = append(p.Order, id)
		p.InsertionTail = id
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlanOrderSequences(t *testing.T) {
	type step struct {
		operation, id string
		position      Position
		want          string
		tail          string
	}
	tests := []struct {
		name    string
		initial []string
		steps   []step
	}{
		{"empty", nil, []step{
			{operation: "add", id: "A", want: "A", tail: "A"},
			{operation: "add", id: "B", want: "A B", tail: "B"},
		}},
		{"unchecked import", []string{"A", "B"}, []step{
			{operation: "add", id: "X", want: "A B X", tail: "X"},
			{operation: "add", id: "Y", want: "A B X Y", tail: "Y"},
		}},
		{"completion out of plan order and clocks going backwards", []string{"A", "B", "C"}, []step{
			{operation: "complete", id: "C", want: "A B C", tail: "C"},
			{operation: "add", id: "X", want: "A B C X", tail: "X"},
			{operation: "complete", id: "A", want: "A B C X", tail: "A"},
			{operation: "add", id: "Y", want: "A Y B C X", tail: "Y"},
			{operation: "add", id: "Z", want: "A Y Z B C X", tail: "Z"},
		}},
		{"successive groups", []string{"A", "B", "C"}, []step{
			{operation: "complete", id: "A", want: "A B C", tail: "A"},
			{operation: "add", id: "X", want: "A X B C", tail: "X"},
			{operation: "add", id: "Y", want: "A X Y B C", tail: "Y"},
			{operation: "complete", id: "C", want: "A X Y B C", tail: "C"},
			{operation: "add", id: "Z", want: "A X Y B C Z", tail: "Z"},
		}},
		{"imported completions without dates", []string{"A", "B", "C", "D"}, []step{
			{operation: "imported", id: "A", want: "A B C D", tail: "A"},
			{operation: "imported", id: "C", want: "A B C D", tail: "C"},
			{operation: "add", id: "X", want: "A B C X D", tail: "X"},
			{operation: "add", id: "Y", want: "A B C X Y D", tail: "Y"},
		}},
		{"moving and archiving the tail", []string{"A", "B"}, []step{
			{operation: "complete", id: "A", want: "A B", tail: "A"},
			{operation: "add", id: "X", want: "A X B", tail: "X"},
			{operation: "add", id: "Y", want: "A X Y B", tail: "Y"},
			{operation: "move", id: "Y", position: Position{End: true}, want: "A X B Y", tail: "Y"},
			{operation: "archive", id: "Y", want: "A X B Y", tail: "Y"},
			{operation: "add", id: "Z", want: "A X B Y Z", tail: "Z"},
		}},
		{"explicit additions keep the automatic tail", []string{"A", "B"}, []step{
			{operation: "complete", id: "A", want: "A B", tail: "A"},
			{operation: "add", id: "E", position: Position{End: true}, want: "A B E", tail: "A"},
			{operation: "add", id: "F", position: Position{Before: "A"}, want: "F A B E", tail: "A"},
			{operation: "add", id: "G", position: Position{After: "A"}, want: "F A G B E", tail: "A"},
			{operation: "add", id: "X", want: "F A X G B E", tail: "X"},
		}},
		{"virtual anchor survives explicit first insertion", nil, []step{
			{operation: "add", id: "E", position: Position{End: true}, want: "E"},
			{operation: "add", id: "A", want: "A E", tail: "A"},
			{operation: "add", id: "B", want: "A B E", tail: "B"},
		}},
		{"archived completion anchor", []string{"A", "B"}, []step{
			{operation: "complete", id: "A", want: "A B", tail: "A"},
			{operation: "archive", id: "A", want: "A B", tail: "A"},
			{operation: "move", id: "B", position: Position{Before: "A"}, want: "B A", tail: "A"},
			{operation: "add", id: "X", want: "B A X", tail: "X"},
		}},
		{"restart and delayed completion preserve insertion group", []string{"A", "B"}, []step{
			{operation: "complete", id: "A", want: "A B", tail: "A"},
			{operation: "again", id: "A", want: "A B", tail: "A"},
			{operation: "add", id: "X", want: "A X B", tail: "X"},
			{operation: "repeat", id: "A", want: "A X B", tail: "X"},
			{operation: "add", id: "Y", want: "A X Y B", tail: "Y"},
			{operation: "complete", id: "A", want: "A X Y B", tail: "A"},
			{operation: "add", id: "Z", want: "A Z X Y B", tail: "Z"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := orderedPlan(t, tt.initial...)
			ids := map[string]string{"": ""}
			for _, id := range tt.initial {
				ids[id] = id
			}
			for i, s := range tt.steps {
				old := p
				before := snapshot(t, old)
				pos := Position{After: ids[s.position.After], Before: ids[s.position.Before], End: s.position.End}
				var changed bool
				var err error
				switch s.operation {
				case "add":
					var added Task
					added, err = p.Add(s.id, "", pos)
					ids[s.id], changed = added.ID, err == nil
				case "move":
					changed, err = p.Move(ids[s.id], pos)
				case "archive":
					changed, err = p.Archive(ids[s.id])
				case "again":
					changed, err = p.Start(ids[s.id], attempt("again-"+s.id, "feature-"+s.id), true)
				case "imported":
					changed, err = p.Complete(ids[s.id], "import-"+s.id, Completion{Source: Imported, TargetBranch: "main"})
				case "complete", "repeat":
					at := testTime.Add(-time.Duration(i) * time.Hour)
					c := Completion{Source: Manual, TargetBranch: "main", CompletedAt: &at, ObservedAt: &at}
					aid := "manual-" + s.id
					current, findErr := p.FindID(ids[s.id])
					if findErr != nil {
						t.Fatal(findErr)
					}
					if s.operation != "repeat" && current.ActiveAttempt != nil {
						aid = current.ActiveAttempt.ID
					}
					changed, err = p.Complete(ids[s.id], aid, c)
				default:
					t.Fatalf("unknown operation %q", s.operation)
				}
				if err != nil || changed != (s.operation != "repeat") {
					t.Fatalf("step %d (%s %s): changed=%v, err=%v", i, s.operation, s.id, changed, err)
				}
				if snapshot(t, old) != before {
					t.Fatalf("step %d mutated the previous snapshot through shared slices", i)
				}
				var want []string
				for _, label := range strings.Fields(s.want) {
					want = append(want, ids[label])
				}
				if !slices.Equal(p.Order, want) || p.InsertionTail != ids[s.tail] {
					t.Fatalf("step %d: order=%v tail=%q; want %v tail=%q", i, p.Order, p.InsertionTail, want, ids[s.tail])
				}
				// Every next operation uses reloaded data: no hidden process-local
				// cursor or sorting on read may supply the expected ordering.
				data := snapshot(t, p)
				var reloaded Plan
				if err := json.Unmarshal([]byte(data), &reloaded); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(p, reloaded) {
					t.Fatalf("step %d: round trip changed the plan", i)
				}
				p = reloaded
			}
		})
	}
}

func TestMovePositions(t *testing.T) {
	tests := []struct {
		id   string
		pos  Position
		want string
	}{
		{"A", Position{After: "C"}, "B C A D"},
		{"A", Position{Before: "C"}, "B A C D"},
		{"D", Position{After: "A"}, "A D B C"},
		{"D", Position{Before: "A"}, "D A B C"},
		{"B", Position{End: true}, "A C D B"},
		{"A", Position{Before: "B"}, "A B C D"},
		{"B", Position{After: "A"}, "A B C D"},
		{"D", Position{End: true}, "A B C D"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%+v", tt.id, tt.pos), func(t *testing.T) {
			p := orderedPlan(t, "A", "B", "C", "D")
			// A completed task and an archived task remain valid move operands.
			changed, err := p.Complete("A", "import-A", Completion{Source: Imported, TargetBranch: "main"})
			requireChange(t, changed, err)
			changed, err = p.Archive("D")
			requireChange(t, changed, err)
			wantChanged := tt.want != "A B C D"
			expected := snapshot(t, p)
			var want Plan
			if err := json.Unmarshal([]byte(expected), &want); err != nil {
				t.Fatal(err)
			}
			want.Order = strings.Fields(tt.want)
			if wantChanged {
				want.Revision++
				for i := range want.Tasks {
					if want.Tasks[i].ID == tt.id {
						want.Tasks[i].Revision++
					}
				}
			}
			changed, err = p.Move(tt.id, tt.pos)
			if err != nil || changed != wantChanged || !reflect.DeepEqual(p, want) {
				t.Fatalf("Move: changed=%v err=%v\ngot %s\nwant %s", changed, err, snapshot(t, p), snapshot(t, want))
			}
		})
	}
	for _, pos := range []Position{{End: true}, {Before: "A"}, {After: "A"}} {
		p := orderedPlan(t, "A")
		before := snapshot(t, p)
		changed, err := p.Move("A", pos)
		if changed || (pos.End && err != nil) || (!pos.End && !errors.Is(err, ErrInvalid)) || snapshot(t, p) != before {
			t.Fatalf("singleton move %+v: changed=%v, err=%v", pos, changed, err)
		}
	}
}

func TestImportedPlanInsertion(t *testing.T) {
	for _, completed := range [][]int{nil, {0}, {0, 2}} {
		t.Run(fmt.Sprint(completed), func(t *testing.T) {
			p := orderedPlan(t)
			var ids []string
			for _, title := range []string{"A", "B", "C", "D"} {
				added, err := p.Add(title, "", Position{})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, added.ID)
			}
			for _, row := range completed {
				changed, err := p.Complete(ids[row], "import-"+ids[row], Completion{Source: Imported, TargetBranch: "main"})
				requireChange(t, changed, err)
				a := p.Tasks[row].Attempts[0]
				if a.StartedAt != nil || a.Branch != "" || a.Completion.CompletedAt != nil || a.Completion.ObservedAt != nil {
					t.Fatalf("import invented historical fields: %+v", a)
				}
			}
			last := 3
			if len(completed) > 0 {
				last = completed[len(completed)-1]
			}
			if !slices.Equal(p.Order, ids) || p.InsertionTail != ids[last] || p.LastEvent != uint64(len(completed)) {
				t.Fatalf("import result changed row order or chronology: %+v", p)
			}
			added, err := p.Add("X", "", Position{})
			if err != nil {
				t.Fatal(err)
			}
			want := slices.Insert(slices.Clone(ids), last+1, added.ID)
			if !slices.Equal(p.Order, want) {
				t.Fatalf("add after import: order=%v; want %v", p.Order, want)
			}
		})
	}
}

func TestPositionErrorsLeavePlanUnchanged(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		err  error
	}{
		{"missing after", Position{After: "missing"}, ErrNotFound},
		{"missing before", Position{Before: "missing"}, ErrNotFound},
		{"number is not ID", Position{After: "display-B"}, ErrNotFound},
		{"after and before", Position{After: "B", Before: "B"}, ErrInvalid},
		{"after and end", Position{After: "B", End: true}, ErrInvalid},
		{"before and end", Position{Before: "B", End: true}, ErrInvalid},
		{"all three", Position{After: "B", Before: "B", End: true}, ErrInvalid},
	}
	for _, tt := range tests {
		for _, op := range []string{"add", "move"} {
			t.Run(op+"/"+tt.name, func(t *testing.T) {
				p := orderedPlan(t, "A", "B")
				before := snapshot(t, p)
				var err error
				if op == "add" {
					var added Task
					added, err = p.Add("X", "", tt.pos)
					if !reflect.DeepEqual(added, Task{}) {
						t.Fatalf("failed Add returned %+v", added)
					}
				} else {
					var changed bool
					changed, err = p.Move("A", tt.pos)
					if changed {
						t.Fatal("failed Move reported a change")
					}
				}
				if !errors.Is(err, tt.err) || snapshot(t, p) != before {
					t.Fatalf("err=%v; want %v and unchanged plan", err, tt.err)
				}
			})
		}
	}
	for _, tt := range []struct {
		id  string
		pos Position
		err error
	}{
		{"A", Position{}, ErrInvalid},
		{"A", Position{Before: "A"}, ErrInvalid},
		{"A", Position{After: "A"}, ErrInvalid},
		{"missing", Position{End: true}, ErrNotFound},
	} {
		p := orderedPlan(t, "A", "B")
		before := snapshot(t, p)
		changed, err := p.Move(tt.id, tt.pos)
		if changed || !errors.Is(err, tt.err) || snapshot(t, p) != before {
			t.Fatalf("Move(%q, %+v): changed=%v err=%v", tt.id, tt.pos, changed, err)
		}
	}
}

func TestMoveRevisionOverflow(t *testing.T) {
	for _, counter := range []string{"plan", "task"} {
		t.Run(counter, func(t *testing.T) {
			p := orderedPlan(t, "A", "B")
			if counter == "plan" {
				p.Revision = math.MaxUint64
			} else {
				p.Tasks[0].Revision = math.MaxUint64
			}
			before := snapshot(t, p)
			changed, err := p.Move("A", Position{End: true})
			if changed || !errors.Is(err, ErrInvalid) || snapshot(t, p) != before {
				t.Fatalf("overflow changed plan: changed=%v err=%v", changed, err)
			}
			changed, err = p.Move("A", Position{Before: "B"})
			if changed || err != nil || snapshot(t, p) != before {
				t.Fatalf("no-op at counter limit: changed=%v err=%v", changed, err)
			}
		})
	}
}

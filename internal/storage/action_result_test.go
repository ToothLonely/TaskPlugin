package storage

import (
	"context"
	"testing"

	"git-task/internal/task"
)

func TestCommittedWriteReturnsOnlyItsPreparedActionID(t *testing.T) {
	for _, mode := range []string{"save", "import", "operation"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := initialized(t)
			ctx := context.Background()
			base := snapshot(t, s)
			data, err := task.SharedBytes(base.Plan)
			if err != nil {
				t.Fatal(err)
			}
			connected := base.Plan
			connected.Team = &task.TeamState{Remote: "origin", Base: data}
			connected.Revision++
			if _, err := s.SaveShared(ctx, base, connected); err != nil {
				t.Fatal(err)
			}
			base = snapshot(t, s)
			next := base.Plan
			if _, err := next.Add("Write receipt", "", task.Position{}); err != nil {
				t.Fatal(err)
			}
			var id string
			switch mode {
			case "save":
				_, id, err = s.SaveWithAction(ctx, base, next)
			case "import":
				_, id, err = s.ImportWithAction(ctx, base, next)
			case "operation":
				err = s.WithOperation(ctx, func(op *Operation) error {
					j, err := op.Prepare(ctx, base, next, map[string]string{"id": "receipt-operation"})
					if err != nil {
						return err
					}
					id, err = op.CommitWithAction(ctx, j)
					if err != nil {
						return err
					}
					return op.Close(j)
				})
			}
			if err != nil || id == "" {
				t.Fatalf("confirmed write missing receipt: %q %v", id, err)
			}
			current := snapshot(t, s)
			if len(current.Plan.Team.Pending) != 1 || current.Plan.Team.Pending[0].ID != id {
				t.Fatal("receipt differs from installed action")
			}
			if changed, receipt, err := s.SaveWithAction(ctx, current, current.Plan); err != nil || changed || receipt != "" {
				t.Fatalf("no-op returned an old receipt: %t %q %v", changed, receipt, err)
			}
			if _, receipt, err := s.SaveWithAction(ctx, base, next); err == nil || receipt != "" {
				t.Fatalf("conflicting write claimed a saved action: %q %v", receipt, err)
			}
		})
	}
}

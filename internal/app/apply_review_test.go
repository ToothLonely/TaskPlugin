package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyReviewTeamOfflineStatusAndReorderNoOp(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	writeManualTasks(t, f.ca)
	if _, _, err := f.a.InitTemplate(ctx, true); err != nil {
		t.Fatal(err)
	}
	base, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan := base.Plan
	plan.Tasks[0], plan.Tasks[1] = plan.Tasks[1], plan.Tasks[0]
	edited, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.ca.Dir, ".git-task", "plan.json"), edited, 0600); err != nil {
		t.Fatal(err)
	}
	if result, _, err := f.a.InitTemplate(ctx, true); err != nil || result.Applied || !bytes.Equal(edited, planBytes(t, f.ca)) {
		t.Fatalf("pure task-array reorder must preserve bytes and queue: %+v %v", result, err)
	}
	trace := filepath.Join(t.TempDir(), "git-trace.log")
	f.ca.Env = append(f.ca.Env, "GIT_TRACE="+trace)
	report, err := f.a.StatusReport(ctx)
	if err != nil || len(report.Tasks) != 3 {
		t.Fatalf("offline status: %+v %v", report, err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"git fetch ", "git push ", "git ls-remote "} {
		if bytes.Contains(data, []byte(command)) {
			t.Fatalf("status performed network command %q: %s", command, data)
		}
	}
	after, err := f.a.store.Load(ctx)
	if err != nil || len(after.Plan.Team.Pending) != len(base.Plan.Team.Pending) || !bytes.Equal(edited, planBytes(t, f.ca)) {
		t.Fatalf("status changed applied plan or offline queue: %v", err)
	}
}

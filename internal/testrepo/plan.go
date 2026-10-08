package testrepo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"git-task/internal/git"

	"git-task/internal/task"
)

func FixtureIDs(plan *task.Plan) {
	ids := map[string]string{}
	for i := range plan.Tasks {
		t := &plan.Tasks[i]
		id := fmt.Sprintf("task-%03d", i+1)
		ids[t.ID] = id
		t.ID = id
	}
	for i, id := range plan.Order {
		plan.Order[i] = ids[id]
	}
	if plan.InsertionTail != "" {
		plan.InsertionTail = ids[plan.InsertionTail]
	}
}

func FixturePlanFile(t *testing.T, c *git.Client) {
	t.Helper()
	path := filepath.Join(c.Dir, ".git-task", "plan.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan task.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	FixtureIDs(&plan)
	plan.Revision++
	data, err = json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

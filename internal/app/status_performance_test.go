package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func statusPerformanceFixture(t testing.TB, count, queue, completed, live int, team bool) (*Plans, task.Plan, []byte) {
	t.Helper()
	ctx := context.Background()
	c := testrepo.New(t)
	testrepo.Commit(t, c)
	head, err := c.BranchCommit(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := &Plans{git: c, store: storage.New(repo.Root, repo.ExcludePath, c)}
	if _, _, err := p.Init(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	plan, err := task.NewPlan("main")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("task-%06d", i)
		item := task.Task{ID: id, Title: id, Status: task.Todo}
		if i < completed {
			branch := fmt.Sprintf("completed-%d", i)
			item.Status = task.Done
			item.Attempts = []task.Attempt{{ID: "attempt-" + id, Author: "benchmark", Status: task.Done, Branch: branch, OriginalBranch: branch, TargetBranch: "main", BaseCommit: head, StartedAt: &now, Completion: &task.Completion{Event: uint64(i + 1), Source: task.Merge, TargetBranch: "main", ObservedAt: &now, Commit: head, WorkCommit: head, MergeKind: task.FastForward}}}
			plan.LastEvent++
		}
		plan.Tasks = append(plan.Tasks, item)
		plan.Order = append(plan.Order, id)
	}
	for i := 0; i < live; i++ {
		id := plan.Tasks[completed+i].ID
		branch := fmt.Sprintf("live-%d", i)
		testrepo.Run(t, c, "branch", branch)
		observation, err := p.baseline(ctx, branch, head, "main", head)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := plan.Start(id, task.Attempt{ID: "attempt-" + id, Author: "benchmark", Branch: branch, OriginalBranch: branch, TargetBranch: "main", BaseCommit: head, StartedAt: &now, Observation: observation}, false); err != nil {
			t.Fatal(err)
		}
		if i%2 != 0 {
			if _, err := plan.Pause(id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if team {
		base, err := task.SharedBytes(plan)
		if err != nil {
			t.Fatal(err)
		}
		plan.Team = &task.TeamState{Remote: "origin", Base: base}
		for _, item := range plan.Tasks {
			for _, a := range item.Attempts {
				plan.Team.LocalAttempts = append(plan.Team.LocalAttempts, a.ID)
			}
		}
		for i := 0; i < queue; i++ {
			next := plan
			if _, err := next.Add(fmt.Sprintf("queued-%d", i), "", task.Position{End: true}); err != nil {
				t.Fatal(err)
			}
			next, err = task.Queue(plan, next)
			if err != nil {
				t.Fatal(err)
			}
			plan = next
		}
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return p, plan, data
}

func BenchmarkStatusPerformance(b *testing.B) {
	for _, scenario := range []struct {
		name                          string
		count, queue, completed, live int
		team                          bool
	}{
		{"personal-100", 100, 0, 0, 0, false},
		{"personal-1000", 1000, 0, 0, 0, false},
		{"team-empty", 100, 0, 0, 0, true},
		{"team-queue", 100, 20, 0, 0, true},
		{"completed", 100, 0, 100, 0, false},
		{"active-paused", 100, 0, 0, 4, false},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			p, plan, data := statusPerformanceFixture(b, scenario.count, scenario.queue, scenario.completed, scenario.live, scenario.team)
			ctx := context.Background()
			for _, phase := range []string{"open", "load", "decode", "validate", "queue", "sync", "report", "status"} {
				if phase == "queue" && !scenario.team {
					continue
				}
				b.Run(phase, func(b *testing.B) {
					snapshot, err := p.store.Load(ctx)
					if err != nil {
						b.Fatal(err)
					}
					trace := filepath.Join(b.TempDir(), "git-trace.json")
					env := p.git.Env
					tracing := os.Getenv("GIT_TASK_BENCH_TRACE") == "1"
					if tracing {
						p.git.Env = append(append([]string(nil), env...), "GIT_TRACE2_EVENT="+filepath.ToSlash(trace))
					}
					defer func() { p.git.Env = env }()
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						switch phase {
						case "open":
							_, err = p.git.Discover(ctx)
						case "load":
							_, err = p.store.Load(ctx)
						case "decode":
							var decoded task.Plan
							err = json.Unmarshal(data, &decoded)
						case "validate":
							err = plan.Validate()
						case "queue":
							err = checkQueue(plan)
						case "sync":
							_, _, err = p.sync(ctx, snapshot, false)
						case "report":
							_, err = statusReport(plan, StatusRepository{}, false)
						case "status":
							_, err = p.StatusReport(ctx)
						}
						if err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(len(data)), "input-bytes")
					b.ReportMetric(float64(scenario.queue), "queued-actions")
					b.ReportMetric(float64(scenario.completed+scenario.live), "attempts")
					traceData, err := os.ReadFile(trace)
					if err != nil && !os.IsNotExist(err) {
						b.Fatal(err)
					}
					processes := 0
					for _, line := range bytes.Split(traceData, []byte{'\n'}) {
						var event struct {
							Event string `json:"event"`
						}
						if json.Unmarshal(line, &event) == nil && event.Event == "start" {
							processes++
						}
					}
					if tracing {
						b.ReportMetric(float64(processes)/float64(b.N), "git-processes/op")
					}
				})
			}
		})
	}
}

package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"git-task/internal/storage"
	"git-task/internal/testrepo"
)

func TestStartCrashChild(t *testing.T) {
	phase := os.Getenv("GIT_TASK_CRASH_PHASE")
	if phase == "" {
		return
	}
	p, err := Open(context.Background(), os.Getenv("GIT_TASK_CRASH_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	p.checkpoint = func(point string) error {
		if point == phase {
			os.Exit(73)
		}
		return nil
	}
	_, err = p.Start(context.Background(), StartOptions{Branch: "crashed", ID: "task-002", Again: os.Getenv("GIT_TASK_CRASH_AGAIN") == "true"})
	t.Fatalf("child did not reach crash point: %v", err)
}

func TestStartProcessCrash(t *testing.T) {
	t.Parallel()
	for _, done := range []bool{false, true} {
		for _, phase := range []string{"prepared", "created", "checked-out", "committed"} {
			t.Run(phase+fmtBool(done), func(t *testing.T) {
				p, c := startFixture(t)
				if done {
					markDone(t, p)
				}
				before := planBytes(t, c)
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartCrashChild$")
				again := "false"
				if done {
					again = "true"
				}
				cmd.Env = append(append([]string{}, c.Env...), "GIT_TASK_CRASH_ROOT="+c.Dir, "GIT_TASK_CRASH_PHASE="+phase, "GIT_TASK_CRASH_AGAIN="+again)
				output, err := cmd.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 73 {
					t.Fatalf("child: %v %s", err, output)
				}
				if phase != "committed" && !bytes.Equal(before, planBytes(t, c)) {
					t.Fatal("crash prematurely saved plan")
				}
				if _, err = p.Start(ctx, StartOptions{Branch: "repeat"}); !errors.Is(err, storage.ErrLocked) {
					t.Fatalf("stale lock not diagnosed: %v", err)
				}
				// The test has waited for the exact owning process. Simulate a separately
				// authorized unlock; product recovery never removes locks by age.
				if err = os.Remove(filepath.Join(c.Dir, ".git-task", "write.lock")); err != nil {
					t.Fatal(err)
				}
				if phase == "created" {
					testrepo.Run(t, c, "checkout", "crashed")
				}
				if _, err = p.RecoverStart(ctx); err != nil {
					t.Fatal(err)
				}
				if phase == "prepared" && !bytes.Equal(before, planBytes(t, c)) {
					t.Fatal("recovery changed no-effect plan")
				}
			})
		}
	}
}

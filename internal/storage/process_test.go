package storage

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-task/internal/git"
)

// TestStorageChild runs in a separate process with an isolated Git environment.
// Pipes form barriers; no scheduling assumptions or sleeps are needed.
func TestStorageChild(t *testing.T) {
	mode := os.Getenv("GIT_TASK_TEST_MODE")
	if mode == "" {
		return
	}
	c, err := git.New(os.Getenv("GIT_TASK_TEST_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := New(repo.Root, repo.ExcludePath, c)
	input := bufio.NewReader(os.Stdin)
	barrier := func() {
		fmt.Fprintln(os.Stdout, "READY")
		if _, err := input.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	switch {
	case mode == "writer":
		snap := snapshot(t, s)
		next := added(t, snap, os.Getenv("GIT_TASK_TEST_TITLE"))
		barrier()
		_, err := s.Save(context.Background(), snap, next)
		if errors.Is(err, ErrConflict) {
			fmt.Fprintln(os.Stdout, "CONFLICT")
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(os.Stdout, "SAVED")
	case strings.HasPrefix(mode, "crash-"):
		stage := strings.TrimPrefix(mode, "crash-")
		s.checkpoint = func(name string) error {
			if name == stage {
				barrier()
			}
			return nil
		}
		snap := snapshot(t, s)
		if _, err := s.Save(context.Background(), snap, added(t, snap, "child")); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown mode %q", mode)
	}
}

type child struct {
	cmd        *exec.Cmd
	input      io.WriteCloser
	output     *bufio.Reader
	diagnostic bytes.Buffer
}

func startChild(t *testing.T, c *git.Client, mode, title string) *child {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStorageChild$")
	cmd.Env = append(append([]string{}, c.Env...), "GIT_TASK_TEST_MODE="+mode, "GIT_TASK_TEST_ROOT="+c.Dir, "GIT_TASK_TEST_TITLE="+title)
	cmd.Dir = c.Dir
	process := &child{cmd: cmd}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	process.input = input
	process.output = bufio.NewReader(output)
	cmd.Stderr = &process.diagnostic
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	if line, err := process.output.ReadString('\n'); err != nil || strings.TrimSpace(line) != "READY" {
		t.Fatalf("child ready: %q %v", line, err)
	}
	return process
}

func (p *child) release(t *testing.T, want string) {
	t.Helper()
	if _, err := io.WriteString(p.input, "continue\n"); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(p.output)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Wait(); err != nil {
		t.Fatalf("child: %v\n%s\n%s", err, output, &p.diagnostic)
	}
	if !strings.Contains(string(output), want) {
		t.Fatalf("child output %q, want %q", output, want)
	}
}

func TestTwoProcessesDetectStaleSnapshot(t *testing.T) {
	s, c := initialized(t)
	first := startChild(t, c, "writer", "first")
	second := startChild(t, c, "writer", "second")
	first.release(t, "SAVED")
	second.release(t, "CONFLICT")
	plan, err := decode(read(t, filepath.Join(s.dir, "plan.json")))
	if err != nil || len(plan.Tasks) != 1 || plan.Tasks[0].Title != "first" {
		t.Fatalf("lost update: %v %v", plan, err)
	}
	files, _ := filepath.Glob(filepath.Join(s.dir, "pending-*.json"))
	if len(files) != 1 {
		t.Fatalf("conflicting candidate not retained: %v", files)
	}
	candidate, err := decode(read(t, files[0]))
	if err != nil || len(candidate.Tasks) != 1 || candidate.Tasks[0].Title != "second" {
		t.Fatalf("candidate: %v %v", candidate, err)
	}
}

func TestProcessCrashPreservesPlanAndBackup(t *testing.T) {
	for _, stage := range []string{"candidate", "backup", "installed"} {
		t.Run(stage, func(t *testing.T) {
			s, c := initialized(t)
			base := snapshot(t, s)
			original := read(t, filepath.Join(s.dir, "plan.json"))
			process := startChild(t, c, "crash-"+stage, "")
			if _, err := s.Save(context.Background(), base, added(t, base, "parent")); !errors.Is(err, ErrLocked) {
				t.Fatalf("parallel lock: %v", err)
			}
			if err := process.cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			io.ReadAll(process.output)
			if err := process.cmd.Wait(); err == nil {
				t.Fatal("killed process succeeded")
			}
			actual := read(t, filepath.Join(s.dir, "plan.json"))
			if stage == "installed" {
				if p, err := decode(actual); err != nil || len(p.Tasks) != 1 {
					t.Fatalf("committed data: %v %v", p, err)
				}
			} else if !bytes.Equal(actual, original) {
				t.Fatal("interruption lost original")
			}
			if stage != "candidate" && !bytes.Equal(original, read(t, filepath.Join(s.dir, "plan.backup.json"))) {
				t.Fatal("backup lost")
			}
			if _, err := s.Init(context.Background(), "main"); !errors.Is(err, ErrLocked) && !errors.Is(err, ErrInterrupted) {
				t.Fatalf("interruption not diagnosed: %v", err)
			}
			if _, err := os.Stat(filepath.Join(s.dir, "write.lock")); err != nil {
				t.Fatal("stale lock automatically removed")
			}
		})
	}
}

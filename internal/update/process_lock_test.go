package update

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateProcessLock(t *testing.T) {
	if path := os.Getenv("GIT_TASK_LOCK_CHILD"); path != "" {
		unlock, err := acquireLock(path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("locked")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		if err := unlock(); err != nil {
			t.Fatal(err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "git-task")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestUpdateProcessLock$")
	cmd.Env = append(os.Environ(), "GIT_TASK_LOCK_CHILD="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("child: %q %v", line, err)
	}
	if unlock, err := acquireLock(path); err == nil {
		_ = unlock()
		t.Fatal("concurrent process acquired lock")
	}
	if _, err := stdin.Write([]byte("release\n")); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	unlock, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
}

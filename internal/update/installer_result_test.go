package update

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestUpdateInstallerResults(t *testing.T) {
	if code := os.Getenv("UPDATE_INSTALLER_EXIT"); code != "" {
		n, err := strconv.Atoi(code)
		if err != nil {
			t.Fatal(err)
		}
		os.Exit(n)
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"0", "1", "3010", "1641"} {
		cmd := exec.CommandContext(context.Background(), path, "-test.run=^TestUpdateInstallerResults$")
		cmd.Env = append(os.Environ(), "UPDATE_INSTALLER_EXIT="+code)
		err := runNativeCommand(context.Background(), cmd)
		if (err == nil) != (code == "0") {
			t.Fatalf("code %s: %v", code, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runNativeCommand(ctx, exec.CommandContext(ctx, path)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestUpdateInstallerCancellation(t *testing.T) {
	if os.Getenv("UPDATE_INSTALLER_WAIT") == "1" {
		os.Stdout.WriteString("ready\n")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-test.run=^TestUpdateInstallerCancellation$")
	cmd.Env = append(os.Environ(), "UPDATE_INSTALLER_WAIT=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runNativeCommand(ctx, cmd) }()
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "ready\n" {
		cancel()
		<-done
		t.Fatalf("helper: %q %v", line, err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("running installer cancellation: %v", err)
	}
}

func TestUpdatePermissionChild(t *testing.T) {
	path := os.Getenv("UPDATE_DENIED_PATH")
	if path == "" {
		t.Skip("permission scenario requires explicit CI preparation")
	}
	unlock, err := acquireLock(path)
	if err == nil {
		_ = unlock()
		t.Fatal("write succeeded in denied directory")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("not an OS permission refusal: %v", err)
	}
}

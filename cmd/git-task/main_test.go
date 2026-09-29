package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"git-task/internal/testrepo"
)

func TestBinaryCommands(t *testing.T) {
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(dir, "git-task"+suffix)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"+suffix), "build", "-o", binary, "-ldflags=-X=main.version=test-build", ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if key == "PATH" || strings.HasPrefix(key, "GIT_") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "PATH="+dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(dir))
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{nil, 0, "Доступные команды:"}, {[]string{"help"}, 0, "Доступные команды:"},
		{[]string{"--version"}, 0, "git-task test-build\n"},
		{[]string{"nonsense"}, 2, "неизвестная команда"}, {[]string{"sync"}, 1, "ещё не реализована"},
	} {
		cmd := exec.CommandContext(ctx, binary, tc.args...)
		cmd.Dir, cmd.Env = dir, env
		var out, diagnostic bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostic
		err := cmd.Run()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != tc.code {
			t.Fatalf("%q: code=%d, stderr=%q", tc.args, code, &diagnostic)
		}
		if code == 0 {
			if !strings.Contains(out.String(), tc.want) || diagnostic.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", &out, &diagnostic)
			}
		} else if out.Len() != 0 || !strings.Contains(diagnostic.String(), tc.want) {
			t.Fatalf("stdout=%q stderr=%q", &out, &diagnostic)
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, git, "--exec-path="+dir, "task", "version")
	cmd.Dir, cmd.Env = dir, env
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != "git-task test-build\n" {
		t.Fatalf("git task version: %q, %v", output, err)
	}

	c := testrepo.New(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"init"}, "План создан"},
		{[]string{"add", "Реальная задача"}, "task-001"},
		{[]string{"status"}, "todo — Реальная задача"},
		{[]string{"init"}, "уже инициализирован"},
	} {
		cmd := exec.CommandContext(ctx, binary, tc.args...)
		cmd.Dir, cmd.Env = c.Dir, c.Env
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), tc.want) {
			t.Fatalf("binary %q: %v %s", tc.args, err, output)
		}
	}
	planPath := filepath.Join(c.Dir, ".git-task", "plan.json")
	before, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	selectCommand := exec.CommandContext(ctx, binary, "start", "feature", "--select")
	selectCommand.Dir, selectCommand.Env = c.Dir, c.Env
	selectCommand.Stdin = strings.NewReader("1\n")
	output, err = selectCommand.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "требует терминал") {
		t.Fatalf("binary select without terminal: %v %s", err, output)
	}
	after, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("nonterminal select changed plan: %v", err)
	}
	testrepo.Commit(t, c)
	startCommand := exec.CommandContext(ctx, binary, "start", "feature")
	startCommand.Dir, startCommand.Env = c.Dir, c.Env
	output, err = startCommand.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "task-001") || !strings.Contains(string(output), "active: feature") {
		t.Fatalf("binary automatic without terminal: %v %s", err, output)
	}
}

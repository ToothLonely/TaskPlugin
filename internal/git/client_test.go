package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMissingGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := New(t.TempDir())
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("New error=%v, want unavailable executable", err)
	}
}

func TestGitExecutableRemoved(t *testing.T) {
	c := &Client{Dir: t.TempDir(), executable: filepath.Join(t.TempDir(), "missing-git")}
	result, err := c.Run(context.Background(), "--version")
	if !errors.Is(err, ErrUnavailable) || result.ExitCode != -1 {
		t.Fatalf("result=%+v, err=%v", result, err)
	}
}

func TestInvalidWorkingDirectory(t *testing.T) {
	c := isolatedClient(t)
	c.Dir = filepath.Join(c.Dir, "missing-directory")
	result, err := c.Run(context.Background(), "--version")
	if err == nil || errors.Is(err, ErrUnavailable) || result.ExitCode != -1 {
		t.Fatalf("working directory failure misclassified: result=%+v, err=%v", result, err)
	}
}

func TestNegativeExitAndGitFailure(t *testing.T) {
	c := isolatedClient(t)
	mustRun(t, c, "commit", "--allow-empty", "-m", "initial")
	initial := outputLine(mustRun(t, c, "rev-parse", "HEAD").Stdout)
	mustRun(t, c, "commit", "--allow-empty", "-m", "next")
	tip := outputLine(mustRun(t, c, "rev-parse", "HEAD").Stdout)
	for _, tc := range []struct {
		name, first, second string
		code                int
	}{
		{"ancestor", initial, tip, 0}, {"not ancestor", tip, initial, 1},
		{"missing object", strings.Repeat("0", 40), tip, 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := c.Run(context.Background(), "merge-base", "--is-ancestor", tc.first, tc.second)
			if result.ExitCode != tc.code {
				t.Fatalf("code=%d want %d: %v", result.ExitCode, tc.code, err)
			}
			if tc.code == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var command *CommandError
			var exit *exec.ExitError
			if !errors.As(err, &command) || !errors.As(err, &exit) {
				t.Fatalf("lost exit cause: %v", err)
			}
			if len(result.Stdout) != 0 {
				t.Fatalf("unexpected stdout=%q", result.Stdout)
			}
			if tc.code == 128 && len(result.Stderr) == 0 {
				t.Fatal("missing Git diagnostic")
			}
		})
	}
}

func TestLiteralArguments(t *testing.T) {
	c := isolatedClient(t)
	literal := "кириллица \"quotes\" ; & echo injected > marker $(echo injected) `echo injected` %PATH%\nsecond line"
	mustRun(t, c, "config", "--local", "test.literal", literal)
	got := mustRun(t, c, "config", "--null", "--get", "test.literal")
	if string(got.Stdout) != literal+"\x00" {
		t.Fatalf("round trip=%q, want %q", got.Stdout, literal+"\x00")
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shell marker: %v", err)
	}
}

func TestCanceledBeforeStart(t *testing.T) {
	c := isolatedClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := c.Run(ctx, "config", "--local", "test.shouldnotexist", "yes")
	if !errors.Is(err, context.Canceled) || result.ExitCode != -1 {
		t.Fatalf("result=%+v, err=%v", result, err)
	}
	result, err = c.Run(context.Background(), "config", "--get", "test.shouldnotexist")
	if result.ExitCode != 1 || err == nil {
		t.Fatalf("canceled call changed config: %+v, %v", result, err)
	}
}

func TestProcessOutputAndCancellation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Dir: t.TempDir(), executable: executable, Env: append(os.Environ(), "GIT_TASK_HELPER=1")}
	result, err := c.Run(context.Background(), "-test.run=^TestGitProcessHelper$", "--", "output")
	var command *CommandError
	if !errors.As(err, &command) || result.ExitCode != 7 || string(result.Stdout) != " out\x00\n" || string(result.Stderr) != "err\n" {
		t.Fatalf("result=%+v, err=%v", result, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err = c.Run(ctx, "-test.run=^TestGitProcessHelper$", "--", "wait")
	if !errors.Is(err, context.DeadlineExceeded) || string(result.Stdout) != "ready\n" {
		t.Fatalf("running child cancellation: stdout=%q, err=%v", result.Stdout, err)
	}
}

// This child process exercises process I/O and cancellation without a shell or
// a platform-specific external sleep command. No repository is touched.
func TestGitProcessHelper(t *testing.T) {
	if os.Getenv("GIT_TASK_HELPER") != "1" {
		return
	}
	if os.Args[len(os.Args)-1] == "wait" {
		fmt.Fprint(os.Stdout, "ready\n")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	fmt.Fprint(os.Stdout, " out\x00\n")
	fmt.Fprint(os.Stderr, "err\n")
	os.Exit(7)
}

func TestLocalOnlyEnvironment(t *testing.T) {
	c := isolatedClient(t)
	c.Env = append(c.Env, "GIT_NO_LAZY_FETCH=0", "GIT_ALLOW_PROTOCOL=https", "GIT_TERMINAL_PROMPT=1")
	settings := map[string]string{}
	for _, entry := range c.environment() {
		key, value, _ := strings.Cut(entry, "=")
		if _, exists := settings[strings.ToUpper(key)]; exists {
			t.Fatalf("duplicate environment key: %s", key)
		}
		settings[strings.ToUpper(key)] = value
	}
	if settings["GIT_NO_LAZY_FETCH"] != "1" || settings["GIT_TERMINAL_PROMPT"] != "0" || settings["GIT_ALLOW_PROTOCOL"] != "" {
		t.Fatal("local-only policy was overridden")
	}
	// A local file transport is enough to prove transport refusal, without any
	// network access even if the implementation regresses.
	result, err := c.Run(context.Background(), "ls-remote", "file:///"+filepath.ToSlash(c.Dir))
	if err == nil || result.ExitCode == 0 {
		t.Fatal("transport unexpectedly allowed")
	}
}

func TestRepositoryEnvironmentIsolation(t *testing.T) {
	poison := t.TempDir()
	config := filepath.Join(poison, "invalid-config")
	original := []byte("this is deliberately invalid config\n")
	if err := os.WriteFile(config, original, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(poison, "not-a-repository"))
	t.Setenv("GIT_WORK_TREE", poison)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(poison, "index"))
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", poison)
	c := isolatedClient(t)
	mustRun(t, c, "commit", "--allow-empty", "-m", "isolated")
	if _, err := os.Stat(filepath.Join(poison, "index")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign index touched: %v", err)
	}
	got, err := os.ReadFile(config)
	if err != nil || string(got) != string(original) {
		t.Fatalf("foreign config changed: %v", err)
	}
}

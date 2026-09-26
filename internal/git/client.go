// Package git runs the Git executable without a shell and decodes its results.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrUnavailable means that the Git executable could not be found or started.
var ErrUnavailable = errors.New("Git недоступен; установите Git и проверьте PATH")

// Client runs Git in Dir. Env is a complete environment; nil inherits the
// process environment. Configure the client before using it and do not mutate
// it during a call. Local-only and noninteractive settings are always enforced.
type Client struct {
	Dir        string
	Env        []string
	executable string
}

// New finds Git without executing it or changing the working directory.
func New(dir string) (*Client, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return &Client{Dir: dir, executable: path}, nil
}

// Result preserves stdout and stderr verbatim. ExitCode is -1 if no ordinary
// exit status is available (for example, a start failure or process kill).
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// CommandError reports a nonzero Git exit, retaining the underlying error.
// Its status does not prove that no effect occurred: a hook can fail after Git
// has already changed the repository. Callers must inspect actual state.
type CommandError struct {
	Args   []string
	Result Result
	err    error
}

func (e *CommandError) Error() string {
	message := fmt.Sprintf("Git %q завершился с кодом %d", e.Args, e.Result.ExitCode)
	if stderr := strings.TrimSpace(string(e.Result.Stderr)); stderr != "" {
		message += ": " + stderr
	}
	return message
}

func (e *CommandError) Unwrap() error { return e.err }

// Run executes trusted Git arguments, with no shell interpolation. It returns
// an error for every nonzero exit: only the caller knows whether a particular
// status (such as merge-base's 1) is an expected negative answer. Never pass
// raw CLI arguments here; validate operands and use command-specific option
// boundaries. Run does not retry or roll back interrupted commands.
func (c *Client) Run(ctx context.Context, args ...string) (Result, error) {
	result := Result{ExitCode: -1}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	cmd := exec.CommandContext(ctx, c.executable, args...)
	cmd.Dir = c.Dir
	cmd.Env = c.environment()
	// A descendant may retain a pipe after cancellation. Bound pipe cleanup;
	// CommandContext kills Git itself, not an arbitrary hook process tree.
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result.Stdout, result.Stderr = stdout.Bytes(), stderr.Bytes()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("вызов Git прерван: %w", ctx.Err())
	}
	if err == nil {
		return result, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return result, &CommandError{Args: append([]string(nil), args...), Result: result, err: err}
	}
	_, executableErr := os.Stat(c.executable)
	if errors.Is(err, exec.ErrNotFound) || errors.Is(executableErr, os.ErrNotExist) {
		return result, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return result, fmt.Errorf("не удалось выполнить Git: %w", err)
}

func (c *Client) environment() []string {
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	result := make([]string, 0, len(env)+3)
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "GIT_NO_LAZY_FETCH", "GIT_TERMINAL_PROMPT", "GIT_ALLOW_PROTOCOL":
			continue
		}
		result = append(result, entry)
	}
	return append(result, "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=")
}

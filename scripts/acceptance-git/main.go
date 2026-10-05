package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	push := false
	for _, arg := range os.Args[1:] {
		if arg == "push" {
			push = true
			break
		}
	}
	mode := os.Getenv("TASK_ACCEPTANCE_MODE")
	if push {
		path := os.Getenv("TASK_ACCEPTANCE_COUNTER")
		count := 0
		if data, err := os.ReadFile(path); err == nil {
			var parseErr error
			count, parseErr = strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				fmt.Fprintln(os.Stderr, parseErr)
				return 98
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, err)
			return 98
		}
		count++
		if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", count)), 0600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 98
		}
		if mode == "cancel" {
			conn, err := net.DialTimeout("tcp", os.Getenv("TASK_ACCEPTANCE_BARRIER"), 10*time.Second)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 98
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
				return 98
			}
			var release [1]byte
			_, _ = conn.Read(release[:])
			return 1
		}
		if mode == "exhaustion" || mode == "retry" && count == 1 {
			cmd := exec.CommandContext(ctx, os.Getenv("TASK_ACCEPTANCE_BINARY"), "add", fmt.Sprintf("Bob barrier %d", count))
			cmd.Dir = os.Getenv("TASK_ACCEPTANCE_OTHER")
			cmd.Env = cleanEnvironment()
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			if code := execute(cmd); code != 0 {
				return 98
			}
		}
	}
	cmd := exec.CommandContext(ctx, os.Getenv("TASK_ACCEPTANCE_REAL_GIT"), os.Args[1:]...)
	cmd.Env = cleanEnvironment()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	code := execute(cmd)
	if push && mode == "lost-ack" && code == 0 {
		fmt.Fprintln(os.Stderr, "acceptance: simulated lost acknowledgement after successful real push")
		return 1
	}
	return code
}

func cleanEnvironment() []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "PATH") || strings.HasPrefix(strings.ToUpper(key), "TASK_ACCEPTANCE_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "PATH="+os.Getenv("TASK_ACCEPTANCE_PATH"))
}

func execute(cmd *exec.Cmd) int {
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 98
	}
	return 0
}

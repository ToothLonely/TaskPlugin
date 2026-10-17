package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"git-task/internal/testrepo"
)

func TestMain(m *testing.M) {
	if os.Setenv("LC_ALL", "ru_RU.UTF-8") != nil || os.Setenv("LANGUAGE", "") != nil {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestBinaryDiagnosticLanguage(t *testing.T) {
	binary := buildHooksBinary(t)
	repo := testrepo.New(t)
	for _, tc := range []struct {
		locale string
		want   string
	}{
		{"ru_RU.UTF-8", "Ошибка: неизвестная команда"},
		{"en_US.UTF-8", "Error: unknown command"},
		{"ja_JP.UTF-8", "Error: unknown command"},
		{"C", "Error: unknown command"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			var env []string
			for _, entry := range repo.Env {
				key, _, _ := strings.Cut(entry, "=")
				if !strings.EqualFold(key, "LC_ALL") && !strings.EqualFold(key, "LANGUAGE") {
					env = append(env, entry)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "unknown-command")
			cmd.Dir, cmd.Env = repo.Dir, append(env, "LC_ALL="+tc.locale, "LANGUAGE=")
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(output), tc.want) {
				t.Fatalf("error=%v output=%q", err, output)
			}
		})
	}
}

package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"git-task/internal/app"
)

func TestExchangeArgumentsBeforeOpeningPlan(t *testing.T) {
	for _, args := range [][]string{
		{"push", "origin"}, {"push", "--force"}, {"push", "--rebase"},
		{"pull", "main"}, {"pull", "--squash"}, {"pull", "--rebase=true"},
		{"pull", "--merge", "--rebase"}, {"pull", "--ff-only", "--ff-only"},
		{"pull", "--help", "--force"}, {"push", "--help", "--help"},
	} {
		var out, diagnostic bytes.Buffer
		open := func(context.Context) (*app.Plans, error) {
			t.Fatal("opened plan for invalid arguments")
			return nil, nil
		}
		if code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%v code=%d out=%q err=%q", args, code, &out, &diagnostic)
		}
	}
}

func TestExchangeHelpWithoutRepository(t *testing.T) {
	for _, args := range [][]string{{"help", "push"}, {"push", "--help"}, {"help", "pull"}, {"pull", "--rebase", "--help"}} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}); code != 0 || !strings.Contains(out.String(), "upstream") {
			t.Fatalf("args=%v code=%d out=%q err=%q", args, code, &out, &diagnostic)
		}
	}
}

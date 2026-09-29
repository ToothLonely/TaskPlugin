package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"git-task/internal/app"
)

func TestStartSyntaxBeforeRepositoryOrMenu(t *testing.T) {
	cases := [][]string{
		{"start"}, {"start", "--id", "task-001"}, {"start", "--title", "Задача"}, {"start", "--new", "Новая"}, {"start", "--select"}, {"start", "--id", "task-001", "--again"},
		{"start", "one", "two"}, {"start", "-b", "one"}, {"start", " "}, {"start", "--", "-bad"},
		{"start", "one", "--id", "a", "--title", "b"}, {"start", "one", "--id", "a", "--id", "a"},
		{"start", "one", "--title", "a", "--title", "a"}, {"start", "one", "--select", "--new", "a"},
		{"start", "one", "--select", "--id", "a"}, {"start", "one", "--select", "--title", "a"},
		{"start", "one", "two", "--select"}, {"start", "-b", "one", "--select"}, {"start", "one", "--select", "--select"},
		{"start", "one", "--again"}, {"start", "one", "--new", "a", "--again"}, {"start", "one", "--select", "--again"},
		{"start", "one", "--title="}, {"start", "one", "--from="}, {"start", "one", "--again=true"}, {"start", "one", "--id"},
		{"attach", "one"}, {"attach", "--id", "a"}, {"attach", "one", "--title", "a"}, {"attach", "one", "--from", "main"}, {"attach", "one", "--id", "a", "--again"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			open := func(context.Context) (*app.Plans, error) {
				t.Fatal("opened repository before syntax validation")
				return nil, nil
			}
			code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, open)
			if code != 2 || out.Len() != 0 || diagnostic.Len() == 0 {
				t.Fatalf("code=%d out=%q err=%q", code, &out, &diagnostic)
			}
		})
	}
}

func TestStartInterspersedFlags(t *testing.T) {
	for _, args := range [][]string{{"feature", "--title", " Добавить профиль ", "--again", "--from", "refs/heads/main"}, {"--title= Добавить профиль ", "--from=refs/heads/main", "--again", "feature"}} {
		got, err := parseStartArgs("start", args)
		if err != nil || got.options.Branch != "feature" || got.options.Title != " Добавить профиль " || !got.options.Again || got.options.From != "refs/heads/main" {
			t.Fatalf("parse=%+v %v", got, err)
		}
	}
}

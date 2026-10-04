package transfer

import (
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func decodeMarkdownFixture(t testing.TB, data []byte, target string) (task.Plan, error) {
	t.Helper()
	p, err := DecodeMarkdown(data, target)
	if err == nil {
		testrepo.FixtureIDs(&p)
	}
	return p, err
}

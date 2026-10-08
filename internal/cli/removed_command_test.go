package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"git-task/internal/app"
)

func TestRemovedMigrationCommand(t *testing.T) {
	open := func(context.Context) (*app.Plans, error) {
		t.Fatal("removed command opened repository")
		return nil, nil
	}
	for _, args := range [][]string{{"migrate"}, {"migrate", "--help"}, {"help", "migrate"}} {
		var out, diagnostic bytes.Buffer
		if code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 2 || diagnostic.Len() == 0 {
			t.Fatalf("removed command accepted %v: %d %q %q", args, code, &out, &diagnostic)
		}
	}
	var out bytes.Buffer
	if err := writeHelp(&out, ""); err != nil || strings.Contains(out.String(), "migrate") {
		t.Fatalf("removed command advertised: %q %v", &out, err)
	}
}

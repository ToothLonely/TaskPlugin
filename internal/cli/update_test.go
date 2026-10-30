package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"git-task/internal/app"
	"git-task/internal/update"
)

func TestUpdateArgumentsBeforeEffects(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"update", "--help"}, 0},
		{[]string{"help", "update"}, 0},
		{[]string{"update", "extra"}, 2},
		{[]string{"update", "--force"}, 2},
		{[]string{"update", "--help", "extra"}, 2},
		{[]string{"update"}, 1},
	} {
		var out, stderr bytes.Buffer
		code := RunWithPlans(context.Background(), tc.args, "dev", Streams{Out: &out, Err: &stderr}, func(context.Context) (*app.Plans, error) { t.Fatal("update opened repository"); return nil, nil })
		if code != tc.code {
			t.Fatalf("args=%v code=%d stderr=%s", tc.args, code, &stderr)
		}
	}
}

func TestUpdateNoopWithLocalRelease(t *testing.T) {
	for _, version := range []string{"2.0.0", "3.0.0"} {
		t.Run(version, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/repos/ToothLonely/TaskPlugin/releases/latest" {
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				_, _ = io.WriteString(w, `{"tag_name":"v2.0.0","draft":false,"prerelease":false}`)
			}))
			defer server.Close()
			destination, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			defer transport.CloseIdleConnections()
			source := update.NewSourceWithTransport(updateVMTransport{transport, destination})
			path := filepath.Join(t.TempDir(), "absent-executable")
			var out, stderr bytes.Buffer
			code := RunWithPlans(context.Background(), []string{"update"}, version, Streams{Out: &out, Err: &stderr, RunUpdate: func(ctx context.Context, v string, out, stderr io.Writer) error {
				return update.RunInstallation(ctx, path, v, source, out, stderr)
			}}, func(context.Context) (*app.Plans, error) {
				t.Fatal("update opened repository")
				return nil, nil
			})
			if code != 0 || !strings.Contains(out.String(), "already up to date") || stderr.Len() != 0 || calls.Load() != 1 {
				t.Fatalf("code=%d requests=%d stdout=%s stderr=%s", code, calls.Load(), &out, &stderr)
			}
		})
	}
}

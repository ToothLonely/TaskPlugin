package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git-task/internal/app"
	"git-task/internal/update"
)

type updateVMTransport struct {
	base        http.RoundTripper
	destination *url.URL
}

func (t updateVMTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	u := *req.URL
	u.Scheme = t.destination.Scheme
	u.Host = t.destination.Host
	copy.URL = &u
	return t.base.RoundTrip(copy)
}

func TestUpdateCommandInDisposableVM(t *testing.T) {
	if os.Getenv("GIT_TASK_UPDATE_DISPOSABLE_VM") != "1" {
		t.Skip("requires disposable VM and installed candidate N")
	}
	value := func(key string) string {
		t.Helper()
		v := os.Getenv(key)
		if v == "" {
			t.Fatalf("missing %s", key)
		}
		return v
	}
	path := value("GIT_TASK_UPDATE_EXECUTABLE")
	bundle := value("GIT_TASK_UPDATE_BUNDLE")
	previous := value("GIT_TASK_UPDATE_PREVIOUS")
	if !filepath.IsAbs(path) || !filepath.IsAbs(bundle) {
		t.Fatal("absolute paths required")
	}
	data, err := os.ReadFile(filepath.Join(bundle, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest update.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var releaseTag atomic.Value
	releaseTag.Store("v" + manifest.Version)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag := releaseTag.Load().(string)
		if r.URL.Path == "/repos/ToothLonely/TaskPlugin/releases/latest" {
			assets := []map[string]string{}
			for _, entry := range entries {
				if !entry.IsDir() {
					assets = append(assets, map[string]string{"name": entry.Name(), "browser_download_url": "https://github.com/ToothLonely/TaskPlugin/releases/download/" + tag + "/" + entry.Name()})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "draft": false, "prerelease": false, "assets": assets})
			return
		}
		prefix := "/ToothLonely/TaskPlugin/releases/download/" + tag + "/"
		name := strings.TrimPrefix(r.URL.Path, prefix)
		if name == r.URL.Path || filepath.Base(name) != name {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(bundle, name))
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sentinel := filepath.Join(t.TempDir(), "project-data")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(version string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := RunWithPlans(ctx, []string{"update"}, version, Streams{Out: &out, Err: &stderr, RunUpdate: func(ctx context.Context, v string, out, stderr io.Writer) error {
			return update.RunInstallation(ctx, path, v, source, out, stderr)
		}}, func(context.Context) (*app.Plans, error) { t.Fatal("opened repository"); return nil, nil })
		if code != 0 {
			t.Fatalf("update code=%d stdout=%s stderr=%s", code, &out, &stderr)
		}
		got, err := exec.CommandContext(ctx, path, "version").Output()
		if err != nil || strings.TrimSpace(string(got)) != "git-task "+manifest.Version {
			t.Fatalf("installed version: %s %v", got, err)
		}
	}
	invoke(previous)
	invoke(manifest.Version)
	releaseTag.Store("v" + previous)
	invoke(manifest.Version)
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "preserve" {
		t.Fatal("project sentinel changed")
	}
	program := value("GIT_TASK_UPDATE_UNINSTALL_PROGRAM")
	if !filepath.IsAbs(program) {
		t.Fatal("absolute native uninstall program required")
	}
	var args []string
	if err := json.Unmarshal([]byte(value("GIT_TASK_UPDATE_UNINSTALL_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(ctx, program, args...).CombinedOutput(); err != nil {
		t.Fatal(fmt.Sprintf("uninstall: %v %s", err, output))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("executable remains after uninstall: %v", err)
	}
}

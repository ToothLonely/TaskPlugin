package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLatestReleaseValidation(t *testing.T) {
	for _, tc := range []struct {
		name, current, tag, manifest         string
		draft, pre, wantError, wantCandidate bool
	}{
		{name: "upgrade", current: "1.9.0", tag: "v1.10.0", manifest: "1.10.0", wantCandidate: true},
		{name: "equal", current: "1.10.0", tag: "v1.10.0"},
		{name: "newer", current: "2.0.0", tag: "v1.10.0"},
		{name: "dev", current: "dev", wantError: true},
		{name: "draft", current: "1.0.0", tag: "v2.0.0", draft: true, wantError: true},
		{name: "prerelease", current: "1.0.0", tag: "v2.0.0", pre: true, wantError: true},
		{name: "unstable tag", current: "1.0.0", tag: "v2.0.0-rc.1", wantError: true},
		{name: "manifest mismatch", current: "1.0.0", tag: "v2.0.0", manifest: "3.0.0", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := release{Tag: tc.tag, Draft: tc.draft, Prerelease: tc.pre}
			for _, name := range []string{"manifest.json", "SHA256SUMS"} {
				r.Assets = append(r.Assets, asset{Name: name, URL: "https://github.com/" + repository + "/releases/download/" + tc.tag + "/" + name})
			}
			calls := 0
			s := &Source{client: &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				var body []byte
				switch {
				case strings.HasSuffix(req.URL.Path, "/latest"):
					body, _ = json.Marshal(r)
				case strings.HasSuffix(req.URL.Path, "/manifest.json"):
					body, _ = json.Marshal(Manifest{Version: tc.manifest})
				default:
					body = []byte(strings.Repeat("0", 64) + "  payload.zip\n")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}}
			got, err := s.Latest(context.Background(), tc.current)
			if (err != nil) != tc.wantError || (got != nil) != tc.wantCandidate {
				t.Fatalf("candidate=%v error=%v", got, err)
			}
			if tc.current == "dev" && calls != 0 {
				t.Fatal("dev version accessed network")
			}
			if !tc.wantError && !tc.wantCandidate && calls != 1 {
				t.Fatal("no-op downloaded assets")
			}
		})
	}
}

func TestDownloadRejectsCorruption(t *testing.T) {
	data := "verified binary"
	sum := sha256.Sum256([]byte(data))
	hash := hex.EncodeToString(sum[:])
	c := &Candidate{release: release{Tag: "v2.0.0", Assets: []asset{{Name: "payload", URL: "https://github.com/" + repository + "/releases/download/v2.0.0/payload"}}}, checksums: map[string]string{"payload": hash}}
	for _, body := range []string{data, "corrupt"} {
		s := &Source{client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		got, err := s.Download(context.Background(), c, "payload", hash)
		if body == data && (err != nil || string(got) != data) {
			t.Fatalf("valid payload: %v", err)
		}
		if body != data && err == nil {
			t.Fatal("accepted corrupt payload")
		}
	}
}

func TestVersionOrder(t *testing.T) {
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.9.0", "1.10.0", "2.0.0"}
	for i := 1; i < len(ordered); i++ {
		a, err := parseVersion(ordered[i-1])
		if err != nil {
			t.Fatal(err)
		}
		b, err := parseVersion(ordered[i])
		if err != nil {
			t.Fatal(err)
		}
		if compare(a, b) >= 0 || compare(b, a) <= 0 {
			t.Fatalf("incorrect order: %s %s", ordered[i-1], ordered[i])
		}
	}
	for _, s := range []string{"dev", "1.0", "01.0.0", "1.0.0-01", "1.0.0-", "1.0.0+", "18446744073709551616.0.0"} {
		if _, err := parseVersion(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}

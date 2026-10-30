package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		limit  int64
		cancel bool
	}{
		{"missing", 404, "missing", 100, false},
		{"rate-limit", 429, "limited", 100, false},
		{"size", 200, "oversized", 3, false},
		{"cancel", 200, "body", 100, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			destination, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			transport := http.DefaultTransport.(*http.Transport).Clone()
			defer transport.CloseIdleConnections()
			transport.Proxy = nil
			s := &Source{client: &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				copy := req.Clone(req.Context())
				u := *req.URL
				u.Scheme = destination.Scheme
				u.Host = destination.Host
				copy.URL = &u
				return transport.RoundTrip(copy)
			})}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			_, err = s.get(ctx, "https://api.github.com/example", tc.limit)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestReleaseTrustBoundary(t *testing.T) {
	for _, address := range []string{"http://github.com/file", "https://github.com.evil.invalid/file", "https://user:password@github.com/file", "https://github.com:443/file"} {
		u, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		if allowedURL(u) {
			t.Fatalf("accepted %s", address)
		}
	}
	s := NewSource()
	for _, r := range []release{
		{Tag: "v1.0.0"},
		{Tag: "v1.0.0", Assets: []asset{{Name: "manifest.json", URL: "https://github.com/another/repo/file"}}},
		{Tag: "v1.0.0", Assets: []asset{{Name: "manifest.json", URL: "one"}, {Name: "manifest.json", URL: "two"}}},
	} {
		if _, err := s.releaseAsset(context.Background(), r, "manifest.json", 100); err == nil {
			t.Fatal("accepted invalid asset metadata")
		}
	}
}

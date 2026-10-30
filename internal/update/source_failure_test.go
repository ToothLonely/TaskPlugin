package update

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type interruptedDownload struct{}

func (interruptedDownload) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestUpdateSourceFailures(t *testing.T) {
	for _, scenario := range []string{"offline", "truncated", "json"} {
		t.Run(scenario, func(t *testing.T) {
			s := NewSourceWithTransport(transportFunc(func(*http.Request) (*http.Response, error) {
				if scenario == "offline" {
					return nil, errors.New("injected offline")
				}
				var reader io.Reader = strings.NewReader("{")
				if scenario == "truncated" {
					reader = interruptedDownload{}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(reader)}, nil
			}))
			if _, err := s.Latest(context.Background(), "1.0.0"); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

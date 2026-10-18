package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/diagnostic"
)

func TestA04MalformedLoadKeepsLocalizedContext(t *testing.T) {
	s, _ := initialized(t)
	path := filepath.Join(s.dir, "plan.json")
	data := []byte(`{"format":`)
	put(t, path, data)
	_, err := s.Load(context.Background())
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("JSON cause lost: %v", err)
	}
	for _, tc := range []struct {
		language diagnostic.Language
		prefix   string
	}{
		{diagnostic.Russian, "повреждённый JSON: "},
		{diagnostic.English, "corrupt JSON: "},
	} {
		if got := diagnostic.Render(tc.language, err); !strings.HasPrefix(got, tc.prefix) {
			t.Errorf("language %s: missing localized JSON context in %q", tc.language, got)
		}
	}
	if !bytes.Equal(data, read(t, path)) {
		t.Fatal("failed load changed the source")
	}
}

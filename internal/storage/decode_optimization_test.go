package storage

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestLoadHeaderKeepsStrictValidation(t *testing.T) {
	for _, scenario := range []string{"duplicate", "unknown", "null-format", "unicode", "invalid-utf8", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			s, _ := initialized(t)
			path := filepath.Join(s.dir, "plan.json")
			original := read(t, path)
			var data []byte
			switch scenario {
			case "duplicate":
				data = bytes.Replace(original, []byte(`"format": "git-task"`), []byte(`"format": "git-task", "format": "git-task"`), 1)
			case "unknown":
				data = bytes.Replace(original, []byte(`"format":`), []byte(`"extra": 1, "format":`), 1)
			case "null-format":
				data = bytes.Replace(original, []byte(`"format": "git-task"`), []byte(`"format": null`), 1)
			case "unicode":
				data = bytes.Replace(original, []byte(`"main"`), []byte(`"\ud800"`), 1)
			case "invalid-utf8":
				data = bytes.Replace(original, []byte(`"main"`), []byte{'"', 0xff, '"'}, 1)
			case "legacy":
				data = bytes.Replace(original, []byte(`"format":`), []byte(`"insertion_tail": "", "format":`), 1)
			}
			if bytes.Equal(data, original) {
				t.Fatal("fixture did not change")
			}
			put(t, path, data)
			_, err := s.Load(context.Background())
			if (err == nil) != (scenario == "legacy") {
				t.Fatalf("decode: %v", err)
			}
			if !bytes.Equal(read(t, path), data) {
				t.Fatal("load rewrote input")
			}
		})
	}
}

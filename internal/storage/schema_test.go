package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestUnsupportedSchemasPreserveStorage(t *testing.T) {
	for _, version := range []int{1, 2, 99} {
		for _, name := range []string{"plan.json", "plan.backup.json"} {
			t.Run(fmt.Sprintf("%d/%s", version, name), func(t *testing.T) {
				s, _ := initialized(t)
				ctx := context.Background()
				data := read(t, filepath.Join(s.dir, "plan.json"))
				old := bytes.Replace(data, []byte(`"schema_version": 3`), []byte(fmt.Sprintf(`"schema_version": %d`, version)), 1)
				put(t, filepath.Join(s.dir, name), old)
				before := doctorFiles(t, s)
				operations := []func() error{
					func() error { _, err := s.Load(ctx); return err },
					func() error { _, err := s.Init(ctx, "main"); return err },
					func() error { _, err := s.InitTemplate(ctx, false); return err },
					func() error { _, err := s.InitTemplate(ctx, true); return err },
				}
				for _, run := range operations {
					if err := run(); !errors.Is(err, ErrUnsupportedSchema) {
						t.Fatalf("unsupported schema accepted: %v", err)
					}
					if !reflect.DeepEqual(before, doctorFiles(t, s)) {
						t.Fatal("unsupported schema changed storage")
					}
				}
				inspection, err := s.Inspect(ctx)
				if err != nil || !reflect.DeepEqual(before, doctorFiles(t, s)) {
					t.Fatalf("inspection changed data: %v", err)
				}
				found := false
				for _, f := range inspection.Findings {
					if f.Name == name && f.Problem {
						found = true
					}
				}
				if !found {
					t.Fatalf("unsupported schema not diagnosed: %+v", inspection)
				}
				if _, err := s.PrepareRepair(ctx, "restore-backup"); err == nil {
					t.Fatal("unsupported schema offered for replacement")
				}
			})
		}
	}
}

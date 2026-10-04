package storage

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestExplicitMigrationKeepsSourceAndBackup(t *testing.T) {
	s, _ := initialized(t)
	ctx := context.Background()
	legacy := []byte("{\n  \"format\":\"git-task\",\"schema_version\":1,\"revision\":0,\"target_branch\":\"main\",\"order\":[],\"tasks\":[],\"last_event\":0\n}\n")
	put(t, filepath.Join(s.dir, "plan.json"), legacy)
	if _, err := s.Load(ctx); !errors.Is(err, ErrMigration) {
		t.Fatalf("silent migration: %v", err)
	}
	if changed, err := s.Migrate(ctx); err != nil || !changed {
		t.Fatalf("migrate: %t %v", changed, err)
	}
	for _, name := range []string{"plan.schema-1.json", "plan.backup.json"} {
		if !bytes.Equal(read(t, filepath.Join(s.dir, name)), legacy) {
			t.Fatalf("source lost: %s", name)
		}
	}
	before := read(t, filepath.Join(s.dir, "plan.json"))
	if changed, err := s.Migrate(ctx); err != nil || changed {
		t.Fatalf("repeat: %t %v", changed, err)
	}
	if !bytes.Equal(read(t, filepath.Join(s.dir, "plan.json")), before) {
		t.Fatal("repeat rewrote plan")
	}
	if _, err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationRefusesPendingOperationAndForeignBackup(t *testing.T) {
	for _, name := range []string{"operation.json", "plan.schema-1.json"} {
		t.Run(name, func(t *testing.T) {
			s, _ := initialized(t)
			legacy := []byte(`{"format":"git-task","schema_version":1,"revision":0,"target_branch":"main","order":[],"tasks":[],"last_event":0}`)
			put(t, filepath.Join(s.dir, "plan.json"), legacy)
			put(t, filepath.Join(s.dir, name), []byte("foreign"))
			if _, err := s.Migrate(context.Background()); err == nil {
				t.Fatal("unsafe migration accepted")
			}
			if !bytes.Equal(read(t, filepath.Join(s.dir, "plan.json")), legacy) || !bytes.Equal(read(t, filepath.Join(s.dir, name)), []byte("foreign")) {
				t.Fatal("foreign data overwritten")
			}
		})
	}
}

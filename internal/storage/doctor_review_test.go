package storage

import (
	"context"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

func TestDoctorReviewConcurrentJournalClosureDoesNotPanic(t *testing.T) {
	s, _ := initialized(t)
	ctx := context.Background()
	base := snapshot(t, s)
	if err := s.WithOperation(ctx, func(op *Operation) error {
		_, err := op.Prepare(ctx, base, added(t, base, "Interrupted"), "review")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, "operation.json")
	parked := filepath.Join(s.dir, "conflict-review-journal.json")
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := os.Rename(path, parked); err == nil {
				os.Rename(parked, path)
			}
		}
	}()
	defer func() { close(stop); <-done }()
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("doctor panicked while journal was closed concurrently: %v\n%s", value, debug.Stack())
		}
	}()
	for i := 0; i < 1000; i++ {
		if _, err := s.Inspect(ctx); err != nil {
			t.Fatalf("doctor inspection: %v", err)
		}
	}
}

package update

import (
	"path/filepath"
	"testing"
)

func TestUpdateLockExcludesConcurrentWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git-task")
	unlock, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		other, err := acquireLock(path)
		if err == nil {
			_ = other()
		}
		result <- err
	}()
	if err := <-result; err == nil {
		t.Fatal("second writer acquired lock")
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	next, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := next(); err != nil {
		t.Fatal(err)
	}
}

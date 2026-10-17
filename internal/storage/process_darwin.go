package storage

import (
	"errors"
	"syscall"

	"git-task/internal/diagnostic"
)

func processAbsent(pid int) error {
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return diagnostic.Errorf("нельзя проверить владельца: %w", err)
	}
	return diagnostic.Errorf("PID %d существует; lock не снимается", pid)
}

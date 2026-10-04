package storage

import (
	"errors"
	"fmt"
	"syscall"
)

func processAbsent(pid int) error {
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("нельзя проверить владельца: %w", err)
	}
	return fmt.Errorf("PID %d существует; lock не снимается", pid)
}

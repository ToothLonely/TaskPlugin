package storage

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

func processAbsent(pid int) (err error) {
	h, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return fmt.Errorf("нельзя проверить владельца: %w", err)
	}
	defer func() { err = errors.Join(err, syscall.CloseHandle(h)) }()
	entry := syscall.ProcessEntry32{Size: uint32(unsafe.Sizeof(syscall.ProcessEntry32{}))}
	for next := syscall.Process32First(h, &entry); ; next = syscall.Process32Next(h, &entry) {
		if errors.Is(next, syscall.ERROR_NO_MORE_FILES) {
			return nil
		}
		if next != nil {
			return fmt.Errorf("нельзя проверить владельца: %w", next)
		}
		if uint64(entry.ProcessID) == uint64(pid) {
			return fmt.Errorf("PID %d существует; lock не снимается", pid)
		}
	}
}

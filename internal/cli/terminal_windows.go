package cli

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var consoleDLL = syscall.NewLazyDLL("kernel32.dll")
var readConsoleEvent = consoleDLL.NewProc("ReadConsoleInputExW")

const consoleReadNoWait = 0x0002

func isTerminal(file *os.File) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(file.Fd()), &mode) == nil
}

type consoleDialogue struct {
	in  *os.File
	out io.Writer
}

func newTerminalDialogue(in, out *os.File) (Dialogue, error) {
	if err := readConsoleEvent.Find(); err != nil {
		return nil, fmt.Errorf("неблокирующее чтение консоли недоступно: %w", err)
	}
	return &consoleDialogue{in: in, out: out}, nil
}

func (d *consoleDialogue) Close() error { return nil }

func (d *consoleDialogue) ReadLine(ctx context.Context) (string, error) {
	var line []rune
	var high uint16
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var count uint32
		var record [20]byte
		ok, _, err := readConsoleEvent.Call(d.in.Fd(), uintptr(unsafe.Pointer(&record[0])), 1, uintptr(unsafe.Pointer(&count)), consoleReadNoWait)
		if ok == 0 {
			return "", fmt.Errorf("чтение консоли: %w", err)
		}
		if count == 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-ticker.C:
				continue
			}
		}
		if count != 1 || binary.LittleEndian.Uint16(record[:2]) != 1 || binary.LittleEndian.Uint32(record[4:8]) == 0 {
			continue
		}
		char := binary.LittleEndian.Uint16(record[14:16])
		if char == 0 {
			continue
		}
		if char == 3 {
			return "", context.Canceled
		}
		if char == 4 || char == 26 {
			return "", io.EOF
		}
		if char == '\r' || char == '\n' {
			_, err := io.WriteString(d.out, "\n")
			return string(line), err
		}
		if char >= 0xd800 && char <= 0xdbff {
			high = char
			continue
		}
		value := rune(char)
		if char >= 0xdc00 && char <= 0xdfff && high != 0 {
			value = utf16.DecodeRune(rune(high), rune(char))
		}
		high = 0
		for range binary.LittleEndian.Uint16(record[8:10]) {
			text := string(value)
			if char == '\b' {
				if len(line) == 0 {
					continue
				}
				line = line[:len(line)-1]
				text = "\b \b"
			} else {
				line = append(line, value)
			}
			if _, err := io.WriteString(d.out, text); err != nil {
				return "", err
			}
		}
	}
}

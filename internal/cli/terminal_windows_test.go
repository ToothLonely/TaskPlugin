package cli

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"
)

func TestTerminalWindowsHiddenConsole(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestTerminalWindowsConsoleProcess$")
	cmd.Env = append(os.Environ(), "GIT_TASK_CONSOLE_TEST=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x10}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hidden console: %v %s", err, output)
	}
}

func TestTerminalWindowsConsoleProcess(t *testing.T) {
	if os.Getenv("GIT_TASK_CONSOLE_TEST") != "1" {
		return
	}
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	dialogue, err := OpenTerminalDialogue(in, out)
	if err != nil {
		t.Fatal(err)
	}
	defer dialogue.Close()
	write := consoleDLL.NewProc("WriteConsoleInputW")
	inject := func(input string) {
		t.Helper()
		for _, char := range utf16.Encode([]rune(input)) {
			var record [20]byte
			binary.LittleEndian.PutUint16(record[:2], 1)
			binary.LittleEndian.PutUint32(record[4:8], 1)
			binary.LittleEndian.PutUint16(record[8:10], 1)
			binary.LittleEndian.PutUint16(record[14:16], char)
			var written uint32
			ok, _, err := write.Call(in.Fd(), uintptr(unsafe.Pointer(&record[0])), 1, uintptr(unsafe.Pointer(&written)))
			if ok == 0 || written != 1 {
				t.Fatalf("inject=%v count=%d", err, written)
			}
		}
	}
	inject("Рус 🐱x\b\r")
	if line, err := dialogue.ReadLine(context.Background()); line != "Рус 🐱" || err != nil {
		t.Fatalf("line=%q err=%v", line, err)
	}
	inject("\x1a")
	if _, err := dialogue.ReadLine(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF=%v", err)
	}
	inject("\x03")
	if _, err := dialogue.ReadLine(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupt=%v", err)
	}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	signalContext, stopTimeout := context.WithTimeout(signalContext, time.Second)
	defer stopTimeout()
	generateInterrupt := consoleDLL.NewProc("GenerateConsoleCtrlEvent")
	if ok, _, err := generateInterrupt.Call(0, 0); ok == 0 {
		t.Fatalf("generate interrupt: %v", err)
	}
	if _, err := dialogue.ReadLine(signalContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("console Ctrl+C: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := dialogue.ReadLine(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline=%v", err)
	}
}

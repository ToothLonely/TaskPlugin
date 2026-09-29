package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestTerminalPTYInputAndCancellation(t *testing.T) {
	for _, mode := range []string{"line", "interrupt", "eof", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			var unlock, number uint32
			for _, request := range []struct {
				code  uintptr
				value *uint32
			}{{syscall.TIOCSPTLCK, &unlock}, {syscall.TIOCGPTN, &number}} {
				if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), request.code, uintptr(unsafe.Pointer(request.value))); err != 0 {
					t.Fatal(err)
				}
			}
			slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer slave.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestTerminalPTYProcess$")
			cmd.Env = append(os.Environ(), "GIT_TASK_PTY_MODE="+mode)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					cmd.Process.Kill()
					cmd.Wait()
				}
			}()
			if err := master.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(master)
			ready, err := reader.ReadString('\n')
			if err != nil || !strings.Contains(ready, "READY") {
				t.Fatalf("ready=%q err=%v", ready, err)
			}
			input := map[string]string{"line": "Рус 🐱\n", "interrupt": "\x03", "eof": "\x04"}[mode]
			if input != "" {
				if _, err := io.WriteString(master, input); err != nil {
					t.Fatal(err)
				}
			}
			err = cmd.Wait()
			waited = true
			if err != nil {
				slave.Close()
				output, _ := io.ReadAll(reader)
				t.Fatalf("child: %v %s", err, output)
			}
		})
	}
}

func TestTerminalPTYProcess(t *testing.T) {
	mode := os.Getenv("GIT_TASK_PTY_MODE")
	if mode == "" {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if mode == "deadline" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
	}
	dialogue, err := OpenTerminalDialogue(os.Stdin, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer dialogue.Close()
	fmt.Fprintln(os.Stderr, "READY")
	line, err := dialogue.ReadLine(ctx)
	switch mode {
	case "line":
		if line != "Рус 🐱\n" || err != nil {
			t.Fatalf("line=%q err=%v", line, err)
		}
	case "interrupt":
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupt=%v", err)
		}
	case "eof":
		if !errors.Is(err, io.EOF) {
			t.Fatalf("eof=%v", err)
		}
	case "deadline":
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline=%v", err)
		}
	}
}

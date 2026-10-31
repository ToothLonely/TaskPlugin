package update

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestUpdateWindowsPermissionProcess(t *testing.T) {
	if os.Getenv("UPDATE_DENIED_PATH") == "" {
		t.Skip("permission scenario requires explicit CI preparation")
	}
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		t.Fatal(err)
	}
	var original syscall.Token
	if err := syscall.OpenProcessToken(process, syscall.TOKEN_DUPLICATE|syscall.TOKEN_ASSIGN_PRIMARY|syscall.TOKEN_QUERY, &original); err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	var restricted syscall.Token
	const disableMaximumPrivileges = 0x1
	create := syscall.NewLazyDLL("advapi32.dll").NewProc("CreateRestrictedToken")
	ok, _, err := create.Call(uintptr(original), disableMaximumPrivileges, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&restricted)))
	if ok == 0 {
		t.Fatalf("CreateRestrictedToken: %v", err)
	}
	defer restricted.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestUpdatePermissionChild$", "-test.v")
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Token: restricted}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restricted permission child: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "--- PASS: TestUpdatePermissionChild") || strings.Contains(string(output), "--- SKIP:") {
		t.Fatalf("restricted permission child did not PASS:\n%s", output)
	}
	t.Logf("restricted permission child:\n%s", output)
}

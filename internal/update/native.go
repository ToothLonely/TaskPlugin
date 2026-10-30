package update

import (
	"context"
	"encoding/json"
	"errors"
	"git-task/internal/diagnostic"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func nativeOwner(ctx context.Context, path, version string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		return windowsOwner(ctx, path, version)
	case "linux":
		owners := []string{}
		if _, err := exec.LookPath("dpkg-query"); err == nil {
			owner, err := commandOutput(ctx, "dpkg-query", "-S", path)
			if err == nil {
				if owner != "git-task: "+path {
					return "", diagnostic.Errorf("бинарник принадлежит другому пакету Debian: %s", owner)
				}
				got, err := commandOutput(ctx, "dpkg-query", "-W", "-f=${Status}\t${Version}\t${Architecture}", "git-task")
				if err != nil {
					return "", err
				}
				if got != "install ok installed\t"+strings.Replace(version, "-", "~", 1)+"\tamd64" {
					return "", diagnostic.Errorf("регистрация Debian не соответствует бинарнику: %s", got)
				}
				owners = append(owners, "deb")
			} else if !exitCode(err, 1) {
				return "", err
			}
		}
		if _, err := exec.LookPath("rpm"); err == nil {
			owner, err := commandOutput(ctx, "rpm", "-qf", "--qf", "%{NAME}\t%{VERSION}\t%{RELEASE}\t%{ARCH}", path)
			if err == nil {
				base, pre, _ := strings.Cut(version, "-")
				release := "1"
				if pre != "" {
					release = "0." + pre
				}
				if owner != "git-task\t"+base+"\t"+release+"\tx86_64" {
					return "", diagnostic.Errorf("регистрация RPM не соответствует бинарнику: %s", owner)
				}
				owners = append(owners, "rpm")
			} else if !exitCode(err, 1) {
				return "", err
			}
		}
		if len(owners) > 1 {
			return "", diagnostic.Errorf("найдено несколько пакетных владельцев")
		}
		if len(owners) == 1 {
			return owners[0], nil
		}
	case "darwin":
		info, err := commandOutput(ctx, "/usr/sbin/pkgutil", "--pkg-info-plist", "io.github.toothlonely.git-task")
		if err == nil {
			files, err := commandOutput(ctx, "/usr/sbin/pkgutil", "--files", "io.github.toothlonely.git-task")
			if err != nil {
				return "", err
			}
			base, _, _ := strings.Cut(version, "-")
			if path != "/usr/local/bin/git-task" || !strings.Contains("\n"+files+"\n", "\nusr/local/bin/git-task\n") || !strings.Contains(info, "<string>"+base+"</string>") {
				return "", diagnostic.Errorf("регистрация PKG не соответствует бинарнику")
			}
			return "pkg", nil
		}
		if !exitCode(err, 1) {
			return "", err
		}
	}
	return "", nil
}

func exitCode(err error, code int) bool {
	var e *exec.ExitError
	return errors.As(err, &e) && e.ExitCode() == code
}

func windowsOwner(ctx context.Context, path, version string) (string, error) {
	script := `$ErrorActionPreference='Stop'; $i=New-Object -ComObject WindowsInstaller.Installer; $items=@(); foreach($p in $i.RelatedProducts('{C0C7D813-8DDD-4F2B-951A-E30DC27F643C}')) { $items+=@{version=$i.ProductInfo($p,'VersionString'); path=$i.ComponentPath($p,'{14CA45AA-0877-4A14-A4B3-AD42A156A904}')} }; ConvertTo-Json -Compress -InputObject $items`
	text, err := commandOutput(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return "", err
	}
	var products []struct {
		Version string `json:"version"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal([]byte(text), &products); err != nil {
		return "", err
	}
	if len(products) == 0 {
		return "", nil
	}
	base, _, _ := strings.Cut(version, "-")
	if len(products) != 1 || products[0].Version != base {
		return "", diagnostic.Errorf("регистрация MSI не соответствует бинарнику или неоднозначна")
	}
	registryScript := `$ErrorActionPreference='Stop'; (Get-ItemProperty -LiteralPath 'HKCU:\Software\GitTask' -Name InstallPath).InstallPath`
	dir, err := commandOutput(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", registryScript)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Clean(filepath.Join(dir, "git-task.exe")), filepath.Clean(path)) || !strings.Contains(strings.ToLower(products[0].Path), "software\\gittask") {
		return "", diagnostic.Errorf("компонент MSI не подтверждает текущую установку")
	}
	return "msi", nil
}

func installNative(ctx context.Context, i Installation, name string, data []byte, version, expectedHash string, out, stderr io.Writer) error {
	work, err := os.MkdirTemp("", "git-task-update-")
	if err != nil {
		return err
	}
	completed := false
	preserved := false
	defer func() {
		if completed || !preserved {
			os.RemoveAll(work)
		}
	}()
	payload := filepath.Join(work, name)
	if err := os.WriteFile(payload, data, 0600); err != nil {
		return err
	}
	current, err := Inspect(ctx, i.Path, i.Version)
	if err != nil {
		return err
	}
	if current.Hash != i.Hash || current.Owner != i.Owner {
		return diagnostic.Errorf("установка изменилась перед обновлением")
	}
	journal := i.Path + ".update-native.json"
	record, err := json.Marshal(struct{ Target, Owner, PreviousVersion, NextVersion, Payload string }{i.Path, i.Owner, i.Version, version, payload})
	if err != nil {
		return err
	}
	if err := writeNew(journal, record, 0600); err != nil {
		return err
	}
	preserved = true
	var program string
	var args []string
	switch i.Owner {
	case "msi":
		program = filepath.Join(os.Getenv("SystemRoot"), "System32", "msiexec.exe")
		args = []string{"/i", payload, "/passive", "/norestart", "MSIRESTARTMANAGERCONTROL=Disable"}
	case "deb":
		program = "dpkg"
		args = []string{"--install", payload}
	case "rpm":
		program = "rpm"
		args = []string{"--upgrade", payload}
	case "pkg":
		program = "/usr/sbin/installer"
		args = []string{"-pkg", payload, "-target", "/"}
	default:
		return diagnostic.Errorf("неподдерживаемый пакетный владелец")
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := runNativeCommand(ctx, cmd); err != nil {
		return err
	}
	if err := verifyVersion(ctx, i.Path, version); err != nil {
		return err
	}
	owner, err := nativeOwner(ctx, i.Path, version)
	if err != nil {
		return err
	}
	if owner != i.Owner {
		return diagnostic.Errorf("регистрация обновлённого пакета не подтверждена")
	}
	hash, _, err := fileHash(i.Path)
	if err != nil {
		return err
	}
	if hash != expectedHash {
		return diagnostic.New("контрольная сумма бинарника в архиве не совпадает")
	}
	if err := os.Remove(journal); err != nil {
		return err
	}
	completed = true
	return nil
}

func runNativeCommand(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
			return diagnostic.Errorf("установщик не подтвердил завершение; перед повтором проверьте состояние пакета и необходимые права: %w", err)
		}
		return diagnostic.Errorf("установщик не подтвердил завершение; перед повтором проверьте состояние пакета и необходимые права: %w", err)
	}
	return nil
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type nativeReceipt struct {
	Version      string        `json:"version"`
	Platform     string        `json:"platform"`
	SourceSHA256 string        `json:"source_sha256"`
	Files        []releaseFile `json:"files"`
}

func buildBinary(ctx context.Context, root, output, version, platform string) error {
	goName := "go"
	if runtime.GOOS == "windows" {
		goName += ".exe"
	}
	command := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", goName), "build", "-trimpath", "-buildvcs=false", "-ldflags=-X=main.version="+version, "-o", output, "./cmd/git-task")
	command.Dir, command.Env = root, buildEnvironment(platform)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build %s: %w", platform, err)
	}
	return nil
}

func nativeNames(version, platform string) []string {
	prefix := "git-task_" + version + "_"
	switch platform {
	case "windows/amd64":
		return []string{prefix + "windows.msi"}
	case "linux/amd64":
		return []string{prefix + "linux.deb", prefix + "linux.rpm"}
	case "darwin/arm64":
		return []string{prefix + "darwin_arm64.pkg"}
	}
	return nil
}

func nativePackages(ctx context.Context, root, destination, version, sourceHash string, assets []entry) (err error) {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	names := nativeNames(version, platform)
	if len(names) == 0 {
		return fmt.Errorf("native installers unsupported on %s", platform)
	}
	work, err := os.MkdirTemp(destination, "build-")
	if err != nil {
		return err
	}
	defer func() {
		if cleanup := os.RemoveAll(work); err == nil && cleanup != nil {
			err = cleanup
		}
	}()
	assetDir := filepath.Join(work, "assets")
	if err := os.Mkdir(assetDir, 0755); err != nil {
		return err
	}
	for _, asset := range assets {
		if asset.name == "README.md" || asset.name == "GO-LICENSE" || asset.name == "GO-PATENTS" {
			if err := os.WriteFile(filepath.Join(assetDir, asset.name), asset.data, 0644); err != nil {
				return err
			}
		}
	}
	binary := filepath.Join(work, "git-task")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := buildBinary(ctx, root, binary, version, platform); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		if err := runTool(ctx, "powershell.exe", "-NoProfile", "-File", filepath.Join(root, "scripts", "installer", "build-windows.ps1"), "-Payload", binary, "-Output", filepath.Join(destination, names[0]), "-Version", version, "-Assets", assetDir); err != nil {
			return err
		}
	case "linux":
		debRoot := filepath.Join(work, "deb")
		if err := packageTree(debRoot, binary, assetDir, "usr/bin/git-task", "usr/share/doc/git-task"); err != nil {
			return err
		}
		base, pre, _ := strings.Cut(version, "-")
		debVersion := base
		if pre != "" {
			debVersion += "~" + pre
		}
		control := fmt.Sprintf("Package: git-task\nVersion: %s\nArchitecture: amd64\nMaintainer: Git Task maintainers <noreply@github.com>\nDepends: git\nSection: devel\nPriority: optional\nHomepage: https://github.com/ToothLonely/TaskPlugin\nDescription: Task tracker for Git branches\n", debVersion)
		if err := writePackageFile(debRoot, "DEBIAN/control", []byte(control), 0644); err != nil {
			return err
		}
		if err := runTool(ctx, "dpkg-deb", "--root-owner-group", "--build", debRoot, filepath.Join(destination, names[0])); err != nil {
			return err
		}
		rpmRoot := filepath.Join(work, "rpm")
		for _, name := range []string{"BUILD", "BUILDROOT", "RPMS", "SOURCES", "SPECS", "SRPMS"} {
			if err := os.MkdirAll(filepath.Join(rpmRoot, name), 0755); err != nil {
				return err
			}
		}
		release := "1"
		if pre != "" {
			release = "0." + pre
		}
		spec := fmt.Sprintf("Name: git-task\nVersion: %s\nRelease: %s\nSummary: Task tracker for Git branches\nLicense: Unspecified\nURL: https://github.com/ToothLonely/TaskPlugin\nBuildArch: x86_64\nAutoReqProv: no\nRequires: git\n\n%%description\nTask tracker for Git branches.\n\n%%install\nmkdir -p \"%%{buildroot}/usr/bin\" \"%%{buildroot}/usr/share/doc/git-task\"\ncp %s \"%%{buildroot}/usr/bin/git-task\"\nchmod 755 \"%%{buildroot}/usr/bin/git-task\"\ncp %s/* \"%%{buildroot}/usr/share/doc/git-task/\"\n\n%%files\n/usr/bin/git-task\n/usr/share/doc/git-task\n", base, release, shellQuote(binary), shellQuote(assetDir))
		specPath := filepath.Join(rpmRoot, "SPECS", "git-task.spec")
		if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
			return err
		}
		if err := runTool(ctx, "rpmbuild", "-bb", "--define", "_topdir "+rpmRoot, "--define", "_build_id_links none", "--define", "__os_install_post %{nil}", specPath); err != nil {
			return err
		}
		produced := filepath.Join(rpmRoot, "RPMS", "x86_64", "git-task-"+base+"-"+release+".x86_64.rpm")
		if err := copyPackageFile(produced, filepath.Join(destination, names[1]), 0644); err != nil {
			return err
		}
	case "darwin":
		pkgRoot := filepath.Join(work, "pkg")
		if err := packageTree(pkgRoot, binary, assetDir, "usr/local/bin/git-task", "usr/local/share/doc/git-task"); err != nil {
			return err
		}
		if err := writePackageFile(pkgRoot, "etc/paths.d/git-task", []byte("/usr/local/bin\n"), 0644); err != nil {
			return err
		}
		base, _, _ := strings.Cut(version, "-")
		if err := runTool(ctx, "/usr/bin/pkgbuild", "--root", pkgRoot, "--identifier", "io.github.toothlonely.git-task", "--version", base, "--install-location", "/", "--ownership", "recommended", filepath.Join(destination, names[0])); err != nil {
			return err
		}
	}
	_, afterHash, err := inputs(root)
	if err != nil {
		return err
	}
	if afterHash != sourceHash {
		return fmt.Errorf("inputs changed during native packaging")
	}
	receipt := nativeReceipt{Version: version, Platform: platform, SourceSHA256: sourceHash}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil {
			return err
		}
		receipt.Files = append(receipt.Files, releaseFile{Name: name, SHA256: digest(data)})
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destination, strings.ReplaceAll(platform, "/", "-")+".json"), append(data, '\n'), 0644)
}

func installerInputs(directory, version, sourceHash string) ([]entry, error) {
	var result []entry
	for _, platform := range []string{"windows/amd64", "linux/amd64", "darwin/arm64"} {
		data, err := os.ReadFile(filepath.Join(directory, strings.ReplaceAll(platform, "/", "-")+".json"))
		if err != nil {
			return nil, err
		}
		var receipt nativeReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return nil, err
		}
		names := nativeNames(version, platform)
		if receipt.Version != version || receipt.Platform != platform || receipt.SourceSHA256 != sourceHash || len(receipt.Files) != len(names) {
			return nil, fmt.Errorf("native receipt differs from release inputs: %s", platform)
		}
		for i, name := range names {
			if receipt.Files[i].Name != name {
				return nil, fmt.Errorf("unexpected installer name: %s", receipt.Files[i].Name)
			}
			contents, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				return nil, err
			}
			if digest(contents) != receipt.Files[i].SHA256 {
				return nil, fmt.Errorf("installer checksum mismatch: %s", name)
			}
			result = append(result, entry{name, contents, 0644})
		}
	}
	return result, nil
}

func runTool(ctx context.Context, program string, args ...string) error {
	command := exec.CommandContext(ctx, program, args...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s: %w", program, err)
	}
	return nil
}

func packageTree(root, binary, assets, target, documents string) error {
	if err := copyPackageFile(binary, filepath.Join(root, filepath.FromSlash(target)), 0755); err != nil {
		return err
	}
	for _, name := range []string{"README.md", "GO-LICENSE", "GO-PATENTS"} {
		if err := copyPackageFile(filepath.Join(assets, name), filepath.Join(root, filepath.FromSlash(documents), name), 0644); err != nil {
			return err
		}
	}
	return nil
}

func copyPackageFile(source, destination string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writePackageFile(filepath.Dir(destination), filepath.Base(destination), data, mode)
}

func writePackageFile(root, name string, data []byte, mode os.FileMode) error {
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(value, "%", "%%"), "'", "'\\''") + "'"
}

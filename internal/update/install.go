package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"git-task/internal/diagnostic"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Installation struct {
	Path    string
	Owner   string
	Version string
	Hash    string
	Mode    os.FileMode
}

type portableReceipt struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Owner   string `json:"owner"`
}

func fileHash(path string) (string, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return "", 0, diagnostic.Errorf("путь не является обычным исполняемым файлом: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if !os.SameFile(info, opened) {
		return "", 0, diagnostic.Errorf("исполняемый файл изменился во время проверки")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, (128<<20)+1)); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), info.Mode().Perm(), nil
}

func commandOutput(ctx context.Context, program string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	data, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", program, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func Inspect(ctx context.Context, path, version string) (Installation, error) {
	i := Installation{Path: path, Version: version}
	if _, err := parseVersion(version); err != nil {
		return i, err
	}
	var err error
	i.Hash, i.Mode, err = fileHash(path)
	if err != nil {
		return i, err
	}
	if err := uniqueExecutable(path); err != nil {
		return i, err
	}
	i.Owner, err = nativeOwner(ctx, path, version)
	if err != nil {
		return i, err
	}
	if i.Owner != "" {
		return i, nil
	}
	receiptPath := filepath.Join(filepath.Dir(path), "git-task-portable.json")
	info, err := os.Lstat(receiptPath)
	if err != nil {
		return i, diagnostic.Errorf("владелец установки неизвестен; используйте исходный установщик или распакуйте полный официальный portable-архив: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return i, diagnostic.New("квитанция portable-установки не соответствует бинарнику")
	}
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		return i, diagnostic.Errorf("владелец установки неизвестен; используйте исходный установщик или распакуйте полный официальный portable-архив: %w", err)
	}
	var receipt portableReceipt
	if len(data) > 4096 || json.Unmarshal(data, &receipt) != nil || receipt.Owner != "portable" || receipt.Version != version || receipt.SHA256 != i.Hash {
		return i, diagnostic.Errorf("квитанция portable-установки не соответствует бинарнику")
	}
	i.Owner = "portable"
	return i, nil
}

func uniqueExecutable(path string) error {
	current, err := os.Stat(path)
	if err != nil {
		return err
	}
	name := "git-task"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !os.SameFile(current, info) {
			return diagnostic.Errorf("несколько копий git-task в PATH; перед обновлением оставьте одну установку")
		}
	}
	return nil
}

func Run(ctx context.Context, version string, out, stderr io.Writer) (result error) {
	path, err := os.Executable()
	if err != nil {
		return err
	}
	return RunInstallation(ctx, path, version, NewSource(), out, stderr)
}

func RunInstallation(ctx context.Context, path, version string, source *Source, out, stderr io.Writer) (result error) {
	if _, err := parseVersion(version); err != nil {
		return err
	}
	candidate, err := source.Latest(ctx, version)
	if err != nil {
		return err
	}
	if candidate == nil {
		_, portableErr := os.Lstat(path + ".update.json")
		_, nativeErr := os.Lstat(path + ".update-native.json")
		if errors.Is(portableErr, os.ErrNotExist) && errors.Is(nativeErr, os.ErrNotExist) {
			_, err := fmt.Fprintln(out, "git-task is already up to date.")
			return err
		}
	}
	unlock, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, unlock()) }()
	if _, err := os.Lstat(path + ".update-native.json"); !errors.Is(err, os.ErrNotExist) {
		return diagnostic.Errorf("предыдущее обновление требует восстановления; сохраните %s и следуйте DOCUMENTATION.md", path+".update-native.json")
	}
	if err := recoverPortable(path, version); err != nil {
		return err
	}
	installation, err := Inspect(ctx, path, version)
	if err != nil {
		return err
	}
	if candidate == nil {
		_, err := fmt.Fprintln(out, "git-task is already up to date.")
		return err
	}
	platform := runtime.GOOS + "/" + runtime.GOARCH
	name, hash, err := candidate.Select(platform, installation.Owner)
	if err != nil {
		return err
	}
	data, err := source.Download(ctx, candidate, name, hash)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var binaryHash string
	for _, a := range candidate.Manifest.Artifacts {
		if a.Platform == platform {
			if binaryHash != "" || !validHash(a.BinarySHA256) {
				return diagnostic.New("неверный portable manifest")
			}
			binaryHash = a.BinarySHA256
		}
	}
	if binaryHash == "" {
		return diagnostic.Errorf("portable-платформа %q отсутствует", platform)
	}
	if installation.Owner == "portable" {
		binary, err := ExtractBinary(data, platform, binaryHash)
		if err != nil {
			return err
		}
		if err := replacePortable(ctx, installation, binary, candidate.Manifest.Version, binaryHash); err != nil {
			return err
		}
	} else {
		if err := installNative(ctx, installation, name, data, candidate.Manifest.Version, binaryHash, out, stderr); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(out, "git-task updated to %s.\n", candidate.Manifest.Version)
	return err
}

func verifyVersion(ctx context.Context, path, version string) error {
	got, err := commandOutput(ctx, path, "version")
	if err != nil {
		return err
	}
	if got != "git-task "+version {
		return diagnostic.Errorf("версия установленного бинарника не совпадает: %q", got)
	}
	return nil
}

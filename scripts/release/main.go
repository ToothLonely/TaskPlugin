package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

type entry struct {
	name string
	data []byte
	mode int64
}

type artifact struct {
	Platform     string `json:"platform"`
	Archive      string `json:"archive"`
	SHA256       string `json:"sha256"`
	BinarySHA256 string `json:"binary_sha256"`
}

type manifest struct {
	Version      string        `json:"version"`
	Go           string        `json:"go"`
	MinimumGit   string        `json:"minimum_git"`
	SourceSHA256 string        `json:"source_sha256"`
	Artifacts    []artifact    `json:"artifacts"`
	Installers   []releaseFile `json:"installers"`
}

type releaseFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:])
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	version := flags.String("version", "", "local candidate version")
	out := flags.String("out", "", "new directory inside the project")
	mode := flags.String("mode", "archives", "archives, native or bundle")
	nativeDirectory := flags.String("installer-dir", "", "native installer artifacts for bundle")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.]+)?$`).MatchString(*version) || *out == "" {
		return errors.New("usage: release -version 0.1.0-rc.1 -out .tools/releases/0.1.0-rc.1")
	}
	if *mode != "archives" && *mode != "native" && *mode != "bundle" {
		return errors.New("unknown release mode")
	}
	if *mode == "bundle" && *nativeDirectory == "" {
		return errors.New("bundle requires -installer-dir")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	destination, err := outputDirectory(ctx, root, *out)
	if err != nil {
		return err
	}
	assets, sourceHash, err := inputs(root)
	if err != nil {
		return err
	}
	var nativeInstallers []entry
	if *mode == "bundle" {
		nativeInstallers, err = installerInputs(*nativeDirectory, *version, sourceHash)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(destination, 0755); err != nil {
		return fmt.Errorf("create new output directory: %w", err)
	}
	if *mode == "native" {
		return nativePackages(ctx, root, destination, *version, sourceHash, assets)
	}
	result := manifest{Version: *version, Go: runtime.Version(), MinimumGit: "2.51.0", SourceSHA256: sourceHash}
	var checksums strings.Builder
	for _, platform := range []string{"windows/amd64", "linux/amd64", "darwin/amd64", "darwin/arm64"} {
		operatingSystem, architecture, _ := strings.Cut(platform, "/")
		if err := ctx.Err(); err != nil {
			return err
		}
		binaryName := "git-task"
		extension := ".tar.gz"
		if operatingSystem == "windows" {
			binaryName += ".exe"
			extension = ".zip"
		}
		binaryPath := filepath.Join(destination, operatingSystem+"-"+architecture+"-"+binaryName)
		if err := buildBinary(ctx, root, binaryPath, *version, platform); err != nil {
			return err
		}
		binary, err := os.ReadFile(binaryPath)
		if err != nil {
			return err
		}
		entries := append([]entry{{binaryName, binary, 0755}}, assets...)
		archive, err := pack(entries, operatingSystem == "windows")
		if err != nil {
			return err
		}
		name := "git-task_" + *version + "_" + operatingSystem + "_" + architecture + extension
		if err := os.WriteFile(filepath.Join(destination, name), archive, 0644); err != nil {
			return err
		}
		result.Artifacts = append(result.Artifacts, artifact{platform, name, digest(archive), digest(binary)})
		fmt.Fprintf(&checksums, "%s  %s\n", digest(archive), name)
		if err := os.Remove(binaryPath); err != nil {
			return err
		}
	}
	_, currentHash, err := inputs(root)
	if err != nil {
		return err
	}
	if currentHash != sourceHash {
		return errors.New("inputs changed during packaging; candidate is incomplete")
	}
	for _, installer := range nativeInstallers {
		if err := os.WriteFile(filepath.Join(destination, installer.name), installer.data, fs.FileMode(installer.mode)); err != nil {
			return err
		}
		result.Installers = append(result.Installers, releaseFile{installer.name, digest(installer.data)})
		fmt.Fprintf(&checksums, "%s  %s\n", digest(installer.data), installer.name)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(destination, "manifest.json"), data, 0644); err != nil {
		return err
	}
	fmt.Fprintf(&checksums, "%s  manifest.json\n", digest(data))
	if err := os.WriteFile(filepath.Join(destination, "SHA256SUMS"), []byte(checksums.String()), 0644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, destination)
	return nil
}

func outputDirectory(ctx context.Context, root, out string) (string, error) {
	root = filepath.Clean(root)
	destination, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, destination)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("output must be a new directory inside the project")
	}
	destination = filepath.Join(root, relative)
	for path := destination; path != root; {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		info, err := os.Lstat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil && info.Mode().Type() != os.ModeDir {
			return "", fmt.Errorf("output path is not an ordinary directory: %s", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", errors.New("output parent traversal did not reach the project root")
		}
		path = parent
	}
	return destination, nil
}

func buildEnvironment(platform string) []string {
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(key) {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOWORK", "GOTOOLCHAIN", "GOENV", "GOPROXY":
			continue
		}
		env = append(env, value)
	}
	operatingSystem, architecture, ok := strings.Cut(platform, "/")
	if !ok {
		architecture = "amd64"
	}
	return append(env, "GOOS="+operatingSystem, "GOARCH="+architecture, "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local", "GOENV=off", "GOPROXY=off")
}

func digest(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func inputs(root string) ([]entry, string, error) {
	var names []string
	for _, directory := range []string{"cmd", "internal", "scripts/release"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, item fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if item.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("symlink input is unsupported: %s", path)
			}
			if !item.IsDir() && strings.HasSuffix(path, ".go") {
				name, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				names = append(names, filepath.ToSlash(name))
			}
			return nil
		})
		if err != nil {
			return nil, "", err
		}
	}
	names = append(names, "go.mod", "README.md", "scripts/installer/build-windows.ps1")
	sort.Strings(names)
	var assets []entry
	var snapshot strings.Builder
	for _, name := range names {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, "", err
		}
		if !info.Mode().IsRegular() {
			return nil, "", fmt.Errorf("input is not a regular file: %s", name)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, "", err
		}
		fmt.Fprintf(&snapshot, "%s  %s\n", digest(data), name)
		if name == "README.md" {
			assets = append(assets, entry{name, data, 0644})
		}
	}
	assets = append(assets, entry{"INPUT-SHA256SUMS", []byte(snapshot.String()), 0644})
	for _, name := range []string{"LICENSE", "PATENTS"} {
		data, err := os.ReadFile(filepath.Join(runtime.GOROOT(), name))
		if err != nil {
			return nil, "", err
		}
		assets = append(assets, entry{"GO-" + name, data, 0644})
	}
	return assets, digest([]byte(snapshot.String())), nil
}

func pack(entries []entry, windows bool) ([]byte, error) {
	var output bytes.Buffer
	if windows {
		writer := zip.NewWriter(&output)
		for _, item := range entries {
			header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
			header.SetMode(fs.FileMode(item.mode))
			file, err := writer.CreateHeader(header)
			if err != nil {
				return nil, err
			}
			if _, err := file.Write(item.data); err != nil {
				return nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
	} else {
		compressed := gzip.NewWriter(&output)
		writer := tar.NewWriter(compressed)
		for _, item := range entries {
			if err := writer.WriteHeader(&tar.Header{Name: item.name, Mode: item.mode, Size: int64(len(item.data)), Typeflag: tar.TypeReg}); err != nil {
				return nil, err
			}
			if _, err := writer.Write(item.data); err != nil {
				return nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		if err := compressed.Close(); err != nil {
			return nil, err
		}
	}
	return output.Bytes(), nil
}

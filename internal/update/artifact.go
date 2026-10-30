package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"git-task/internal/diagnostic"
	"io"
	"path"
	"strings"
)

func (c *Candidate) Select(platform, owner string) (string, string, error) {
	v := c.Manifest.Version
	name := ""
	switch owner + ":" + platform {
	case "msi:windows/amd64":
		name = "git-task_" + v + "_windows.msi"
	case "deb:linux/amd64":
		name = "git-task_" + v + "_linux.deb"
	case "rpm:linux/amd64":
		name = "git-task_" + v + "_linux.rpm"
	case "pkg:darwin/arm64":
		name = "git-task_" + v + "_darwin_arm64.pkg"
	default:
		if owner != "portable" {
			return "", "", diagnostic.Errorf("неподдерживаемый владелец установки или платформа: %s/%s", owner, platform)
		}
		var selected *Artifact
		for i := range c.Manifest.Artifacts {
			a := &c.Manifest.Artifacts[i]
			if a.Platform != platform {
				continue
			}
			if selected != nil {
				return "", "", diagnostic.Errorf("повтор платформы в manifest: %q", platform)
			}
			selected = a
		}
		if selected == nil {
			return "", "", diagnostic.Errorf("portable-платформа %q отсутствует", platform)
		}
		extension := ".tar.gz"
		if platform == "windows/amd64" {
			extension = ".zip"
		}
		expected := "git-task_" + v + "_" + strings.ReplaceAll(platform, "/", "_") + extension
		if selected.Archive != expected || !validHash(selected.BinarySHA256) {
			return "", "", diagnostic.Errorf("неверный portable manifest")
		}
		return selected.Archive, selected.SHA256, nil
	}
	hash := ""
	for _, installer := range c.Manifest.Installers {
		if installer.Name != name {
			continue
		}
		if hash != "" || !validHash(installer.SHA256) {
			return "", "", diagnostic.Errorf("неверный manifest установщика")
		}
		hash = installer.SHA256
	}
	if hash == "" {
		return "", "", diagnostic.Errorf("установщик %q отсутствует", name)
	}
	return name, hash, nil
}

func ExtractBinary(data []byte, platform, expectedHash string) ([]byte, error) {
	if !validHash(expectedHash) {
		return nil, diagnostic.Errorf("неверная контрольная сумма бинарника")
	}
	name := "git-task"
	if strings.HasPrefix(platform, "windows/") {
		name += ".exe"
	}
	var binary []byte
	seen := make(map[string]bool)
	accept := func(nameInArchive string, regular bool, size int64, reader io.Reader) error {
		if nameInArchive == "" || path.Clean(nameInArchive) != nameInArchive || strings.ContainsAny(nameInArchive, "\\:") || strings.HasPrefix(nameInArchive, "/") || nameInArchive == ".." || strings.HasPrefix(nameInArchive, "../") || seen[nameInArchive] || !regular {
			return diagnostic.Errorf("небезопасный элемент архива %q", nameInArchive)
		}
		seen[nameInArchive] = true
		if size < 0 || size > 128<<20 || len(seen) > 128 {
			return diagnostic.Errorf("архив превышает ограничения")
		}
		if nameInArchive != name {
			return nil
		}
		var err error
		binary, err = io.ReadAll(io.LimitReader(reader, (128<<20)+1))
		if err != nil {
			return err
		}
		if int64(len(binary)) != size {
			return diagnostic.Errorf("размер бинарника в архиве не совпадает")
		}
		return nil
	}
	if strings.HasPrefix(platform, "windows/") {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, file := range archive.File {
			reader, err := file.Open()
			if err != nil {
				return nil, err
			}
			err = accept(file.Name, file.Mode().IsRegular(), int64(file.UncompressedSize64), reader)
			closeErr := reader.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
	} else {
		gzipReader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer gzipReader.Close()
		reader := tar.NewReader(io.LimitReader(gzipReader, 256<<20))
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if err := accept(header.Name, header.Typeflag == tar.TypeReg, header.Size, reader); err != nil {
				return nil, err
			}
		}
	}
	sum := sha256.Sum256(binary)
	if len(binary) == 0 || hex.EncodeToString(sum[:]) != expectedHash {
		return nil, diagnostic.Errorf("контрольная сумма бинарника в архиве не совпадает")
	}
	return binary, nil
}

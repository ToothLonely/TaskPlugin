package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestExtractBinary(t *testing.T) {
	payload := []byte("candidate binary")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	for _, platform := range []string{"windows/amd64", "linux/amd64"} {
		for _, entry := range []string{"valid", "../escape", "/absolute", "link", "duplicate", "missing", "corrupt"} {
			t.Run(platform+"/"+entry, func(t *testing.T) {
				name := "git-task"
				if platform == "windows/amd64" {
					name += ".exe"
				}
				if entry == "../escape" || entry == "/absolute" {
					name = entry
				}
				if entry == "missing" {
					name = "README.md"
				}
				content := payload
				if entry == "corrupt" {
					content = []byte("wrong")
				}
				count := 1
				if entry == "duplicate" {
					count = 2
				}
				var buffer bytes.Buffer
				if platform == "windows/amd64" {
					writer := zip.NewWriter(&buffer)
					for range count {
						header := &zip.FileHeader{Name: name, Method: zip.Deflate}
						header.SetMode(0755)
						if entry == "link" {
							header.SetMode(0755 | os.ModeSymlink)
						}
						file, err := writer.CreateHeader(header)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := file.Write(content); err != nil {
							t.Fatal(err)
						}
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					gz := gzip.NewWriter(&buffer)
					writer := tar.NewWriter(gz)
					for range count {
						header := &tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}
						if entry == "link" {
							header.Typeflag = tar.TypeSymlink
							header.Linkname = "outside"
							header.Size = 0
						}
						if err := writer.WriteHeader(header); err != nil {
							t.Fatal(err)
						}
						if entry != "link" {
							if _, err := writer.Write(content); err != nil {
								t.Fatal(err)
							}
						}
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := gz.Close(); err != nil {
						t.Fatal(err)
					}
				}
				got, err := ExtractBinary(buffer.Bytes(), platform, hash)
				if entry == "valid" {
					if err != nil || !bytes.Equal(got, payload) {
						t.Fatalf("payload=%q error=%v", got, err)
					}
				} else if err == nil {
					t.Fatal("accepted unsafe archive")
				}
			})
		}
	}
}

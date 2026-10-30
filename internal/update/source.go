package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"git-task/internal/diagnostic"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const repository = "ToothLonely/TaskPlugin"

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type Artifact struct {
	Platform     string `json:"platform"`
	Archive      string `json:"archive"`
	SHA256       string `json:"sha256"`
	BinarySHA256 string `json:"binary_sha256"`
}

type Installer struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Version    string      `json:"version"`
	Artifacts  []Artifact  `json:"artifacts"`
	Installers []Installer `json:"installers"`
}

type Source struct{ client *http.Client }

func NewSourceWithTransport(transport http.RoundTripper) *Source {
	source := NewSource()
	source.client.Transport = transport
	return source
}

func NewSource() *Source {
	return &Source{client: &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !allowedURL(req.URL) {
				return diagnostic.Errorf("недоверенное перенаправление загрузки")
			}
			return nil
		},
	}}
}

func allowedURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com":
		return true
	}
	return false
}

func (s *Source) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil || !allowedURL(u) {
		return nil, diagnostic.Errorf("недоверенный URL релиза")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "git-task-updater")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, diagnostic.Errorf("загрузка релиза: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, diagnostic.Errorf("сервер релизов вернул HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, diagnostic.Errorf("загрузка релиза превышает ограничение размера")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, diagnostic.Errorf("чтение загрузки релиза: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, diagnostic.Errorf("загрузка релиза превышает ограничение размера")
	}
	return data, nil
}

type Candidate struct {
	Manifest  Manifest
	release   release
	checksums map[string]string
}

func (s *Source) Latest(ctx context.Context, current string) (*Candidate, error) {
	installed, err := parseVersion(current)
	if err != nil {
		return nil, err
	}
	data, err := s.get(ctx, "https://api.github.com/repos/"+repository+"/releases/latest", 2<<20)
	if err != nil {
		return nil, err
	}
	var r release
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, diagnostic.Errorf("разбор релиза: %w", err)
	}
	latest, err := parseVersion(r.Tag)
	if err != nil {
		return nil, err
	}
	if r.Draft || r.Prerelease || len(latest.pre) != 0 {
		return nil, diagnostic.Errorf("релиз не является стабильным")
	}
	if compare(installed, latest) >= 0 {
		return nil, nil
	}
	manifestData, err := s.releaseAsset(ctx, r, "manifest.json", 2<<20)
	if err != nil {
		return nil, err
	}
	c := &Candidate{release: r}
	if err := json.Unmarshal(manifestData, &c.Manifest); err != nil {
		return nil, diagnostic.Errorf("разбор manifest: %w", err)
	}
	if c.Manifest.Version != strings.TrimPrefix(r.Tag, "v") {
		return nil, diagnostic.Errorf("версия manifest не совпадает с тегом релиза")
	}
	checksums, err := s.releaseAsset(ctx, r, "SHA256SUMS", 2<<20)
	if err != nil {
		return nil, err
	}
	c.checksums, err = parseChecksums(string(checksums))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Source) releaseAsset(ctx context.Context, r release, name string, limit int64) ([]byte, error) {
	address := ""
	for _, a := range r.Assets {
		if a.Name != name {
			continue
		}
		if address != "" {
			return nil, diagnostic.Errorf("повтор файла релиза %q", name)
		}
		address = a.URL
	}
	expected := "https://github.com/" + repository + "/releases/download/" + r.Tag + "/" + name
	if address == "" {
		return nil, diagnostic.Errorf("в релизе отсутствует обязательный файл %q", name)
	}
	if address != expected {
		return nil, diagnostic.Errorf("файл %q не принадлежит релизу", name)
	}
	return s.get(ctx, address, limit)
}

func parseChecksums(text string) (map[string]string, error) {
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		sum, name, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "  ")
		if !ok || !validHash(sum) || name == "" || strings.ContainsAny(name, "/\\") || result[name] != "" {
			return nil, diagnostic.Errorf("неверный список контрольных сумм релиза")
		}
		result[name] = sum
	}
	return result, nil
}

func validHash(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == sha256.Size && value == strings.ToLower(value)
}

func (s *Source) Download(ctx context.Context, c *Candidate, name, checksum string) ([]byte, error) {
	if !validHash(checksum) || c.checksums[name] != checksum {
		return nil, diagnostic.Errorf("manifest и список контрольных сумм не согласованы для %q", name)
	}
	data, err := s.releaseAsset(ctx, c.release, name, 128<<20)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != checksum {
		return nil, diagnostic.Errorf("контрольная сумма %q не совпадает", name)
	}
	return data, nil
}

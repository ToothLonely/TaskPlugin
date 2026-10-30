package update

import (
	"strings"
	"testing"
)

func TestSelectInstallationAsset(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, tc := range []struct{ owner, platform, name string }{
		{"msi", "windows/amd64", "git-task_2.0.0_windows.msi"},
		{"deb", "linux/amd64", "git-task_2.0.0_linux.deb"},
		{"rpm", "linux/amd64", "git-task_2.0.0_linux.rpm"},
		{"pkg", "darwin/arm64", "git-task_2.0.0_darwin_arm64.pkg"},
	} {
		c := &Candidate{Manifest: Manifest{Version: "2.0.0", Installers: []Installer{{Name: tc.name, SHA256: hash}}}}
		name, sum, err := c.Select(tc.platform, tc.owner)
		if err != nil || name != tc.name || sum != hash {
			t.Fatalf("%s: %s %s %v", tc.owner, name, sum, err)
		}
		c.Manifest.Installers = append(c.Manifest.Installers, c.Manifest.Installers[0])
		if _, _, err := c.Select(tc.platform, tc.owner); err == nil {
			t.Fatal("accepted duplicate installer")
		}
	}
	c := &Candidate{Manifest: Manifest{Version: "2.0.0", Artifacts: []Artifact{{Platform: "windows/amd64", Archive: "git-task_2.0.0_windows_amd64.zip", SHA256: hash, BinarySHA256: hash}}}}
	if _, _, err := c.Select("windows/amd64", "portable"); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"brew", "unknown", "pkg"} {
		if _, _, err := c.Select("windows/amd64", owner); err == nil {
			t.Fatalf("accepted %s", owner)
		}
	}
	if _, _, err := c.Select("linux/arm64", "portable"); err == nil {
		t.Fatal("accepted missing architecture")
	}
}

func TestInvalidChecksums(t *testing.T) {
	line := strings.Repeat("a", 64) + "  file.zip"
	for _, input := range []string{"", "invalid", line + "\n" + line, strings.Repeat("a", 64) + "  ../file", strings.Repeat("A", 64) + "  file"} {
		if _, err := parseChecksums(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxLauncherSelectsAgentBundle(t *testing.T) {
	release := githubRelease{TagName: "v0.2.0", Assets: []githubReleaseAsset{
		{Name: "palserver-launcher-windows-amd64.exe", BrowserDownloadURL: "https://example.test/windows"},
		{Name: "palserver-agent-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.test/linux", Digest: "sha256:abc"},
	}}
	asset, err := selectLauncherReleaseAsset(release)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "palserver-agent-linux-amd64.tar.gz" {
		t.Fatalf("selected asset = %#v", asset)
	}
}

func TestPrepareLinuxLauncherReplacementExtractsAgent(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "agent.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	payload := []byte("linux-agent")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "./pal-agent", Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := prepareLauncherReplacement(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("replacement payload = %q", data)
	}
}

func TestLinuxSaveInspectorSelectsAndExtractsAsset(t *testing.T) {
	release := githubRelease{Assets: []githubReleaseAsset{
		{Name: "pst_v0.12.2_windows_x86_64.zip"},
		{Name: "pst_v0.12.2_linux_x86_64.tar.gz", BrowserDownloadURL: "https://example.test/linux"},
	}}
	asset, err := selectSaveInspectorAsset(release)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "pst_v0.12.2_linux_x86_64.tar.gz" {
		t.Fatalf("selected save inspector = %#v", asset)
	}
	root := t.TempDir()
	archivePath := filepath.Join(root, "inspector.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	payload := []byte("linux-save-inspector")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "pst/sav_cli", Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "extract")
	if err := extractSaveInspectorExecutable(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "sav_cli"))
	if err != nil || string(data) != string(payload) {
		t.Fatalf("save inspector payload=%q err=%v", data, err)
	}
}

func TestReplaceLauncherForServiceValidatesReplacement(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "pal-agent")
	replacement := filepath.Join(root, "replacement")
	if err := os.WriteFile(target, []byte("old-agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	trueBinary, err := os.ReadFile("/bin/true")
	if err != nil {
		t.Skip("/bin/true is unavailable")
	}
	if err := os.WriteFile(replacement, trueBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceLauncherForService(target, replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target + ".previous"); err != nil {
		t.Fatalf("previous agent was not retained: %v", err)
	}
}

func TestReplaceLauncherForServiceRollsBackInvalidReplacement(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "pal-agent")
	replacement := filepath.Join(root, "replacement")
	if err := os.WriteFile(target, []byte("old-agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("not-an-executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceLauncherForService(target, replacement); err == nil {
		t.Fatal("invalid replacement was accepted")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old-agent" {
		t.Fatalf("previous agent was not restored: data=%q err=%v", data, err)
	}
}

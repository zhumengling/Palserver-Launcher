package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildLinuxBundleCreatesDeterministicModesAndChecksum(t *testing.T) {
	root := t.TempDir()
	options := packageOptions{
		Agent: writeFixture(t, root, "agent", "agent"), Install: writeFixture(t, root, "install", "install"),
		Remove: writeFixture(t, root, "remove", "remove"), Service: writeFixture(t, root, "service", "service"),
		Readme: writeFixture(t, root, "readme", "readme"), Output: filepath.Join(root, "bundle.tar.gz"),
	}
	if err := buildLinuxBundle(options); err != nil {
		t.Fatal(err)
	}
	firstDigest, err := fileDigest(options.Output)
	if err != nil {
		t.Fatal(err)
	}
	if err := buildLinuxBundle(options); err != nil {
		t.Fatalf("rebuilding existing bundle: %v", err)
	}
	secondDigest, err := fileDigest(options.Output)
	if err != nil || firstDigest != secondDigest {
		t.Fatalf("bundle is not reproducible: first=%s second=%s err=%v", firstDigest, secondDigest, err)
	}
	file, err := os.Open(options.Output)
	if err != nil {
		t.Fatal(err)
	}
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	tarReader := tar.NewReader(gzipReader)
	want := []struct {
		name, content string
		mode          int64
	}{
		{"pal-agent", "agent", 0o755}, {"install.sh", "install", 0o755}, {"uninstall.sh", "remove", 0o755},
		{"palserver-agent.service", "service", 0o644}, {"README-linux.md", "readme", 0o644},
	}
	for index, expected := range want {
		header, err := tarReader.Next()
		if err != nil {
			t.Fatalf("entry %d: %v", index, err)
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != expected.name || header.Mode != expected.mode || string(data) != expected.content || header.Uid != 0 || header.Gid != 0 {
			t.Fatalf("entry %d = %#v %q", index, header, data)
		}
	}
	if _, err := tarReader.Next(); err != io.EOF {
		t.Fatalf("archive has unexpected trailing entry: %v", err)
	}
	if err := gzipReader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(options.Output)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	wantChecksum := hex.EncodeToString(digest[:]) + "  bundle.tar.gz\n"
	checksum, err := os.ReadFile(options.Output + ".sha256")
	if err != nil || string(checksum) != wantChecksum {
		t.Fatalf("checksum = %q, want %q, err=%v", checksum, wantChecksum, err)
	}
}

func TestBuildLinuxBundleRejectsMissingOrNonRegularSource(t *testing.T) {
	root := t.TempDir()
	regular := writeFixture(t, root, "regular", "x")
	options := packageOptions{Agent: filepath.Join(root, "missing"), Install: regular, Remove: regular, Service: regular, Readme: regular, Output: filepath.Join(root, "bundle.tar.gz")}
	if err := buildLinuxBundle(options); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing source error = %v", err)
	}
	options.Agent = root
	if err := buildLinuxBundle(options); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory source error = %v", err)
	}
}

//go:build !linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLauncherVersionNormalization(t *testing.T) {
	tests := map[string]string{
		"v0.1":     "0.1.0",
		"0.1.0":    "0.1.0",
		" v2.7.4 ": "2.7.4",
	}
	for input, expected := range tests {
		actual, err := normalizeLauncherVersion(input)
		if err != nil || actual != expected {
			t.Fatalf("normalize %q = %q, %v; want %q", input, actual, err, expected)
		}
	}
	if _, err := normalizeLauncherVersion("release-next"); err == nil {
		t.Fatal("invalid launcher version was accepted")
	}
}

func TestLauncherVersionComparison(t *testing.T) {
	tests := []struct {
		current, latest string
		expected        int
	}{
		{"v0.1", "0.1.0", 0},
		{"0.1.9", "v0.2.0", -1},
		{"1.0.0", "0.9.9", 1},
	}
	for _, test := range tests {
		actual, err := compareLauncherVersions(test.current, test.latest)
		if err != nil || actual != test.expected {
			t.Fatalf("compare %q with %q = %d, %v; want %d", test.current, test.latest, actual, err, test.expected)
		}
	}
}

func TestSelectLauncherReleaseAssetRejectsNonStableRelease(t *testing.T) {
	for _, release := range []githubRelease{
		{TagName: "v0.2.0", Draft: true},
		{TagName: "v0.2.0", Prerelease: true},
	} {
		if _, err := selectLauncherReleaseAsset(release); err == nil {
			t.Fatalf("non-stable release was accepted: %#v", release)
		}
	}
}

func TestSelectLauncherReleaseAssetUsesWindowsAMD64Executable(t *testing.T) {
	release := githubRelease{TagName: "v0.2.0", Assets: []githubReleaseAsset{
		{Name: "palserver-launcher-v0.2.0-linux-amd64", BrowserDownloadURL: "https://example.test/linux"},
		{Name: "palserver-launcher-v0.2.0-windows-amd64.zip", BrowserDownloadURL: "https://example.test/windows.zip"},
		{Name: "palserver-launcher-v0.2.0-windows-amd64.exe", BrowserDownloadURL: "https://example.test/windows.exe", Digest: "sha256:abc", Size: 42},
	}}
	asset, err := selectLauncherReleaseAsset(release)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "palserver-launcher-v0.2.0-windows-amd64.exe" || asset.URL != "https://example.test/windows.exe" || asset.Digest != "sha256:abc" || asset.Size != 42 {
		t.Fatalf("selected asset = %#v", asset)
	}
}

func TestSelectLauncherReleaseAssetAcceptsCanonicalWindowsExecutable(t *testing.T) {
	release := githubRelease{TagName: "v0.1.1", Assets: []githubReleaseAsset{
		{Name: "palserver-launcher-linux-amd64", BrowserDownloadURL: "https://example.test/linux"},
		{Name: "palserver-launcher-windows-arm64.exe", BrowserDownloadURL: "https://example.test/arm64"},
		{Name: "palserver-launcher.exe", BrowserDownloadURL: "https://example.test/windows.exe", Digest: "sha256:abc", Size: 42},
	}}
	asset, err := selectLauncherReleaseAsset(release)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "palserver-launcher.exe" || asset.URL != "https://example.test/windows.exe" {
		t.Fatalf("selected asset = %#v", asset)
	}
}

func TestSelectLauncherReleaseAssetAcceptsAutomatedWorkflowName(t *testing.T) {
	release := githubRelease{TagName: "v0.2.0", Assets: []githubReleaseAsset{{
		Name: "palserver-launcher-windows-amd64.exe", BrowserDownloadURL: "https://example.test/workflow.exe", Digest: "sha256:abc", Size: 42,
	}}}
	asset, err := selectLauncherReleaseAsset(release)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "palserver-launcher-windows-amd64.exe" {
		t.Fatalf("selected asset = %#v", asset)
	}
}

func TestVerifyLauncherSHA256File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.exe")
	content := []byte("verified launcher")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(hash[:])
	if err := verifyLauncherSHA256File(path, digest); err != nil {
		t.Fatalf("valid digest rejected: %v", err)
	}
	if err := verifyLauncherSHA256File(path, "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("invalid digest accepted")
	}
	if err := verifyLauncherSHA256File(path, ""); err == nil {
		t.Fatal("missing digest accepted")
	}
}

func TestQuoteWindowsArgSupportsSpacesAndQuotes(t *testing.T) {
	actual := quoteWindowsArg(`C:\Program Files\Pal "Launcher"\palserver.exe`)
	expected := `"C:\Program Files\Pal \"Launcher\"\palserver.exe"`
	if actual != expected {
		t.Fatalf("quoted argument = %q, want %q", actual, expected)
	}
}

func TestReplaceLauncherExecutableCreatesBackup(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "palserver.exe")
	replacement := filepath.Join(root, "palserver-new.exe")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	backup, err := replaceLauncherExecutable(target, replacement)
	if err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, target, "new")
	assertFileContent(t, backup, "old")
}

func TestReplaceLauncherExecutableRollsBackWhenReplacementIsMissing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "palserver.exe")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := replaceLauncherExecutable(target, filepath.Join(root, "missing.exe")); err == nil {
		t.Fatal("missing replacement did not fail")
	}
	assertFileContent(t, target, "old")
}

func TestBuildLauncherUpdateInfoShowsStableNewerRelease(t *testing.T) {
	release := githubRelease{
		TagName: "v0.2.0", Name: "Palserver Launcher 0.2", Body: "修复启动与更新流程", PublishedAt: "2026-07-14T04:00:00Z",
		Assets: []githubReleaseAsset{{Name: "palserver-launcher-v0.2.0-windows-amd64.exe", BrowserDownloadURL: "https://example.test/launcher.exe", Digest: "sha256:" + strings.Repeat("a", 64), Size: 1024}},
	}
	info, asset, err := buildLauncherUpdateInfo(release, "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !info.UpdateAvailable || info.CurrentVersion != "0.1.0" || info.LatestVersion != "0.2.0" || info.Title != release.Name || info.Notes != release.Body || info.PublishedAt != release.PublishedAt || info.AssetSize != 1024 {
		t.Fatalf("update info = %#v", info)
	}
	if asset.URL != "https://example.test/launcher.exe" {
		t.Fatalf("release asset = %#v", asset)
	}
}

func TestFetchLauncherReleaseUsesGitHubHeadersAndDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") != "palserver-launcher/"+LauncherVersion || request.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatalf("unexpected request headers: %#v", request.Header)
		}
		_ = json.NewEncoder(response).Encode(githubRelease{TagName: "v0.2.0"})
	}))
	defer server.Close()
	release, err := fetchLauncherRelease(server.Client(), server.URL)
	if err != nil || release.TagName != "v0.2.0" {
		t.Fatalf("release = %#v, %v", release, err)
	}
}

func TestDownloadLauncherAssetVerifiesDigestAndReportsCompletion(t *testing.T) {
	content := []byte(strings.Repeat("launcher-data", 128))
	hash := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(hash[:])
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Length", "1664")
		_, _ = response.Write(content)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "launcher.exe")
	lastPercent := 0
	err := downloadLauncherAsset(server.Client(), launcherReleaseAsset{URL: server.URL, Digest: digest, Size: int64(len(content))}, destination, func(downloaded, total int64) {
		if total != int64(len(content)) {
			t.Fatalf("download total = %d, want %d", total, len(content))
		}
		lastPercent = int(downloaded * 100 / total)
	})
	if err != nil {
		t.Fatal(err)
	}
	if lastPercent != 100 {
		t.Fatalf("last download percent = %d", lastPercent)
	}
	file, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	downloaded, _ := io.ReadAll(file)
	if string(downloaded) != string(content) {
		t.Fatal("downloaded launcher content differs")
	}
}

func TestDownloadLauncherAssetRejectsMissingDigestBeforeNetwork(t *testing.T) {
	requested := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) { requested = true }))
	defer server.Close()
	err := downloadLauncherAsset(server.Client(), launcherReleaseAsset{URL: server.URL}, filepath.Join(t.TempDir(), "launcher.exe"), nil)
	if err == nil || requested {
		t.Fatalf("missing digest result = %v, requested = %v", err, requested)
	}
}

func TestParseLauncherUpdaterArgsPreservesPaths(t *testing.T) {
	target := `C:\Program Files\Pal Launcher\palserver.exe`
	replacement := `D:\Updates\Pal "Launcher"\new.exe`
	options, handled, err := parseLauncherUpdaterArgs([]string{
		"updater.exe", "--apply-launcher-update", "--target", target, "--replacement", replacement, "--pid", strconv.Itoa(1234), "--elevated",
	})
	if err != nil || !handled {
		t.Fatalf("parse updater args = %#v, %v, %v", options, handled, err)
	}
	if options.Target != target || options.Replacement != replacement || options.PID != 1234 || !options.Elevated {
		t.Fatalf("updater options = %#v", options)
	}
	if _, handled, err := parseLauncherUpdaterArgs([]string{"palserver.exe"}); err != nil || handled {
		t.Fatalf("normal launch was treated as updater: handled=%v err=%v", handled, err)
	}
}

func TestLauncherUpdatePathsStayInsideVersionDirectory(t *testing.T) {
	root := t.TempDir()
	download, helper, err := launcherUpdatePaths(root, "v0.2")
	if err != nil {
		t.Fatal(err)
	}
	wantedRoot := filepath.Join(root, "updates", "0.2.0")
	for _, path := range []string{download, helper} {
		relative, err := filepath.Rel(wantedRoot, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			t.Fatalf("update path escaped version directory: %q (%v)", path, err)
		}
	}
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || string(content) != expected {
		t.Fatalf("%s content = %q, %v; want %q", path, content, err, expected)
	}
}

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type extensionZipEntry struct {
	name    string
	content string
	mode    os.FileMode
}

type extensionRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip extensionRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type trackedExtensionBody struct {
	reader *bytes.Reader
	read   int
	closed bool
}

func (body *trackedExtensionBody) Read(buffer []byte) (int, error) {
	count, err := body.reader.Read(buffer)
	body.read += count
	return count, err
}

func (body *trackedExtensionBody) Close() error {
	body.closed = true
	return nil
}

func writeExtensionTestZip(t *testing.T, entries []extensionZipEntry) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "extension.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, item := range entries {
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		if item.mode != 0 {
			header.SetMode(item.mode)
		}
		entry, createErr := writer.CreateHeader(header)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(item.content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}

func extensionTestZipBytes(t *testing.T, entries []extensionZipEntry) []byte {
	t.Helper()
	data, err := os.ReadFile(writeExtensionTestZip(t, entries))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func extensionFixtureDirectory(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCopyTreeRejectsSymlinkOrWindowsReparsePointWithoutFollowing(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.dll")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(source, "linked.dll")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	destination := filepath.Join(t.TempDir(), "destination")
	if err := copyTree(source, destination); err == nil {
		t.Fatal("copyTree followed a symlink or reparse point")
	}
	if _, err := os.Stat(filepath.Join(destination, "linked.dll")); !os.IsNotExist(err) {
		t.Fatalf("copyTree wrote data through a symlink or reparse point: %v", err)
	}
}

func TestExtensionReleaseSourcesUsePalDefenderStableAndUE4SSExperimental(t *testing.T) {
	palDefender, err := extensionReleaseSourceFor("paldefender")
	if err != nil {
		t.Fatal(err)
	}
	ue4ss, err := extensionReleaseSourceFor("ue4ss")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(palDefender.Endpoint, "/Ultimeit/PalDefender/releases/latest") {
		t.Fatalf("PalDefender endpoint = %q", palDefender.Endpoint)
	}
	if !strings.HasSuffix(ue4ss.Endpoint, "/UE4SS-RE/RE-UE4SS/releases/tags/experimental-latest") {
		t.Fatalf("UE4SS endpoint = %q", ue4ss.Endpoint)
	}
}

func TestInstallLatestExtensionForInstanceUsesStablePalDefenderPipelineWithoutStoredID(t *testing.T) {
	archive := extensionTestZipBytes(t, []extensionZipEntry{{name: "PalDefender.dll", content: "managed plugin"}, {name: "d3d9.dll", content: "managed loader"}})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/Ultimeit/PalDefender/releases/latest":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"tag_name":"v-managed","published_at":"2026-07-15T02:00:00Z","assets":[{"name":"PalDefender.dll","browser_download_url":%q},{"name":"paldefender-dev.zip","browser_download_url":%q},{"id":42,"name":"PalDefender.zip","browser_download_url":%q,"size":%d,"updated_at":"2026-07-15T01:52:46Z"}]}`, server.URL+"/wrong-dll", server.URL+"/wrong-zip", server.URL+"/asset", len(archive))
		case "/asset":
			_, _ = response.Write(archive)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	stableEndpoint := ""
	instance := ServerInstance{RootPath: t.TempDir()}
	result, err := installLatestExtensionForInstance(instance, "paldefender", server.Client(), func(extensionID string) (extensionReleaseSource, error) {
		source, sourceErr := extensionReleaseSourceFor(extensionID)
		if sourceErr != nil {
			return extensionReleaseSource{}, sourceErr
		}
		stableEndpoint = source.Endpoint
		source.Endpoint = server.URL + strings.TrimPrefix(source.Endpoint, githubAPIBase)
		return source, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if instance.ID != "" {
		t.Fatalf("test instance unexpectedly has an ID: %q", instance.ID)
	}
	if stableEndpoint != githubAPIBase+"/repos/Ultimeit/PalDefender/releases/latest" {
		t.Fatalf("PalDefender install source = %q", stableEndpoint)
	}
	if result.ExtensionID != "paldefender" || result.Version != "v-managed" || result.Pending {
		t.Fatalf("managed install result = %#v", result)
	}
	for name, expected := range map[string]string{"PalDefender.dll": "managed plugin", "d3d9.dll": "managed loader", "palguard.version.txt": "v-managed\n"} {
		data, readErr := os.ReadFile(filepath.Join(win64Path(instance), name))
		if readErr != nil || string(data) != expected {
			t.Fatalf("managed install %s = %q, %v", name, data, readErr)
		}
	}
	installedPath, err := extensionInstalledManifestPath(instance, "paldefender")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readExtensionUpdateManifest(installedPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "v-managed" || manifest.Asset.ID != 42 || manifest.Asset.Name != "PalDefender.zip" || manifest.Asset.UpdatedAt != "2026-07-15T01:52:46Z" {
		t.Fatalf("managed install manifest = %#v", manifest)
	}
	pending, err := extensionPendingPath(instance, "paldefender")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("managed install left pending update: %v", err)
	}
}

func TestQuickSetupUsesUnifiedPalDefenderPipelineBeforeStoreUpsert(t *testing.T) {
	data, err := os.ReadFile("setup.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	start := strings.Index(source, "func (a *App) QuickSetup")
	end := strings.Index(source[start:], "\n}\n\nvar settingValuePattern")
	if start < 0 || end < 0 {
		t.Fatal("QuickSetup source body was not found")
	}
	body := source[start : start+end]
	serverInstall := strings.Index(body, "a.installOrUpdate(instance")
	validateInstall := strings.Index(body, "validateInstalledServerExecutable(instance)")
	install := strings.Index(body, `installLatestExtensionForInstance(instance, "paldefender"`)
	upsert := strings.Index(body, "a.store.Upsert(instance)")
	if serverInstall < 0 || validateInstall < 0 || validateInstall <= serverInstall || validateInstall >= upsert {
		t.Fatalf("QuickSetup server validation order is invalid: install=%d validate=%d upsert=%d", serverInstall, validateInstall, upsert)
	}
	if install < 0 || upsert < 0 || install >= upsert {
		t.Fatalf("QuickSetup unified install/upsert order is invalid: install=%d upsert=%d", install, upsert)
	}
	for _, expected := range []string{
		"extensionReleaseSourceFor",
		"PalDefender 已下载但安装失败，将在首次启动前重试",
		"PalDefender 自动安装失败，可稍后在插件页重试",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("QuickSetup is missing %q", expected)
		}
	}
	if strings.Contains(body, `downloadLatestRelease("Ultimeit/PalDefender"`) {
		t.Fatal("QuickSetup still uses the legacy direct PalDefender downloader")
	}
	if strings.Contains(body, "StartServer(") {
		t.Fatal("QuickSetup starts a server while installing PalDefender")
	}
}

func TestSelectPalDefenderAssetRequiresExactZip(t *testing.T) {
	release := githubRelease{TagName: "v1.8.3", Assets: []githubReleaseAsset{
		{Name: "PalDefender.dll"},
		{Name: "PalDefender.zip", UpdatedAt: "2026-07-15T01:52:46Z"},
	}}
	asset, version, err := selectExtensionAsset("paldefender", release)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "PalDefender.zip" || version != "v1.8.3" {
		t.Fatalf("asset=%#v version=%q", asset, version)
	}
}

func TestSelectUE4SSExperimentalAssetRejectsZDEV(t *testing.T) {
	release := githubRelease{TagName: "experimental-latest", Assets: []githubReleaseAsset{
		{Name: "zDEV-UE4SS_v3.0.1-1011-gb50986bd.zip"},
		{Name: "zCustomGameConfigs.zip"},
		{Name: "UE4SS_v3.0.1-1011-gb50986bd.zip", UpdatedAt: "2026-07-13T00:29:54Z"},
	}}
	asset, version, err := selectExtensionAsset("ue4ss", release)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "UE4SS_v3.0.1-1011-gb50986bd.zip" || version != "v3.0.1-1011-gb50986bd" {
		t.Fatalf("asset=%#v version=%q", asset, version)
	}
}

func TestSelectUE4SSAssetExtractsVersionFromUppercaseZipExtension(t *testing.T) {
	release := githubRelease{Assets: []githubReleaseAsset{{Name: "UE4SS_v3.0.1-1011-gb50986bd.ZIP"}}}
	_, version, err := selectExtensionAsset("ue4ss", release)
	if err != nil {
		t.Fatal(err)
	}
	if version != "v3.0.1-1011-gb50986bd" {
		t.Fatalf("version = %q", version)
	}
}

func TestSelectExtensionAssetRejectsUnknownExtensionWithoutAssets(t *testing.T) {
	_, _, err := selectExtensionAsset("unknown", githubRelease{})
	if err == nil || err.Error() != "unknown extension" {
		t.Fatalf("error = %v", err)
	}
}

func TestExtensionUpdateAvailableDetectsUE4SSAssetTimestampChange(t *testing.T) {
	latest := extensionReleaseInfo{
		ExtensionID: "ue4ss",
		Version:     "v3.0.1-1011-gb50986bd",
		Asset:       githubReleaseAsset{Name: "UE4SS_v3.0.1-1011-gb50986bd.zip", UpdatedAt: "2026-07-13T00:29:54Z"},
	}
	local := ExtensionStatus{
		ID:                 "ue4ss",
		Installed:          true,
		Version:            latest.Version,
		InstalledAsset:     latest.Asset.Name,
		InstalledUpdatedAt: "2026-07-12T00:29:54Z",
	}
	if !extensionUpdateAvailable(local, latest) {
		t.Fatal("expected changed UE4SS asset timestamp to be an update")
	}
}

func TestExtensionUpdateAvailableKeepsMatchingUE4SSAssetCurrent(t *testing.T) {
	latest := extensionReleaseInfo{
		ExtensionID: "ue4ss",
		Version:     "v3.0.1-1011-gb50986bd",
		Asset:       githubReleaseAsset{Name: "UE4SS_v3.0.1-1011-gb50986bd.zip", UpdatedAt: "2026-07-13T00:29:54Z"},
	}
	local := ExtensionStatus{
		ID:                 "ue4ss",
		Installed:          true,
		Version:            latest.Version,
		InstalledAsset:     latest.Asset.Name,
		InstalledUpdatedAt: latest.Asset.UpdatedAt,
	}
	if extensionUpdateAvailable(local, latest) {
		t.Fatal("expected matching UE4SS asset metadata to be current")
	}
}

func TestExtensionUpdateAvailableTreatsMissingPalDefenderMetadataAsUpdate(t *testing.T) {
	latest := extensionReleaseInfo{
		ExtensionID: "paldefender",
		Version:     "v1.8.3",
		Asset:       githubReleaseAsset{Name: "PalDefender.zip", UpdatedAt: "2026-07-15T01:52:46Z"},
	}
	local := ExtensionStatus{ID: "paldefender", Installed: true, Version: latest.Version}
	if !extensionUpdateAvailable(local, latest) {
		t.Fatal("expected missing PalDefender install metadata to be an update")
	}
}

func TestExtensionUpdateAvailableIgnoresUnknownExtensionWithoutMetadata(t *testing.T) {
	local := ExtensionStatus{ID: "unknown", Installed: true}
	latest := extensionReleaseInfo{ExtensionID: "unknown", Version: "v1"}
	if extensionUpdateAvailable(local, latest) {
		t.Fatal("expected unknown extension to be ignored")
	}
}

func TestCheckExtensionUpdatesReportsLatestVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/paldefender":
			_, _ = response.Write([]byte(`{"tag_name":"v1.8.3","published_at":"2026-07-15T01:53:44Z","assets":[{"id":1,"name":"PalDefender.zip","updated_at":"2026-07-15T01:52:46Z"}]}`))
		case "/ue4ss":
			_, _ = response.Write([]byte(`{"tag_name":"experimental-latest","assets":[{"id":2,"name":"UE4SS_v3.0.1-1011-gb50986bd.zip","updated_at":"2026-07-13T00:29:54Z"}]}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	local := []ExtensionStatus{{ID: "paldefender", Version: "v1.8.1", Installed: true}, {ID: "ue4ss", Version: "v3.0.1", Installed: true}}
	statuses := checkExtensionUpdatesWith(local, server.Client(), func(extensionID string) (extensionReleaseSource, error) {
		return extensionReleaseSource{ID: extensionID, Endpoint: server.URL + "/" + extensionID}, nil
	})
	if len(statuses) != 2 {
		t.Fatalf("statuses = %#v", statuses)
	}
	if statuses[0].LatestVersion != "v1.8.3" || !statuses[0].UpdateAvailable || statuses[0].UpdateCheckError != "" {
		t.Fatalf("PalDefender status = %#v", statuses[0])
	}
	if statuses[1].LatestVersion != "v3.0.1-1011-gb50986bd" || statuses[1].LatestAsset != "UE4SS_v3.0.1-1011-gb50986bd.zip" || !statuses[1].UpdateAvailable {
		t.Fatalf("UE4SS status = %#v", statuses[1])
	}
}

func TestCheckExtensionUpdatesKeepsPartialRepositoryErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/paldefender" {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"tag_name":"v1.8.3","assets":[{"name":"PalDefender.zip"}]}`))
			return
		}
		http.Error(response, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	local := []ExtensionStatus{{ID: "paldefender", Version: "v1.8.1", Installed: true}, {ID: "ue4ss", Version: "v3.0.1", Installed: true}}
	statuses := checkExtensionUpdatesWith(local, server.Client(), func(extensionID string) (extensionReleaseSource, error) {
		return extensionReleaseSource{ID: extensionID, Endpoint: server.URL + "/" + extensionID}, nil
	})
	if statuses[0].UpdateCheckError != "" || statuses[1].UpdateCheckError == "" {
		t.Fatalf("partial statuses = %#v", statuses)
	}
}

func TestStageExtensionUpdateDoesNotTouchRunningServerFiles(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(base, "PalDefender.dll")
	if err := os.WriteFile(live, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, err := stageExtensionPayload(instance, extensionReleaseInfo{
		ExtensionID: "paldefender",
		Version:     "v1.8.3",
		Asset:       githubReleaseAsset{ID: 42, Name: "PalDefender.zip", UpdatedAt: "2026-07-15T01:52:46Z", Size: 123, Digest: "sha256:abcd"},
	}, extensionFixtureDirectory(t, map[string]string{
		"PalDefender.dll": "new",
		"d3d9.dll":        "loader",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ExtensionID != "paldefender" || manifest.Version != "v1.8.3" || manifest.Asset.ID != 42 || manifest.Layout == "" || !manifest.DownloadedAt.Equal(manifest.DownloadedAt.UTC()) {
		t.Fatalf("manifest = %#v", manifest)
	}
	data, err := os.ReadFile(live)
	if err != nil || string(data) != "old" {
		t.Fatalf("live PalDefender.dll = %q, %v", data, err)
	}
	staged, err := os.ReadFile(filepath.Join(base, ".palserver-launcher", "staged", "paldefender", "pending", "payload", "PalDefender.dll"))
	if err != nil || string(staged) != "new" {
		t.Fatalf("staged PalDefender.dll = %q, %v", staged, err)
	}
}

func TestValidateStagedPalDefenderRejectsExtraFiles(t *testing.T) {
	payload := extensionFixtureDirectory(t, map[string]string{
		"PalDefender.dll": "plugin",
		"d3d9.dll":        "loader",
		"README.txt":      "unexpected",
	})
	if err := validateStagedExtension("paldefender", payload); err == nil {
		t.Fatal("PalDefender payload with an extra file was accepted")
	}
}

func TestValidateStagedUE4SSAcceptsNestedLayout(t *testing.T) {
	payload := extensionFixtureDirectory(t, map[string]string{
		"dwmapi.dll":                            "proxy",
		"ue4ss/UE4SS.dll":                       "core",
		"ue4ss/UE4SS-settings.ini":              "[Debug]\nConsoleEnabled = 1\n",
		"ue4ss/Mods/ConsoleEnablerMod/main.lua": "return {}",
	})
	if err := validateStagedExtension("ue4ss", payload); err != nil {
		t.Fatalf("nested UE4SS payload rejected: %v", err)
	}
}

func TestValidateStagedUE4SSRejectsLegacyRootLayout(t *testing.T) {
	payload := extensionFixtureDirectory(t, map[string]string{
		"dwmapi.dll":               "proxy",
		"UE4SS.dll":                "legacy core",
		"UE4SS-settings.ini":       "legacy settings",
		"Mods/mods.txt":            "Legacy : 1",
		"ue4ss/UE4SS.dll":          "core",
		"ue4ss/UE4SS-settings.ini": "[Debug]",
	})
	if err := validateStagedExtension("ue4ss", payload); err == nil {
		t.Fatal("UE4SS payload containing legacy root layout was accepted")
	}
}

func TestDownloadExtensionAssetVerifiesSizeAndSHA256(t *testing.T) {
	body := []byte("verified archive bytes")
	digest := sha256.Sum256(body)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(body)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "asset.zip")
	info := extensionReleaseInfo{ExtensionID: "paldefender", Asset: githubReleaseAsset{
		BrowserDownloadURL: server.URL,
		Size:               int64(len(body)),
		Digest:             "sha256:" + fmt.Sprintf("%x", digest),
	}}
	if err := downloadExtensionAsset(server.Client(), info, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != string(body) {
		t.Fatalf("downloaded asset = %q, %v", data, err)
	}
}

func TestDownloadExtensionAssetRejectsMismatch(t *testing.T) {
	body := []byte("archive bytes")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(body)
	}))
	defer server.Close()
	for name, asset := range map[string]githubReleaseAsset{
		"size": {BrowserDownloadURL: server.URL, Size: int64(len(body) + 1)},
		"sha":  {BrowserDownloadURL: server.URL, Size: int64(len(body)), Digest: "sha256:" + strings.Repeat("0", 64)},
	} {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "asset.zip")
			err := downloadExtensionAsset(server.Client(), extensionReleaseInfo{ExtensionID: "paldefender", Asset: asset}, destination)
			if err == nil {
				t.Fatal("mismatched asset was accepted")
			}
			if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
				t.Fatalf("failed download was left behind: %v", statErr)
			}
		})
	}
}

func TestDownloadExtensionAssetRejectsOversizedDeclaredAssetBeforeNetwork(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: extensionRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader("body")), Header: make(http.Header)}, nil
	})}
	destination := filepath.Join(t.TempDir(), "asset.zip")
	err := downloadExtensionAsset(client, extensionReleaseInfo{ExtensionID: "paldefender", Asset: githubReleaseAsset{
		BrowserDownloadURL: "https://example.invalid/PalDefender.zip",
		Size:               (512 << 20) + 1,
	}}, destination)
	if err == nil {
		t.Fatal("asset larger than 512 MiB was accepted")
	}
	if requests != 0 {
		t.Fatalf("oversized declared asset made %d network request(s)", requests)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("oversized declared asset left a destination file: %v", statErr)
	}
}

func TestDownloadExtensionAssetStopsAtDeclaredOrGlobalLimitAndClosesBody(t *testing.T) {
	for _, test := range []struct {
		name         string
		declaredSize int64
		limit        int64
		wantRead     int
	}{
		{name: "declared-size", declaredSize: 4, limit: 8, wantRead: 5},
		{name: "global-limit", declaredSize: 0, limit: 8, wantRead: 9},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &trackedExtensionBody{reader: bytes.NewReader(bytes.Repeat([]byte("x"), 64))}
			client := &http.Client{Transport: extensionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: body, Header: make(http.Header), ContentLength: -1}, nil
			})}
			destination := filepath.Join(t.TempDir(), "asset.zip")
			err := downloadExtensionAssetWithLimit(client, extensionReleaseInfo{ExtensionID: "paldefender", Asset: githubReleaseAsset{
				BrowserDownloadURL: "https://example.invalid/PalDefender.zip",
				Size:               test.declaredSize,
			}}, destination, test.limit)
			if err == nil {
				t.Fatal("oversized response body was accepted")
			}
			if body.read != test.wantRead {
				t.Fatalf("response bytes read = %d, want %d", body.read, test.wantRead)
			}
			if !body.closed {
				t.Fatal("oversized response body was not closed")
			}
			if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
				t.Fatalf("oversized response left a destination file: %v", statErr)
			}
		})
	}
}

func TestExtractExtensionArchiveRejectsUnsafeEntriesBeforeWriting(t *testing.T) {
	cases := map[string][]extensionZipEntry{
		"traversal":      {{name: "../escape.dll", content: "bad"}},
		"absolute":       {{name: "/absolute.dll", content: "bad"}},
		"backslash":      {{name: `ue4ss\\UE4SS.dll`, content: "bad"}},
		"ads":            {{name: "UE4SS.dll:payload", content: "bad"}},
		"reserved":       {{name: "CON/file.dll", content: "bad"}},
		"clock-device":   {{name: "cLoCk$.txt", content: "bad"}},
		"console-input":  {{name: "ConIn$/file.dll", content: "bad"}},
		"console-output": {{name: "CONOUT$.DLL", content: "bad"}},
		"tail-dot":       {{name: "ue4ss./UE4SS.dll", content: "bad"}},
		"duplicate":      {{name: "ue4ss/UE4SS.dll", content: "one"}, {name: "UE4SS/ue4ss.DLL", content: "two"}},
		"symlink":        {{name: "ue4ss/link", content: "target", mode: os.ModeSymlink | 0o777}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "payload")
			err := extractExtensionArchive(writeExtensionTestZip(t, entries), destination)
			if err == nil {
				t.Fatal("unsafe extension archive was accepted")
			}
			if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
				t.Fatalf("preflight wrote destination before rejecting archive: %v", statErr)
			}
		})
	}
}

func TestExtractExtensionArchiveUsesExclusiveFileCreation(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "payload")
	if err := os.MkdirAll(filepath.Join(destination, "ue4ss"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(destination, "ue4ss", "UE4SS.dll")
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := writeExtensionTestZip(t, []extensionZipEntry{{name: "ue4ss/UE4SS.dll", content: "replacement"}})
	if err := extractExtensionArchive(archive, destination); err == nil {
		t.Fatal("archive extraction overwrote an existing target")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "existing" {
		t.Fatalf("existing target = %q, %v", data, err)
	}
}

func TestMigratePalDefenderConfigRemovesObsoleteCrashOption(t *testing.T) {
	input := []byte(`{"version":"1.8.1","blockTowerBossCapture":true,"logChat":false,"nested":{"keep":1}}`)
	updated, err := migratePalDefenderConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(updated, []byte("blockTowerBossCapture")) {
		t.Fatalf("obsolete option remains in %s", updated)
	}
	for _, expected := range []string{`"version": "1.8.1"`, `"logChat": false`, `"keep": 1`} {
		if !bytes.Contains(updated, []byte(expected)) {
			t.Fatalf("migrated config lost %s: %s", expected, updated)
		}
	}
}

func TestApplyUE4SSPendingUpdateMigratesLegacyLayoutAndCustomMods(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	legacySettings := "[General]\r\nbUseUObjectArrayCache = true\r\nKeepSetting = legacy\r\nRemovedSetting = old\r\n[Debug]\r\nConsoleEnabled = 1\r\nGuiConsoleEnabled = 1\r\nGuiConsoleVisible = 1\r\n"
	for name, content := range map[string]string{
		"dwmapi.dll":                            "old proxy",
		"UE4SS.dll":                             "old core",
		"UE4SS-settings.ini":                    legacySettings,
		"Mods/CustomMod/main.lua":               "legacy custom",
		"Mods/ConsoleEnablerMod/main.lua":       "old builtin",
		"Mods/PackageBuiltin/main.lua":          "old package builtin",
		"Mods/mods.txt":                         "CustomMod : 0\r\nConsoleEnablerMod : 1\r\n",
		"ue4ss/Mods/CustomMod/main.lua":         "current custom",
		"ue4ss/Mods/ConsoleEnablerMod/main.lua": "current old builtin",
		"ue4ss/Mods/mods.txt":                   "CustomMod : 1\r\nConsoleEnablerMod : 0\r\n",
	} {
		path := filepath.Join(base, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	payload := extensionFixtureDirectory(t, map[string]string{
		"dwmapi.dll":                            "new proxy",
		"ue4ss/UE4SS.dll":                       "new core",
		"ue4ss/UE4SS-settings.ini":              "[General]\n bUseUObjectArrayCache = true\nKeepSetting = package\nNewSetting = package\n[Debug]\nConsoleEnabled = 1\nGuiConsoleEnabled = 1\nGuiConsoleVisible = 1\n",
		"ue4ss/Mods/ConsoleEnablerMod/main.lua": "new builtin",
		"ue4ss/Mods/PackageBuiltin/main.lua":    "new package builtin",
		"ue4ss/Mods/mods.txt":                   "ConsoleEnablerMod : 1\n",
	})
	if _, err := stageExtensionPayload(instance, extensionReleaseInfo{
		ExtensionID: "ue4ss", Version: "v3.0.1-1011-gb50986bd",
		Asset: githubReleaseAsset{ID: 7, Name: "UE4SS_v3.0.1-1011-gb50986bd.zip", UpdatedAt: "2026-07-13T00:29:54Z"},
	}, payload); err != nil {
		t.Fatal(err)
	}
	if err := applyPendingExtensionUpdate(instance, "ue4ss"); err != nil {
		t.Fatal(err)
	}
	for relative, expected := range map[string]string{
		"dwmapi.dll":                            "new proxy",
		"ue4ss/UE4SS.dll":                       "new core",
		"ue4ss/Mods/CustomMod/main.lua":         "current custom",
		"ue4ss/Mods/ConsoleEnablerMod/main.lua": "new builtin",
		"ue4ss/Mods/PackageBuiltin/main.lua":    "new package builtin",
	} {
		data, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(relative)))
		if err != nil || string(data) != expected {
			t.Fatalf("%s = %q, %v", relative, data, err)
		}
	}
	settings, err := os.ReadFile(filepath.Join(base, "ue4ss", "UE4SS-settings.ini"))
	if err != nil {
		t.Fatal(err)
	}
	settingsText := string(settings)
	for _, expected := range []string{"KeepSetting = legacy", "NewSetting = package", "bUseUObjectArrayCache = false", "ConsoleEnabled = 0", "GuiConsoleEnabled = 0", "GuiConsoleVisible = 0"} {
		if !strings.Contains(settingsText, expected) {
			t.Fatalf("migrated settings missing %q:\n%s", expected, settingsText)
		}
	}
	if strings.Contains(settingsText, "RemovedSetting") {
		t.Fatalf("removed package setting was migrated:\n%s", settingsText)
	}
	mods, err := os.ReadFile(filepath.Join(base, "ue4ss", "Mods", "mods.txt"))
	if err != nil || !strings.Contains(string(mods), "CustomMod : 1") {
		t.Fatalf("migrated mods.txt = %q, %v", mods, err)
	}
	for _, legacy := range []string{"UE4SS.dll", "UE4SS.disabled.dll", "UE4SS-settings.ini", "Mods"} {
		if _, err := os.Stat(filepath.Join(base, legacy)); !os.IsNotExist(err) {
			t.Fatalf("legacy path %s remains: %v", legacy, err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, ".palserver-launcher", "staged", "ue4ss", "pending")); !os.IsNotExist(err) {
		t.Fatalf("successful update left pending payload: %v", err)
	}
}

func TestApplyPendingExtensionRollsBackAfterCommitFailure(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	oldFiles := map[string]string{
		"dwmapi.dll":              "old proxy",
		"UE4SS.dll":               "old core",
		"UE4SS-settings.ini":      "[General]\nOld = setting\n",
		"Mods/CustomMod/main.lua": "old mod",
		"Mods/mods.txt":           "CustomMod : 0\n",
	}
	for name, content := range oldFiles {
		path := filepath.Join(base, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := stageExtensionPayload(instance, extensionReleaseInfo{ExtensionID: "ue4ss", Version: "v-next"}, extensionFixtureDirectory(t, map[string]string{
		"dwmapi.dll":                  "new proxy",
		"ue4ss/UE4SS.dll":             "new core",
		"ue4ss/UE4SS-settings.ini":    "[General]\nOld = package\n",
		"ue4ss/Mods/Builtin/main.lua": "new builtin",
	})); err != nil {
		t.Fatal(err)
	}
	mutations := 0
	err := applyPendingExtensionUpdateWithOps(instance, "ue4ss", extensionApplyOps{AfterMutation: func(step string) error {
		mutations++
		if step == "ue4ss-legacy-cleanup" {
			return errors.New("injected commit failure")
		}
		return nil
	}})
	if err == nil || !strings.Contains(err.Error(), "injected commit failure") || mutations < 2 {
		t.Fatalf("apply error = %v, mutations = %d", err, mutations)
	}
	for name, expected := range oldFiles {
		data, readErr := os.ReadFile(filepath.Join(base, filepath.FromSlash(name)))
		if readErr != nil || string(data) != expected {
			t.Fatalf("rollback %s = %q, %v", name, data, readErr)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "ue4ss", "UE4SS.dll")); !os.IsNotExist(err) {
		t.Fatalf("rollback left new nested core: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, ".palserver-launcher", "staged", "ue4ss", "pending", "manifest.json")); err != nil {
		t.Fatalf("failed update did not retain pending: %v", err)
	}
	backups, err := os.ReadDir(filepath.Join(base, ".palserver-launcher", "backups", "ue4ss"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("failed update did not retain backup: %v, entries=%d", err, len(backups))
	}
}

func TestRenameExtensionStagePathRetriesTransientFailures(t *testing.T) {
	attempts := 0
	delays := make([]time.Duration, 0)
	err := renameExtensionStagePathWith(func(source, destination string) error {
		attempts++
		if source != "incoming" || destination != "pending" {
			t.Fatalf("rename paths = %q -> %q", source, destination)
		}
		if attempts < 4 {
			return os.ErrPermission
		}
		return nil
	}, func(delay time.Duration) {
		delays = append(delays, delay)
	}, "incoming", "pending")
	if err != nil || attempts != 4 {
		t.Fatalf("retry result attempts=%d err=%v", attempts, err)
	}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("retry delays = %v, want %v", delays, want)
	}
}

func TestRenameExtensionStagePathReturnsLastFailure(t *testing.T) {
	attempts := 0
	want := errors.New("still locked")
	err := renameExtensionStagePathWith(func(string, string) error {
		attempts++
		return want
	}, func(time.Duration) {}, "incoming", "pending")
	if !errors.Is(err, want) || attempts != 8 {
		t.Fatalf("retry result attempts=%d err=%v", attempts, err)
	}
}

func TestPrepareServerBeforeLaunchAppliesValidPendingExtensions(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	if _, err := stageExtensionPayload(instance, extensionReleaseInfo{
		ExtensionID: "paldefender",
		Version:     "v-prelaunch",
		Asset:       githubReleaseAsset{Name: "PalDefender.zip", UpdatedAt: "2026-07-15T01:52:46Z"},
	}, extensionFixtureDirectory(t, map[string]string{
		"PalDefender.dll": "prepared plugin",
		"d3d9.dll":        "prepared loader",
	})); err != nil {
		t.Fatal(err)
	}
	if err := prepareServerBeforeLaunch(instance); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{"PalDefender.dll": "prepared plugin", "d3d9.dll": "prepared loader", "palguard.version.txt": "v-prelaunch\n"} {
		data, err := os.ReadFile(filepath.Join(win64Path(instance), name))
		if err != nil || string(data) != expected {
			t.Fatalf("prepared %s = %q, %v", name, data, err)
		}
	}
	pending, err := extensionPendingPath(instance, "paldefender")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("successful prelaunch left pending update: %v", err)
	}
}

func TestPrepareServerBeforeLaunchRejectsDamagedPendingExtension(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	if _, err := stageExtensionPayload(instance, extensionReleaseInfo{ExtensionID: "paldefender", Version: "v-damaged"}, extensionFixtureDirectory(t, map[string]string{
		"PalDefender.dll": "plugin",
		"d3d9.dll":        "loader",
	})); err != nil {
		t.Fatal(err)
	}
	pending, err := extensionPendingPath(instance, "paldefender")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(pending, "payload", "d3d9.dll")); err != nil {
		t.Fatal(err)
	}
	err = prepareServerBeforeLaunch(instance)
	if err == nil || !strings.Contains(err.Error(), "apply pending extension updates") {
		t.Fatalf("damaged pending preparation error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(win64Path(instance), "PalDefender.dll")); !os.IsNotExist(err) {
		t.Fatalf("damaged pending update changed live plugin: %v", err)
	}
}

func TestStartServerPreparesPendingBeforeRuntimeChecksAndCommandCreation(t *testing.T) {
	data, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	start := strings.Index(source, "func (a *App) StartServer")
	if start < 0 {
		t.Fatal("StartServer source body was not found")
	}
	end := strings.Index(source[start:], "\n}\n\nfunc ")
	if end < 0 {
		t.Fatal("StartServer source body end was not found")
	}
	body := source[start : start+end]
	tokens := []string{
		"a.extensionStageMu.Lock()",
		"prepareErr := prepareServerBeforeLaunch(instance)",
		"a.extensionStageMu.Unlock()",
		"if prepareErr != nil",
		"return prepareErr",
		"ensureDirectXRuntime",
		"applyPerformanceConfig",
		"exec.Command",
	}
	previous := -1
	for _, token := range tokens {
		index := strings.Index(body, token)
		if index < 0 {
			t.Fatalf("StartServer is missing %q", token)
		}
		if index <= previous {
			t.Fatalf("StartServer has %q out of prelaunch order", token)
		}
		previous = index
	}
}

func TestListExtensionsSupportsNestedUE4SSAndPendingMetadata(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	for name, content := range map[string]string{
		"dwmapi.disabled.dll":      "proxy",
		"ue4ss/UE4SS.dll":          "core",
		"ue4ss/UE4SS-settings.ini": "[Debug]\nConsoleEnabled = 0\n",
		"ue4ss.version.txt":        "v-installed\n",
	} {
		path := filepath.Join(base, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	installed := extensionUpdateManifest{
		Schema: extensionUpdateManifestSchema, ExtensionID: "ue4ss", Version: "v-installed",
		Asset:        extensionUpdateAsset{ID: 5, Name: "UE4SS_installed.zip", UpdatedAt: "2026-07-12T00:00:00Z"},
		DownloadedAt: time.Now().UTC(), Layout: "ue4ss-nested-v1",
	}
	installedPath, err := extensionInstalledManifestPath(instance, "ue4ss")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(installed)
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceFileData(installedPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := stageExtensionPayload(instance, extensionReleaseInfo{ExtensionID: "ue4ss", Version: "v-pending"}, extensionFixtureDirectory(t, map[string]string{
		"dwmapi.dll":               "new proxy",
		"ue4ss/UE4SS.dll":          "new core",
		"ue4ss/UE4SS-settings.ini": "[Debug]\nConsoleEnabled = 0\n",
	})); err != nil {
		t.Fatal(err)
	}
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	statuses, err := app.ListExtensions(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	var status ExtensionStatus
	for _, candidate := range statuses {
		if candidate.ID == "ue4ss" {
			status = candidate
		}
	}
	if !status.Installed || status.Enabled || status.Version != "v-installed" || status.Path != filepath.Join(base, "dwmapi.disabled.dll") {
		t.Fatalf("nested UE4SS status = %#v", status)
	}
	if status.InstalledAsset != "UE4SS_installed.zip" || status.InstalledUpdatedAt != "2026-07-12T00:00:00Z" || !status.Pending || status.PendingVersion != "v-pending" {
		t.Fatalf("nested UE4SS metadata = %#v", status)
	}
}

func TestToggleUE4SSRenamesProxyLoader(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir(), Executable: filepath.Join(t.TempDir(), "PalServer.exe")}
	base := win64Path(instance)
	for name, content := range map[string]string{
		"dwmapi.dll":               "proxy",
		"ue4ss/UE4SS.dll":          "core",
		"ue4ss/UE4SS-settings.ini": "[Debug]\nConsoleEnabled = 0\n",
	} {
		path := filepath.Join(base, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	if err := app.ToggleExtension(instance.ID, "ue4ss", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "dwmapi.disabled.dll")); err != nil {
		t.Fatalf("disabled proxy is missing: %v", err)
	}
	core, err := os.ReadFile(filepath.Join(base, "ue4ss", "UE4SS.dll"))
	if err != nil || string(core) != "core" {
		t.Fatalf("toggle changed nested core = %q, %v", core, err)
	}
	if err := app.ToggleExtension(instance.ID, "ue4ss", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "dwmapi.dll")); err != nil {
		t.Fatalf("enabled proxy is missing: %v", err)
	}
}

func TestToggleLegacyUE4SSRollsBackCoreWhenProxyRenameFails(t *testing.T) {
	base := t.TempDir()
	for name, content := range map[string]string{"UE4SS.dll": "core", "dwmapi.dll": "proxy"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	proxyErr := errors.New("proxy rename failed")
	renameCalls := 0
	err := toggleLegacyUE4SSState(base, false, func(from, to string) (bool, error) {
		renameCalls++
		if renameCalls == 2 {
			return false, proxyErr
		}
		return renameExtensionStateFileChanged(from, to)
	})
	if !errors.Is(err, proxyErr) {
		t.Fatalf("toggle error = %v, want proxy failure", err)
	}
	for name, expected := range map[string]string{"UE4SS.dll": "core", "dwmapi.dll": "proxy"} {
		data, readErr := os.ReadFile(filepath.Join(base, name))
		if readErr != nil || string(data) != expected {
			t.Fatalf("original %s after rollback = %q, %v", name, data, readErr)
		}
	}
	for _, name := range []string{"UE4SS.disabled.dll", "dwmapi.disabled.dll"} {
		if _, statErr := os.Stat(filepath.Join(base, name)); !os.IsNotExist(statErr) {
			t.Fatalf("partial legacy toggle left %s: %v", name, statErr)
		}
	}
}

func TestToggleLegacyUE4SSJoinsProxyAndCoreRollbackFailures(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"UE4SS.dll", "dwmapi.dll"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	proxyErr := errors.New("proxy rename failed")
	rollbackErr := errors.New("core rollback failed")
	renameCalls := 0
	err := toggleLegacyUE4SSState(base, false, func(from, to string) (bool, error) {
		renameCalls++
		switch renameCalls {
		case 2:
			return false, proxyErr
		case 3:
			return false, rollbackErr
		default:
			return renameExtensionStateFileChanged(from, to)
		}
	})
	if !errors.Is(err, proxyErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("joined toggle error = %v", err)
	}
}

func TestUpdateExtensionStagesWhileRunning(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"PalDefender.dll": "old plugin", "d3d9.dll": "old loader"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	archive := extensionTestZipBytes(t, []extensionZipEntry{{name: "PalDefender.dll", content: "new plugin"}, {name: "d3d9.dll", content: "new loader"}})
	digest := sha256.Sum256(archive)
	downloadLockState := make(chan [2]bool, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"tag_name":"v1.8.3","assets":[{"id":9,"name":"PalDefender.zip","browser_download_url":%q,"size":%d,"digest":"sha256:%x","updated_at":"2026-07-15T01:52:46Z"}]}`, server.URL+"/asset", len(archive), digest)
		case "/asset":
			startUnlocked := app.serverStartMu.TryLock()
			if startUnlocked {
				app.serverStartMu.Unlock()
			}
			stageUnlocked := app.extensionStageMu.TryLock()
			if stageUnlocked {
				app.extensionStageMu.Unlock()
			}
			downloadLockState <- [2]bool{startUnlocked, stageUnlocked}
			_, _ = response.Write(archive)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	result, err := app.updateExtensionWith(instance.ID, "paldefender", extensionUpdateDependencies{
		Client: server.Client(),
		SourceFor: func(string) (extensionReleaseSource, error) {
			return extensionReleaseSource{ID: "paldefender", Endpoint: server.URL + "/release"}, nil
		},
		StatusFor: func(ServerInstance) (RuntimeStatus, error) { return RuntimeStatus{Running: true}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	lockState := <-downloadLockState
	if !lockState[0] || !lockState[1] {
		t.Fatalf("UpdateExtension held a mutation lock during asset download: serverStart=%v extensionStage=%v", lockState[0], lockState[1])
	}
	if result.ExtensionID != "paldefender" || result.Version != "v1.8.3" || !result.Pending {
		t.Fatalf("running update result = %#v", result)
	}
	for name, expected := range map[string]string{"PalDefender.dll": "old plugin", "d3d9.dll": "old loader"} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil || string(data) != expected {
			t.Fatalf("running update changed %s = %q, %v", name, data, err)
		}
	}
}

func TestConcurrentExtensionUpdatesSerializePendingPublicationAndApply(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}

	type updateServer struct {
		server     *httptest.Server
		downloaded <-chan struct{}
	}
	newUpdateServer := func(version, plugin string) updateServer {
		archive := extensionTestZipBytes(t, []extensionZipEntry{{name: "PalDefender.dll", content: plugin}, {name: "d3d9.dll", content: "loader-" + version}})
		downloaded := make(chan struct{}, 1)
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/release":
				response.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(response, `{"tag_name":%q,"assets":[{"name":"PalDefender.zip","browser_download_url":%q,"size":%d}]}`, version, server.URL+"/asset", len(archive))
			case "/asset":
				_, _ = response.Write(archive)
				downloaded <- struct{}{}
			default:
				http.NotFound(response, request)
			}
		}))
		return updateServer{server: server, downloaded: downloaded}
	}
	firstServer := newUpdateServer("v-first", "plugin-first")
	defer firstServer.server.Close()
	secondServer := newUpdateServer("v-second", "plugin-second")
	defer secondServer.server.Close()

	type updateResult struct {
		result ExtensionUpdateResult
		err    error
	}
	firstAtStatus := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstReleased := false
	t.Cleanup(func() {
		if !firstReleased {
			close(releaseFirst)
		}
	})
	firstDone := make(chan updateResult, 1)
	go func() {
		result, err := app.updateExtensionWith(instance.ID, "paldefender", extensionUpdateDependencies{
			Client: firstServer.server.Client(),
			SourceFor: func(string) (extensionReleaseSource, error) {
				return extensionReleaseSource{ID: "paldefender", Endpoint: firstServer.server.URL + "/release"}, nil
			},
			StatusFor: func(ServerInstance) (RuntimeStatus, error) {
				close(firstAtStatus)
				<-releaseFirst
				return RuntimeStatus{Running: false}, nil
			},
		})
		firstDone <- updateResult{result: result, err: err}
	}()
	<-firstAtStatus

	pending, err := extensionPendingPath(instance, "paldefender")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readExtensionUpdateManifest(filepath.Join(pending, "manifest.json"))
	if err != nil || manifest.Version != "v-first" {
		t.Fatalf("first pending manifest = %#v, %v", manifest, err)
	}

	secondDone := make(chan updateResult, 1)
	go func() {
		result, err := app.updateExtensionWith(instance.ID, "paldefender", extensionUpdateDependencies{
			Client: secondServer.server.Client(),
			SourceFor: func(string) (extensionReleaseSource, error) {
				return extensionReleaseSource{ID: "paldefender", Endpoint: secondServer.server.URL + "/release"}, nil
			},
			StatusFor: func(ServerInstance) (RuntimeStatus, error) { return RuntimeStatus{Running: false}, nil },
		})
		secondDone <- updateResult{result: result, err: err}
	}()
	<-secondServer.downloaded

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		manifest, readErr := readExtensionUpdateManifest(filepath.Join(pending, "manifest.json"))
		if readErr != nil {
			t.Fatalf("read pending while first update is applying: %v", readErr)
		}
		if manifest.Version != "v-first" {
			t.Fatalf("second update replaced pending before the first apply completed: %#v", manifest)
		}
		select {
		case completed := <-secondDone:
			t.Fatalf("second update completed before the first update released mutation locks: %#v, %v", completed.result, completed.err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	firstReleased = true
	close(releaseFirst)

	first := <-firstDone
	second := <-secondDone
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent update errors: first=%v second=%v", first.err, second.err)
	}
	if first.result.Version != "v-first" || second.result.Version != "v-second" || first.result.Pending || second.result.Pending {
		t.Fatalf("concurrent update results: first=%#v second=%#v", first.result, second.result)
	}
	version, err := os.ReadFile(filepath.Join(win64Path(instance), "palguard.version.txt"))
	if err != nil || strings.TrimSpace(string(version)) != "v-second" {
		t.Fatalf("installed version after concurrent updates = %q, %v", version, err)
	}
	plugin, err := os.ReadFile(filepath.Join(win64Path(instance), "PalDefender.dll"))
	if err != nil || string(plugin) != "plugin-second" {
		t.Fatalf("installed plugin after concurrent updates = %q, %v", plugin, err)
	}
}

func TestToggleExtensionWaitsForPendingApplyAndPreservesTheRequestedState(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"PalDefender.dll": "old plugin", "d3d9.dll": "old loader"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	archive := extensionTestZipBytes(t, []extensionZipEntry{{name: "PalDefender.dll", content: "new plugin"}, {name: "d3d9.dll", content: "new loader"}})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"tag_name":"v-next","assets":[{"name":"PalDefender.zip","browser_download_url":%q,"size":%d}]}`, server.URL+"/asset", len(archive))
		case "/asset":
			_, _ = response.Write(archive)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	applyPaused := make(chan struct{})
	releaseApply := make(chan struct{})
	applyReleased := false
	t.Cleanup(func() {
		if !applyReleased {
			close(releaseApply)
		}
	})
	updateDone := make(chan error, 1)
	go func() {
		_, err := app.updateExtensionWith(instance.ID, "paldefender", extensionUpdateDependencies{
			Client: server.Client(),
			SourceFor: func(string) (extensionReleaseSource, error) {
				return extensionReleaseSource{ID: "paldefender", Endpoint: server.URL + "/release"}, nil
			},
			StatusFor: func(ServerInstance) (RuntimeStatus, error) { return RuntimeStatus{Running: false}, nil },
			ApplyOps: extensionApplyOps{AfterMutation: func(step string) error {
				if step == "paldefender-loader" {
					close(applyPaused)
					<-releaseApply
				}
				return nil
			}},
		})
		updateDone <- err
	}()
	<-applyPaused

	toggleDone := make(chan error, 1)
	go func() {
		toggleDone <- app.toggleExtensionWith(instance.ID, "paldefender", false, func(ServerInstance) (RuntimeStatus, error) {
			return RuntimeStatus{Running: false}, nil
		})
	}()
	select {
	case err := <-toggleDone:
		t.Fatalf("toggle interleaved with pending apply: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	applyReleased = true
	close(releaseApply)
	if err := <-updateDone; err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if err := <-toggleDone; err != nil {
		t.Fatalf("toggle after update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "PalDefender.dll")); !os.IsNotExist(err) {
		t.Fatalf("enabled plugin remains after serialized disable: %v", err)
	}
	plugin, err := os.ReadFile(filepath.Join(base, "PalDefender.disabled.dll"))
	if err != nil || string(plugin) != "new plugin" {
		t.Fatalf("disabled plugin after update = %q, %v", plugin, err)
	}
}

func TestUpdateExtensionAppliesWhileStopped(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	archive := extensionTestZipBytes(t, []extensionZipEntry{{name: "PalDefender.dll", content: "new plugin"}, {name: "d3d9.dll", content: "new loader"}})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"tag_name":"v1.8.3","assets":[{"id":9,"name":"PalDefender.zip","browser_download_url":%q,"size":%d}]}`, server.URL+"/asset", len(archive))
		case "/asset":
			_, _ = response.Write(archive)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	result, err := app.updateExtensionWith(instance.ID, "paldefender", extensionUpdateDependencies{
		Client: server.Client(),
		SourceFor: func(string) (extensionReleaseSource, error) {
			return extensionReleaseSource{ID: "paldefender", Endpoint: server.URL + "/release"}, nil
		},
		StatusFor: func(ServerInstance) (RuntimeStatus, error) { return RuntimeStatus{Running: false}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExtensionID != "paldefender" || result.Version != "v1.8.3" || result.Pending {
		t.Fatalf("stopped update result = %#v", result)
	}
	for name, expected := range map[string]string{"PalDefender.dll": "new plugin", "d3d9.dll": "new loader"} {
		data, err := os.ReadFile(filepath.Join(win64Path(instance), name))
		if err != nil || string(data) != expected {
			t.Fatalf("stopped update %s = %q, %v", name, data, err)
		}
	}
	if _, err := os.Stat(filepath.Join(win64Path(instance), ".palserver-launcher", "staged", "paldefender", "pending")); !os.IsNotExist(err) {
		t.Fatalf("stopped update remained pending: %v", err)
	}
}

func TestUpdateExtensionReReadsInstanceAfterStaging(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir(), PublicPort: 8211}
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	archive := extensionTestZipBytes(t, []extensionZipEntry{{name: "PalDefender.dll", content: "new plugin"}, {name: "d3d9.dll", content: "new loader"}})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"tag_name":"v1.8.3","assets":[{"name":"PalDefender.zip","browser_download_url":%q,"size":%d}]}`, server.URL+"/asset", len(archive))
		case "/asset":
			_, _ = response.Write(archive)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	seenPort := 0
	_, err := app.updateExtensionWith(instance.ID, "paldefender", extensionUpdateDependencies{
		Client: server.Client(),
		SourceFor: func(extensionID string) (extensionReleaseSource, error) {
			app.store.mu.Lock()
			app.store.config.Instances[0].PublicPort = 9000
			app.store.mu.Unlock()
			return extensionReleaseSource{ID: extensionID, Endpoint: server.URL + "/release"}, nil
		},
		StatusFor: func(current ServerInstance) (RuntimeStatus, error) {
			seenPort = current.PublicPort
			return RuntimeStatus{Running: true}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenPort != 9000 {
		t.Fatalf("StatusFor received stale PublicPort %d, want 9000", seenPort)
	}
}

func TestUpdateExtensionRejectsInstancePathChangeAfterStaging(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir(), Executable: filepath.Join(t.TempDir(), "PalServer.exe")}
	newRoot := t.TempDir()
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	archive := extensionTestZipBytes(t, []extensionZipEntry{{name: "PalDefender.dll", content: "new plugin"}, {name: "d3d9.dll", content: "new loader"}})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"tag_name":"v1.8.3","assets":[{"name":"PalDefender.zip","browser_download_url":%q,"size":%d}]}`, server.URL+"/asset", len(archive))
		case "/asset":
			_, _ = response.Write(archive)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	_, err := app.updateExtensionWith(instance.ID, "paldefender", extensionUpdateDependencies{
		Client: server.Client(),
		SourceFor: func(extensionID string) (extensionReleaseSource, error) {
			app.store.mu.Lock()
			app.store.config.Instances[0].RootPath = newRoot
			app.store.config.Instances[0].Executable = filepath.Join(newRoot, "PalServer.exe")
			app.store.mu.Unlock()
			return extensionReleaseSource{ID: extensionID, Endpoint: server.URL + "/release"}, nil
		},
		StatusFor: func(ServerInstance) (RuntimeStatus, error) { return RuntimeStatus{Running: false}, nil },
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "changed") {
		t.Fatalf("instance path change error = %v", err)
	}
	pending, pendingErr := extensionPendingPath(instance, "paldefender")
	if pendingErr != nil {
		t.Fatal(pendingErr)
	}
	if _, statErr := os.Stat(filepath.Join(pending, "manifest.json")); statErr != nil {
		t.Fatalf("path change did not retain staged pending update: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(win64Path(ServerInstance{RootPath: newRoot}), "PalDefender.dll")); !os.IsNotExist(statErr) {
		t.Fatalf("path change applied update to new instance path: %v", statErr)
	}
}

func TestUpdateAllExtensionsSkipsUninstalledExtensions(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"PalDefender.dll": "old plugin", "d3d9.dll": "old loader"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archive := extensionTestZipBytes(t, []extensionZipEntry{{name: "PalDefender.dll", content: "new plugin"}, {name: "d3d9.dll", content: "new loader"}})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release/paldefender":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"tag_name":"v1.8.3","assets":[{"name":"PalDefender.zip","browser_download_url":%q,"size":%d}]}`, server.URL+"/asset/paldefender", len(archive))
		case "/asset/paldefender":
			_, _ = response.Write(archive)
		default:
			http.Error(response, "unexpected uninstalled extension request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	results, err := app.updateAllExtensionsWith(instance.ID, extensionUpdateDependencies{
		Client: server.Client(),
		SourceFor: func(extensionID string) (extensionReleaseSource, error) {
			return extensionReleaseSource{ID: extensionID, Endpoint: server.URL + "/release/" + extensionID}, nil
		},
		StatusFor: func(ServerInstance) (RuntimeStatus, error) { return RuntimeStatus{Running: true}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ExtensionID != "paldefender" || !results[0].Pending {
		t.Fatalf("update all results = %#v", results)
	}
}

func TestUpdateAllExtensionsSkipsAlreadyCurrentExtensions(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"PalDefender.dll":      "current plugin",
		"d3d9.dll":             "current loader",
		"palguard.version.txt": "v1.8.3\n",
	} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	updatedAt := "2026-07-15T01:52:46Z"
	manifest := extensionUpdateManifest{
		Schema: extensionUpdateManifestSchema, ExtensionID: "paldefender", Version: "v1.8.3",
		Asset:        extensionUpdateAsset{ID: 9, Name: "PalDefender.zip", UpdatedAt: updatedAt},
		DownloadedAt: time.Now().UTC(), Layout: "paldefender-root-v1",
	}
	manifestPath, err := extensionInstalledManifestPath(instance, "paldefender")
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(manifestData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	archive := extensionTestZipBytes(t, []extensionZipEntry{{name: "PalDefender.dll", content: "unexpected plugin"}, {name: "d3d9.dll", content: "unexpected loader"}})
	releaseRequests, assetRequests := 0, 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release/paldefender":
			releaseRequests++
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"tag_name":"v1.8.3","assets":[{"id":9,"name":"PalDefender.zip","browser_download_url":%q,"size":%d,"updated_at":%q}]}`, server.URL+"/asset/paldefender", len(archive), updatedAt)
		case "/asset/paldefender":
			assetRequests++
			_, _ = response.Write(archive)
		default:
			http.Error(response, "unexpected extension request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	results, err := app.updateAllExtensionsWith(instance.ID, extensionUpdateDependencies{
		Client: server.Client(),
		SourceFor: func(extensionID string) (extensionReleaseSource, error) {
			return extensionReleaseSource{ID: extensionID, Endpoint: server.URL + "/release/" + extensionID}, nil
		},
		StatusFor: func(ServerInstance) (RuntimeStatus, error) { return RuntimeStatus{Running: true}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if releaseRequests != 1 {
		t.Fatalf("release requests = %d, want 1", releaseRequests)
	}
	if assetRequests != 0 {
		t.Fatalf("current extension asset requests = %d, want 0", assetRequests)
	}
	if len(results) != 0 {
		t.Fatalf("current extension update-all results = %#v, want none", results)
	}
}

func TestUpdateAllExtensionsReturnsCheckErrorsBeforeDownloading(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"PalDefender.dll": "plugin", "d3d9.dll": "loader"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	assetRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release/paldefender":
			http.Error(response, "release service unavailable", http.StatusServiceUnavailable)
		case "/asset/paldefender":
			assetRequests++
			response.WriteHeader(http.StatusInternalServerError)
		default:
			http.Error(response, "unexpected extension request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	results, err := app.updateAllExtensionsWith(instance.ID, extensionUpdateDependencies{
		Client: server.Client(),
		SourceFor: func(extensionID string) (extensionReleaseSource, error) {
			return extensionReleaseSource{ID: extensionID, Endpoint: server.URL + "/release/" + extensionID}, nil
		},
		StatusFor: func(ServerInstance) (RuntimeStatus, error) { return RuntimeStatus{Running: true}, nil },
	})
	if err == nil {
		t.Fatal("update all silently ignored the release check failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "paldefender") {
		t.Fatalf("update all check error = %q, want plugin name or ID", err)
	}
	if len(results) != 0 {
		t.Fatalf("update all check failure results = %#v, want none", results)
	}
	if assetRequests != 0 {
		t.Fatalf("update all requested %d assets after a check failure", assetRequests)
	}
}

func TestExtensionBackupRetentionKeepsLatestThree(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	base := win64Path(instance)
	if err := os.MkdirAll(filepath.Join(base, "PalDefender"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"PalDefender.disabled.dll":  "old plugin",
		"d3d9.dll":                  "old loader",
		"PalDefender/Config.json":   `{"version":"1.8.1","blockTowerBossCapture":true,"keep":1}`,
		"PalDefender/UserData.json": "preserve me",
	} {
		if err := os.WriteFile(filepath.Join(base, filepath.FromSlash(name)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 5; index++ {
		version := fmt.Sprintf("v1.8.%d", index+2)
		payload := extensionFixtureDirectory(t, map[string]string{
			"PalDefender.dll": fmt.Sprintf("plugin-%d", index),
			"d3d9.dll":        fmt.Sprintf("loader-%d", index),
		})
		if _, err := stageExtensionPayload(instance, extensionReleaseInfo{ExtensionID: "paldefender", Version: version}, payload); err != nil {
			t.Fatal(err)
		}
		if err := applyPendingExtensionUpdate(instance, "paldefender"); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(base, ".palserver-launcher", "backups", "paldefender"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("retained backup directories = %d, want 3", count)
	}
	if _, err := os.Stat(filepath.Join(base, "PalDefender.dll")); !os.IsNotExist(err) {
		t.Fatalf("disabled PalDefender update created enabled DLL: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(base, "PalDefender", "UserData.json"))
	if err != nil || string(data) != "preserve me" {
		t.Fatalf("PalDefender user data = %q, %v", data, err)
	}
	config, err := os.ReadFile(filepath.Join(base, "PalDefender", "Config.json"))
	if err != nil || bytes.Contains(config, []byte("blockTowerBossCapture")) || !bytes.Contains(config, []byte(`"keep": 1`)) {
		t.Fatalf("PalDefender migrated config = %s, %v", config, err)
	}
}

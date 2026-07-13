package main

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifiedServerModCatalogOnlyContainsPostOnePointZeroDedicatedMods(t *testing.T) {
	released := time.Date(2026, 7, 10, 0, 0, 0, 0, time.Local)
	if len(verifiedServerModCatalog) == 0 {
		t.Fatal("verified server mod catalog is empty")
	}
	for _, entry := range verifiedServerModCatalog {
		updated, err := time.ParseInLocation("2006-01-02 15:04", entry.UpdatedAt, time.Local)
		if err != nil {
			t.Fatalf("%s has invalid update time %q: %v", entry.Name, entry.UpdatedAt, err)
		}
		if !updated.After(released) {
			t.Fatalf("%s was not updated after Palworld 1.0: %s", entry.Name, entry.UpdatedAt)
		}
		if entry.FolderName == "" || entry.Version == "" || entry.Dependency == "" {
			t.Fatalf("%s is missing installation metadata: %#v", entry.Name, entry)
		}
		if !strings.HasPrefix(entry.NexusURL, "https://www.nexusmods.com/palworld/mods/") {
			t.Fatalf("%s has an unexpected Nexus URL: %s", entry.Name, entry.NexusURL)
		}
	}
}

func TestServerModCatalogIncludesVerifiedUE4SSServerPluginsSortedNewestFirst(t *testing.T) {
	entries := sortedServerModCatalog()
	wanted := map[string]string{
		"pal-evo":        "PalEvolution",
		"rewards-engine": "RewardsEngine",
		"starter-kit":    "StarterKit",
	}
	for index, entry := range entries {
		if index > 0 && entries[index-1].UpdatedAt < entry.UpdatedAt {
			t.Fatalf("catalog is not sorted newest first at %q then %q", entries[index-1].UpdatedAt, entry.UpdatedAt)
		}
		if folder, ok := wanted[entry.ID]; ok {
			if entry.FolderName != folder {
				t.Fatalf("%s folder = %q, want %q", entry.ID, entry.FolderName, folder)
			}
			delete(wanted, entry.ID)
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing verified UE4SS server plugins: %#v", wanted)
	}
}

func TestFetchNexusModInfoReadsVersionAndUpdateTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("X-GraphQL-OperationName") != "GameModsListing" {
			t.Fatalf("unexpected Nexus request: %s %#v", request.Method, request.Header)
		}
		var payload struct {
			Variables struct {
				Filter struct {
					GameID []struct {
						Value string `json:"value"`
					} `json:"gameId"`
					ModID []struct {
						Value string `json:"value"`
					} `json:"modId"`
				} `json:"filter"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Variables.Filter.GameID) != 1 || payload.Variables.Filter.GameID[0].Value != "6063" || len(payload.Variables.Filter.ModID) != 1 || payload.Variables.Filter.ModID[0].Value != "3679" {
			t.Fatalf("unexpected Nexus filter: %#v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":{"mods":{"nodes":[{"modId":3679,"name":"PalEvo","updatedAt":"2026-07-13T05:21:06Z","version":"1.1"}]}}}`))
	}))
	defer server.Close()

	info, err := fetchNexusModInfo(server.Client(), server.URL, 3679)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "1.1" || info.UpdatedAt != "2026-07-13 13:21" {
		t.Fatalf("Nexus mod info = %#v", info)
	}
}

func TestCatalogUpdateStatusComparesInstalledMetadataWithNexus(t *testing.T) {
	entry := ServerModCatalogEntry{ID: "pal-evo", Installed: true, Version: "1", UpdatedAt: "2026-07-13 13:01"}
	metadata := serverModMetadata{CatalogID: "pal-evo", Version: "1", UpdatedAt: "2026-07-13 13:01"}
	entry = applyNexusUpdateInfo(entry, metadata, nexusModInfo{ModID: 3679, Version: "1.1", UpdatedAt: "2026-07-13 13:21"})
	if !entry.UpdateAvailable || entry.InstalledVersion != "1" || entry.LatestVersion != "1.1" || entry.LatestUpdatedAt != "2026-07-13 13:21" {
		t.Fatalf("update status = %#v", entry)
	}
	entry = applyNexusUpdateInfo(entry, serverModMetadata{Version: "1.1", UpdatedAt: "2026-07-13 13:21"}, nexusModInfo{ModID: 3679, Version: "1.1", UpdatedAt: "2026-07-13 13:21"})
	if entry.UpdateAvailable {
		t.Fatalf("current installation was marked outdated: %#v", entry)
	}
}

func TestCatalogInstallStateRecognizesEnabledAndDisabledFolders(t *testing.T) {
	modsRoot := t.TempDir()
	enabled := filepath.Join(modsRoot, "PalZones")
	if err := os.MkdirAll(enabled, 0o755); err != nil {
		t.Fatal(err)
	}
	installed, active, path := catalogModInstallState(modsRoot, "PalZones")
	if !installed || !active || path != enabled {
		t.Fatalf("enabled state = installed:%v active:%v path:%q", installed, active, path)
	}
	if err := os.Rename(enabled, enabled+".disabled"); err != nil {
		t.Fatal(err)
	}
	installed, active, path = catalogModInstallState(modsRoot, "PalZones")
	if !installed || active || path != enabled+".disabled" {
		t.Fatalf("disabled state = installed:%v active:%v path:%q", installed, active, path)
	}
}

func TestCatalogArchiveFindsNestedUE4SSModAndUpdatesModsTxt(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "palzones.zip")
	writeTestZip(t, archive, map[string]string{
		"package/ue4ss/Mods/PalZones/Scripts/main.lua": "return true",
	})
	extracted := t.TempDir()
	if err := unzipSafe(archive, extracted); err != nil {
		t.Fatal(err)
	}
	source, err := findCatalogModDirectory(extracted, "PalZones")
	if err != nil || !strings.HasSuffix(source, filepath.Join("Mods", "PalZones")) {
		t.Fatalf("nested mod root = %q, %v", source, err)
	}
	modsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(modsRoot, "mods.txt"), []byte("PalZones : 0\r\nOtherMod : 1\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setUE4SSModEnabled(modsRoot, "PalZones", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(modsRoot, "mods.txt"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Count(content, "PalZones : 1") != 1 || !strings.Contains(content, "OtherMod : 1") {
		t.Fatalf("unexpected mods.txt: %q", content)
	}
}

func TestUE4SSModsRootUsesCurrentPalworldLayoutAndKeepsLegacyCompatibility(t *testing.T) {
	instance := ServerInstance{RootPath: t.TempDir()}
	current := filepath.Join(win64Path(instance), "ue4ss", "Mods")
	legacy := filepath.Join(win64Path(instance), "Mods")
	if got := ue4ssModsRoot(instance); got != current {
		t.Fatalf("fresh server mods root = %q, want %q", got, current)
	}
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ue4ssModsRoot(instance); got != legacy {
		t.Fatalf("legacy server mods root = %q, want %q", got, legacy)
	}
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ue4ssModsRoot(instance); got != current {
		t.Fatalf("current server mods root = %q, want %q", got, current)
	}
}

func TestInstallServerModArchiveInstallsUpdatesBacksUpAndUninstalls(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	serverRoot := t.TempDir()
	instance := withDefaults(ServerInstance{ID: "server-1", Name: "Test", RootPath: serverRoot})
	app := &App{store: &Store{path: filepath.Join(t.TempDir(), "config.json"), config: AppConfig{Instances: []ServerInstance{instance}}}}

	firstArchive := filepath.Join(t.TempDir(), "first.zip")
	writeTestZip(t, firstArchive, map[string]string{"ue4ss/Mods/PalZones/version.txt": "1.0"})
	if err := app.InstallServerModArchive(instance.ID, "palzones", firstArchive); err != nil {
		t.Fatalf("first install: %v", err)
	}
	modsRoot := ue4ssModsRoot(instance)
	modFile := filepath.Join(modsRoot, "PalZones", "version.txt")
	if data, err := os.ReadFile(modFile); err != nil || string(data) != "1.0" {
		t.Fatalf("installed file = %q, %v", data, err)
	}
	metadata, err := readServerModMetadata(filepath.Dir(modFile))
	if err != nil || metadata.CatalogID != "palzones" || metadata.Version != "1.37" {
		t.Fatalf("installed metadata = %#v, %v", metadata, err)
	}

	if err := os.MkdirAll(filepath.Dir(modFile)+".disabled", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(modFile)+".disabled", "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	secondArchive := filepath.Join(t.TempDir(), "second.zip")
	writeTestZip(t, secondArchive, map[string]string{"PalZones/version.txt": "1.37"})
	if err := app.InstallServerModArchive(instance.ID, "palzones", secondArchive); err != nil {
		t.Fatalf("update install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modsRoot, "PalZones.disabled")); !os.IsNotExist(err) {
		t.Fatalf("disabled stale copy remains after update: %v", err)
	}
	if data, err := os.ReadFile(modFile); err != nil || string(data) != "1.37" {
		t.Fatalf("updated file = %q, %v", data, err)
	}
	backupRoot := filepath.Join(localAppData, "palserver-launcher", "mod-backups", instance.ID)
	backups, err := os.ReadDir(backupRoot)
	if err != nil || len(backups) != 1 {
		t.Fatalf("update backups = %d, %v", len(backups), err)
	}
	backupFile := filepath.Join(backupRoot, backups[0].Name(), "version.txt")
	if data, err := os.ReadFile(backupFile); err != nil || string(data) != "1.0" {
		t.Fatalf("backup file = %q, %v", data, err)
	}

	if err := os.Rename(filepath.Dir(modFile), filepath.Dir(modFile)+".disabled"); err != nil {
		t.Fatal(err)
	}
	if err := app.UninstallServerMod(instance.ID, "palzones"); err != nil {
		t.Fatalf("uninstall disabled mod: %v", err)
	}
	if installed, _, _ := catalogModInstallState(modsRoot, "PalZones"); installed {
		t.Fatal("catalog mod remains installed after uninstall")
	}
	modsData, err := os.ReadFile(filepath.Join(modsRoot, "mods.txt"))
	if err != nil || !strings.Contains(string(modsData), "PalZones : 0") {
		t.Fatalf("uninstall mods.txt = %q, %v", modsData, err)
	}
}

func TestServerModCatalogRejectsUnknownIDsAndNonZipArchives(t *testing.T) {
	serverRoot := t.TempDir()
	instance := withDefaults(ServerInstance{ID: "server-1", Name: "Test", RootPath: serverRoot})
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}}
	archive := filepath.Join(t.TempDir(), "mod.rar")
	if err := os.WriteFile(archive, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.InstallServerModArchive(instance.ID, "palzones", archive); err == nil || !strings.Contains(err.Error(), "ZIP") {
		t.Fatalf("non-ZIP archive error = %v", err)
	}
	if err := app.InstallServerModArchive(instance.ID, "unknown", archive+".zip"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown catalog id error = %v", err)
	}
	if _, err := nexusURLForCatalog("unknown"); err == nil {
		t.Fatal("unknown catalog id resolved to a Nexus URL")
	}
}

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, createErr := writer.Create(filepath.ToSlash(name))
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

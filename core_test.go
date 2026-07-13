package main

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestRCONPacketLayout(t *testing.T) {
	packet := rconPacket(7, 2, "Info")
	if got := int(binary.LittleEndian.Uint32(packet[:4])); got != len(packet)-4 {
		t.Fatalf("packet length = %d, want %d", got, len(packet)-4)
	}
	if got := int32(binary.LittleEndian.Uint32(packet[4:8])); got != 7 {
		t.Fatalf("packet id = %d, want 7", got)
	}
	if got := int32(binary.LittleEndian.Uint32(packet[8:12])); got != 2 {
		t.Fatalf("packet type = %d, want 2", got)
	}
	if payload := strings.TrimRight(string(packet[12:]), "\x00"); payload != "Info" {
		t.Fatalf("payload = %q, want Info", payload)
	}
}

func TestSafeJoinRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	if _, err := safeJoin(root, "..", "outside"); err == nil {
		t.Fatal("safeJoin accepted a path outside the server root")
	}
	want := filepath.Join(root, "Pal", "Saved")
	got, err := safeJoin(root, "Pal", "Saved")
	if err != nil || got != want {
		t.Fatalf("safeJoin = %q, %v; want %q", got, err, want)
	}
}

func TestInstanceDefaults(t *testing.T) {
	instance := withDefaults(ServerInstance{RootPath: `D:\servers\pal`})
	if instance.PublicPort != 8211 || instance.RCONPort != 25575 || instance.RESTPort != 8212 || instance.QueryPort != 27015 {
		t.Fatalf("unexpected default ports: %+v", instance)
	}
	if !strings.HasSuffix(instance.Executable, "PalServer.exe") {
		t.Fatalf("unexpected executable: %s", instance.Executable)
	}
}

func TestManagedInstanceUsesLauncherOwnedPaths(t *testing.T) {
	base := t.TempDir()
	instance := buildManagedInstance(base, "周末服务器")

	if instance.Name != "周末服务器" {
		t.Fatalf("name = %q", instance.Name)
	}
	if !strings.HasPrefix(instance.RootPath, filepath.Join(base, "servers")) {
		t.Fatalf("root path = %q", instance.RootPath)
	}
	if instance.SteamCMDPath != filepath.Join(base, "runtime", "steamcmd", "steamcmd.exe") {
		t.Fatalf("steamcmd path = %q", instance.SteamCMDPath)
	}
	if instance.Executable != filepath.Join(instance.RootPath, "PalServer.exe") {
		t.Fatalf("executable = %q", instance.Executable)
	}
	if len(instance.AdminPassword) < 12 {
		t.Fatalf("generated admin password is too short: %q", instance.AdminPassword)
	}
}

func TestManagedInstanceNamesUseSafeDirectoryNames(t *testing.T) {
	instance := buildManagedInstance(t.TempDir(), `帕鲁 / 测试:*?`)
	base := filepath.Base(instance.RootPath)
	if strings.ContainsAny(base, `\\/:*?"<>|`) {
		t.Fatalf("unsafe managed directory name: %q", base)
	}
	for _, character := range base {
		if character > 127 {
			t.Fatalf("SteamCMD directory is not ASCII: %q", base)
		}
	}
}

func TestManagedInstanceUsesSelectedInstallDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "PalworldServer")
	instance := buildManagedInstanceAt(t.TempDir(), "中文服名", root)
	if instance.RootPath != root || instance.Executable != filepath.Join(root, "PalServer.exe") {
		t.Fatalf("selected install directory was not used: %#v", instance)
	}
}

func TestInstallDirectoryRejectsNonASCIIPaths(t *testing.T) {
	if err := validateInstallDirectory(`D:\帕鲁服务器`); err == nil {
		t.Fatal("SteamCMD-incompatible path was accepted")
	}
	if err := validateInstallDirectory(`D:\PalworldServers\Server1`); err != nil {
		t.Fatalf("ASCII install path was rejected: %v", err)
	}
}

func TestSteamCMDErrorIncludesActionableMissingConfigurationMessage(t *testing.T) {
	err := formatSteamCMDError(errors.New("exit status 7"), []string{"ERROR! Failed to install app '2394010' (Missing configuration)"})
	if !strings.Contains(err.Error(), "AppInfo") || !strings.Contains(err.Error(), "steamcmd.log") {
		t.Fatalf("SteamCMD error is not actionable: %v", err)
	}
}

func TestSteamCMDExecutableAcceptsDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "steamcmd")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	got := steamCMDExecutable(directory)
	want := filepath.Join(directory, "steamcmd.exe")
	if got != want {
		t.Fatalf("steamCMDExecutable = %q, want %q", got, want)
	}
}

func TestEmbeddedSteamCMDBootstrapIsPinnedAndExtractable(t *testing.T) {
	if len(steamcmdBootstrap) == 0 || steamCMDArchiveHash() != steamcmdBootstrapSHA256 {
		t.Fatal("embedded SteamCMD bootstrap is missing or changed")
	}
	destination := filepath.Join(t.TempDir(), "runtime", "steamcmd", "steamcmd.exe")
	if err := ensureSteamCMD(destination, nil); err != nil {
		t.Fatalf("extract embedded SteamCMD: %v", err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("steamcmd.exe was not extracted: %v", err)
	}
}

func TestSteamCMDScriptAndProgressAreWrapped(t *testing.T) {
	script := steamCMDScript(`D:\PalworldServers\Server 1`)
	if !strings.Contains(script, "force_install_dir") || !strings.Contains(script, "app_info_update 1") || !strings.Contains(script, "app_update 2394010 validate") || !strings.Contains(script, "login anonymous") {
		t.Fatalf("unexpected SteamCMD script: %s", script)
	}
	progress := parseSteamCMDLine("Update state (0x61) downloading, progress: 42.50 (425 of 1000 KB)")
	if progress.Percent < 47 || progress.Percent > 49 || !strings.Contains(progress.Message, "42%") {
		t.Fatalf("unexpected wrapped progress: %#v", progress)
	}
	if got := parseSteamCMDLine("Success! App '2394010' fully installed"); got.Percent != 82 {
		t.Fatalf("unexpected completion progress: %#v", got)
	}
}

func TestManagedInstanceAvoidsPortsUsedByExistingServers(t *testing.T) {
	instance := buildManagedInstance(t.TempDir(), "第二个服务器")
	existing := []ServerInstance{{
		PublicPort: 8211,
		RESTPort:   8212,
		RCONPort:   25575,
		QueryPort:  27015,
	}}

	instance = assignAvailablePorts(instance, existing)
	used := map[int]bool{8211: true, 8212: true, 25575: true, 27015: true}
	for _, port := range []int{instance.PublicPort, instance.RESTPort, instance.RCONPort, instance.QueryPort} {
		if used[port] {
			t.Fatalf("assigned port %d is already in use", port)
		}
		if port < 1 || port > 65535 {
			t.Fatalf("assigned invalid port %d", port)
		}
		used[port] = true
	}
}

func TestLegacyInstanceDefaults(t *testing.T) {
	instance := withDefaults(ServerInstance{})
	if instance.IconID != "SheepBall" {
		t.Fatalf("icon id = %q, want SheepBall", instance.IconID)
	}
	if restartDelay(6) != 6*time.Hour || restartDelay(0) != 0 || restartDelay(-1) != 0 {
		t.Fatal("unexpected automatic restart delay")
	}
}

func TestDuplicateInstanceFilesCopiesServerData(t *testing.T) {
	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "copy")
	file := filepath.Join(source, "Pal", "Saved", "marker.txt")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("save-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := duplicateInstanceFiles(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "Pal", "Saved", "marker.txt"))
	if err != nil || string(data) != "save-data" {
		t.Fatalf("duplicated data = %q, %v", data, err)
	}
}

func TestWorldSettingValuesRoundTripPreservesUnknownFields(t *testing.T) {
	content := "[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"Old, Server (PVE)\",ExpRate=1.000000,bIsPvP=False,UnknownFutureSetting=42)\n"
	values := parseWorldSettingValues(content)
	if values["ServerName"] != "Old, Server (PVE)" || values["ExpRate"] != "1.000000" || values["bIsPvP"] != "False" {
		t.Fatalf("unexpected parsed values: %#v", values)
	}

	updated, err := mergeWorldSettingValues(content, map[string]string{
		"ServerName": "New Server",
		"ExpRate":    "2.5",
		"bIsPvP":     "True",
		"PublicIP":   "203.0.113.10",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`ServerName="New Server"`, "ExpRate=2.5", "bIsPvP=True", "UnknownFutureSetting=42", `PublicIP="203.0.113.10"`} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("updated settings missing %q: %s", expected, updated)
		}
	}
}

func TestWorldSettingsSupportOnePointZeroNestedAndEmptyValues(t *testing.T) {
	content := "[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(CrossplayPlatforms=(Steam,Xbox,PS5,Mac),DenyTechnologyList=,bEnableVoiceChat=False,ServerName=\"1.0 Server\")\n"
	values := parseWorldSettingValues(content)
	if values["CrossplayPlatforms"] != "(Steam,Xbox,PS5,Mac)" || values["DenyTechnologyList"] != "" || values["bEnableVoiceChat"] != "False" {
		t.Fatalf("unexpected 1.0 values: %#v", values)
	}
	updated, err := mergeWorldSettingValues(content, map[string]string{"CrossplayPlatforms": "(Steam,PS5)", "DenyTechnologyList": "TechA;TechB", "bEnableVoiceChat": "True"})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"CrossplayPlatforms=(Steam,PS5)", "DenyTechnologyList=TechA;TechB", "bEnableVoiceChat=True", `ServerName="1.0 Server"`} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("updated settings missing %q: %s", expected, updated)
		}
	}
}

func TestWorldSettingsFillNewDefaultsWithoutOverwritingExistingValues(t *testing.T) {
	values := mergeMissingWorldSettingDefaults(map[string]string{"ExpRate": "2"}, map[string]string{"ExpRate": "1", "bEnableVoiceChat": "False"})
	if values["ExpRate"] != "2" || values["bEnableVoiceChat"] != "False" {
		t.Fatalf("merged settings = %#v", values)
	}
}

func TestManagedServerSettingsPreserveOnePointZeroDefaults(t *testing.T) {
	root := t.TempDir()
	defaults := "[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"Default\",bAllowClientMod=False,CrossplayPlatforms=(Steam,Xbox,PS5,Mac),bEnableVoiceChat=False)\n"
	if err := os.WriteFile(filepath.Join(root, "DefaultPalWorldSettings.ini"), []byte(defaults), 0o600); err != nil {
		t.Fatal(err)
	}
	instance := withDefaults(ServerInstance{RootPath: root, Name: "Pal 1.0", AdminPassword: "admin", PublicPort: 8211, RCONPort: 25575, RESTPort: 8212})
	if err := writeManagedWorldSettings(instance); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, expected := range []string{`ServerName="Pal 1.0"`, "bAllowClientMod=True", "CrossplayPlatforms=(Steam,Xbox,PS5,Mac)", "RESTAPIEnabled=True"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("managed 1.0 settings missing %q: %s", expected, content)
		}
	}
}

func TestInstanceEditorSynchronizesAuthoritativeWorldSettings(t *testing.T) {
	root := t.TempDir()
	instance := withDefaults(ServerInstance{RootPath: root, Name: "Edited Server", PublicIP: "203.0.113.10", PublicPort: 9000, RCONPort: 25580, RESTPort: 8220, AdminPassword: "new-admin", ServerPassword: "join-me"})
	path, err := worldSettingsPath(instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := "[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"Old\",PublicPort=8211,RCONEnabled=False,RCONPort=25575,RESTAPIEnabled=False,RESTAPIPort=8212)\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncInstanceWorldSettings(instance); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`ServerName="Edited Server"`, "PublicPort=9000", "RCONEnabled=True", "RCONPort=25580", "RESTAPIEnabled=True", "RESTAPIPort=8220", `AdminPassword="new-admin"`} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("synchronised settings missing %q: %s", expected, data)
		}
	}
}

func TestLegacySaveAndBanDiscovery(t *testing.T) {
	root := t.TempDir()
	save := filepath.Join(root, "Pal", "Saved", "SaveGames", "0", "ABC123")
	backup := filepath.Join(save, "backup", "world", "2026.07.12-12.30.00")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := findSaveGameRoot(root)
	if err != nil || found != save {
		t.Fatalf("findSaveGameRoot = %q, %v; want %q", found, err, save)
	}
	backups, err := discoverOfficialBackups(found)
	if err != nil || len(backups) != 1 || backups[0].Name != "2026.07.12-12.30.00" {
		t.Fatalf("official backups = %#v, %v", backups, err)
	}
	bans := parseBanList("steam_1\r\n\nsteam_2\n")
	if len(bans) != 2 || bans[0] != "steam_1" || bans[1] != "steam_2" {
		t.Fatalf("ban list = %#v", bans)
	}
}

func TestClientModExportFiltersServerFiles(t *testing.T) {
	for _, name := range []string{"Pal-WindowsServer.pak", "Pal-WindowsServer.ucas", "Pal-WindowsServer.utoc", "global.ucas", "global.utoc"} {
		if shouldExportPak(name) {
			t.Fatalf("server file %q should not be exported", name)
		}
	}
	if !shouldExportPak("MyClientMod.pak") {
		t.Fatal("client mod was filtered out")
	}
	instance := ServerInstance{RootPath: t.TempDir()}
	if got := modRoots(instance)["paklogic"]; got != filepath.Join(instance.RootPath, "Pal", "Content", "Paks", "LogicMods") {
		t.Fatalf("pak logic root = %q", got)
	}
}

func TestPerformanceEngineConfigMatchesSelectedMode(t *testing.T) {
	if !strings.Contains(string(performanceEngineConfig(true)), "NetServerMaxTickRate=120") {
		t.Fatal("optimized Engine.ini is missing network tuning")
	}
	if strings.Contains(string(performanceEngineConfig(false)), "NetServerMaxTickRate=120") {
		t.Fatal("standard Engine.ini unexpectedly contains optimization tuning")
	}
}

func TestMemoryUsagePercent(t *testing.T) {
	if got := memoryUsagePercent(16000, 4000); got != 75 {
		t.Fatalf("memory usage = %v, want 75", got)
	}
	if got := memoryUsagePercent(0, 0); got != 0 {
		t.Fatalf("zero memory usage = %v, want 0", got)
	}
}

func TestNextMaintenanceRunSupportsIntervalsAndDailyTimes(t *testing.T) {
	now := time.Date(2026, 7, 12, 14, 30, 0, 0, time.Local)
	interval := MaintenanceTask{Schedule: "interval", IntervalMinutes: 90}
	if got := nextMaintenanceRun(interval, now); !got.Equal(now.Add(90 * time.Minute)) {
		t.Fatalf("interval next run = %v", got)
	}
	daily := MaintenanceTask{Schedule: "daily", DailyTime: "03:15"}
	want := time.Date(2026, 7, 13, 3, 15, 0, 0, time.Local)
	if got := nextMaintenanceRun(daily, now); !got.Equal(want) {
		t.Fatalf("daily next run = %v, want %v", got, want)
	}
}

func TestMaintenanceOperationIsExclusivePerServer(t *testing.T) {
	app := &App{operations: map[string]string{}}
	if !app.tryBeginOperation("server-1", "backup") {
		t.Fatal("first operation was rejected")
	}
	if app.tryBeginOperation("server-1", "update") {
		t.Fatal("overlapping operation was accepted")
	}
	app.endOperation("server-1")
	if !app.tryBeginOperation("server-1", "update") {
		t.Fatal("operation remained locked after completion")
	}
}

func TestTieredBackupRetentionKeepsRecentDailyAndMonthlyRestorePoints(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.Local)
	entry := func(name string, age time.Duration) BackupEntry {
		return BackupEntry{Name: name, CreatedAt: now.Add(-age).UnixMilli()}
	}
	entries := []BackupEntry{
		entry("recent-a", 2*time.Hour), entry("recent-b", 6*24*time.Hour),
		entry("day-new", 10*24*time.Hour), entry("day-old", 10*24*time.Hour+time.Hour),
		entry("month-new", 45*24*time.Hour), entry("month-old", 50*24*time.Hour),
	}
	deleted := selectBackupsToDelete(entries, BackupRetentionPolicy{Mode: "tiered"}, now)
	deletedNames := map[string]bool{}
	for _, item := range deleted {
		deletedNames[item.Name] = true
	}
	if !deletedNames["day-old"] || !deletedNames["month-old"] {
		t.Fatalf("tiered retention deleted %#v", deletedNames)
	}
	if deletedNames["recent-a"] || deletedNames["recent-b"] || deletedNames["day-new"] || deletedNames["month-new"] {
		t.Fatalf("tiered retention removed required restore points: %#v", deletedNames)
	}
}

func TestSteamBuildIDParsers(t *testing.T) {
	acf := `"AppState" { "appid" "2394010" "buildid" "19004567" }`
	if got := parseACFBuildID(acf); got != "19004567" {
		t.Fatalf("local build id = %q", got)
	}
	remote := []byte(`{"data":{"2394010":{"depots":{"branches":{"public":{"buildid":"19009999"}}}}}}`)
	if got, err := parseRemoteBuildID(remote, "public"); err != nil || got != "19009999" {
		t.Fatalf("remote build id = %q, %v", got, err)
	}
	numeric := []byte(`{"data":{"2394010":{"depots":{"branches":{"public":{"buildid":19009999}}}}}}`)
	if got, err := parseRemoteBuildID(numeric, "public"); err != nil || got != "19009999" {
		t.Fatalf("numeric remote build id = %q, %v", got, err)
	}
}

func TestMissingSaveDirectoryHasStableSentinel(t *testing.T) {
	if !errors.Is(missingSaveDirectoryError(), ErrSaveDirectoryNotFound) {
		t.Fatal("missing save directory error cannot be recognized")
	}
}

func TestGuardianUsesFailureThresholdAndRestartBudget(t *testing.T) {
	if guardianFailureReached(2, 3) {
		t.Fatal("guardian restarted before reaching failure threshold")
	}
	if !guardianFailureReached(3, 3) {
		t.Fatal("guardian did not restart at failure threshold")
	}
	now := time.Date(2026, 7, 12, 15, 0, 0, 0, time.Local)
	attempts := []time.Time{now.Add(-50 * time.Minute), now.Add(-20 * time.Minute), now.Add(-5 * time.Minute)}
	if guardianRestartAllowed(attempts, now, 3, time.Hour) {
		t.Fatal("guardian exceeded restart budget")
	}
	if !guardianRestartAllowed(attempts, now.Add(2*time.Hour), 3, time.Hour) {
		t.Fatal("guardian did not recover after backoff window")
	}
}

func TestPlayerHistoryMergesAliasesAndCountsNewSessions(t *testing.T) {
	now := time.Date(2026, 7, 12, 16, 0, 0, 0, time.Local)
	entries := mergePlayerHistory(nil, []Player{{Name: "Alice", UserID: "steam-1", PlayerID: "pal-1", IP: "10.0.0.1"}}, now)
	if len(entries) != 1 || entries[0].Visits != 1 || len(entries[0].Aliases) != 1 {
		t.Fatalf("first player snapshot = %#v", entries)
	}
	entries = mergePlayerHistory(entries, []Player{{Name: "Alice2", UserID: "steam-1", PlayerID: "pal-1", IP: "10.0.0.2"}}, now.Add(time.Minute))
	if entries[0].Visits != 1 || len(entries[0].Aliases) != 2 || entries[0].LastIP != "10.0.0.2" {
		t.Fatalf("online alias merge = %#v", entries[0])
	}
	entries = mergePlayerHistory(entries, nil, now.Add(2*time.Minute))
	entries = mergePlayerHistory(entries, []Player{{Name: "Alice2", UserID: "steam-1", PlayerID: "pal-1"}}, now.Add(3*time.Minute))
	if entries[0].Visits != 2 {
		t.Fatalf("new session visits = %d", entries[0].Visits)
	}
}

func TestWhitelistDecisionRequiresEnforcementAndKnownApproval(t *testing.T) {
	entry := PlayerHistoryEntry{UserID: "steam-1", Whitelisted: true}
	if shouldKickForWhitelist(false, entry) || shouldKickForWhitelist(true, entry) {
		t.Fatal("approved player was selected for kick")
	}
	entry.Whitelisted = false
	if !shouldKickForWhitelist(true, entry) {
		t.Fatal("unapproved player was not selected for kick")
	}
}

func TestGamePresetsAndTemporaryEventExpiry(t *testing.T) {
	preset, ok := gamePresetByID("hardcore")
	if !ok || preset.Values["DeathPenalty"] == "None" || preset.Values["ExpRate"] == "" {
		t.Fatalf("hardcore preset = %#v", preset)
	}
	now := time.Date(2026, 7, 12, 17, 0, 0, 0, time.Local)
	if eventExpired(ActiveGameEvent{EndsAt: now.Add(time.Minute).UnixMilli()}, now) {
		t.Fatal("active event expired early")
	}
	if !eventExpired(ActiveGameEvent{EndsAt: now.Add(-time.Second).UnixMilli()}, now) {
		t.Fatal("expired event remained active")
	}
}

func TestDiscordWebhookValidationRedactionAndEncryption(t *testing.T) {
	webhook := "https://discord.com/api/webhooks/123456/secret-token"
	if err := validateDiscordWebhook(webhook); err != nil {
		t.Fatalf("valid webhook rejected: %v", err)
	}
	if validateDiscordWebhook("https://example.com/api/webhooks/1/x") == nil {
		t.Fatal("non-Discord webhook accepted")
	}
	if redacted := redactDiscordWebhook(webhook); strings.Contains(redacted, "secret-token") {
		t.Fatalf("webhook token leaked: %s", redacted)
	}
	ciphertext, err := protectSecret(webhook)
	if err != nil {
		t.Fatalf("protect secret: %v", err)
	}
	plaintext, err := unprotectSecret(ciphertext)
	if err != nil || plaintext != webhook {
		t.Fatalf("DPAPI round trip = %q, %v", plaintext, err)
	}
	if !discordEventEnabled([]string{"backup", "update"}, "backup") || discordEventEnabled([]string{"backup"}, "player-join") {
		t.Fatal("Discord event routing is incorrect")
	}
}

func TestPlayerPresenceTransitions(t *testing.T) {
	before := []PlayerHistoryEntry{{UserID: "a", Online: true}, {UserID: "b", Online: false}}
	after := []PlayerHistoryEntry{{UserID: "a", Online: false}, {UserID: "b", Online: true}}
	joined, left := playerPresenceTransitions(before, after)
	if len(joined) != 1 || joined[0].UserID != "b" || len(left) != 1 || left[0].UserID != "a" {
		t.Fatalf("presence transitions joined=%#v left=%#v", joined, left)
	}
}

func TestSaveInspectorAssetSelectionAndChecksum(t *testing.T) {
	release := githubRelease{TagName: "v1", Assets: []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Digest             string `json:"digest"`
		Size               int64  `json:"size"`
	}{{Name: "pst_v1_windows_x86_64.zip", BrowserDownloadURL: "https://example.test/pst.zip", Digest: "sha256:abc"}}}
	asset, err := selectSaveInspectorAsset(release)
	if err != nil || asset.Name != "pst_v1_windows_x86_64.zip" {
		t.Fatalf("save inspector asset = %#v, %v", asset, err)
	}
	if err := verifySHA256([]byte("palworld"), "sha256:deadbeef"); err == nil {
		t.Fatal("invalid sidecar checksum accepted")
	}
}

func TestPowerShellCommandsUseHiddenWindow(t *testing.T) {
	command := newHiddenPowerShell("Write-Output ok")
	settings := command.SysProcAttr
	if settings == nil || settings.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatal("PowerShell helper does not suppress child windows")
	}
}

func TestServerProcessUsesHiddenConsole(t *testing.T) {
	settings := hiddenServerSysProcAttr()
	if settings == nil || settings.CreationFlags&windows.CREATE_NO_WINDOW == 0 || !settings.HideWindow {
		t.Fatal("PalServer process is not configured as a hidden process")
	}
}

func TestServerLaunchExecutablePrefersNonConsoleShippingBinary(t *testing.T) {
	root := t.TempDir()
	shipping := filepath.Join(root, "Pal", "Binaries", "Win64", "PalServer-Win64-Shipping.exe")
	if err := os.MkdirAll(filepath.Dir(shipping), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shipping, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	instance := ServerInstance{RootPath: root, Executable: filepath.Join(root, "PalServer.exe")}
	if got := serverLaunchExecutable(instance); got != shipping {
		t.Fatalf("launch executable = %q, want %q", got, shipping)
	}
}

func TestModClassificationSeparatesUE4SSAndContentMods(t *testing.T) {
	origin, _, system := classifyMod("lua", "ConsoleEnablerMod")
	if origin != "ue4ss-system" || !system {
		t.Fatalf("UE4SS built-in classification = %q, %v", origin, system)
	}
	origin, _, system = classifyMod("paklogic", "Example.pak")
	if origin != "logicmods" || system {
		t.Fatalf("LogicMods classification = %q, %v", origin, system)
	}
}

func TestDiscordSettingsUseEmptyArrayInsteadOfNull(t *testing.T) {
	if events := nonNilStrings(nil); events == nil || len(events) != 0 {
		t.Fatalf("empty Discord events = %#v", events)
	}
}

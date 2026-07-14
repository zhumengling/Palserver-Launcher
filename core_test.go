package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestRCONSuccessfulCommandWithoutResponseDoesNotSurfaceTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port, err := strconv.Atoi(strings.TrimPrefix(listener.Addr().String(), "127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}
	commandReceived := make(chan string, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _, _, _ = readRCONPacket(conn)
		_, _ = conn.Write(rconPacket(1, 2, ""))
		_, _, command, readErr := readRCONPacket(conn)
		if readErr == nil {
			commandReceived <- command
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		buffer := make([]byte, 1)
		_, _ = conn.Read(buffer)
	}()

	response, err := sendRCONWithTimeout(ServerInstance{RCONPort: port, AdminPassword: "secret"}, "Info", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("successful fire-and-forget command returned an error: %v", err)
	}
	if response != "" {
		t.Fatalf("response = %q, want empty response", response)
	}
	select {
	case command := <-commandReceived:
		if command != "Info" {
			t.Fatalf("command = %q, want Info", command)
		}
	case <-time.After(time.Second):
		t.Fatal("RCON command was not received")
	}
}

func TestBuildPlayerActionCommandsSupportsOnePointZeroRewards(t *testing.T) {
	tests := []struct {
		name    string
		request ActionRequest
		want    []string
	}{
		{
			name:    "unallocated stat points",
			request: ActionRequest{Action: "stats", UserID: "steam_76561190000000000", Amount: 4},
			want:    []string{"givestats steam_76561190000000000 4"},
		},
		{
			name:    "unlock all technology",
			request: ActionRequest{Action: "learntech", UserID: "steam_76561190000000000", Value: "all"},
			want:    []string{"learntech steam_76561190000000000 all"},
		},
		{
			name:    "pal egg with selected pal and level",
			request: ActionRequest{Action: "egg", UserID: "steam_76561190000000000", Value: "PalEgg_Dragon_05", Extra: "GoldenHorse", Amount: 50},
			want:    []string{"giveegg steam_76561190000000000 PalEgg_Dragon_05 GoldenHorse 50"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := buildPlayerActionCommands(test.request)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n") != strings.Join(test.want, "\n") {
				t.Fatalf("commands = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestBuildPlayerActionCommandsRejectsCommandInjection(t *testing.T) {
	tests := []ActionRequest{
		{Action: "item", UserID: "steam_1\nShutdown 0", Value: "Wood", Amount: 1},
		{Action: "item", UserID: "steam_1", Value: "Wood;Shutdown 0", Amount: 1},
		{Action: "learntech", UserID: "steam_1", Value: "all Shutdown 0"},
		{Action: "egg", UserID: "steam_1", Value: "PalEgg_Normal_01", Extra: "SheepBall\r\nShutdown 0", Amount: 1},
	}
	for _, request := range tests {
		if commands, err := buildPlayerActionCommands(request); err == nil {
			t.Fatalf("unsafe request was accepted with commands %#v: %#v", commands, request)
		}
	}
}

func TestFrontendPlayerRewardsAndWorldMapAreExposed(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("frontend", "src", "App.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, expected := range []string{
		`<option value="stats">属性点</option>`,
		`<option value="egg">帕鲁蛋</option>`,
		`<option value="learntech">解锁科技</option>`,
		`/map/palworld-world-map.webp`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("frontend is missing %q", expected)
		}
	}
}

func TestOpenServerDirectoryUsesTheSharedActionErrorHandler(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("frontend", "src", "App.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `onRun('open-server-path', () => API.OpenPath(instance.rootPath), '服务器目录已打开')`) {
		t.Fatal("opening the server directory bypasses the shared action error handler")
	}
}

func TestRCONProbeStopsAfterAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port, err := strconv.Atoi(strings.TrimPrefix(listener.Addr().String(), "127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}
	extraPacket := make(chan bool, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _, _, _ = readRCONPacket(conn)
		_, _ = conn.Write(rconPacket(1, 2, ""))
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		buffer := make([]byte, 1)
		n, _ := conn.Read(buffer)
		extraPacket <- n > 0
	}()

	if err := probeRCONWithTimeout(ServerInstance{RCONPort: port, AdminPassword: "secret"}, 100*time.Millisecond); err != nil {
		t.Fatalf("authenticated RCON probe failed: %v", err)
	}
	select {
	case gotExtraPacket := <-extraPacket:
		if gotExtraPacket {
			t.Fatal("RCON health probe sent a command after authentication")
		}
	case <-time.After(time.Second):
		t.Fatal("RCON probe connection did not close")
	}
}

func TestRCONAvailabilityUsesTheServerListenerWithoutSendingCommands(t *testing.T) {
	if !rconListenerMatchesProcess(true, 42, 42) {
		t.Fatal("matching RCON listener was not reported as available")
	}
	if rconListenerMatchesProcess(true, 42, 99) {
		t.Fatal("listener owned by another process was reported as available")
	}
	if rconListenerMatchesProcess(false, 42, 42) {
		t.Fatal("stopped server was reported as RCON available")
	}
}

func TestServerProcessPatternDoesNotMatchSiblingDirectoryPrefixes(t *testing.T) {
	pattern := serverProcessRootPattern(filepath.Join("D:\\PalworldServers\\Server1", "PalServer.exe"))
	if !strings.HasSuffix(pattern, "Server1\\*") {
		t.Fatalf("server process pattern = %q, want a directory-bound wildcard", pattern)
	}
	if matched, err := filepath.Match(pattern, filepath.Join("D:\\PalworldServers\\Server10", "PalServer.exe")); err != nil || matched {
		t.Fatalf("sibling server directory matched pattern %q: matched=%v err=%v", pattern, matched, err)
	}
}

func TestPalServerWrapperIsTrackedThroughItsChildProcess(t *testing.T) {
	if !usesPalServerWrapper(`D:\PalworldServers\Server1\PalServer.exe`) {
		t.Fatal("PalServer.exe launcher fallback was not recognized as a child-process launcher")
	}
	if usesPalServerWrapper(`D:\PalworldServers\Server1\Pal\Binaries\Win64\PalServer-Win64-Shipping.exe`) {
		t.Fatal("Shipping binary was incorrectly treated as the launcher wrapper")
	}
}

func TestFrontendStatusPollingDoesNotOverlapSlowStatusRequests(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("frontend", "src", "App.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if strings.Contains(source, "window.setInterval(() => refreshStatuses(), 3000)") {
		t.Fatal("status polling starts a new refresh every three seconds even when the previous request is still running")
	}
	for _, expected := range []string{"await refreshStatuses()", "window.setTimeout(poll, 3000)"} {
		if !strings.Contains(source, expected) {
			t.Fatalf("serial status polling is missing %q", expected)
		}
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

func TestDirectXRuntimeCheckIdentifiesMissingLegacyDependencies(t *testing.T) {
	present := map[string]bool{
		"d3dcompiler_43.dll": true,
		"d3dx9_43.dll":       true,
		"xinput1_3.dll":      true,
	}
	missing := missingDirectXRuntimeFiles(`C:\Windows\System32`, func(path string) bool {
		return present[strings.ToLower(filepath.Base(path))]
	})
	if strings.Join(missing, ",") != "xaudio2_7.dll" {
		t.Fatalf("missing DirectX files = %#v, want xaudio2_7.dll", missing)
	}
}

func TestDirectXRepairSourceRequiresExecutableAndDataDirectory(t *testing.T) {
	root := t.TempDir()
	if _, err := directXRepairExecutable(root); err == nil {
		t.Fatal("repair source without executable and Data directory was accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "Data"), 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "DirectX Repair.exe")
	if err := os.WriteFile(executable, []byte("repair"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := directXRepairExecutable(root)
	if err != nil || got != executable {
		t.Fatalf("repair executable = %q, %v; want %q", got, err, executable)
	}
}

func TestDirectXRepairDoesNotEmbedAUserSpecificPath(t *testing.T) {
	data, err := os.ReadFile("directx.go")
	if err != nil {
		t.Fatal(err)
	}
	privatePath := `C:\Users\` + "Ad" + "min"
	if strings.Contains(string(data), privatePath) {
		t.Fatal("DirectX repair source embeds a user-specific Windows path")
	}
}

func TestServerStartupFailureExplainsEarlyExit(t *testing.T) {
	err := serverStartupFailure(errors.New("exit status 3221225781"), "")
	if !strings.Contains(err.Error(), "启动后立即退出") || !strings.Contains(err.Error(), "DirectX") {
		t.Fatalf("startup error is not actionable: %v", err)
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

func TestServerInstancePortsMustBeUniqueAcrossInstances(t *testing.T) {
	existing := []ServerInstance{{ID: "server-a", Name: "一号服", PublicPort: 8211, QueryPort: 27015, RCONPort: 25575, RESTPort: 8212}}
	candidate := ServerInstance{ID: "server-b", Name: "二号服", PublicPort: 8211, QueryPort: 27016, RCONPort: 25576, RESTPort: 8213}
	if err := validateServerInstancePorts(candidate, existing); err == nil || !strings.Contains(err.Error(), "8211") {
		t.Fatalf("duplicate server port was accepted: %v", err)
	}
	candidate.PublicPort = 8214
	if err := validateServerInstancePorts(candidate, existing); err != nil {
		t.Fatalf("unique server ports were rejected: %v", err)
	}
	self := existing[0]
	if err := validateServerInstancePorts(self, existing); err != nil {
		t.Fatalf("editing an existing instance conflicted with itself: %v", err)
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

func TestClaimBackupDestinationCreatesDistinctDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "backups", "server-a")
	now := time.Date(2026, 7, 14, 10, 30, 45, 123000000, time.Local)
	first, err := claimBackupDestination(root, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := claimBackupDestination(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || filepath.Base(second) != filepath.Base(first)+"-2" {
		t.Fatalf("claimed backup destinations = %q, %q", first, second)
	}
}

func TestClaimBackupDestinationIsSafeForConcurrentBackups(t *testing.T) {
	root := filepath.Join(t.TempDir(), "backups", "server-a")
	now := time.Date(2026, 7, 14, 10, 30, 45, 123000000, time.Local)
	paths := make(chan string, 8)
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			path, err := claimBackupDestination(root, now)
			if err != nil {
				errs <- err
				return
			}
			paths <- path
		}()
	}
	seen := map[string]bool{}
	for range 8 {
		select {
		case err := <-errs:
			t.Fatal(err)
		case path := <-paths:
			if seen[path] {
				t.Fatalf("backup destination was claimed twice: %q", path)
			}
			seen[path] = true
		}
	}
}

func TestRestoreBackupPreservesExistingSaveWhenStagingCopyFails(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "SaveGames")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(destination, "original.sav")
	if err := os.WriteFile(original, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackupTree(filepath.Join(root, "missing-backup"), destination); err == nil {
		t.Fatal("restore accepted a missing backup")
	}
	data, err := os.ReadFile(original)
	if err != nil || string(data) != "keep" {
		t.Fatalf("existing save was changed after failed restore: %q, %v", data, err)
	}
}

func TestRestoreBackupReplacesSaveOnlyAfterStagingCompletes(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, "backup")
	destination := filepath.Join(root, "SaveGames")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "new.sav"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "old.sav"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackupTree(backup, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "old.sav")); !os.IsNotExist(err) {
		t.Fatalf("old save remained after restore: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "new.sav"))
	if err != nil || string(data) != "new" {
		t.Fatalf("restored save = %q, %v", data, err)
	}
}

type failingBackupDirEntry struct{}

func (failingBackupDirEntry) Name() string               { return "partial-backup" }
func (failingBackupDirEntry) IsDir() bool                { return true }
func (failingBackupDirEntry) Type() os.FileMode          { return os.ModeDir }
func (failingBackupDirEntry) Info() (os.FileInfo, error) { return nil, errors.New("entry disappeared") }

func TestBackupListingSkipsDirectoryEntriesThatDisappearDuringScan(t *testing.T) {
	if _, ok := backupEntryFromDirEntry(t.TempDir(), failingBackupDirEntry{}); ok {
		t.Fatal("backup listing accepted an entry whose metadata could not be read")
	}
}

func TestDirEntryInfoRejectsEntriesThatDisappearDuringScan(t *testing.T) {
	if _, ok := readableDirEntryInfo(failingBackupDirEntry{}); ok {
		t.Fatal("disappeared directory entry returned file metadata")
	}
}

func TestReleaseDownloadTemporaryArchiveUsesDestinationVolume(t *testing.T) {
	destination := t.TempDir()
	archive, err := createReleaseTempArchive(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if filepath.Dir(archive.Name()) != destination {
		t.Fatalf("temporary archive directory = %q, want %q", filepath.Dir(archive.Name()), destination)
	}
}

func TestReleaseDownloadClientHasFiniteTimeout(t *testing.T) {
	if client := releaseDownloadClient(); client.Timeout < time.Minute {
		t.Fatalf("release download timeout = %s, want at least one minute", client.Timeout)
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

func TestRunningWorldSettingsChangeWarnsOneMinuteBeforeRestart(t *testing.T) {
	steps := []string{}
	var waited time.Duration
	err := executeWorldSettingsChange(
		true,
		func() error { steps = append(steps, "announce"); return nil },
		func(delay time.Duration) { waited = delay; steps = append(steps, "wait") },
		func() error { steps = append(steps, "stop"); return nil },
		func() error { steps = append(steps, "write"); return nil },
		func() error { steps = append(steps, "start"); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if waited != time.Minute {
		t.Fatalf("warning delay = %s, want 1m", waited)
	}
	if got := strings.Join(steps, ","); got != "announce,wait,stop,write,start" {
		t.Fatalf("settings change order = %s", got)
	}
}

func TestWorldSettingsChangeAbortsWhenRestartWarningCannotBeSent(t *testing.T) {
	steps := []string{}
	err := executeWorldSettingsChange(
		true,
		func() error { steps = append(steps, "announce"); return errors.New("offline") },
		func(time.Duration) { steps = append(steps, "wait") },
		func() error { steps = append(steps, "stop"); return nil },
		func() error { steps = append(steps, "write"); return nil },
		func() error { steps = append(steps, "start"); return nil },
	)
	if err == nil {
		t.Fatal("settings change continued without sending the restart warning")
	}
	if got := strings.Join(steps, ","); got != "announce" {
		t.Fatalf("settings change continued after announcement failure: %s", got)
	}
}

func TestStoppedWorldSettingsChangeWritesWithoutRestartDelay(t *testing.T) {
	steps := []string{}
	err := executeWorldSettingsChange(
		false,
		func() error { steps = append(steps, "announce"); return nil },
		func(time.Duration) { steps = append(steps, "wait") },
		func() error { steps = append(steps, "stop"); return nil },
		func() error { steps = append(steps, "write"); return nil },
		func() error { steps = append(steps, "start"); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(steps, ","); got != "write" {
		t.Fatalf("stopped-server settings change order = %s", got)
	}
}

func TestGameEventLifecycleActivatesPendingEventAfterServerStarts(t *testing.T) {
	now := time.Date(2026, 7, 13, 20, 0, 0, 0, time.Local)
	pending := ActiveGameEvent{State: "pending-start", DurationMinutes: 120}
	active := activateGameEvent(pending, now)
	if active.State != "active" || active.StartedAt != now.UnixMilli() || active.EndsAt != now.Add(2*time.Hour).UnixMilli() {
		t.Fatalf("activated event = %#v", active)
	}
	legacy := ActiveGameEvent{StartedAt: now.UnixMilli(), EndsAt: now.Add(time.Hour).UnixMilli()}
	if normalizedGameEventState(legacy) != "active" {
		t.Fatal("legacy active event state was not preserved")
	}
}

func TestFRPConfigBuildsUDPGameTunnelAndOptionalManagementTunnels(t *testing.T) {
	instance := ServerInstance{PublicPort: 8211, QueryPort: 27015, RCONPort: 25575, RESTPort: 8212}
	settings := FrpSettings{
		ServerAddress: "frps.example.com", ServerPort: 7000, ProxyName: "pal-main", RemoteGamePort: 8211,
		QueryEnabled: true, RemoteQueryPort: 27015, RCONEnabled: true, RemoteRCONPort: 25575,
	}
	config, err := buildFrpcConfig(instance, settings, "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`serverAddr = "frps.example.com"`, `serverPort = 7000`, `auth.token = "secret-token"`,
		`name = "pal-main-game"`, `type = "udp"`, `localPort = 8211`, `remotePort = 8211`,
		`name = "pal-main-query"`, `localPort = 27015`, `remotePort = 27015`,
		`name = "pal-main-rcon"`, `type = "tcp"`, `localPort = 25575`, `remotePort = 25575`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("FRP config missing %q:\n%s", expected, config)
		}
	}
	if strings.Contains(config, "-rest\"") {
		t.Fatalf("disabled REST tunnel was emitted:\n%s", config)
	}
}

func TestFRPSettingsRejectUnsafeOrConflictingValues(t *testing.T) {
	valid := FrpSettings{ServerAddress: "frp.example.com", ServerPort: 7000, ProxyName: "pal-main", RemoteGamePort: 8211}
	if err := validateFrpSettings(valid); err != nil {
		t.Fatalf("valid FRP settings rejected: %v", err)
	}
	invalidName := valid
	invalidName.ProxyName = "pal main; rm"
	if validateFrpSettings(invalidName) == nil {
		t.Fatal("unsafe FRP proxy name was accepted")
	}
	conflict := valid
	conflict.QueryEnabled = true
	conflict.RemoteQueryPort = conflict.RemoteGamePort
	if validateFrpSettings(conflict) == nil {
		t.Fatal("conflicting UDP remote ports were accepted")
	}
}

func TestFRPRuntimeRejectsOnlyActiveRemotePortConflictsOnTheSameFRPS(t *testing.T) {
	running := []frpRuntimeClaim{{
		ServerID: "server-a", ServerName: "一号服",
		Settings: FrpSettings{ServerAddress: "frps.example.com", ServerPort: 7000, ProxyName: "pal-a", RemoteGamePort: 8211, RCONEnabled: true, RemoteRCONPort: 25575},
	}}
	candidate := frpRuntimeClaim{
		ServerID: "server-b", ServerName: "二号服",
		Settings: FrpSettings{ServerAddress: "FRPS.EXAMPLE.COM", ServerPort: 7000, ProxyName: "pal-b", RemoteGamePort: 8211},
	}
	if err := validateFrpRuntimeClaim(candidate, running); err == nil || !strings.Contains(err.Error(), "8211/UDP") {
		t.Fatalf("active UDP conflict was accepted: %v", err)
	}
	candidate.Settings.RemoteGamePort = 25575
	if err := validateFrpRuntimeClaim(candidate, running); err != nil {
		t.Fatalf("same numeric port with a different protocol was rejected: %v", err)
	}
	candidate.Settings.RemoteGamePort = 8211
	candidate.Settings.ServerAddress = "other.example.com"
	if err := validateFrpRuntimeClaim(candidate, running); err != nil {
		t.Fatalf("same remote port on another FRPS was rejected: %v", err)
	}
	if err := validateFrpRuntimeClaim(candidate, nil); err != nil {
		t.Fatalf("saved but stopped FRP configuration blocked startup: %v", err)
	}
}

func TestFRPRuntimeRejectsDuplicateProxyNamesOnTheSameFRPS(t *testing.T) {
	running := []frpRuntimeClaim{{ServerID: "server-a", ServerName: "一号服", Settings: FrpSettings{ServerAddress: "frps.example.com", ServerPort: 7000, ProxyName: "pal-main", RemoteGamePort: 8211}}}
	candidate := frpRuntimeClaim{ServerID: "server-b", ServerName: "二号服", Settings: FrpSettings{ServerAddress: "frps.example.com", ServerPort: 7000, ProxyName: "pal-main", RemoteGamePort: 8212}}
	if err := validateFrpRuntimeClaim(candidate, running); err == nil || !strings.Contains(err.Error(), "代理名称") {
		t.Fatalf("duplicate proxy name was accepted: %v", err)
	}
}

func TestFRPReleaseSelectionUsesWindowsAMD64Archive(t *testing.T) {
	release := githubRelease{TagName: "v0.70.0", Assets: []githubReleaseAsset{
		{Name: "frp_0.70.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.test/linux"},
		{Name: "frp_0.70.0_windows_amd64.zip", BrowserDownloadURL: "https://example.test/windows", Digest: "sha256:abc"},
	}}
	name, url, digest, err := selectFRPReleaseAsset(release)
	if err != nil || name != "frp_0.70.0_windows_amd64.zip" || url != "https://example.test/windows" || digest != "sha256:abc" {
		t.Fatalf("selected FRP asset = %q, %q, %q, %v", name, url, digest, err)
	}
}

func TestFRPInstallerDownloadsLatestWindowsClient(t *testing.T) {
	if os.Getenv("PALSERVER_FRP_INTEGRATION") != "1" {
		t.Skip("set PALSERVER_FRP_INTEGRATION=1 to verify the live FRP release")
	}
	status, err := (&App{}).InstallFrp()
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(status.Path, "-v").CombinedOutput()
	if err != nil {
		t.Fatalf("frpc -v: %v: %s", err, output)
	}
	if !status.Installed || status.Version == "" || !strings.Contains(string(output), strings.TrimPrefix(status.Version, "v")) {
		t.Fatalf("installed FRP status = %#v, version output = %q", status, output)
	}
	config, err := buildFrpcConfig(
		ServerInstance{PublicPort: 8211, QueryPort: 27015, RCONPort: 25575, RESTPort: 8212},
		FrpSettings{ServerAddress: "127.0.0.1", ServerPort: 7000, ProxyName: "pal-test", RemoteGamePort: 8211, QueryEnabled: true, RemoteQueryPort: 27015},
		"test-token",
	)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "frpc.toml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(status.Path, "verify", "-c", configPath).CombinedOutput(); err != nil {
		t.Fatalf("frpc config verification: %v: %s", err, output)
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
	release := githubRelease{TagName: "v1", Assets: []githubReleaseAsset{{Name: "pst_v1_windows_x86_64.zip", BrowserDownloadURL: "https://example.test/pst.zip", Digest: "sha256:abc"}}}
	asset, err := selectSaveInspectorAsset(release)
	if err != nil || asset.Name != "pst_v1_windows_x86_64.zip" {
		t.Fatalf("save inspector asset = %#v, %v", asset, err)
	}
	if err := verifySHA256([]byte("palworld"), "sha256:deadbeef"); err == nil {
		t.Fatal("invalid sidecar checksum accepted")
	}
}

func TestExecutableExtractionReplacesAnExistingVersion(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("frp_0.70.0_windows_amd64/frpc.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("new-version")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "frp.zip")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	executable := filepath.Join(destination, "frpc.exe")
	if err := os.WriteFile(executable, []byte("old-version"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := extractNamedExecutable(archivePath, destination, "frpc.exe"); err != nil {
		t.Fatalf("replace existing executable: %v", err)
	}
	data, err := os.ReadFile(executable)
	if err != nil || string(data) != "new-version" {
		t.Fatalf("extracted executable = %q, %v", data, err)
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

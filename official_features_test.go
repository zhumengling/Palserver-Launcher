package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOfficialRESTTypedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		username, password, authenticated := request.BasicAuth()
		if !authenticated || username != "admin" || password != "secret" {
			t.Fatalf("Basic auth = %q/%q/%v", username, password, authenticated)
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/api/info":
			_, _ = w.Write([]byte(`{"version":"v1.0.0","servername":"Official","description":"Test world","worldguid":"WORLD-1"}`))
		case "/v1/api/metrics":
			_, _ = w.Write([]byte(`{"serverfps":57,"currentplayernum":2,"serverframetime":16.7,"maxplayernum":32,"uptime":3600,"basecampnum":4,"days":12}`))
		case "/v1/api/players":
			_, _ = w.Write([]byte(`{"players":[{"name":"PalUser","accountName":"paluser","playerId":"PLAYER-1","userId":"steam_1","ip":"127.0.0.1","ping":3.14,"location_x":123.45,"location_y":67.89,"level":42,"building_count":119}]}`))
		case "/v1/api/settings":
			_, _ = w.Write([]byte(`{"ServerName":"Official","ServerPlayerMaxNum":32,"bIsPvP":true}`))
		case "/v1/api/game-data":
			_, _ = w.Write([]byte(`{"Time":"2026-06-17 13:00:40","FPS":91.71,"AverageFPS":33.78,"ActorData":[{"Type":"Character","InstanceID":"CHAR-1","NickName":"Lamball","level":5,"HP":100,"MaxHP":120,"LocationX":10,"LocationY":20,"LocationZ":30},{"Type":"PalBox","InstanceID":"BOX-1","GuildName":"Builders","LocationX":40,"LocationY":50,"LocationZ":60}]}`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	instance := ServerInstance{RESTPort: port, AdminPassword: "secret"}

	info, err := getOfficialServerInfo(instance)
	if err != nil || info.ServerName != "Official" || info.WorldGUID != "WORLD-1" {
		t.Fatalf("server info = %#v, %v", info, err)
	}
	metrics, err := getOfficialServerMetrics(instance)
	if err != nil || metrics.ServerFPS != 57 || metrics.BaseCampNum != 4 || metrics.Days != 12 {
		t.Fatalf("metrics = %#v, %v", metrics, err)
	}
	players, err := getOfficialPlayers(instance)
	if err != nil || len(players) != 1 || players[0].AccountName != "paluser" || players[0].BuildingCount != 119 {
		t.Fatalf("players = %#v, %v", players, err)
	}
	settings, err := getOfficialServerSettings(instance)
	if err != nil || settings.Values["ServerPlayerMaxNum"] != "32" || settings.Values["bIsPvP"] != "True" {
		t.Fatalf("settings = %#v, %v", settings, err)
	}
	snapshot, err := getOfficialWorldSnapshot(instance)
	if err != nil || !snapshot.Available || len(snapshot.ActorData) != 2 || snapshot.ActorData[0].Type != "Character" || snapshot.ActorData[1].Type != "PalBox" {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
}

func TestOfficialRESTRecoversMissingAdminPasswordFromWorldSettings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		username, password, authenticated := request.BasicAuth()
		if !authenticated || username != "admin" || password != "world-settings-secret" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"version":"v1.0.0"}`))
	}))
	defer server.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	instance := ServerInstance{RootPath: t.TempDir(), RESTPort: port}
	settingsPath, err := worldSettingsPath(instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(AdminPassword="world-settings-secret",ServerPassword="")`
	if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := getOfficialServerInfo(instance); err != nil {
		t.Fatalf("REST fallback authentication failed: %v", err)
	}
}

func TestOfficialWorldSnapshotTreatsDisabledGameDataAPIAsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/api/game-data" {
			http.NotFound(w, request)
			return
		}
		http.Error(w, "PalGameDataBridge GameData API is not enabled", http.StatusNotFound)
	}))
	defer server.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)

	snapshot, err := getOfficialWorldSnapshot(ServerInstance{RESTPort: port, AdminPassword: "secret"})
	if err != nil {
		t.Fatalf("disabled game-data returned an API failure: %v", err)
	}
	if snapshot.Available {
		t.Fatalf("disabled game-data snapshot was marked available: %#v", snapshot)
	}
	if !strings.Contains(snapshot.UnavailableReason, "未启用") {
		t.Fatalf("unavailable reason = %q", snapshot.UnavailableReason)
	}
}

func TestOfficialRESTActionsUseDocumentedBodies(t *testing.T) {
	requests := make(chan struct {
		path string
		body map[string]any
	}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil && request.URL.Path != "/v1/api/save" {
			t.Fatal(err)
		}
		requests <- struct {
			path string
			body map[string]any
		}{request.URL.Path, body}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	instance := ServerInstance{RESTPort: port, AdminPassword: "secret"}
	if err := saveWorldREST(instance); err != nil {
		t.Fatal(err)
	}
	if err := shutdownServerREST(instance, 15, "Maintenance"); err != nil {
		t.Fatal(err)
	}
	first, second := <-requests, <-requests
	if first.path != "/v1/api/save" {
		t.Fatalf("save path = %q", first.path)
	}
	if second.path != "/v1/api/shutdown" || second.body["waittime"] != float64(15) || second.body["message"] != "Maintenance" {
		t.Fatalf("shutdown request = %#v", second)
	}
}

func TestShutdownServerMarksExpectedStopAndSuppressesRecovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	instance := ServerInstance{ID: "server-1", RESTPort: port, AdminPassword: "secret"}
	restartCancel := make(chan struct{})
	app := &App{
		store:              &Store{config: AppConfig{Instances: []ServerInstance{instance}}},
		expectedStops:      map[string]bool{},
		restartCancels:     map[string]chan struct{}{instance.ID: restartCancel},
		guardianSuppressed: map[string]bool{},
	}

	if err := app.ShutdownServer(instance.ID, 15, "Maintenance"); err != nil {
		t.Fatal(err)
	}
	if !app.expectedStops[instance.ID] {
		t.Fatal("official shutdown was not marked as an expected stop")
	}
	if !app.guardianSuppressed[instance.ID] {
		t.Fatal("guardian was not suppressed for official shutdown")
	}
	if _, exists := app.restartCancels[instance.ID]; exists {
		t.Fatal("scheduled restart was not cancelled")
	}
	select {
	case <-restartCancel:
	default:
		t.Fatal("scheduled restart cancellation channel was not closed")
	}
}

func TestShutdownServerRestoresRecoveryStateWhenRequestFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	instance := ServerInstance{ID: "server-1", RESTPort: port, AdminPassword: "secret"}
	app := &App{
		store:              &Store{config: AppConfig{Instances: []ServerInstance{instance}}},
		expectedStops:      map[string]bool{},
		restartCancels:     map[string]chan struct{}{},
		guardianSuppressed: map[string]bool{},
	}

	if err := app.ShutdownServer(instance.ID, 15, "Maintenance"); err == nil {
		t.Fatal("expected shutdown request to fail")
	}
	if app.expectedStops[instance.ID] {
		t.Fatal("failed shutdown remained marked as expected")
	}
	if app.guardianSuppressed[instance.ID] {
		t.Fatal("failed shutdown left guardian suppressed")
	}
}

func TestParseOfficialWorkshopMod(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "Example")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	info := `{"PackageName":"ExampleMod","Version":"1.2.3","Dependencies":["CoreMod"],"InstallRules":[{"IsServer":true}]}`
	if err := os.WriteFile(filepath.Join(mod, "Info.json"), []byte(info), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseOfficialWorkshopMod(mod)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.PackageName != "ExampleMod" || !parsed.ServerCompatible || parsed.Version != "1.2.3" || len(parsed.Dependencies) != 1 || parsed.Dependencies[0] != "CoreMod" {
		t.Fatalf("parsed workshop mod = %#v", parsed)
	}
}

func TestMergePalModSettingsTracksActivePackages(t *testing.T) {
	content := "[PalModSettings]\nbGlobalEnableMod=false\nActiveModList=Old\nWorkshopRootDir=C:\\Workshop\nCustomOption=Keep\n"
	updated := mergePalModSettings(content, []string{"ExampleMod", "CoreMod"}, "")
	for _, expected := range []string{"bGlobalEnableMod=true", "ActiveModList=CoreMod", "ActiveModList=ExampleMod", "WorkshopRootDir=C:\\Workshop", "CustomOption=Keep"} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("updated settings missing %q:\n%s", expected, updated)
		}
	}
	if strings.Contains(updated, "ActiveModList=Old") {
		t.Fatalf("old active package remained:\n%s", updated)
	}
}

func TestFlushWorldSaveBeforeBackupUsesOfficialRESTEndpoint(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		called = request.Method == http.MethodPost && request.URL.Path == "/v1/api/save"
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	if err := flushWorldSaveBeforeBackup(ServerInstance{RESTPort: port, AdminPassword: "password"}, true); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("official REST /save was not called before backup")
	}
}

func TestPvpPresetContainsOfficialRequiredSettings(t *testing.T) {
	updates := officialPvPPresetUpdates()
	for key, expected := range map[string]string{"bIsPvP": "True", "bEnablePlayerToPlayerDamage": "True", "bEnableDefenseOtherGuildPlayer": "True", "bBuildAreaLimit": "True", "DenyTechnologyList": `(SkillUnlock_JetDragon,SkillUnlock_IceHorse,SkillUnlock_IceHorse_Dark,SkillUnlock_SaintCentaur,SkillUnlock_BlackCentaur,SkillUnlock_DarkMechaDragon,SkillUnlock_PoseidonOrca,GrapplingGun,GrapplingGun2,GrapplingGun3,GrapplingGun4,GrapplingGun5,GuildChest)`, "BlockRespawnTime": "5.0", "BaseCampMaxNumInGuild": "2", "bAdditionalDropItemWhenPlayerKillingInPvPMode": "True"} {
		if updates[key] != expected {
			t.Fatalf("PvP preset %s = %q, want %q", key, updates[key], expected)
		}
	}
}

func TestOfficialWorldSettingRanges(t *testing.T) {
	for key, want := range map[string][2]float64{"ServerReplicatePawnCullDistance": {5000, 15000}, "BaseCampWorkerMaxNum": {0, 50}, "BaseCampMaxNumInGuild": {0, 10}} {
		got := officialWorldSettingRange(key)
		if got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("range %s = %#v, want %#v", key, got, want)
		}
	}
}

func TestPerformanceAdviceIdentifiesOfficialLoadSettings(t *testing.T) {
	advice := buildPerformanceAdvice(RuntimeStatus{Running: true, FPS: 35, FrameTime: 30, BaseCampNum: 9}, map[string]string{"BaseCampWorkerMaxNum": "50", "PalSpawnNumRate": "3", "PhysicsActiveDropItemMaxNum": "500"})
	if len(advice) < 3 {
		t.Fatalf("advice = %#v", advice)
	}
}

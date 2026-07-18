package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"testing"
)

func TestInvokeWebRPCUsesExistingAppMethods(t *testing.T) {
	app := &App{}
	result, err := invokeWebRPC(app, "GetLauncherVersion", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != LauncherVersion {
		t.Fatalf("version result = %#v", result)
	}
	if _, err := invokeWebRPC(app, "NotExported", nil); err == nil {
		t.Fatal("unknown web RPC method was accepted")
	}
}

func TestNormalizeAgentPlatform(t *testing.T) {
	for _, value := range []string{"linux", "LINUX", " windows "} {
		if _, err := normalizeAgentPlatform(value); err != nil {
			t.Fatalf("normalizeAgentPlatform(%q): %v", value, err)
		}
	}
	if _, err := normalizeAgentPlatform("darwin"); err == nil {
		t.Fatal("unsupported preview platform was accepted")
	}
}

func TestAgentHealthPlatformSelection(t *testing.T) {
	app := &App{store: &Store{}}
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		name           string
		platform       string
		defaultHandler bool
	}{
		{name: "production", platform: runtime.GOOS, defaultHandler: true},
		{name: "linux preview", platform: "linux"},
		{name: "windows preview", platform: "windows"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var handler http.Handler
			var err error
			if test.defaultHandler {
				handler, err = newAgentHTTPHandler(app, newAgentAuth(token))
			} else {
				handler, err = newAgentHTTPHandlerForPlatform(app, newAgentAuth(token), test.platform)
			}
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("health status = %d", response.Code)
			}
			var payload struct {
				Platform string `json:"platform"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Platform != test.platform {
				t.Fatalf("platform = %q, want %q", payload.Platform, test.platform)
			}
		})
	}
}

func TestWebGetConfigDoesNotExposeEncryptedPasswordFields(t *testing.T) {
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{{
		ID: "srv-secret", Name: "Secret", AdminPassword: "editable-admin", ServerPassword: "editable-join",
		EncryptedAdminPassword: "admin-ciphertext", EncryptedServerPassword: "join-ciphertext",
	}}}}}
	result, err := invokeWebRPC(app, "GetConfig", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("admin-ciphertext")) || bytes.Contains(data, []byte("join-ciphertext")) {
		t.Fatalf("GetConfig exposed encrypted password fields: %s", data)
	}
	if !bytes.Contains(data, []byte("editable-admin")) || !bytes.Contains(data, []byte("editable-join")) {
		t.Fatalf("GetConfig did not preserve editable password values: %s", data)
	}
}

func TestWebRPCAllowlistCoversFrontendAPI(t *testing.T) {
	pattern := regexp.MustCompile(`API\.([A-Z][A-Za-z0-9_]*)`)
	missing := map[string]bool{}
	err := filepath.Walk(filepath.Join("frontend", "src"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".tsx" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range pattern.FindAllSubmatch(data, -1) {
			name := string(match[1])
			if !webRPCMethods[name] {
				missing[name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("frontend methods missing from Linux web RPC allowlist: %#v", missing)
	}
}

func TestWebRPCAllowlistCoversDesktopBindings(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("frontend", "wailsjs", "go", "main", "App.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`export function ([A-Z][A-Za-z0-9_]*)\(`)
	missing := make([]string, 0)
	for _, match := range pattern.FindAllSubmatch(data, -1) {
		name := string(match[1])
		if !webRPCMethods[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("desktop methods missing from Linux web RPC allowlist: %v", missing)
	}
	appType := reflect.TypeOf(&App{})
	for name := range webRPCMethods {
		if _, ok := appType.MethodByName(name); !ok {
			t.Fatalf("Linux web RPC allowlist contains unknown method %q", name)
		}
	}
}

func TestAgentHTTPLoginAndAuthenticatedRPC(t *testing.T) {
	root := t.TempDir()
	app := &App{store: &Store{path: filepath.Join(root, "config.json")}}
	handler, err := newAgentHTTPHandler(app, newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	unauthorizedBody := bytes.NewBufferString(`{"args":[]}`)
	response, err := http.Post(server.URL+"/api/v1/rpc/GetLauncherVersion", "application/json", unauthorizedBody)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}

	loginBody, _ := json.Marshal(agentLoginRequest{Password: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
	response, err = http.Post(server.URL+"/api/v1/session", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	cookies := response.Cookies()
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || len(cookies) == 0 {
		t.Fatalf("login status = %d, cookies = %d", response.StatusCode, len(cookies))
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/rpc/GetLauncherVersion", bytes.NewBufferString(`{"args":[]}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookies[0])
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated RPC status = %d", response.StatusCode)
	}
	var payload webRPCResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Result != LauncherVersion {
		t.Fatalf("RPC result = %#v", payload.Result)
	}
}

func TestAgentFileTransferRequiresAuthentication(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", root)
	app := &App{store: &Store{path: filepath.Join(root, "config.json")}}
	handler, err := newAgentHTTPHandler(app, newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/upload/server-import",
		"/api/v1/upload/mods/server/pak",
		"/api/v1/upload/server-mod/server/catalog",
		"/api/v1/download/client-mods/server",
	} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s unauthenticated status = %d", path, response.Code)
		}
	}
}

func TestAgentStaticFrontendIsNeverCached(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", root)
	app := &App{store: &Store{path: filepath.Join(root, "config.json")}}
	handler, err := newAgentHTTPHandlerForPlatform(app, newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"), "linux")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("static response code=%d cache-control=%q", response.Code, response.Header().Get("Cache-Control"))
	}
}

func TestAgentPasswordCredentialIsCreatedWithoutPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent", "admin-auth.json")
	auth, err := newPersistentAgentAuth(path)
	if err != nil {
		t.Fatalf("fresh auth err=%v", err)
	}
	if !auth.setupRequired() {
		t.Fatalf("fresh auth setupRequired=%v", auth.setupRequired())
	}
	password := "CorrectHorse!123"
	if err := auth.setupPassword(password); err != nil {
		t.Fatal(err)
	}
	if auth.setupRequired() || !auth.passwordMatches(password) || auth.passwordMatches("wrong-password") {
		t.Fatal("created administrator password did not authenticate correctly")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(password)) {
		t.Fatal("administrator password was written in plaintext")
	}
	reloaded, err := newPersistentAgentAuth(path)
	if err != nil {
		t.Fatalf("reloaded auth err=%v", err)
	}
	if reloaded.setupRequired() || !reloaded.passwordMatches(password) {
		t.Fatalf("reloaded auth setupRequired=%v", reloaded.setupRequired())
	}
}

func TestAgentFirstRunPasswordSetupEndpoint(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", root)
	app := &App{store: &Store{path: filepath.Join(root, "config.json")}}
	auth, err := newPersistentAgentAuth(filepath.Join(root, "admin-auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newAgentHTTPHandler(app, auth)
	if err != nil {
		t.Fatal(err)
	}
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	var healthPayload struct {
		SetupRequired bool `json:"setupRequired"`
	}
	if err := json.NewDecoder(health.Body).Decode(&healthPayload); err != nil || !healthPayload.SetupRequired {
		t.Fatalf("fresh health setupRequired=%v err=%v", healthPayload.SetupRequired, err)
	}
	setup := httptest.NewRequest(http.MethodPost, "/api/v1/setup", bytes.NewBufferString(`{"password":"CorrectHorse!123"}`))
	setup.Header.Set("Content-Type", "application/json")
	setupResponse := httptest.NewRecorder()
	handler.ServeHTTP(setupResponse, setup)
	if setupResponse.Code != http.StatusCreated || len(setupResponse.Result().Cookies()) != 1 {
		t.Fatalf("setup status=%d cookies=%d body=%s", setupResponse.Code, len(setupResponse.Result().Cookies()), setupResponse.Body.String())
	}
	secondSetup := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/api/v1/setup", bytes.NewBufferString(`{"password":"AnotherPassword!"}`))
	handler.ServeHTTP(secondSetup, secondRequest)
	if secondSetup.Code != http.StatusConflict {
		t.Fatalf("duplicate setup status=%d body=%s", secondSetup.Code, secondSetup.Body.String())
	}
}

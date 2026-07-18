package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticBundleRedactsSecretsAddressesAndPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	serverRoot := filepath.Join(home, "servers", "private-server")
	instance := withDefaults(ServerInstance{
		ID: "srv-diagnostic", Name: "Diagnostic", RootPath: serverRoot, Executable: filepath.Join(serverRoot, "PalServer.exe"),
		SteamCMDPath: filepath.Join(home, "runtime", "steamcmd.exe"), PublicIP: "203.0.113.25",
		AdminPassword: "admin-secret-value", ServerPassword: "join-secret-value",
	})
	configPath := filepath.Join(serverRoot, "Pal", "Saved", "Config", serverConfigDirectoryName(), "PalWorldSettings.ini")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	config := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(AdminPassword="admin-secret-value",ServerPassword="join-secret-value",PublicIP="203.0.113.25")
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(serverRoot, "launcher-logs", "server.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	log := "password=admin-secret-value client=198.51.100.9 root=" + serverRoot + ` profile=C:\Users\PrivateUser\file` + "\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{
		store:         &Store{path: filepath.Join(home, "config.json"), config: AppConfig{Instances: []ServerInstance{instance}}},
		expectedStops: map[string]bool{}, restartCancels: map[string]chan struct{}{}, guardianSuppressed: map[string]bool{}, observedProcesses: map[string]observedServerProcess{},
	}
	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = &fakeProcessRuntime{}
	defer func() { defaultProcessRuntime = previousRuntime }()

	files, err := diagnosticBundleFiles(app, instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	combined := bytes.Join(func() [][]byte {
		values := make([][]byte, 0, len(files))
		for _, value := range files {
			values = append(values, value)
		}
		return values
	}(), []byte("\n"))
	text := string(combined)
	for _, secret := range []string{"admin-secret-value", "join-secret-value", "203.0.113.25", "198.51.100.9", serverRoot, `C:\Users\PrivateUser`} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostic bundle exposed %q in:\n%s", secret, text)
		}
	}
	for _, marker := range []string{"[admin-password-redacted]", "[server-password-redacted]", "[ip-redacted]", "[server-root]", "[user-profile]"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("diagnostic bundle is missing redaction marker %q", marker)
		}
	}
}

func TestWriteDiagnosticZIPContainsExpectedFiles(t *testing.T) {
	var output bytes.Buffer
	if err := writeDiagnosticZIP(&output, map[string][]byte{"summary.json": []byte("{}\n"), "logs/server.log": []byte("log\n")}); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{}
	for _, entry := range reader.File {
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s: %v / %v", entry.Name, readErr, closeErr)
		}
		contents[entry.Name] = string(data)
	}
	if contents["summary.json"] != "{}\n" || contents["logs/server.log"] != "log\n" {
		t.Fatalf("diagnostic ZIP contents = %#v", contents)
	}
}

func TestWriteDiagnosticZIPRejectsUnsafeNames(t *testing.T) {
	if err := writeDiagnosticZIP(io.Discard, map[string][]byte{"../secret.txt": []byte("x")}); err == nil {
		t.Fatal("unsafe diagnostic ZIP entry was accepted")
	}
}

func TestAgentDiagnosticDownloadRequiresAuthenticationAndAudits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	instance := withDefaults(ServerInstance{ID: "srv-web-diagnostic", Name: "Web", RootPath: filepath.Join(home, "server"), AdminPassword: "secret"})
	app := &App{
		store:         &Store{path: filepath.Join(home, "config.json"), config: AppConfig{Instances: []ServerInstance{instance}}},
		expectedStops: map[string]bool{}, restartCancels: map[string]chan struct{}{}, guardianSuppressed: map[string]bool{}, observedProcesses: map[string]observedServerProcess{},
	}
	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = &fakeProcessRuntime{}
	defer func() { defaultProcessRuntime = previousRuntime }()
	auth := newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	handler, err := newAgentHTTPHandler(app, auth)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/download/diagnostic/" + instance.ID
	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, path, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated diagnostic status = %d", unauthenticated.Code)
	}
	session, err := auth.createSession()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.RemoteAddr = "127.0.0.1:4567"
	request.AddCookie(&http.Cookie{Name: agentSessionCookie, Value: session})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("diagnostic response = %d, %q, %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	reader, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	foundSummary := false
	for _, entry := range reader.File {
		foundSummary = foundSummary || entry.Name == "summary.json"
	}
	if !foundSummary {
		t.Fatal("downloaded diagnostic bundle has no summary.json")
	}
	entries, err := app.ListAgentAudit(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Method != "DownloadDiagnosticBundle" || !entries[0].Successful || entries[0].ServerID != instance.ID {
		t.Fatalf("diagnostic audit entries = %#v", entries)
	}
}

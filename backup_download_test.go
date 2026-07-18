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

func createTestBackup(t *testing.T, home, serverID, name string) string {
	t.Helper()
	root := filepath.Join(home, "backups", serverID, name)
	if err := os.MkdirAll(filepath.Join(root, "Players"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Level.sav"), []byte("level-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Players", "0001.sav"), []byte("player-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBackupDownloadSourceRejectsUnsafeAndInvalidPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	createTestBackup(t, home, "srv-safe", "20260717-120000.000")

	for _, serverID := range []string{"", ".", "..", filepath.Join("nested", "server"), filepath.Join(home, "outside")} {
		if _, err := backupDownloadSource(serverID, "20260717-120000.000"); err == nil {
			t.Fatalf("unsafe server id %q was accepted", serverID)
		}
	}
	for _, name := range []string{"", ".", "..", filepath.Join("nested", "backup"), filepath.Join(home, "outside")} {
		if _, err := backupDownloadSource("srv-safe", name); err == nil {
			t.Fatalf("unsafe backup name %q was accepted", name)
		}
	}

	fileName := "not-a-directory"
	if err := os.WriteFile(filepath.Join(home, "backups", "srv-safe", fileName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backupDownloadSource("srv-safe", fileName); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("regular file result = %v", err)
	}
}

func TestBackupDownloadRejectsSymbolicLink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.sav"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "backups", "srv-safe")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked-backup")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links are unavailable in this test environment: %v", err)
	}
	if _, err := backupDownloadSource("srv-safe", "linked-backup"); err == nil {
		t.Fatal("symbolic-link backup was accepted")
	}
}

func TestWriteBackupZIPRejectsNestedSymbolicLink(t *testing.T) {
	home := t.TempDir()
	source := createTestBackup(t, home, "srv-safe", "backup-with-link")
	outside := filepath.Join(t.TempDir(), "outside.sav")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "Players", "linked.sav")); err != nil {
		t.Skipf("symbolic links are unavailable in this test environment: %v", err)
	}
	if err := writeBackupZIP(io.Discard, source); err == nil {
		t.Fatal("ZIP writer accepted a nested symbolic link")
	}
}

func TestWriteBackupZIPContainsNestedFiles(t *testing.T) {
	home := t.TempDir()
	source := createTestBackup(t, home, "srv-safe", "backup-one")
	var output bytes.Buffer
	if err := writeBackupZIP(&output, source); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{}
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
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
	if contents["Level.sav"] != "level-data" || contents["Players/0001.sav"] != "player-data" {
		t.Fatalf("ZIP contents = %#v", contents)
	}
}

func TestAgentBackupDownloadRequiresAuthenticationAndWritesAudit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	const serverID = "srv-download"
	const backupName = "20260717-120000.000"
	createTestBackup(t, home, serverID, backupName)
	createTestBackup(t, home, "srv-other", backupName)
	app := &App{store: &Store{path: filepath.Join(home, "config.json"), config: AppConfig{Instances: []ServerInstance{{ID: serverID, Name: "Test", RootPath: filepath.Join(home, "server")}}}}}
	auth := newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	handler, err := newAgentHTTPHandler(app, auth)
	if err != nil {
		t.Fatal(err)
	}

	path := "/api/v1/download/backup/" + serverID + "/" + backupName
	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, path, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
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
	if response.Code != http.StatusOK {
		t.Fatalf("download status = %d, body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/zip" || !strings.Contains(response.Header().Get("Content-Disposition"), backupName+".zip") {
		t.Fatalf("download headers = %#v", response.Header())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control = %q", response.Header().Get("Cache-Control"))
	}
	reader, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil || len(reader.File) == 0 {
		t.Fatalf("downloaded ZIP = files:%d err:%v", len(reader.File), err)
	}

	entries, err := app.ListAgentAudit(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Method != "DownloadBackup" || entries[0].ServerID != serverID || !entries[0].Successful || entries[0].RemoteIP != "127.0.0.1" {
		t.Fatalf("audit entries = %#v", entries)
	}
	auditData, err := os.ReadFile(filepath.Join(home, "audit", "web-agent.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(auditData), home) || strings.Contains(string(auditData), "player-data") || strings.Contains(string(auditData), "0123456789abcdef") {
		t.Fatalf("audit exposed sensitive backup data: %s", auditData)
	}

	otherRequest := httptest.NewRequest(http.MethodGet, "/api/v1/download/backup/srv-other/"+backupName, nil)
	otherRequest.AddCookie(&http.Cookie{Name: agentSessionCookie, Value: session})
	otherResponse := httptest.NewRecorder()
	handler.ServeHTTP(otherResponse, otherRequest)
	if otherResponse.Code != http.StatusNotFound {
		t.Fatalf("unmanaged server backup status = %d", otherResponse.Code)
	}
}

func TestAgentOfficialBackupDownloadUsesManagedServerSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	const serverID = "srv-official"
	const backupName = "2026.07.17-12.00.00"
	serverRoot := filepath.Join(home, "server")
	backupRoot := filepath.Join(serverRoot, "Pal", "Saved", "SaveGames", "0", "WORLD-GUID", "backup", "world", backupName)
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupRoot, "Level.sav"), []byte("official-level"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{store: &Store{path: filepath.Join(home, "config.json"), config: AppConfig{Instances: []ServerInstance{{ID: serverID, Name: "Test", RootPath: serverRoot}}}}}
	auth := newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	session, err := auth.createSession()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newAgentHTTPHandler(app, auth)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/download/official-backup/"+serverID+"/"+backupName, nil)
	request.RemoteAddr = "127.0.0.1:4567"
	request.AddCookie(&http.Cookie{Name: agentSessionCookie, Value: session})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("official backup download status = %d, body=%s", response.Code, response.Body.String())
	}
	reader, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil || len(reader.File) != 1 || reader.File[0].Name != "Level.sav" {
		t.Fatalf("official backup ZIP = %#v, err=%v", reader, err)
	}
	entries, err := app.ListAgentAudit(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Method != "DownloadOfficialBackup" || !entries[0].Successful || entries[0].ServerID != serverID {
		t.Fatalf("official backup audit = %#v", entries)
	}
}

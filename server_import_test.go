package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectServerImportLayoutFindsWindowsServerData(t *testing.T) {
	root := t.TempDir()
	saved := filepath.Join(root, "MyServer", "Pal", "Saved")
	world := filepath.Join(saved, "SaveGames", "0", "WORLD")
	config := filepath.Join(saved, "Config", "WindowsServer", "PalWorldSettings.ini")
	if err := os.MkdirAll(world, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(world, "Level.sav"), []byte("save"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="迁移测试服",AdminPassword="secret")`
	if err := os.WriteFile(config, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	layout, err := detectServerImportLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	if layout.SaveRoot != saved || layout.Settings != config || layout.DetectedName != "迁移测试服" {
		t.Fatalf("layout = %#v", layout)
	}
}

func TestCopyServerImportDataOnlyCopiesPortableData(t *testing.T) {
	source := t.TempDir()
	saved := filepath.Join(source, "Pal", "Saved")
	world := filepath.Join(saved, "SaveGames", "0", "WORLD")
	config := filepath.Join(saved, "Config", "WindowsServer", "PalWorldSettings.ini")
	if err := os.MkdirAll(world, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(world, "Level.sav"), []byte("save"), 0o600)
	_ = os.WriteFile(config, []byte("[/Script/Pal.PalGameWorldSettings]\nOptionSettings=()"), 0o600)
	_ = os.WriteFile(filepath.Join(source, "PalServer.exe"), []byte("windows-binary"), 0o600)
	target := t.TempDir()
	instance := ServerInstance{RootPath: target}
	if err := copyServerImportData(instance, serverImportLayout{SaveRoot: saved, Settings: config}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "Pal", "Saved", "SaveGames", "0", "WORLD", "Level.sav")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "PalServer.exe")); !os.IsNotExist(err) {
		t.Fatal("Windows server executable was copied into the fresh runtime")
	}
}

func TestExtractServerImportZIPRejectsTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "unsafe.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("../escape.txt")
	_, _ = entry.Write([]byte("escape"))
	_ = writer.Close()
	_ = file.Close()
	if err := extractServerImportZIP(archive, t.TempDir()); err == nil {
		t.Fatal("unsafe ZIP traversal was accepted")
	}
}

func TestSaveWebServerImportPreservesBrowserRelativePaths(t *testing.T) {
	base := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", base)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "Level.sav")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("save"))
	_ = writer.WriteField("paths", "LocalServer/Pal/Saved/SaveGames/0/WORLD/Level.sav")
	_ = writer.WriteField("name", "浏览器迁移")
	_ = writer.Close()
	request, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/server-import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	id, err := saveWebServerImport(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if root, pathErr := serverImportDirectory(id); pathErr == nil {
			_ = os.RemoveAll(root)
		}
	}()
	root, _ := serverImportDirectory(id)
	data, err := os.ReadFile(filepath.Join(root, "LocalServer", "Pal", "Saved", "SaveGames", "0", "WORLD", "Level.sav"))
	if err != nil || strings.TrimSpace(string(data)) != "save" {
		t.Fatalf("staged data=%q err=%v", data, err)
	}
}

func TestLinuxWebResultRedactsAgentHostPaths(t *testing.T) {
	config := AppConfig{Instances: []ServerInstance{{ID: "server", Name: "Server", RootPath: "/var/lib/palserver-launcher/servers/server", Executable: "/var/lib/palserver-launcher/servers/server/PalServer.sh", SteamCMDPath: "/var/lib/palserver-launcher/steamcmd/steamcmd.sh"}}}
	redacted, ok := sanitizeAgentWebResult("GetConfig", config).(AppConfig)
	if !ok || len(redacted.Instances) != 1 {
		t.Fatal("sanitized config has unexpected type")
	}
	instance := redacted.Instances[0]
	if instance.RootPath != "" || instance.Executable != "" || instance.SteamCMDPath != "" {
		t.Fatalf("Agent paths leaked to browser: %#v", instance)
	}
	backups := sanitizeAgentWebResult("ListBackups", []BackupEntry{{Name: "20260718-120000", Path: "/private/backup"}}).([]BackupEntry)
	if backups[0].Path != backups[0].Name {
		t.Fatalf("backup path was not converted to an opaque name: %#v", backups[0])
	}
}

func TestLinuxWebRejectsHostPathArguments(t *testing.T) {
	root, _ := json.Marshal("/srv/manually-selected")
	if err := validateLinuxWebRPCArguments("QuickSetup", []json.RawMessage{json.RawMessage(`"Server"`), root}); err == nil {
		t.Fatal("Linux web QuickSetup accepted a browser-selected host path")
	}
	instance, _ := json.Marshal(ServerInstance{Name: "Manual", RootPath: "/srv/manual"})
	if err := validateLinuxWebRPCArguments("SaveInstance", []json.RawMessage{instance}); err == nil {
		t.Fatal("Linux web SaveInstance accepted creation through a host path")
	}
}

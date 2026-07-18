//go:build !linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzePluginCrashRecognizesUE4SSAccessViolation(t *testing.T) {
	record := analyzePluginCrash("Unhandled Exception: EXCEPTION_ACCESS_VIOLATION\nUE4SS.dll!Hook")
	if !record.PluginRelated || record.Signature != "UE4SS_ACCESS_VIOLATION" {
		t.Fatalf("unexpected crash record: %#v", record)
	}
}

func TestBuildPluginCompatibilityReportWarnsWhenGameBuildChanges(t *testing.T) {
	root := t.TempDir()
	instance := ServerInstance{ID: "server-1", RootPath: root, SteamCMDPath: filepath.Join(root, "steamcmd.exe")}
	manifestPath := filepath.Join(root, "steamapps", "appmanifest_2394010.acf")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`"AppState" { "buildid" "200" }`), 0o600); err != nil {
		t.Fatal(err)
	}
	extensions := []ExtensionStatus{{ID: "paldefender", Name: "PalDefender", Installed: true, Enabled: true, Version: "1.8.1"}}
	report := buildPluginCompatibilityReport(instance, extensions, pluginCompatibilityBaseline{GameBuildID: "100"}, pluginCrashRecord{}, SafeModeStatus{})
	if !report.SafeModeRecommended || len(report.Issues) == 0 {
		t.Fatalf("build change was not surfaced: %#v", report)
	}
}

func TestBuildPluginCompatibilityReportSurfacesPluginCrash(t *testing.T) {
	instance := ServerInstance{ID: "server-1", RootPath: t.TempDir()}
	report := buildPluginCompatibilityReport(instance, nil, pluginCompatibilityBaseline{}, pluginCrashRecord{Signature: "PALDEFENDER_ACCESS_VIOLATION", PluginRelated: true, Summary: "crash"}, SafeModeStatus{})
	if report.Compatible || !report.SafeModeRecommended || report.LastCrashSummary != "crash" {
		t.Fatalf("plugin crash report = %#v", report)
	}
}

func TestSafeModeDisablesAndRestoresInstalledCorePlugins(t *testing.T) {
	root := t.TempDir()
	instance := ServerInstance{ID: "server-1", RootPath: root, Executable: filepath.Join(root, "missing-PalServer.exe")}
	base := win64Path(instance)
	for path, content := range map[string]string{
		filepath.Join(base, "PalDefender.dll"):             "paldefender",
		filepath.Join(base, "d3d9.dll"):                    "proxy",
		filepath.Join(base, "dwmapi.dll"):                  "proxy",
		filepath.Join(base, "ue4ss", "UE4SS.dll"):          "ue4ss",
		filepath.Join(base, "ue4ss", "UE4SS-settings.ini"): "settings",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{store: &Store{path: filepath.Join(root, "config.json"), config: AppConfig{Instances: []ServerInstance{instance}}}}
	if err := app.StartServerSafeMode(instance.ID); err == nil {
		t.Fatal("safe mode unexpectedly started a missing server executable")
	}
	status, err := app.GetSafeModeStatus(instance.ID)
	if err != nil || !status.Active || !status.PalDefenderCurrentlyOff || !status.UE4SSCurrentlyOff {
		t.Fatalf("safe mode status = %#v, %v", status, err)
	}
	if err := app.RestorePluginsAfterSafeMode(instance.ID); err != nil {
		t.Fatal(err)
	}
	extensions := listExtensionStatuses(instance)
	if !extensionByID(extensions, "paldefender").Enabled || !extensionByID(extensions, "ue4ss").Enabled {
		t.Fatalf("plugins were not restored: %#v", extensions)
	}
}

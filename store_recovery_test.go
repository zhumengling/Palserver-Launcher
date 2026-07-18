package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRecoversPreviousConfigWhenMainFileIsCorrupt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	instance, err := store.Upsert(ServerInstance{Name: "上一份配置", RootPath: filepath.Join(home, "server")})
	if err != nil {
		t.Fatal(err)
	}
	instance.Name = "最新配置"
	if _, err := store.Upsert(instance); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"instances":`), 0o600); err != nil {
		t.Fatal(err)
	}

	recovered, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := recovered.Snapshot()
	if len(snapshot.Instances) != 1 || snapshot.Instances[0].Name != "上一份配置" {
		t.Fatalf("recovered config = %#v", snapshot)
	}
	warnings := recovered.Warnings()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "已自动恢复上一份备份") {
		t.Fatalf("recovery warnings = %#v", warnings)
	}
	corrupt, err := filepath.Glob(filepath.Join(home, "config.json.corrupt-*"))
	if err != nil || len(corrupt) != 1 {
		t.Fatalf("quarantined config = %#v, %v", corrupt, err)
	}
	app := &App{store: recovered}
	if got := app.GetConfig().StartupWarnings; len(got) != 1 {
		t.Fatalf("GetConfig startup warnings = %#v", got)
	}

	reopened, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	if warnings := reopened.Warnings(); len(warnings) != 0 {
		t.Fatalf("recovery warning was persisted: %#v", warnings)
	}
}

func TestStoreQuarantinesCorruptMainAndBackupThenStartsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json.bak"), []byte(`also-not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := store.Snapshot(); len(snapshot.Instances) != 0 || snapshot.Language != "zh-CN" {
		t.Fatalf("empty recovered config = %#v", snapshot)
	}
	if warnings := store.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "已使用空白配置启动") {
		t.Fatalf("empty recovery warnings = %#v", warnings)
	}
	mainFiles, _ := filepath.Glob(filepath.Join(home, "config.json.corrupt-*"))
	backupFiles, _ := filepath.Glob(filepath.Join(home, "config.json.bak.corrupt-*"))
	if len(mainFiles) != 1 || len(backupFiles) != 1 {
		t.Fatalf("quarantined files main=%#v backup=%#v", mainFiles, backupFiles)
	}
	if _, err := decodeAppConfig(mustReadFile(t, filepath.Join(home, "config.json"))); err != nil {
		t.Fatalf("replacement config is invalid: %v", err)
	}
}

func TestStoreRestoresBackupWhenMainConfigIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	backup := AppConfig{Language: "zh-CN", Instances: []ServerInstance{{ID: "srv-backup", Name: "备份服务器", RootPath: filepath.Join(home, "server")}}}
	if err := writeAppConfig(filepath.Join(home, "config.json.bak"), backup); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := store.Snapshot(); len(snapshot.Instances) != 1 || snapshot.Instances[0].Name != "备份服务器" {
		t.Fatalf("missing-main recovery = %#v", snapshot)
	}
	if warnings := store.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "主配置缺失") {
		t.Fatalf("missing-main warning = %#v", warnings)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

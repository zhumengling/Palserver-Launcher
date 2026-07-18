package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreEncryptsServerPasswordsAtRestAndDecryptsOnLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Upsert(ServerInstance{
		Name: "加密测试", RootPath: filepath.Join(home, "server"),
		AdminPassword: "admin-plaintext-secret", ServerPassword: "join-plaintext-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	data := mustReadFile(t, filepath.Join(home, "config.json"))
	for _, secret := range [][]byte{[]byte("admin-plaintext-secret"), []byte("join-plaintext-secret")} {
		if bytes.Contains(data, secret) {
			t.Fatalf("config contains plaintext secret %q: %s", secret, data)
		}
	}
	for _, field := range [][]byte{[]byte("encryptedAdminPassword"), []byte("encryptedServerPassword")} {
		if !bytes.Contains(data, field) {
			t.Fatalf("config is missing encrypted field %q: %s", field, data)
		}
	}

	reopened, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	instance, err := reopened.Find(stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if instance.AdminPassword != "admin-plaintext-secret" || instance.ServerPassword != "join-plaintext-secret" {
		t.Fatalf("decrypted instance = %#v", instance)
	}
	public := (&App{store: reopened}).GetConfig().Instances[0]
	if public.EncryptedAdminPassword != "" || public.EncryptedServerPassword != "" {
		t.Fatalf("GetConfig exposed encrypted fields: %#v", public)
	}
	if public.AdminPassword != "admin-plaintext-secret" || public.ServerPassword != "join-plaintext-secret" {
		t.Fatalf("GetConfig lost editable passwords: %#v", public)
	}
}

func TestStoreMigratesLegacyPlaintextPasswords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	legacy := AppConfig{Language: "zh-CN", Instances: []ServerInstance{{
		ID: "srv-legacy-secret", Name: "旧配置", RootPath: filepath.Join(home, "server"),
		AdminPassword: "legacy-admin-secret", ServerPassword: "legacy-join-secret",
	}}}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	instance, err := store.Find("srv-legacy-secret")
	if err != nil || instance.AdminPassword != "legacy-admin-secret" || instance.ServerPassword != "legacy-join-secret" {
		t.Fatalf("legacy secret migration = %#v, %v", instance, err)
	}
	persisted := mustReadFile(t, filepath.Join(home, "config.json"))
	if bytes.Contains(persisted, []byte("legacy-admin-secret")) || bytes.Contains(persisted, []byte("legacy-join-secret")) {
		t.Fatalf("legacy plaintext remained after migration: %s", persisted)
	}
	backup := mustReadFile(t, filepath.Join(home, "config.json.bak"))
	if bytes.Contains(backup, []byte("legacy-admin-secret")) || bytes.Contains(backup, []byte("legacy-join-secret")) {
		t.Fatalf("legacy plaintext remained in config backup: %s", backup)
	}
}

func TestStoreRecoversMissingPasswordsFromWorldSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	root := filepath.Join(home, "server")
	instance := ServerInstance{ID: "srv-recover-secret", Name: "恢复配置", RootPath: root}
	settingsPath, err := worldSettingsPath(instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(AdminPassword="actual-admin-secret",ServerPassword="actual-join-secret")`
	if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := json.MarshalIndent(AppConfig{Language: "zh-CN", Instances: []ServerInstance{instance}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), config, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Find(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.AdminPassword != "actual-admin-secret" || recovered.ServerPassword != "actual-join-secret" {
		t.Fatalf("recovered credentials = %#v", recovered)
	}
	persisted := mustReadFile(t, filepath.Join(home, "config.json"))
	if bytes.Contains(persisted, []byte("actual-admin-secret")) || bytes.Contains(persisted, []byte("actual-join-secret")) {
		t.Fatalf("recovered credentials were stored as plaintext: %s", persisted)
	}
	if !bytes.Contains(persisted, []byte("encryptedAdminPassword")) || !bytes.Contains(persisted, []byte("encryptedServerPassword")) {
		t.Fatalf("recovered encrypted fields are missing: %s", persisted)
	}
}

func TestStoreCanClearEncryptedPasswordsFromPublicInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Upsert(ServerInstance{Name: "清除密码", RootPath: filepath.Join(home, "server"), AdminPassword: "admin", ServerPassword: "join"})
	if err != nil {
		t.Fatal(err)
	}
	public := (&App{store: store}).GetConfig().Instances[0]
	public.AdminPassword = ""
	public.ServerPassword = ""
	if _, err := store.Upsert(public); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	instance, err := reopened.Find(stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if instance.AdminPassword != "" || instance.ServerPassword != "" || instance.EncryptedAdminPassword != "" || instance.EncryptedServerPassword != "" {
		t.Fatalf("cleared passwords were retained: %#v", instance)
	}
}

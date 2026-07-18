package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createManifestBackup(t *testing.T, root, serverID string) string {
	t.Helper()
	source := filepath.Join(root, "source")
	backup := filepath.Join(root, "backup")
	if err := os.MkdirAll(filepath.Join(source, "0", "WORLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "0", "WORLD", "Level.sav"), []byte("level-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "0", "WORLD", "LevelMeta.sav"), []byte("meta-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := copyBackupTree(source, backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBackupManifest(backup, serverID, time.Now().UnixMilli(), files); err != nil {
		t.Fatal(err)
	}
	return backup
}

func TestBackupManifestVerifiesCompleteBackup(t *testing.T) {
	backup := createManifestBackup(t, t.TempDir(), "srv-integrity")
	manifest, found, err := verifyBackupManifest(backup)
	if err != nil || !found || manifest.Version != 1 || manifest.ServerID != "srv-integrity" || len(manifest.Files) != 2 {
		t.Fatalf("verified manifest = %#v, found=%v err=%v", manifest, found, err)
	}
}

func TestBackupManifestRejectsChangedMissingAndUnlistedFiles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(string) error
		want   string
	}{
		{name: "changed", mutate: func(root string) error {
			return os.WriteFile(filepath.Join(root, "0", "WORLD", "Level.sav"), []byte("tamper-dat"), 0o600)
		}, want: "checksum mismatch"},
		{name: "missing", mutate: func(root string) error { return os.Remove(filepath.Join(root, "0", "WORLD", "Level.sav")) }, want: "is missing"},
		{name: "unlisted", mutate: func(root string) error {
			return os.WriteFile(filepath.Join(root, "unexpected.sav"), []byte("extra"), 0o600)
		}, want: "unlisted file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backup := createManifestBackup(t, t.TempDir(), "srv-integrity")
			if err := test.mutate(backup); err != nil {
				t.Fatal(err)
			}
			if _, found, err := verifyBackupManifest(backup); !found || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("verifyBackupManifest() found=%v err=%v", found, err)
			}
		})
	}
}

func TestRestoreRejectsTamperedManifestBackupWithoutReplacingSave(t *testing.T) {
	root := t.TempDir()
	backup := createManifestBackup(t, filepath.Join(root, "fixture"), "srv-integrity")
	if err := os.WriteFile(filepath.Join(backup, "0", "WORLD", "Level.sav"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "SaveGames")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(destination, "old.sav")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackupTree(backup, destination); err == nil || !strings.Contains(err.Error(), "integrity check failed") {
		t.Fatalf("tampered restore error = %v", err)
	}
	if data, err := os.ReadFile(oldPath); err != nil || string(data) != "old" {
		t.Fatalf("existing save changed after rejected restore: %q, %v", data, err)
	}
}

func TestLegacyBackupWithoutManifestRemainsRestorable(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, "legacy")
	destination := filepath.Join(root, "SaveGames")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "Level.sav"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackupTree(backup, destination); err != nil {
		t.Fatalf("legacy restore failed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "Level.sav")); err != nil || string(data) != "legacy" {
		t.Fatalf("legacy restored data = %q, %v", data, err)
	}
}

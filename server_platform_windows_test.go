//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsServerExecutableMustRemainInsideInstanceRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server")
	inside := ServerInstance{RootPath: root, Executable: filepath.Join(root, "PalServer.exe")}
	if err := validatePlatformServerExecutable(inside); err != nil {
		t.Fatalf("inside executable was rejected: %v", err)
	}
	outside := ServerInstance{RootPath: root, Executable: filepath.Join(filepath.Dir(root), "other", "PalServer.exe")}
	if err := validatePlatformServerExecutable(outside); err == nil {
		t.Fatal("outside executable was accepted")
	}
}

func TestWindowsInstalledServerRequiresWrapperAndShippingBinary(t *testing.T) {
	root := t.TempDir()
	instance := ServerInstance{RootPath: root, Executable: filepath.Join(root, "PalServer.exe")}
	if err := os.WriteFile(instance.Executable, []byte("wrapper"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateInstalledServerExecutable(instance); err == nil {
		t.Fatal("installation without the shipping executable was accepted")
	}
	shipping := filepath.Join(root, "Pal", "Binaries", "Win64", "PalServer-Win64-Shipping.exe")
	if err := os.MkdirAll(filepath.Dir(shipping), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shipping, []byte("shipping"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateInstalledServerExecutable(instance); err != nil {
		t.Fatal(err)
	}
}

//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxManagedServerRootHonorsSystemdAllowedRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	t.Setenv("PALSERVER_ALLOWED_SERVER_ROOTS", root)
	if err := validateLinuxManagedServerRoot(filepath.Join(root, "server-a")); err != nil {
		t.Fatalf("managed child path was rejected: %v", err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := validateLinuxManagedServerRoot(outside); err == nil || !strings.Contains(err.Error(), root) {
		t.Fatalf("outside path result = %v", err)
	}
}

func TestLinuxManagedServerRootRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "managed")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PALSERVER_ALLOWED_SERVER_ROOTS", root)
	if err := validateLinuxManagedServerRoot(filepath.Join(escape, "server")); err == nil {
		t.Fatal("server path escaped the managed root through a symlink")
	}
}

func TestLinuxManagedServerRootAllowsSymlinkThatStaysInsideRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	realRoot := filepath.Join(root, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "inside")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PALSERVER_ALLOWED_SERVER_ROOTS", root)
	if err := validateLinuxManagedServerRoot(filepath.Join(link, "server")); err != nil {
		t.Fatalf("inside-root symlink was rejected: %v", err)
	}
}

func TestLinuxServerExecutableMustRemainInsideInstanceRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "server")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := ServerInstance{RootPath: root, Executable: filepath.Join(root, "PalServer.sh")}
	if err := validatePlatformServerExecutable(inside); err != nil {
		t.Fatalf("inside executable was rejected: %v", err)
	}
	outside := ServerInstance{RootPath: root, Executable: filepath.Join(base, "outside", "PalServer.sh")}
	if err := validatePlatformServerExecutable(outside); err == nil {
		t.Fatal("outside executable was accepted")
	}
}

func TestLinuxManagedServerRootRequiresAbsolutePath(t *testing.T) {
	t.Setenv("PALSERVER_ALLOWED_SERVER_ROOTS", "")
	if err := validateLinuxManagedServerRoot("relative/server"); err == nil {
		t.Fatal("relative Linux server path was accepted")
	}
}

func TestLinuxInstalledServerRequiresRunnableWrapperAndShippingBinary(t *testing.T) {
	root := t.TempDir()
	instance := ServerInstance{RootPath: root, Executable: filepath.Join(root, "PalServer.sh")}
	if err := os.WriteFile(instance.Executable, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateInstalledServerExecutable(instance); err == nil {
		t.Fatal("installation without the shipping executable was accepted")
	}
	shipping := filepath.Join(root, "Pal", "Binaries", "Linux", "PalServer-Linux-Shipping")
	if err := os.MkdirAll(filepath.Dir(shipping), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shipping, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateInstalledServerExecutable(instance); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{instance.Executable, shipping} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("installed executable mode %s = %v", path, info.Mode())
		}
	}
}

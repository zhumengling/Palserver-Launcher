//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxAgentSelfTestValidatesWritableDeploymentPaths(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	servers := filepath.Join(root, "servers")
	t.Setenv("HOME", home)
	t.Setenv("PALSERVER_ALLOWED_SERVER_ROOTS", servers)
	report, err := runLinuxAgentSelfTest(data, filepath.Join(data, "admin-auth.json"))
	if err != nil || !report.OK || report.Platform != "linux" || report.Architecture != "amd64" {
		t.Fatalf("self-test report = %#v, %v", report, err)
	}
	for _, path := range []string{data, home, servers} {
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			t.Fatalf("self-test path %s = %#v, %v", path, info, statErr)
		}
	}
}

func TestLinuxAgentSelfTestReportsUnusableAllowedRoot(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("PALSERVER_ALLOWED_SERVER_ROOTS", filepath.Join(blocked, "servers"))
	report, err := runLinuxAgentSelfTest(filepath.Join(root, "data"), filepath.Join(root, "data", "admin-auth.json"))
	if err == nil || report.OK {
		t.Fatalf("unusable deployment root passed self-test: %#v, %v", report, err)
	}
}

func TestLinuxAgentPreflightAppMethodReturnsSelfTestReport(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", filepath.Join(root, "data"))
	t.Setenv("PALSERVER_ALLOWED_SERVER_ROOTS", filepath.Join(root, "servers"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	app := &App{}
	report := app.GetAgentPreflight()
	if !report.OK || report.SimulatedPlatform || report.Platform != "linux" || report.HostPlatform != "linux" {
		t.Fatalf("Linux Agent preflight = %#v", report)
	}
}

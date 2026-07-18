package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxSystemdServicePreservesServersAndAllowsRequiredWrites(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("deploy", "linux", "palserver-agent.service"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, expected := range []string{
		"Environment=PALSERVER_ALLOWED_SERVER_ROOTS=/var/lib/palserver-launcher/servers",
		"Environment=HOME=/var/lib/palserver",
		"WorkingDirectory=/var/lib/palserver-launcher",
		"KillMode=process",
		"UMask=0027",
		"ReadWritePaths=/var/lib/palserver-launcher /var/lib/palserver",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Linux service is missing %q", expected)
		}
	}
}

func TestLinuxInstallerCreatesSteamHomeAndManagedServerRoot(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("deploy", "linux", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, path := range []string{"/var/lib/palserver", "/var/lib/palserver-launcher", "/var/lib/palserver-launcher/servers"} {
		if !strings.Contains(source, path) {
			t.Fatalf("Linux installer does not prepare %s", path)
		}
	}
	for _, expected := range []string{
		`"${NEW_BINARY}" --version`,
		`"${NEW_BINARY}" --self-test`,
		"PALSERVER_ALLOWED_SERVER_ROOTS=/var/lib/palserver-launcher/servers",
		"systemctl restart palserver-agent.service",
		"http://127.0.0.1:8210/api/v1/health",
		"rollback_install",
		"install-previous",
		"journalctl -u palserver-agent.service",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Linux installer is missing safe-upgrade step %q", expected)
		}
	}
}

func TestLinuxSmokeTestRunsDeploymentSelfTest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("deploy", "linux", "smoke-test.sh"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, expected := range []string{"--self-test", `grep -q '"ok":true'`, "PALSERVER_ALLOWED_SERVER_ROOTS"} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Linux smoke test is missing %q", expected)
		}
	}
}

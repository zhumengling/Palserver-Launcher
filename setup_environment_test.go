package main

import (
	"path/filepath"
	"testing"
)

func TestResolveManagedInstallRoot(t *testing.T) {
	base := "/var/lib/palserver-launcher"
	if got := resolveManagedInstallRoot(base, "", "linux"); got != filepath.Join(base, "servers") {
		t.Fatalf("Linux empty install root = %q", got)
	}
	if got := resolveManagedInstallRoot(base, "/srv/custom", "linux"); got != "/srv/custom" {
		t.Fatalf("explicit Linux install root = %q", got)
	}
	if got := resolveManagedInstallRoot(base, "", "windows"); got != "" {
		t.Fatalf("Windows empty install root = %q", got)
	}
}

func TestNextAutomaticManagedServerRootUsesIndependentSubdirectories(t *testing.T) {
	base := t.TempDir()
	first := nextAutomaticManagedServerRoot(base, "Linux Server", nil)
	wantFirst := filepath.Join(base, "servers", "Linux-Server")
	if first != wantFirst {
		t.Fatalf("first automatic server root = %q, want %q", first, wantFirst)
	}
	second := nextAutomaticManagedServerRoot(base, "Linux Server", []ServerInstance{{RootPath: first}})
	wantSecond := filepath.Join(base, "servers", "Linux-Server-2")
	if second != wantSecond {
		t.Fatalf("second automatic server root = %q, want %q", second, wantSecond)
	}
}

func TestEvaluateSetupEnvironmentOfficialRecommendations(t *testing.T) {
	report := evaluateSetupEnvironment("linux", 4, 16*1024, 20<<30, true, "安装目录有效")
	if !report.CanInstall || !report.CPURecommended || !report.MemoryRecommended || !report.DiskMinimum || len(report.Warnings) != 0 {
		t.Fatalf("recommended report = %#v", report)
	}
}

func TestEvaluateSetupEnvironmentAllowsEightGBWithWarning(t *testing.T) {
	report := evaluateSetupEnvironment("linux", 2, 8*1024, 20<<30, true, "安装目录有效")
	if !report.CanInstall || report.CPURecommended || report.MemoryRecommended || len(report.Warnings) < 2 {
		t.Fatalf("minimum report = %#v", report)
	}
}

func TestEvaluateSetupEnvironmentAllowsHardwareReservedMemory(t *testing.T) {
	report := evaluateSetupEnvironment("windows", 4, 15.5*1024, 20<<30, true, "安装目录有效")
	if !report.CanInstall || !report.MemoryRecommended {
		t.Fatalf("hardware-reserved memory report = %#v", report)
	}
}

func TestEvaluateSetupEnvironmentRejectsUnsafeHost(t *testing.T) {
	report := evaluateSetupEnvironment("linux", 2, 2*1024, 4<<30, false, "路径不可用")
	if report.CanInstall || report.MemoryMinimum || report.DiskMinimum || len(report.Warnings) < 3 {
		t.Fatalf("unsafe report = %#v", report)
	}
}

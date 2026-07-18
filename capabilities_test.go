package main

import "testing"

func TestBuildServerCapabilityReportSeparatesOfficialAndPluginActions(t *testing.T) {
	instance := ServerInstance{ID: "server-1"}
	status := RuntimeStatus{Running: true, PID: 42, Version: "1.0.0", RESTAvailable: true, RCONAvailable: true}
	extensions := []ExtensionStatus{
		{ID: "paldefender", Installed: true, Enabled: true, Version: "1.8.1"},
		{ID: "ue4ss", Installed: true, Enabled: false, Version: "experimental"},
	}
	report := buildServerCapabilityReport(instance, status, extensions, true)
	for _, id := range []string{"process", "rest", "rcon", "player-list", "player-moderation", "player-rewards", "custom-pal"} {
		if item := findCapability(report, id); !item.Available {
			t.Errorf("capability %s unavailable: %#v", id, item)
		}
	}
	if item := findCapability(report, "ue4ss"); item.Available {
		t.Fatalf("disabled UE4SS reported available: %#v", item)
	}
}

func TestBuildServerCapabilityReportExplainsUnavailablePlayerActions(t *testing.T) {
	report := buildServerCapabilityReport(ServerInstance{ID: "server-1"}, RuntimeStatus{Running: true}, nil, false)
	for _, id := range []string{"player-list", "player-moderation", "rcon-admin", "player-rewards", "custom-pal"} {
		item := findCapability(report, id)
		if item.Available || item.Reason == "" {
			t.Errorf("capability %s did not explain its unavailable state: %#v", id, item)
		}
	}
}

func TestLinuxPreviewCapabilityReportDoesNotExposeWindowsPlugins(t *testing.T) {
	report := buildServerCapabilityReport(ServerInstance{ID: "server-1"}, RuntimeStatus{Running: true, RESTAvailable: true, RCONAvailable: true}, []ExtensionStatus{{ID: "paldefender", Installed: true, Enabled: true}, {ID: "ue4ss", Installed: true, Enabled: true}}, true)
	report = normalizeCapabilityReportPlatform(report, "linux")
	if report.Platform != "linux" {
		t.Fatalf("reported platform = %q", report.Platform)
	}
	for _, id := range []string{"paldefender", "ue4ss", "rcon-admin", "player-rewards", "custom-pal", "server-mods", "safe-mode"} {
		item := findCapability(report, id)
		if item.Available || item.Reason != linuxServerModsUnsupportedMessage {
			t.Fatalf("Linux preview capability %s = %#v", id, item)
		}
	}
}

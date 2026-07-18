//go:build windows

package main

import "testing"

func TestWindowsWebPreviewReportsLinuxSimulation(t *testing.T) {
	app := &App{}
	app.setReportedPlatform("linux")
	report := app.GetAgentPreflight()
	if !report.OK || !report.SimulatedPlatform || report.Platform != "linux" || report.HostPlatform != "windows" {
		t.Fatalf("preview preflight = %#v", report)
	}
}

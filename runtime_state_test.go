package main

import (
	"testing"
	"time"
)

func TestApplyRuntimeStateRepresentsProcessAndRESTHealth(t *testing.T) {
	now := time.Unix(100, 0)
	tests := []struct {
		name        string
		status      RuntimeStatus
		operation   string
		starting    bool
		stopping    bool
		wantState   string
		wantRunning bool
	}{
		{name: "stopped", wantState: "stopped"},
		{name: "starting", starting: true, wantState: "starting"},
		{name: "running", status: RuntimeStatus{Running: true, RESTAvailable: true}, wantState: "running", wantRunning: true},
		{name: "degraded", status: RuntimeStatus{Running: true}, wantState: "degraded", wantRunning: true},
		{name: "stopping", status: RuntimeStatus{Running: true}, stopping: true, wantState: "stopping", wantRunning: true},
		{name: "stopped after expected stop", stopping: true, wantState: "stopped"},
		{name: "updating", operation: "update", wantState: "updating"},
		{name: "backup", status: RuntimeStatus{Running: true}, operation: "backup", wantState: "backing_up", wantRunning: true},
		{name: "restore", operation: "restore", wantState: "restoring"},
		{name: "save inspector", operation: "save-inspector", wantState: "inspecting"},
		{name: "duplicate", operation: "duplicate", wantState: "duplicating"},
		{name: "delete", operation: "delete", wantState: "deleting"},
		{name: "guardian", operation: "guardian", wantState: "restarting"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := applyRuntimeState(test.status, test.operation, test.starting, test.stopping, now)
			if result.State != test.wantState || result.Running != test.wantRunning || result.CheckedAt != now.UnixMilli() || result.StateMessage == "" {
				t.Fatalf("runtime state = %#v", result)
			}
		})
	}
}

func TestServerTransitionFlagsAreIndependentByServer(t *testing.T) {
	app := &App{startingServers: map[string]bool{}, expectedStops: map[string]bool{}}
	app.setServerStarting("server-a", true)
	app.markExpectedStop("server-b")
	starting, stopping := app.serverTransitionFlags("server-a")
	if !starting || stopping {
		t.Fatalf("server-a flags = %v/%v", starting, stopping)
	}
	starting, stopping = app.serverTransitionFlags("server-b")
	if starting || !stopping {
		t.Fatalf("server-b flags = %v/%v", starting, stopping)
	}
}

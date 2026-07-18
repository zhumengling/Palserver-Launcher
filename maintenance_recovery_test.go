package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoverInterruptedMaintenanceTasks(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.Local)
	store := &Store{
		path: filepath.Join(root, "config.json"),
		config: AppConfig{MaintenanceTasks: []MaintenanceTask{
			{ID: "running", ServerID: "srv-a", Enabled: true, Type: "backup", Schedule: "interval", IntervalMinutes: 60, LastStatus: "running"},
			{ID: "complete", ServerID: "srv-a", Enabled: true, Type: "restart", Schedule: "daily", DailyTime: "04:00", LastStatus: "ok"},
		}},
	}
	recovered, err := store.RecoverInterruptedMaintenanceTasks(now)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered tasks = %d, want 1", recovered)
	}
	running, err := store.MaintenanceTask("running")
	if err != nil {
		t.Fatal(err)
	}
	if running.LastStatus != "interrupted" || !strings.Contains(running.LastMessage, "Agent") || running.NextRun <= now.UnixMilli() {
		t.Fatalf("recovered running task = %#v", running)
	}
	complete, err := store.MaintenanceTask("complete")
	if err != nil || complete.LastStatus != "ok" {
		t.Fatalf("completed task changed = %#v, %v", complete, err)
	}
}

func TestRunMaintenanceTaskRejectsBusyServerBeforeStartingGoroutine(t *testing.T) {
	root := t.TempDir()
	task := MaintenanceTask{ID: "task-busy", ServerID: "srv-busy", Type: "backup", LastStatus: "waiting"}
	app := &App{
		store:      &Store{path: filepath.Join(root, "config.json"), config: AppConfig{MaintenanceTasks: []MaintenanceTask{task}}},
		operations: map[string]string{"srv-busy": "restore"},
	}
	if err := app.RunMaintenanceTask(task.ID); err == nil || err.Error() != "server is busy" {
		t.Fatalf("RunMaintenanceTask() error = %v", err)
	}
	stored, err := app.store.MaintenanceTask(task.ID)
	if err != nil || stored.LastStatus != "waiting" {
		t.Fatalf("busy task status changed = %#v, %v", stored, err)
	}
	if operation := app.currentOperation("srv-busy"); operation != "restore" {
		t.Fatalf("existing operation changed to %q", operation)
	}
}

func TestMarkMaintenanceTaskRunningIsPersisted(t *testing.T) {
	root := t.TempDir()
	task := MaintenanceTask{ID: "task-running", ServerID: "srv-a", Type: "backup", LastStatus: "waiting", LastMessage: "old"}
	store := &Store{path: filepath.Join(root, "config.json"), config: AppConfig{MaintenanceTasks: []MaintenanceTask{task}}}
	now := time.Unix(1234, 0)
	if err := store.MarkMaintenanceTaskRunning(task.ID, now); err != nil {
		t.Fatal(err)
	}
	stored, err := store.MaintenanceTask(task.ID)
	if err != nil || stored.LastStatus != "running" || stored.LastMessage != "" || stored.LastRun != now.UnixMilli() {
		t.Fatalf("running task = %#v, %v", stored, err)
	}
	if _, err := os.Stat(filepath.Join(root, "config.json")); err != nil {
		t.Fatalf("running state was not persisted: %v", err)
	}
}

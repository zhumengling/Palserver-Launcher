package main

import (
	"errors"
	"path/filepath"
	"testing"
)

func operationLockTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	instance := ServerInstance{ID: "srv-operation", Name: "Operation", RootPath: filepath.Join(root, "server")}
	return &App{
		store:      &Store{path: filepath.Join(root, "config.json"), config: AppConfig{Instances: []ServerInstance{instance}}},
		operations: map[string]string{},
	}
}

func TestBackupRestoreAndInstallRejectConflictingServerOperation(t *testing.T) {
	tests := []struct {
		name string
		run  func(*App) error
	}{
		{name: "backup", run: func(app *App) error { _, err := app.CreateBackup("srv-operation"); return err }},
		{name: "restore", run: func(app *App) error { return app.RestoreBackup("srv-operation", "unused") }},
		{name: "install", run: func(app *App) error { return app.InstallOrUpdateServer("srv-operation") }},
		{name: "duplicate", run: func(app *App) error { _, err := app.DuplicateInstance("srv-operation"); return err }},
		{name: "delete", run: func(app *App) error { return app.DeleteInstance("srv-operation", false) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := operationLockTestApp(t)
			app.operations["srv-operation"] = "update"
			if err := test.run(app); err == nil || err.Error() != "server is busy" {
				t.Fatalf("conflicting operation error = %v", err)
			}
			if operation := app.currentOperation("srv-operation"); operation != "update" {
				t.Fatalf("existing operation changed to %q", operation)
			}
		})
	}
}

func TestBackupOperationLockIsReleasedAfterFailure(t *testing.T) {
	app := operationLockTestApp(t)
	_, err := app.CreateBackup("srv-operation")
	if !errors.Is(err, ErrSaveDirectoryNotFound) {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if operation := app.currentOperation("srv-operation"); operation != "" {
		t.Fatalf("backup operation remained locked as %q", operation)
	}
}

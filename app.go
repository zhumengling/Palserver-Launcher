package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx                context.Context
	store              *Store
	processMu          sync.Mutex
	processTuningMu    sync.Mutex
	serverStartMu      sync.Mutex
	operationMu        sync.Mutex
	expectedStops      map[string]bool
	restartCancels     map[string]chan struct{}
	operations         map[string]string
	maintenanceCancel  chan struct{}
	guardianFailures   map[string]int
	guardianRestarts   map[string][]time.Time
	guardianLastCheck  map[string]time.Time
	guardianSuppressed map[string]bool
	serverModUpdateMu  sync.RWMutex
	serverModUpdates   map[string]nexusModInfo
	launcherUpdateMu   sync.Mutex
	launcherUpdating   bool
	frpProcesses       map[string]*exec.Cmd
	frpClaims          map[string]frpRuntimeClaim
}

func NewApp() *App {
	store, err := NewStore()
	if err != nil {
		panic(err)
	}
	return &App{
		store: store, expectedStops: map[string]bool{}, restartCancels: map[string]chan struct{}{}, operations: map[string]string{},
		guardianFailures: map[string]int{}, guardianRestarts: map[string][]time.Time{}, guardianLastCheck: map[string]time.Time{}, guardianSuppressed: map[string]bool{},
		serverModUpdates: map[string]nexusModInfo{}, frpProcesses: map[string]*exec.Cmd{}, frpClaims: map[string]frpRuntimeClaim{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startMaintenanceLoop()
	go a.startAutomaticFrpClients()
	go func() { _ = a.rebalanceServerProcesses() }()
}

func (a *App) shutdown(context.Context) {
	a.stopMaintenanceLoop()
	a.stopAllFrpClients()
}

func (a *App) GetConfig() AppConfig { return a.store.Snapshot() }

func (a *App) SaveInstance(instance ServerInstance) (ServerInstance, error) {
	if instance.Name == "" || instance.RootPath == "" {
		return ServerInstance{}, errors.New("name and root path are required")
	}
	if err := os.MkdirAll(instance.RootPath, 0o755); err != nil {
		return ServerInstance{}, err
	}
	instance = withDefaults(instance)
	if err := validateServerInstancePorts(instance, a.store.Snapshot().Instances); err != nil {
		return ServerInstance{}, err
	}
	if err := syncInstanceWorldSettings(instance); err != nil {
		return ServerInstance{}, fmt.Errorf("sync server settings: %w", err)
	}
	stored, err := a.store.Upsert(instance)
	if err != nil {
		return ServerInstance{}, err
	}
	go func() {
		if tuningErr := a.rebalanceServerProcesses(); tuningErr != nil {
			appendProcessTuningWarning(stored, tuningErr)
		}
	}()
	return stored, nil
}

func (a *App) DuplicateInstance(id string) (ServerInstance, error) {
	source, err := a.store.Find(id)
	if err != nil {
		return ServerInstance{}, err
	}
	status, _ := serverStatus(source)
	if status.Running {
		return ServerInstance{}, errors.New("stop the server before duplicating it")
	}
	instance := source
	instance.ID = ""
	instance.Name += " Copy"
	instance.RootPath = filepath.Join(filepath.Dir(instance.RootPath), filepath.Base(instance.RootPath)+"-copy")
	if _, statErr := os.Stat(instance.RootPath); statErr == nil {
		instance.RootPath += "-" + newID()[4:10]
	}
	instance.Executable = filepath.Join(instance.RootPath, "PalServer.exe")
	instance = assignAvailablePorts(instance, a.store.Snapshot().Instances)
	if err := duplicateInstanceFiles(source.RootPath, instance.RootPath); err != nil {
		return ServerInstance{}, err
	}
	return a.store.Upsert(instance)
}

func duplicateInstanceFiles(source, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		return errors.New("duplicate destination already exists")
	}
	return copyTree(source, destination)
}

func validateInstanceRemoval(running bool) error {
	if running {
		return errors.New("stop the server before removing it from the launcher")
	}
	return nil
}

func (a *App) DeleteInstance(id string, deleteFiles bool) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if err := validateInstanceRemoval(status.Running); err != nil {
		return err
	}
	_ = a.StopFrp(id)
	if deleteFiles {
		if err := os.RemoveAll(instance.RootPath); err != nil {
			return err
		}
		if base, dataErr := appDataDir(); dataErr == nil {
			_ = os.RemoveAll(filepath.Join(base, "backups", id))
		}
	}
	if err := a.store.DeleteServerData(id); err != nil {
		return err
	}
	a.operationMu.Lock()
	delete(a.operations, id)
	a.operationMu.Unlock()
	a.processMu.Lock()
	delete(a.expectedStops, id)
	if cancel, ok := a.restartCancels[id]; ok {
		close(cancel)
		delete(a.restartCancels, id)
	}
	delete(a.guardianFailures, id)
	delete(a.guardianRestarts, id)
	delete(a.guardianLastCheck, id)
	delete(a.guardianSuppressed, id)
	a.processMu.Unlock()
	go func() { _ = a.rebalanceServerProcesses() }()
	return nil
}

func (a *App) SelectInstance(id string) error { return a.store.Select(id) }

func (a *App) ChooseDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select server directory"})
}

func (a *App) ChooseFiles(title string) ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

func (a *App) OpenPath(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return errors.New("path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("path is not available: %w", err)
	}
	var args []string
	if info.IsDir() {
		args = []string{path}
	} else {
		args = []string{"/select," + path}
	}
	cmd := exec.Command("explorer.exe", args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open Explorer: %w", err)
	}
	return nil
}

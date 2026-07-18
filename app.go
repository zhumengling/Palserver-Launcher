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
)

type App struct {
	ctx                  context.Context
	store                *Store
	processMu            sync.Mutex
	processMonitorMu     sync.Mutex
	platformMu           sync.RWMutex
	statusMu             sync.RWMutex
	officialCacheMu      sync.Mutex
	processTuningMu      sync.Mutex
	serverStartMu        sync.Mutex
	extensionStageMu     sync.Mutex
	operationMu          sync.Mutex
	expectedStops        map[string]bool
	startingServers      map[string]bool
	restartCancels       map[string]chan struct{}
	operations           map[string]string
	maintenanceCancel    chan struct{}
	guardianFailures     map[string]int
	guardianRestarts     map[string][]time.Time
	guardianLastCheck    map[string]time.Time
	guardianSuppressed   map[string]bool
	serverModUpdateMu    sync.RWMutex
	serverModUpdates     map[string]nexusModInfo
	launcherUpdateMu     sync.Mutex
	webAuditMu           sync.Mutex
	launcherUpdating     bool
	frpProcesses         map[string]*exec.Cmd
	frpClaims            map[string]frpRuntimeClaim
	processMonitorCancel chan struct{}
	reportedPlatform     string
	agentAuthFile        string
	observedProcesses    map[string]observedServerProcess
	statusCache          map[string]cachedServerStatus
	officialCache        map[string]*officialCacheEntry
	events               appEventHub
	quit                 func()
}

func NewApp() *App {
	store, err := NewStore()
	if err != nil {
		panic(err)
	}
	return &App{
		store: store, expectedStops: map[string]bool{}, startingServers: map[string]bool{}, restartCancels: map[string]chan struct{}{}, operations: map[string]string{},
		guardianFailures: map[string]int{}, guardianRestarts: map[string][]time.Time{}, guardianLastCheck: map[string]time.Time{}, guardianSuppressed: map[string]bool{},
		serverModUpdates: map[string]nexusModInfo{}, frpProcesses: map[string]*exec.Cmd{}, frpClaims: map[string]frpRuntimeClaim{},
		observedProcesses: map[string]observedServerProcess{}, statusCache: map[string]cachedServerStatus{},
		officialCache: map[string]*officialCacheEntry{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if recovered, err := a.store.RecoverInterruptedMaintenanceTasks(time.Now()); err != nil {
		a.store.AddWarning("恢复维护任务状态失败：" + err.Error())
	} else if recovered > 0 {
		a.store.AddWarning(fmt.Sprintf("检测到 %d 个因 Agent 重启而中断的维护任务，请检查服务器状态后重试", recovered))
	}
	a.startMaintenanceLoop()
	a.startProcessMonitor()
	go a.startAutomaticFrpClients()
	go a.startAutomaticServers()
	go func() { _ = a.rebalanceServerProcesses() }()
}

func (a *App) shutdown(context.Context) {
	a.stopMaintenanceLoop()
	a.stopProcessMonitor()
	a.stopAllFrpClients()
}

func (a *App) GetConfig() AppConfig {
	config := a.store.Snapshot()
	for index := range config.Instances {
		config.Instances[index].EncryptedAdminPassword = ""
		config.Instances[index].EncryptedServerPassword = ""
	}
	config.StartupWarnings = a.store.Warnings()
	return config
}

func (a *App) SaveInstance(instance ServerInstance) (ServerInstance, error) {
	// Linux web clients receive no Agent-host paths. Preserve the internal
	// managed paths when they submit ordinary name/port/policy edits.
	if a.reportedPlatformName() == "linux" && strings.TrimSpace(instance.ID) != "" {
		if current, err := a.store.Find(instance.ID); err == nil {
			instance.RootPath = current.RootPath
			instance.Executable = current.Executable
			instance.SteamCMDPath = current.SteamCMDPath
		}
	}
	if instance.Name == "" || instance.RootPath == "" {
		return ServerInstance{}, errors.New("name and root path are required")
	}
	// Encrypted fields are internal persistence details. Public desktop/web
	// callers must provide plaintext through the password inputs so arbitrary
	// ciphertext cannot be injected into the configuration store.
	instance.EncryptedAdminPassword = ""
	instance.EncryptedServerPassword = ""
	if err := validateManagedServerRoot(instance.RootPath); err != nil {
		return ServerInstance{}, err
	}
	if err := os.MkdirAll(instance.RootPath, 0o755); err != nil {
		return ServerInstance{}, err
	}
	instance = withDefaults(instance)
	if err := validatePlatformServerExecutable(instance); err != nil {
		return ServerInstance{}, err
	}
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
	a.invalidateOfficialCache(stored.ID)
	go func() {
		if tuningErr := a.rebalanceServerProcesses(); tuningErr != nil {
			appendProcessTuningWarning(stored, tuningErr)
		}
	}()
	return stored, nil
}

func (a *App) DuplicateInstance(id string) (ServerInstance, error) {
	if !a.tryBeginOperation(id, "duplicate") {
		return ServerInstance{}, errors.New("server is busy")
	}
	defer a.endOperation(id)
	source, err := a.store.Find(id)
	if err != nil {
		return ServerInstance{}, err
	}
	if err := validateManagedServerRoot(source.RootPath); err != nil {
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
	instance.Executable = defaultServerExecutable(instance.RootPath)
	instance = assignAvailablePorts(instance, a.store.Snapshot().Instances)
	if err := validateManagedServerRoot(instance.RootPath); err != nil {
		return ServerInstance{}, err
	}
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
	if !a.tryBeginOperation(id, "delete") {
		return errors.New("server is busy")
	}
	defer a.endOperation(id)
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
		if err := validateManagedServerRoot(instance.RootPath); err != nil {
			return err
		}
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
	a.processMu.Lock()
	delete(a.expectedStops, id)
	delete(a.startingServers, id)
	if cancel, ok := a.restartCancels[id]; ok {
		close(cancel)
		delete(a.restartCancels, id)
	}
	delete(a.guardianFailures, id)
	delete(a.guardianRestarts, id)
	delete(a.guardianLastCheck, id)
	delete(a.guardianSuppressed, id)
	a.processMu.Unlock()
	a.statusMu.Lock()
	delete(a.statusCache, id)
	a.statusMu.Unlock()
	a.invalidateOfficialCache(id)
	go func() { _ = a.rebalanceServerProcesses() }()
	return nil
}

func (a *App) SelectInstance(id string) error { return a.store.Select(id) }

func (a *App) ChooseDirectory() (string, error) {
	return chooseDirectoryPlatform(a, "Select server directory")
}

func (a *App) ChooseFiles(title string) ([]string, error) {
	return chooseFilesPlatform(a, title)
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
	return openPathPlatform(path, info.IsDir())
}

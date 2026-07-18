package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeProcessRuntime struct {
	mu      sync.Mutex
	process serverProcessSnapshot
	found   bool
	calls   int
}

func (runtime *fakeProcessRuntime) FindServerProcess(ServerInstance) (serverProcessSnapshot, bool, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.calls++
	return runtime.process, runtime.found, nil
}

func (runtime *fakeProcessRuntime) callCount() int {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.calls
}

func (runtime *fakeProcessRuntime) HostResources() (HostResources, error) {
	return HostResources{}, nil
}

func (runtime *fakeProcessRuntime) TCPListenerOwner(int) (int, bool, error) {
	return 0, false, nil
}

func (runtime *fakeProcessRuntime) set(process serverProcessSnapshot, found bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.process, runtime.found = process, found
}

func TestProcessMonitorAdoptsAndDetectsExternallyStartedServer(t *testing.T) {
	root := t.TempDir()
	instance := ServerInstance{ID: "server-1", Name: "Server", RootPath: root, Executable: filepath.Join(root, "PalServer.exe")}
	app := &App{
		store:         &Store{path: filepath.Join(root, "config.json"), config: AppConfig{Instances: []ServerInstance{instance}}},
		expectedStops: map[string]bool{}, restartCancels: map[string]chan struct{}{}, guardianSuppressed: map[string]bool{}, observedProcesses: map[string]observedServerProcess{},
	}
	fake := &fakeProcessRuntime{process: serverProcessSnapshot{PID: 1234, Path: filepath.Join(root, "Pal", "Binaries", "Win64", "PalServer-Win64-Shipping.exe")}, found: true}
	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = fake
	defer func() { defaultProcessRuntime = previousRuntime }()

	app.pollServerProcesses()
	if observed := app.observedProcesses[instance.ID]; observed.PID != 1234 || observed.Watcher != "monitor" {
		t.Fatalf("external process was not adopted: %#v", observed)
	}
	fake.set(serverProcessSnapshot{}, false)
	app.pollServerProcesses()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := readJSONFile[pluginCrashRecord](launcherCompatibilityPath(instance, "plugin-crash.json")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("external process exit was not recorded")
}

func TestGetStatusUsesFreshProcessMonitorCache(t *testing.T) {
	root := t.TempDir()
	instance := ServerInstance{ID: "server-cache", Name: "Server", RootPath: root, Executable: filepath.Join(root, "PalServer.exe")}
	app := &App{
		store:         &Store{path: filepath.Join(root, "config.json"), config: AppConfig{Instances: []ServerInstance{instance}}},
		expectedStops: map[string]bool{}, startingServers: map[string]bool{}, operations: map[string]string{}, statusCache: map[string]cachedServerStatus{},
	}
	fake := &fakeProcessRuntime{}
	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = fake
	defer func() { defaultProcessRuntime = previousRuntime }()
	app.cacheServerStatus(instance.ID, RuntimeStatus{Running: true, PID: 4321, RESTAvailable: true}, time.Now())
	status, err := app.GetStatus(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fake.callCount() != 0 || status.State != "running" || status.PID != 4321 {
		t.Fatalf("cached status = %#v, process scans=%d", status, fake.callCount())
	}
}

func TestProcessMonitorPublishesAndCachesServerStatus(t *testing.T) {
	root := t.TempDir()
	instance := ServerInstance{ID: "server-event", Name: "Server", RootPath: root, Executable: filepath.Join(root, "PalServer.exe")}
	app := &App{
		store:         &Store{path: filepath.Join(root, "config.json"), config: AppConfig{Instances: []ServerInstance{instance}}},
		expectedStops: map[string]bool{}, startingServers: map[string]bool{}, operations: map[string]string{}, statusCache: map[string]cachedServerStatus{}, observedProcesses: map[string]observedServerProcess{},
	}
	fake := &fakeProcessRuntime{}
	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = fake
	defer func() { defaultProcessRuntime = previousRuntime }()
	_, events := app.events.subscribe()
	app.pollServerProcesses()
	select {
	case event := <-events:
		if event.Name != "server:status" || len(event.Args) != 2 || event.Args[0] != instance.ID {
			t.Fatalf("status event = %#v", event)
		}
		status, ok := event.Args[1].(RuntimeStatus)
		if !ok || status.State != "stopped" {
			t.Fatalf("event status = %#v", event.Args[1])
		}
	case <-time.After(time.Second):
		t.Fatal("process monitor did not publish a status event")
	}
	if cached, ok := app.cachedServerStatus(instance.ID, time.Second); !ok || cached.Running {
		t.Fatalf("cached monitor status = %#v, %v", cached, ok)
	}
}

//go:build !linux

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestPerformanceLaunchArgsDefaultToPalworldOnePointZeroBehavior(t *testing.T) {
	instance := withDefaults(ServerInstance{PerformanceMode: true, WorkerThreads: 8})
	args := performanceLaunchArgs(instance)
	for _, unwanted := range []string{"-useperfthreads", "-NoAsyncLoadingThread", "-UseMultithreadForDS", "-NumberOfWorkerThreadsServer=8"} {
		if containsString(args, unwanted) {
			t.Fatalf("default Palworld 1.0 launch args contain %q: %#v", unwanted, args)
		}
	}
}

func TestPerformanceLaunchArgsRequireMultithreadFlagsForWorkerThreads(t *testing.T) {
	instance := withDefaults(ServerInstance{WorkerThreads: 8})
	args := performanceLaunchArgs(instance)
	if containsString(args, "-NumberOfWorkerThreadsServer=8") {
		t.Fatalf("worker thread argument was emitted without its required multithread flags: %#v", args)
	}
}

func TestNewServersKeep120FPSButDisableLegacyFlags(t *testing.T) {
	instance := buildManagedInstanceAt(t.TempDir(), "Performance Server", filepath.Join(t.TempDir(), "server"))
	if !instance.PerformanceMode {
		t.Fatal("new server disabled the default 120 FPS Engine.ini profile")
	}
	if instance.LegacyPerformanceFlags {
		t.Fatal("new Palworld 1.0 server enabled legacy performance flags")
	}
	managed := managedPerformanceEngineSettings()
	for _, expected := range []string{"NetServerMaxTickRate=120", "FixedFrameRate=120.000000", "NetClientTicksPerSecond=120"} {
		if !strings.Contains(managed, expected) {
			t.Fatalf("managed 120 FPS profile is missing %q", expected)
		}
	}
}

func TestPerformanceLaunchArgsSupportExplicitLegacyWorkerThreads(t *testing.T) {
	instance := withDefaults(ServerInstance{LegacyPerformanceFlags: true, WorkerThreads: 6})
	args := performanceLaunchArgs(instance)
	for _, expected := range []string{"-useperfthreads", "-NoAsyncLoadingThread", "-UseMultithreadForDS", "-NumberOfWorkerThreadsServer=6"} {
		if !containsString(args, expected) {
			t.Fatalf("legacy performance args are missing %q: %#v", expected, args)
		}
	}
}

func TestApplyPerformanceConfigPreservesUserEngineSettings(t *testing.T) {
	root := t.TempDir()
	instance := withDefaults(ServerInstance{RootPath: root, PerformanceMode: true})
	path := filepath.Join(root, "Pal", "Saved", "Config", "WindowsServer", "Engine.ini")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "[Custom.Server]\nKeepThisValue=42\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applyPerformanceConfig(instance); err != nil {
		t.Fatal(err)
	}
	configured, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configured), original) || !strings.Contains(string(configured), "NetServerMaxTickRate=120") {
		t.Fatalf("managed config did not preserve custom content and 120 FPS defaults:\n%s", configured)
	}
	backup, err := os.ReadFile(path + performanceEngineBackupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Fatalf("Engine.ini backup = %q, want %q", backup, original)
	}

	first := string(configured)
	if err := applyPerformanceConfig(instance); err != nil {
		t.Fatal(err)
	}
	configured, _ = os.ReadFile(path)
	if string(configured) != first {
		t.Fatal("applying managed Engine.ini settings twice was not idempotent")
	}

	instance.PerformanceMode = false
	if err := applyPerformanceConfig(instance); err != nil {
		t.Fatal(err)
	}
	configured, _ = os.ReadFile(path)
	if string(configured) != original {
		t.Fatalf("disabling managed settings changed user Engine.ini:\n%s", configured)
	}
}

func TestApplyPerformanceConfigMigratesLegacyLauncherTemplate(t *testing.T) {
	root := t.TempDir()
	instance := withDefaults(ServerInstance{RootPath: root, PerformanceMode: true})
	path := filepath.Join(root, "Pal", "Saved", "Config", "WindowsServer", "Engine.ini")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, performanceEngineConfig(true), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applyPerformanceConfig(instance); err != nil {
		t.Fatal(err)
	}
	configured, _ := os.ReadFile(path)
	if count := strings.Count(string(configured), "NetServerMaxTickRate=120"); count != 1 {
		t.Fatalf("legacy Engine.ini migration left %d copies of managed settings", count)
	}
}

func TestAllocateServerCPUSetsKeepsPhysicalCoresTogetherAcrossGroups(t *testing.T) {
	sets := []systemCPUSet{
		{ID: 0, Group: 0, CoreIndex: 0, LogicalProcessorIndex: 0},
		{ID: 1, Group: 0, CoreIndex: 0, LogicalProcessorIndex: 1},
		{ID: 2, Group: 0, CoreIndex: 1, LogicalProcessorIndex: 2},
		{ID: 3, Group: 0, CoreIndex: 1, LogicalProcessorIndex: 3},
		{ID: 64, Group: 1, CoreIndex: 0, LogicalProcessorIndex: 0},
		{ID: 65, Group: 1, CoreIndex: 0, LogicalProcessorIndex: 1},
		{ID: 66, Group: 1, CoreIndex: 1, LogicalProcessorIndex: 2},
		{ID: 67, Group: 1, CoreIndex: 1, LogicalProcessorIndex: 3},
	}
	allocated := allocateServerCPUSets([]string{"server-b", "server-a"}, sets)
	if !reflect.DeepEqual(allocated["server-a"], []uint32{0, 1, 64, 65}) {
		t.Fatalf("server-a CPU sets = %#v", allocated["server-a"])
	}
	if !reflect.DeepEqual(allocated["server-b"], []uint32{2, 3, 66, 67}) {
		t.Fatalf("server-b CPU sets = %#v", allocated["server-b"])
	}
	all := append(append([]uint32{}, allocated["server-a"]...), allocated["server-b"]...)
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	if !reflect.DeepEqual(all, []uint32{0, 1, 2, 3, 64, 65, 66, 67}) {
		t.Fatalf("CPU set allocation is incomplete or overlapping: %#v", all)
	}
}

func TestSystemCPUSetsExposeWindowsProcessorGroups(t *testing.T) {
	sets, err := systemCPUSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) == 0 {
		t.Fatal("Windows returned no CPU Sets")
	}
	seen := make(map[uint32]bool, len(sets))
	for _, cpuSet := range sets {
		if seen[cpuSet.ID] {
			t.Fatalf("duplicate CPU Set ID %d", cpuSet.ID)
		}
		seen[cpuSet.ID] = true
	}
}

func TestApplyServerProcessTuningUsesWindowsCPUSetAPI(t *testing.T) {
	sets, err := systemCPUSets()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("powershell", "-NoProfile", "-Command", "Start-Sleep -Seconds 10")
	command.SysProcAttr = hiddenServerSysProcAttr()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	}()
	time.Sleep(100 * time.Millisecond)
	if err := applyServerProcessTuning(command.Process.Pid, "above_normal", []uint32{sets[0].ID}, true); err != nil {
		t.Fatal(err)
	}
	if err := applyServerProcessTuning(command.Process.Pid, "normal", nil, true); err != nil {
		t.Fatalf("clearing process CPU Sets failed: %v", err)
	}
}

func TestPerformanceDefaultsUseAboveNormalPriorityAndAutomaticIsolation(t *testing.T) {
	instance := withDefaults(ServerInstance{})
	if instance.ProcessPriority != "above_normal" || instance.CPUAffinityMode != "auto" {
		t.Fatalf("performance defaults = priority %q, affinity %q", instance.ProcessPriority, instance.CPUAffinityMode)
	}
}

func TestStoreMigratesExistingServersToPalworldOnePointZeroDefaults(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	directory := filepath.Join(localAppData, "palserver-launcher")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyConfig := AppConfig{Instances: []ServerInstance{{ID: "server-1", Name: "Existing", RootPath: filepath.Join(t.TempDir(), "server"), PerformanceMode: true}}}
	data, _ := json.Marshal(legacyConfig)
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	instance := store.Snapshot().Instances[0]
	if !instance.PerformanceMode || instance.LegacyPerformanceFlags || instance.ProcessPriority != "above_normal" || instance.CPUAffinityMode != "auto" {
		t.Fatalf("migrated performance settings = %#v", instance)
	}
	persisted, err := os.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), `"processPriority": "above_normal"`) || !strings.Contains(string(persisted), `"cpuAffinityMode": "auto"`) {
		t.Fatalf("performance migration was not persisted:\n%s", persisted)
	}
}

func TestValidateInstanceRemovalRejectsRunningServer(t *testing.T) {
	if err := validateInstanceRemoval(true); err == nil {
		t.Fatal("running server instance was allowed to be removed")
	}
	if err := validateInstanceRemoval(false); err != nil {
		t.Fatalf("stopped server instance cannot be removed: %v", err)
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

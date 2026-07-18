//go:build linux

package main

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func copyLinuxTestExecutable(t *testing.T, source, destination string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func freeLinuxTestPorts(t *testing.T, count int) []int {
	t.Helper()
	listeners := make([]net.Listener, 0, count)
	ports := make([]int, 0, count)
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	for _, listener := range listeners {
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return ports
}

func waitForLinuxServerState(t *testing.T, instance ServerInstance, running bool) RuntimeStatus {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		status, err := serverStatus(instance)
		if err == nil && status.Running == running {
			return status
		}
		time.Sleep(100 * time.Millisecond)
	}
	status, err := serverStatus(instance)
	t.Fatalf("server running=%v, want %v, err=%v", status.Running, running, err)
	return RuntimeStatus{}
}

func TestLinuxPalServerScriptLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", filepath.Join(root, "launcher-data"))
	shipping := filepath.Join(root, "server", "Pal", "Binaries", "Linux", "PalServer-Linux-Shipping")
	copyLinuxTestExecutable(t, "/bin/sleep", shipping)
	launcher := filepath.Join(root, "server", "PalServer.sh")
	script := "#!/bin/sh\nexec \"$(dirname \"$0\")/Pal/Binaries/Linux/PalServer-Linux-Shipping\" 30\n"
	if err := os.WriteFile(launcher, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ports := freeLinuxTestPorts(t, 4)
	instance := withDefaults(ServerInstance{
		ID: "srv-linux-lifecycle", Name: "Linux lifecycle", RootPath: filepath.Dir(launcher), Executable: launcher,
		PublicPort: ports[0], QueryPort: ports[1], RCONPort: ports[2], RESTPort: ports[3],
		ProcessPriority: "normal", CPUAffinityMode: "manual",
	})
	app := NewApp()
	if _, err := app.store.Upsert(instance); err != nil {
		t.Fatal(err)
	}
	if err := app.StartServer(instance.ID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if status, _ := serverStatus(instance); status.Running {
			_ = terminateProcessTree(status.PID, true)
		}
	}()
	running := waitForLinuxServerState(t, instance, true)
	if running.PID <= 0 || filepath.Base(runningProcessPath(t, running.PID)) != "PalServer-Linux-Shipping" {
		t.Fatalf("unexpected Linux server process: pid=%d path=%q", running.PID, runningProcessPath(t, running.PID))
	}
	if err := app.StopServer(instance.ID); err != nil {
		t.Fatal(err)
	}
	waitForLinuxServerState(t, instance, false)
}

func TestLinuxAgentAdoptsAndStopsExternallyStartedServerWithoutKillingSharedGroup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", filepath.Join(root, "launcher-data"))
	shipping := filepath.Join(root, "server", "Pal", "Binaries", "Linux", "PalServer-Linux-Shipping")
	copyLinuxTestExecutable(t, "/bin/sleep", shipping)
	launcher := filepath.Join(root, "server", "PalServer.sh")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(shipping, "30")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	processGroup, err := syscall.Getpgid(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if processGroup == command.Process.Pid {
		t.Skip("test child unexpectedly became its own process group leader")
	}
	ports := freeLinuxTestPorts(t, 4)
	instance := withDefaults(ServerInstance{
		ID: "srv-linux-adopted", Name: "Linux adopted", RootPath: filepath.Dir(launcher), Executable: launcher,
		PublicPort: ports[0], QueryPort: ports[1], RCONPort: ports[2], RESTPort: ports[3], ProcessPriority: "normal", CPUAffinityMode: "manual",
	})
	app := NewApp()
	if _, err := app.store.Upsert(instance); err != nil {
		t.Fatal(err)
	}
	waitForLinuxServerState(t, instance, true)
	app.pollServerProcesses()
	app.processMonitorMu.Lock()
	observed := app.observedProcesses[instance.ID]
	app.processMonitorMu.Unlock()
	if observed.PID != command.Process.Pid || observed.Watcher != "monitor" {
		t.Fatalf("externally started server was not adopted: %#v", observed)
	}
	if err := app.StopServer(instance.ID); err != nil {
		t.Fatal(err)
	}
	waitForLinuxServerState(t, instance, false)
	if err := command.Wait(); err == nil {
		t.Fatal("externally started server exited without the expected termination signal")
	}
}

func runningProcessPath(t *testing.T, pid int) string {
	t.Helper()
	path, err := os.Readlink(filepath.Join("/proc", fmtInt(pid), "exe"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func fmtInt(value int) string {
	return strconv.Itoa(value)
}

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestServerProcessPathMatchesOnlyConfiguredServerTree(t *testing.T) {
	instance := ServerInstance{Executable: filepath.Join(`D:\Servers\PalA`, "PalServer.exe")}
	if !serverProcessPathMatches(instance, filepath.Join(`D:\Servers\PalA`, "Pal", "Binaries", "Win64", "PalServer-Win64-Shipping.exe")) {
		t.Fatal("shipping process inside the configured server was not matched")
	}
	if serverProcessPathMatches(instance, filepath.Join(`D:\Servers\PalABackup`, "PalServer.exe")) {
		t.Fatal("sibling server path was matched")
	}
}

func TestWindowsProcessRuntimeFindsTCPListenerOwner(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	owner, found, err := newPlatformProcessRuntime().TCPListenerOwner(port)
	if err != nil {
		t.Fatal(err)
	}
	if !found || owner != os.Getpid() {
		t.Fatalf("listener owner = %d, found=%v, want PID %d", owner, found, os.Getpid())
	}
}

func TestWindowsProcessRuntimeReadsHostResourcesWithoutPowerShell(t *testing.T) {
	runtime := newPlatformProcessRuntime()
	resources, err := runtime.HostResources()
	if err != nil {
		t.Fatal(err)
	}
	if resources.MemoryTotalMB <= 0 || resources.MemoryUsedMB < 0 || resources.MemoryPercent < 0 || resources.MemoryPercent > 100 {
		t.Fatalf("invalid host resources: %#v", resources)
	}
}

func TestWindowsProcessRuntimeDoesNotMatchUnrelatedServerRoot(t *testing.T) {
	runtime := newPlatformProcessRuntime()
	instance := ServerInstance{Executable: filepath.Join(t.TempDir(), "PalServer.exe")}
	if process, found, err := runtime.FindServerProcess(instance); err != nil || found {
		t.Fatalf("unrelated server process match: found=%v process=%#v err=%v", found, process, err)
	}
}

//go:build linux

package main

import (
	"net"
	"os"
	"testing"
)

func TestParseLinuxProcessStat(t *testing.T) {
	// pid (comm with spaces) state ... utime=11, stime=7, starttime=2200
	data := "42 (PalServer Linux) S 1 2 3 4 5 6 7 8 9 10 11 7 0 0 0 0 0 0 2200 0 0"
	cpu, started, err := parseLinuxProcessStat(data)
	if err != nil {
		t.Fatal(err)
	}
	if cpu != 0.18 || started != 2200 {
		t.Fatalf("cpu=%v started=%d", cpu, started)
	}
}

func TestLinuxProcessRuntimeReadsHostResources(t *testing.T) {
	resources, err := newPlatformProcessRuntime().HostResources()
	if err != nil {
		t.Fatal(err)
	}
	if resources.MemoryTotalMB <= 0 || resources.MemoryUsedMB < 0 || resources.MemoryPercent < 0 || resources.MemoryPercent > 100 {
		t.Fatalf("invalid Linux resources: %#v", resources)
	}
}

func TestLinuxProcessRuntimeFindsTCPListenerOwner(t *testing.T) {
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
		t.Fatalf("owner=%d found=%v want=%d", owner, found, os.Getpid())
	}
}

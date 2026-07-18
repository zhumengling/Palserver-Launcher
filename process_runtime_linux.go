//go:build linux

package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const linuxClockTicks = 100.0

type linuxProcessRuntime struct {
	cpuMu      sync.Mutex
	lastIdle   uint64
	lastTotal  uint64
	lastCPU    float64
	bootTime   time.Time
	bootTimeMu sync.Mutex
	processCPU processCPUSampler
}

func newPlatformProcessRuntime() processRuntime { return &linuxProcessRuntime{} }

func hiddenServerSysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func newHiddenPowerShell(query string) *exec.Cmd { return exec.Command("sh", "-c", query) }

func linuxProcessCandidate(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.Contains(name, "palserver") && (strings.Contains(name, "shipping") || name == "palserver.sh")
}

func parseLinuxProcessStat(data string) (cpuSeconds float64, startedTicks uint64, err error) {
	closing := strings.LastIndex(data, ")")
	if closing < 0 || closing+2 >= len(data) {
		return 0, 0, errors.New("invalid /proc stat")
	}
	fields := strings.Fields(data[closing+2:])
	// Fields begin at process state (field 3); utime=14, stime=15, starttime=22.
	if len(fields) <= 19 {
		return 0, 0, errors.New("truncated /proc stat")
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	startedTicks, err = strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return float64(utime+stime) / linuxClockTicks, startedTicks, nil
}

func (runtime *linuxProcessRuntime) linuxBootTime() time.Time {
	runtime.bootTimeMu.Lock()
	defer runtime.bootTimeMu.Unlock()
	if !runtime.bootTime.IsZero() {
		return runtime.bootTime
	}
	file, err := os.Open("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "btime" {
			seconds, _ := strconv.ParseInt(fields[1], 10, 64)
			runtime.bootTime = time.Unix(seconds, 0)
			break
		}
	}
	return runtime.bootTime
}

func linuxProcessSnapshot(runtime *linuxProcessRuntime, pid int, path string) serverProcessSnapshot {
	statData, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	cpuSeconds, startedTicks, _ := parseLinuxProcessStat(string(statData))
	startedAt := time.Time{}
	if boot := runtime.linuxBootTime(); !boot.IsZero() && startedTicks > 0 {
		startedAt = boot.Add(time.Duration(float64(startedTicks) / linuxClockTicks * float64(time.Second)))
	}
	memoryMB := 0.0
	if statm, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "statm")); err == nil {
		fields := strings.Fields(string(statm))
		if len(fields) > 1 {
			residentPages, _ := strconv.ParseUint(fields[1], 10, 64)
			memoryMB = float64(residentPages*uint64(os.Getpagesize())) / 1024 / 1024
		}
	}
	return serverProcessSnapshot{PID: pid, Path: path, CPUSeconds: cpuSeconds, CPUPercent: runtime.processCPU.sample(pid, cpuSeconds, time.Now()), MemoryMB: memoryMB, StartedAt: startedAt}
}

func (runtime *linuxProcessRuntime) FindServerProcess(instance ServerInstance) (serverProcessSnapshot, bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return serverProcessSnapshot{}, false, err
	}
	best := serverProcessSnapshot{}
	bestScore := 0
	for _, entry := range entries {
		pid, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil || !entry.IsDir() {
			continue
		}
		executable, readErr := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if readErr != nil {
			continue
		}
		executable = strings.TrimSuffix(executable, " (deleted)")
		if !serverProcessPathMatches(instance, executable) || !linuxProcessCandidate(executable) {
			continue
		}
		score := 1
		if strings.Contains(strings.ToLower(filepath.Base(executable)), "shipping") {
			score = 3
		}
		if score > bestScore {
			best = linuxProcessSnapshot(runtime, pid, executable)
			bestScore = score
		}
	}
	return best, bestScore > 0, nil
}

func readLinuxCPUStat() (idle, total uint64, err error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0, 0, errors.New("/proc/stat is empty")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, errors.New("invalid CPU line in /proc/stat")
	}
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, parseErr := strconv.ParseUint(field, 10, 64)
		if parseErr != nil {
			return 0, 0, parseErr
		}
		values = append(values, value)
		total += value
	}
	idle = values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return idle, total, nil
}

func (runtime *linuxProcessRuntime) HostResources() (HostResources, error) {
	idle, total, err := readLinuxCPUStat()
	if err != nil {
		return HostResources{}, err
	}
	runtime.cpuMu.Lock()
	if runtime.lastTotal != 0 && total > runtime.lastTotal {
		totalDelta := total - runtime.lastTotal
		idleDelta := idle - runtime.lastIdle
		if idleDelta <= totalDelta {
			runtime.lastCPU = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
		}
	}
	runtime.lastIdle, runtime.lastTotal = idle, total
	cpuPercent := runtime.lastCPU
	runtime.cpuMu.Unlock()

	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return HostResources{}, err
	}
	defer file.Close()
	var totalKB, availableKB float64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, _ := strconv.ParseFloat(fields[1], 64)
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			totalKB = value
		case "MemAvailable":
			availableKB = value
		}
	}
	if totalKB <= 0 {
		return HostResources{}, errors.New("Linux memory total is unavailable")
	}
	totalMB, availableMB := totalKB/1024, availableKB/1024
	return HostResources{CPUPercent: cpuPercent, MemoryPercent: memoryUsagePercent(totalMB, availableMB), MemoryUsedMB: totalMB - availableMB, MemoryTotalMB: totalMB}, nil
}

func listenerSocketInode(port int) (string, bool, error) {
	file, err := os.Open("/proc/net/tcp")
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if scanner.Scan() { // header
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[3] != "0A" {
			continue
		}
		address := strings.Split(fields[1], ":")
		if len(address) != 2 {
			continue
		}
		parsedPort, _ := strconv.ParseInt(address[1], 16, 32)
		if int(parsedPort) == port {
			return fields[9], true, nil
		}
	}
	return "", false, scanner.Err()
}

func (runtime *linuxProcessRuntime) TCPListenerOwner(port int) (int, bool, error) {
	if port < 1 || port > 65535 {
		return 0, false, fmt.Errorf("invalid TCP port %d", port)
	}
	inode, found, err := listenerSocketInode(port)
	if err != nil || !found {
		return 0, found, err
	}
	wanted := "socket:[" + inode + "]"
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, false, err
	}
	for _, entry := range entries {
		pid, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil {
			continue
		}
		fds, readErr := os.ReadDir(filepath.Join("/proc", entry.Name(), "fd"))
		if readErr != nil {
			continue
		}
		for _, fd := range fds {
			target, linkErr := os.Readlink(filepath.Join("/proc", entry.Name(), "fd", fd.Name()))
			if linkErr == nil && target == wanted {
				return pid, true, nil
			}
		}
	}
	return 0, false, nil
}

func terminateProcessTree(pid int, force bool) error {
	if pid <= 0 {
		return errors.New("invalid process id")
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	group, err := syscall.Getpgid(pid)
	// Processes launched by this Agent use Setpgid and therefore lead their
	// own process group (PGID == PID). An imported or manually launched server
	// may share a shell/systemd process group; killing that whole group could
	// terminate unrelated processes, including the management session.
	if err == nil && group == pid {
		if killErr := syscall.Kill(-group, signal); killErr == nil || errors.Is(killErr, syscall.ESRCH) {
			return nil
		}
	}
	if err := syscall.Kill(pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func localListenerAddress(port int) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) }

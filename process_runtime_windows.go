package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const processPathBufferLength = 32768

var (
	kernel32ProcessRuntime   = windows.NewLazySystemDLL("kernel32.dll")
	psapiProcessRuntime      = windows.NewLazySystemDLL("psapi.dll")
	iphlpapiProcessRuntime   = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetProcessMemoryInfo = psapiProcessRuntime.NewProc("GetProcessMemoryInfo")
	procGlobalMemoryStatusEx = kernel32ProcessRuntime.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = kernel32ProcessRuntime.NewProc("GetSystemTimes")
	procGetExtendedTcpTable  = iphlpapiProcessRuntime.NewProc("GetExtendedTcpTable")
)

type windowsProcessRuntime struct {
	cpuMu      sync.Mutex
	lastIdle   uint64
	lastKernel uint64
	lastUser   uint64
	lastCPU    float64
	processCPU processCPUSampler
}

const (
	windowsAFInet                  = 2
	tcpTableOwnerPIDListener       = 3
	mibTCPStateListen              = 2
	mibTCPRowOwnerPIDSize          = 24
	windowsErrorInsufficientBuffer = 122
)

type processMemoryCounters struct {
	Size                       uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type memoryStatusEx struct {
	Length            uint32
	MemoryLoad        uint32
	TotalPhysical     uint64
	AvailablePhysical uint64
	TotalPageFile     uint64
	AvailablePageFile uint64
	TotalVirtual      uint64
	AvailableVirtual  uint64
	AvailableExtended uint64
}

func newPlatformProcessRuntime() processRuntime { return &windowsProcessRuntime{} }

func hiddenServerSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW | syscall.CREATE_NEW_PROCESS_GROUP, HideWindow: true}
}

// Kept for infrequent repair and diagnostic commands. Runtime status and host
// metrics must use the native processRuntime implementation above.
func newHiddenPowerShell(query string) *exec.Cmd {
	command := exec.Command("powershell", "-NoProfile", "-Command", query)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, HideWindow: true}
	return command
}

func filetimeTicks(value windows.Filetime) uint64 {
	return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
}

func queryProcessPath(handle windows.Handle) (string, error) {
	buffer := make([]uint16, processPathBufferLength)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return filepath.Clean(windows.UTF16ToString(buffer[:size])), nil
}

func queryProcessMemory(handle windows.Handle) float64 {
	counters := processMemoryCounters{Size: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	result, _, _ := procGetProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.Size),
	)
	if result == 0 {
		return 0
	}
	return float64(counters.WorkingSetSize) / 1024 / 1024
}

func queryProcessTimes(handle windows.Handle) (float64, time.Time) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return 0, time.Time{}
	}
	cpuSeconds := float64(filetimeTicks(kernel)+filetimeTicks(user)) / 10_000_000
	return cpuSeconds, time.Unix(0, created.Nanoseconds())
}

func serverProcessCandidateScore(name, path string) int {
	base := strings.ToLower(filepath.Base(path))
	if base == "palserver-win64-shipping.exe" || base == "palserver-win64-shipping-cmd.exe" {
		return 3
	}
	if strings.EqualFold(name, "PalServer-Win64-Shipping.exe") || strings.EqualFold(name, "PalServer-Win64-Shipping-Cmd.exe") {
		return 2
	}
	return 1
}

func (runtime *windowsProcessRuntime) FindServerProcess(instance ServerInstance) (serverProcessSnapshot, bool, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return serverProcessSnapshot{}, false, fmt.Errorf("create process snapshot: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return serverProcessSnapshot{}, false, fmt.Errorf("enumerate processes: %w", err)
	}
	best := serverProcessSnapshot{}
	bestScore := 0
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, "PalServer.exe") || strings.EqualFold(name, "PalServer-Win64-Shipping.exe") || strings.EqualFold(name, "PalServer-Win64-Shipping-Cmd.exe") {
			access := uint32(windows.PROCESS_QUERY_INFORMATION | windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.PROCESS_VM_READ)
			handle, openErr := windows.OpenProcess(access, false, entry.ProcessID)
			if openErr != nil {
				handle, openErr = windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
			}
			if openErr == nil {
				path, pathErr := queryProcessPath(handle)
				if pathErr == nil && serverProcessPathMatches(instance, path) {
					score := serverProcessCandidateScore(name, path)
					if score > bestScore {
						cpuSeconds, startedAt := queryProcessTimes(handle)
						sampledAt := time.Now()
						best = serverProcessSnapshot{PID: int(entry.ProcessID), Path: path, CPUSeconds: cpuSeconds, CPUPercent: runtime.processCPU.sample(int(entry.ProcessID), cpuSeconds, sampledAt), MemoryMB: queryProcessMemory(handle), StartedAt: startedAt}
						bestScore = score
					}
				}
				windows.CloseHandle(handle)
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
				break
			}
			return serverProcessSnapshot{}, false, fmt.Errorf("enumerate processes: %w", err)
		}
	}
	return best, bestScore > 0, nil
}

func (runtime *windowsProcessRuntime) hostCPUPercent() float64 {
	var idle, kernel, user windows.Filetime
	result, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if result == 0 {
		return runtime.lastCPU
	}
	idleTicks, kernelTicks, userTicks := filetimeTicks(idle), filetimeTicks(kernel), filetimeTicks(user)
	runtime.cpuMu.Lock()
	defer runtime.cpuMu.Unlock()
	if runtime.lastKernel != 0 || runtime.lastUser != 0 {
		total := (kernelTicks - runtime.lastKernel) + (userTicks - runtime.lastUser)
		idleDelta := idleTicks - runtime.lastIdle
		if total > 0 && idleDelta <= total {
			runtime.lastCPU = float64(total-idleDelta) / float64(total) * 100
		}
	}
	runtime.lastIdle, runtime.lastKernel, runtime.lastUser = idleTicks, kernelTicks, userTicks
	return runtime.lastCPU
}

func (runtime *windowsProcessRuntime) HostResources() (HostResources, error) {
	memory := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	result, _, callErr := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memory)))
	if result == 0 {
		return HostResources{}, fmt.Errorf("query Windows memory status: %w", callErr)
	}
	totalMB := float64(memory.TotalPhysical) / 1024 / 1024
	availableMB := float64(memory.AvailablePhysical) / 1024 / 1024
	return HostResources{
		CPUPercent: runtime.hostCPUPercent(), MemoryPercent: memoryUsagePercent(totalMB, availableMB),
		MemoryUsedMB: totalMB - availableMB, MemoryTotalMB: totalMB,
	}, nil
}

func (runtime *windowsProcessRuntime) TCPListenerOwner(port int) (int, bool, error) {
	if port < 1 || port > 65535 {
		return 0, false, fmt.Errorf("invalid TCP port %d", port)
	}
	var size uint32
	result, _, callErr := procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, windowsAFInet, tcpTableOwnerPIDListener, 0)
	if result != windowsErrorInsufficientBuffer || size < 4 {
		return 0, false, fmt.Errorf("query TCP table size: result %d: %w", result, callErr)
	}
	buffer := make([]byte, size)
	result, _, callErr = procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), 0, windowsAFInet, tcpTableOwnerPIDListener, 0)
	if result != 0 {
		return 0, false, fmt.Errorf("query TCP listeners: result %d: %w", result, callErr)
	}
	rowCount := int(binary.LittleEndian.Uint32(buffer[:4]))
	for index := 0; index < rowCount; index++ {
		offset := 4 + index*mibTCPRowOwnerPIDSize
		if offset+mibTCPRowOwnerPIDSize > len(buffer) {
			return 0, false, errors.New("Windows returned a truncated TCP listener table")
		}
		state := binary.LittleEndian.Uint32(buffer[offset : offset+4])
		localPort := int(binary.BigEndian.Uint16(buffer[offset+8 : offset+10]))
		if state == mibTCPStateListen && localPort == port {
			return int(binary.LittleEndian.Uint32(buffer[offset+20 : offset+24])), true, nil
		}
	}
	return 0, false, nil
}

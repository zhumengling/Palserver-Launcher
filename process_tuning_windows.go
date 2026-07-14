package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const processSetLimitedInformation = 0x2000

var (
	kernel32CPUSet                 = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemCPUSetInformation = kernel32CPUSet.NewProc("GetSystemCpuSetInformation")
	procSetProcessDefaultCPUSets   = kernel32CPUSet.NewProc("SetProcessDefaultCpuSets")
)

func systemCPUSets() ([]systemCPUSet, error) {
	if err := procGetSystemCPUSetInformation.Find(); err != nil {
		return nil, fmt.Errorf("CPU Sets are not supported by this Windows version: %w", err)
	}
	var required uint32
	procGetSystemCPUSetInformation.Call(0, 0, uintptr(unsafe.Pointer(&required)), 0, 0)
	if required == 0 {
		return nil, errors.New("Windows returned no CPU Set information")
	}
	buffer := make([]byte, required)
	result, _, callErr := procGetSystemCPUSetInformation.Call(
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&required)),
		0,
		0,
	)
	if result == 0 {
		return nil, fmt.Errorf("query Windows CPU Sets: %w", callErr)
	}
	sets := make([]systemCPUSet, 0)
	for offset := 0; offset+20 <= int(required); {
		size := int(binary.LittleEndian.Uint32(buffer[offset : offset+4]))
		if size < 20 || offset+size > int(required) {
			break
		}
		informationType := binary.LittleEndian.Uint32(buffer[offset+4 : offset+8])
		if informationType == 0 {
			flags := buffer[offset+19]
			if flags&0x02 == 0 {
				sets = append(sets, systemCPUSet{
					ID:                    binary.LittleEndian.Uint32(buffer[offset+8 : offset+12]),
					Group:                 binary.LittleEndian.Uint16(buffer[offset+12 : offset+14]),
					LogicalProcessorIndex: buffer[offset+14],
					CoreIndex:             buffer[offset+15],
					EfficiencyClass:       buffer[offset+18],
				})
			}
		}
		offset += size
	}
	if len(sets) == 0 {
		return nil, errors.New("Windows returned no available CPU Sets")
	}
	sort.Slice(sets, func(i, j int) bool {
		if sets[i].Group != sets[j].Group {
			return sets[i].Group < sets[j].Group
		}
		if sets[i].CoreIndex != sets[j].CoreIndex {
			return sets[i].CoreIndex < sets[j].CoreIndex
		}
		return sets[i].LogicalProcessorIndex < sets[j].LogicalProcessorIndex
	})
	return sets, nil
}

func processPriorityClass(value string) uint32 {
	switch value {
	case "normal":
		return windows.NORMAL_PRIORITY_CLASS
	case "high":
		return windows.HIGH_PRIORITY_CLASS
	default:
		return windows.ABOVE_NORMAL_PRIORITY_CLASS
	}
}

func setProcessDefaultCPUSets(process windows.Handle, cpuSetIDs []uint32) error {
	if err := procSetProcessDefaultCPUSets.Find(); err != nil {
		return fmt.Errorf("CPU Sets are not supported by this Windows version: %w", err)
	}
	var pointer uintptr
	if len(cpuSetIDs) > 0 {
		pointer = uintptr(unsafe.Pointer(&cpuSetIDs[0]))
	}
	result, _, callErr := procSetProcessDefaultCPUSets.Call(uintptr(process), pointer, uintptr(len(cpuSetIDs)))
	if result == 0 {
		return fmt.Errorf("set process CPU Sets: %w", callErr)
	}
	return nil
}

func applyServerProcessTuning(pid int, priority string, cpuSetIDs []uint32, manageAffinity bool) error {
	access := uint32(windows.PROCESS_SET_INFORMATION | windows.PROCESS_QUERY_LIMITED_INFORMATION | processSetLimitedInformation)
	process, err := windows.OpenProcess(access, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open server process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	var tuningErrors []error
	if err := windows.SetPriorityClass(process, processPriorityClass(priority)); err != nil {
		tuningErrors = append(tuningErrors, fmt.Errorf("set process priority: %w", err))
	}
	if manageAffinity {
		if err := setProcessDefaultCPUSets(process, cpuSetIDs); err != nil {
			tuningErrors = append(tuningErrors, err)
		}
	}
	return errors.Join(tuningErrors...)
}

func (a *App) rebalanceServerProcesses() error {
	a.processTuningMu.Lock()
	defer a.processTuningMu.Unlock()
	type target struct {
		instance ServerInstance
		pid      int
	}
	targets := make([]target, 0)
	for _, instance := range a.store.Snapshot().Instances {
		instance = withDefaults(instance)
		status, err := serverStatus(instance)
		if err == nil && status.Running && status.PID > 0 {
			targets = append(targets, target{instance: instance, pid: status.PID})
		}
	}
	if len(targets) == 0 {
		return nil
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].instance.ID < targets[j].instance.ID })
	automaticIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.instance.CPUAffinityMode == "auto" {
			automaticIDs = append(automaticIDs, target.instance.ID)
		}
	}
	allocations := map[string][]uint32{}
	var cpuSetErr error
	if len(automaticIDs) > 1 {
		sets, err := systemCPUSets()
		if err != nil {
			cpuSetErr = err
		} else {
			allocations = allocateServerCPUSets(automaticIDs, sets)
		}
	}
	var tuningErrors []error
	if cpuSetErr != nil {
		tuningErrors = append(tuningErrors, cpuSetErr)
	}
	for _, target := range targets {
		manageAffinity := target.instance.CPUAffinityMode == "all" || len(automaticIDs) <= 1 || cpuSetErr == nil
		cpuSetIDs := allocations[target.instance.ID]
		if err := applyServerProcessTuning(target.pid, target.instance.ProcessPriority, cpuSetIDs, manageAffinity); err != nil {
			tuningErrors = append(tuningErrors, fmt.Errorf("%s: %w", target.instance.Name, err))
		}
	}
	return errors.Join(tuningErrors...)
}

func appendProcessTuningWarning(instance ServerInstance, err error) {
	if err == nil {
		return
	}
	path := filepath.Join(instance.RootPath, "launcher-logs", "server.log")
	file, openErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if openErr != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "[launcher] CPU tuning warning: %v\n", err)
}

func (a *App) rebalanceAfterServerExit(instance ServerInstance) {
	go func() {
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
			status, _ := serverStatus(instance)
			if !status.Running {
				if err := a.rebalanceServerProcesses(); err != nil {
					appendProcessTuningWarning(instance, err)
				}
				return
			}
		}
	}()
}

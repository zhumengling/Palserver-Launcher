//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"golang.org/x/sys/unix"
)

func systemCPUSets() ([]systemCPUSet, error) {
	count := runtime.NumCPU()
	if count < 1 {
		return nil, errors.New("Linux returned no logical CPUs")
	}
	sets := make([]systemCPUSet, count)
	for index := range sets {
		sets[index] = systemCPUSet{ID: uint32(index), LogicalProcessorIndex: uint8(index), CoreIndex: uint8(index)}
	}
	return sets, nil
}

func linuxNiceValue(priority string) int {
	switch priority {
	case "normal":
		return 0
	case "high":
		return -10
	default:
		return -5
	}
}

func applyServerProcessTuning(pid int, priority string, cpuSetIDs []uint32, manageAffinity bool) error {
	var tuningErrors []error
	if err := unix.Setpriority(unix.PRIO_PROCESS, pid, linuxNiceValue(priority)); err != nil {
		tuningErrors = append(tuningErrors, fmt.Errorf("set Linux process priority: %w", err))
	}
	if manageAffinity {
		var set unix.CPUSet
		set.Zero()
		if len(cpuSetIDs) == 0 {
			for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
				set.Set(cpu)
			}
		} else {
			for _, cpu := range cpuSetIDs {
				set.Set(int(cpu))
			}
		}
		if err := unix.SchedSetaffinity(pid, &set); err != nil {
			tuningErrors = append(tuningErrors, fmt.Errorf("set Linux CPU affinity: %w", err))
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
	if len(automaticIDs) > 1 {
		sets, err := systemCPUSets()
		if err != nil {
			return err
		}
		allocations = allocateServerCPUSets(automaticIDs, sets)
	}
	var tuningErrors []error
	for _, target := range targets {
		manageAffinity := target.instance.CPUAffinityMode != "manual"
		if err := applyServerProcessTuning(target.pid, target.instance.ProcessPriority, allocations[target.instance.ID], manageAffinity); err != nil {
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

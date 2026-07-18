package main

import (
	"errors"
	"sync"
	"time"
)

type observedServerProcess struct {
	PID     int
	Watcher string
}

type cachedServerStatus struct {
	Status    RuntimeStatus
	SampledAt time.Time
}

func (a *App) cacheServerStatus(id string, status RuntimeStatus, sampledAt time.Time) {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	if a.statusCache == nil {
		a.statusCache = map[string]cachedServerStatus{}
	}
	a.statusCache[id] = cachedServerStatus{Status: status, SampledAt: sampledAt}
}

func (a *App) cachedServerStatus(id string, maximumAge time.Duration) (RuntimeStatus, bool) {
	a.statusMu.RLock()
	entry, found := a.statusCache[id]
	a.statusMu.RUnlock()
	if !found || maximumAge <= 0 || time.Since(entry.SampledAt) > maximumAge {
		return RuntimeStatus{}, false
	}
	return entry.Status, true
}

func (a *App) emitServerStatus(id string, status RuntimeStatus) {
	starting, stopping := a.serverTransitionFlags(id)
	a.emit("server:status", id, applyRuntimeState(status, a.currentOperation(id), starting, stopping, time.Now()))
}

func (a *App) registerObservedProcess(id string, pid int, watcher string) {
	if pid <= 0 {
		return
	}
	a.processMonitorMu.Lock()
	defer a.processMonitorMu.Unlock()
	if a.observedProcesses == nil {
		a.observedProcesses = map[string]observedServerProcess{}
	}
	a.observedProcesses[id] = observedServerProcess{PID: pid, Watcher: watcher}
}

func (a *App) clearObservedProcess(id string) {
	a.processMonitorMu.Lock()
	defer a.processMonitorMu.Unlock()
	delete(a.observedProcesses, id)
}

func (a *App) pollServerProcess(instance ServerInstance) {
	process, found, err := defaultProcessRuntime.FindServerProcess(instance)
	if err != nil {
		a.emit("server:status-error", instance.ID, err.Error())
		return
	}
	rawStatus := a.cachedStatusFromProcess(instance, process, found)
	a.cacheServerStatus(instance.ID, rawStatus, time.Now())
	a.processMonitorMu.Lock()
	previous, observed := a.observedProcesses[instance.ID]
	if found {
		if !observed || previous.PID != process.PID {
			a.observedProcesses[instance.ID] = observedServerProcess{PID: process.PID, Watcher: "monitor"}
			scheduleCompatibilityBaseline(instance)
		}
		a.processMonitorMu.Unlock()
		a.emitServerStatus(instance.ID, rawStatus)
		return
	}
	if observed {
		delete(a.observedProcesses, instance.ID)
	}
	a.processMonitorMu.Unlock()
	if observed && previous.Watcher == "monitor" {
		go a.handleServerExit(instance, errors.New("server process exited"))
	}
	a.emitServerStatus(instance.ID, rawStatus)
}

func (a *App) pollServerProcesses() {
	instances := a.store.Snapshot().Instances
	known := make(map[string]ServerInstance, len(instances))
	maximumConcurrency := min(8, max(1, len(instances)))
	semaphore := make(chan struct{}, maximumConcurrency)
	var wait sync.WaitGroup
	for _, instance := range instances {
		known[instance.ID] = instance
		instance := instance
		wait.Add(1)
		go func() {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			a.pollServerProcess(instance)
		}()
	}
	wait.Wait()
	a.processMonitorMu.Lock()
	for id := range a.observedProcesses {
		if _, exists := known[id]; !exists {
			delete(a.observedProcesses, id)
		}
	}
	a.processMonitorMu.Unlock()
}

func (a *App) startProcessMonitor() {
	a.processMonitorMu.Lock()
	if a.processMonitorCancel != nil {
		a.processMonitorMu.Unlock()
		return
	}
	cancel := make(chan struct{})
	a.processMonitorCancel = cancel
	a.processMonitorMu.Unlock()
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		a.pollServerProcesses()
		for {
			select {
			case <-ticker.C:
				a.pollServerProcesses()
			case <-cancel:
				return
			}
		}
	}()
}

func (a *App) stopProcessMonitor() {
	a.processMonitorMu.Lock()
	defer a.processMonitorMu.Unlock()
	if a.processMonitorCancel != nil {
		close(a.processMonitorCancel)
		a.processMonitorCancel = nil
	}
}

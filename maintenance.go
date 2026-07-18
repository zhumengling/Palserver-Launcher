package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func nextMaintenanceRun(task MaintenanceTask, from time.Time) time.Time {
	if task.Schedule == "daily" {
		parsed, err := time.Parse("15:04", task.DailyTime)
		if err == nil {
			next := time.Date(from.Year(), from.Month(), from.Day(), parsed.Hour(), parsed.Minute(), 0, 0, from.Location())
			if !next.After(from) {
				next = next.AddDate(0, 0, 1)
			}
			return next
		}
	}
	minutes := task.IntervalMinutes
	if minutes < 5 {
		minutes = 60
	}
	return from.Add(time.Duration(minutes) * time.Minute)
}

func (a *App) tryBeginOperation(serverID, operation string) bool {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if a.operations == nil {
		a.operations = map[string]string{}
	}
	if a.operations[serverID] != "" {
		return false
	}
	a.operations[serverID] = operation
	return true
}

func (a *App) endOperation(serverID string) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	delete(a.operations, serverID)
}

func (a *App) currentOperation(serverID string) string {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	return a.operations[serverID]
}

func (a *App) startMaintenanceLoop() {
	if a.maintenanceCancel != nil {
		return
	}
	a.maintenanceCancel = make(chan struct{})
	go func(cancel <-chan struct{}) {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		a.pollMaintenanceTasks(time.Now())
		for {
			select {
			case now := <-ticker.C:
				a.pollMaintenanceTasks(now)
			case <-cancel:
				return
			}
		}
	}(a.maintenanceCancel)
}

func (a *App) stopMaintenanceLoop() {
	if a.maintenanceCancel != nil {
		close(a.maintenanceCancel)
		a.maintenanceCancel = nil
	}
}

func (a *App) pollMaintenanceTasks(now time.Time) {
	a.pollGuardian(now)
	a.pollPlayerHistory(now)
	a.pollGameEvents(now)
	tasks, err := a.store.ClaimDueMaintenanceTasks(now)
	if err != nil {
		return
	}
	for _, task := range tasks {
		task := task
		go a.executeMaintenanceTask(task)
	}
}

func (a *App) executeMaintenanceTask(task MaintenanceTask) {
	if !a.tryBeginOperation(task.ServerID, task.Type) {
		_ = a.store.CompleteMaintenanceTask(task.ID, "skipped", "server is busy")
		a.emit("maintenance:finished", task.ID, "skipped", "server is busy")
		return
	}
	if err := a.store.MarkMaintenanceTaskRunning(task.ID, time.Now()); err != nil {
		a.endOperation(task.ServerID)
		return
	}
	a.executeMaintenanceTaskLocked(task)
}

func (a *App) executeMaintenanceTaskLocked(task MaintenanceTask) {
	defer a.endOperation(task.ServerID)
	a.emit("maintenance:started", task)
	var err error
	switch task.Type {
	case "backup":
		_, err = a.createBackup(task.ServerID)
	case "restart":
		err = a.restartServerForMaintenance(task.ServerID)
	case "update":
		err = a.performServerUpdate(task.ServerID, false)
	default:
		err = errors.New("unsupported maintenance task")
	}
	if errors.Is(err, ErrNoUpdateAvailable) || errors.Is(err, ErrPlayersOnline) {
		_ = a.store.CompleteMaintenanceTask(task.ID, "skipped", err.Error())
		a.emit("maintenance:finished", task.ID, "skipped", err.Error())
		return
	}
	if err != nil {
		_ = a.store.CompleteMaintenanceTask(task.ID, "error", err.Error())
		a.emit("maintenance:finished", task.ID, "error", err.Error())
		return
	}
	_ = a.store.CompleteMaintenanceTask(task.ID, "ok", "completed")
	a.emit("maintenance:finished", task.ID, "ok", "completed")
}

func (a *App) restartServerForMaintenance(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		_, _ = sendRCON(instance, "Save")
		if err := a.StopServer(id); err != nil {
			return err
		}
		for attempt := 0; attempt < 45; attempt++ {
			time.Sleep(2 * time.Second)
			status, _ = serverStatus(instance)
			if !status.Running {
				break
			}
		}
		if status.Running {
			return errors.New("server did not stop before restart")
		}
	}
	return a.StartServer(id)
}

func validateMaintenanceTask(task MaintenanceTask) error {
	if strings.TrimSpace(task.ServerID) == "" {
		return errors.New("server id is required")
	}
	if task.Type != "backup" && task.Type != "restart" && task.Type != "update" {
		return errors.New("task type must be backup, restart, or update")
	}
	if task.Schedule == "daily" {
		if _, err := time.Parse("15:04", task.DailyTime); err != nil {
			return errors.New("daily time must use HH:MM")
		}
	} else if task.Schedule == "interval" {
		if task.IntervalMinutes < 5 {
			return errors.New("interval must be at least 5 minutes")
		}
	} else {
		return errors.New("schedule must be interval or daily")
	}
	return nil
}

func (a *App) SaveMaintenanceTask(task MaintenanceTask) (MaintenanceTask, error) {
	if err := validateMaintenanceTask(task); err != nil {
		return MaintenanceTask{}, err
	}
	if _, err := a.store.Find(task.ServerID); err != nil {
		return MaintenanceTask{}, err
	}
	if strings.TrimSpace(task.Name) == "" {
		task.Name = fmt.Sprintf("%s task", task.Type)
	}
	task.NextRun = nextMaintenanceRun(task, time.Now()).UnixMilli()
	return a.store.UpsertMaintenanceTask(task)
}

func (a *App) ListMaintenanceTasks(serverID string) []MaintenanceTask {
	return a.store.MaintenanceTasks(serverID)
}

func (a *App) DeleteMaintenanceTask(id string) error { return a.store.DeleteMaintenanceTask(id) }

func (a *App) RunMaintenanceTask(id string) error {
	task, err := a.store.MaintenanceTask(id)
	if err != nil {
		return err
	}
	if !a.tryBeginOperation(task.ServerID, task.Type) {
		return errors.New("server is busy")
	}
	if err := a.store.MarkMaintenanceTaskRunning(task.ID, time.Now()); err != nil {
		a.endOperation(task.ServerID)
		return err
	}
	go a.executeMaintenanceTaskLocked(task)
	return nil
}

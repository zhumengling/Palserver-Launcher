package main

import (
	"fmt"
	"strings"
	"time"
)

const guardianRestartWindow = time.Hour

func guardianFailureReached(failures, threshold int) bool {
	if threshold < 1 {
		threshold = 1
	}
	return failures >= threshold
}

func guardianRestartAllowed(attempts []time.Time, now time.Time, max int, window time.Duration) bool {
	if max < 1 {
		max = 1
	}
	if window <= 0 {
		window = guardianRestartWindow
	}
	cutoff := now.Add(-window)
	count := 0
	for _, attempt := range attempts {
		if !attempt.Before(cutoff) {
			count++
		}
	}
	return count < max
}

func guardianServiceHealthy(status RuntimeStatus, restEnabled, rconEnabled bool) bool {
	if !restEnabled && !rconEnabled {
		return true
	}
	return (!restEnabled || status.RESTAvailable) && (!rconEnabled || status.RCONAvailable)
}

func settingEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func (a *App) setGuardianSuppressed(id string, suppressed bool) {
	a.processMu.Lock()
	defer a.processMu.Unlock()
	if a.guardianSuppressed == nil {
		a.guardianSuppressed = map[string]bool{}
	}
	if suppressed {
		a.guardianSuppressed[id] = true
	} else {
		delete(a.guardianSuppressed, id)
	}
}

func (a *App) guardianIsSuppressed(id string) bool {
	a.processMu.Lock()
	defer a.processMu.Unlock()
	return a.guardianSuppressed[id]
}

func (a *App) pollGuardian(now time.Time) {
	for _, instance := range a.store.Snapshot().Instances {
		if !instance.GuardianEnabled || a.guardianIsSuppressed(instance.ID) || a.currentOperation(instance.ID) != "" {
			continue
		}
		a.processMu.Lock()
		last := a.guardianLastCheck[instance.ID]
		if !last.IsZero() && now.Sub(last) < time.Duration(instance.GuardianCheckSeconds)*time.Second {
			a.processMu.Unlock()
			continue
		}
		a.guardianLastCheck[instance.ID] = now
		a.processMu.Unlock()

		status, _ := a.GetStatus(instance.ID)
		if !status.Running {
			go a.recoverWithGuardian(instance, "server process is not running")
			continue
		}
		content, err := a.ReadWorldSettings(instance.ID)
		if err != nil {
			continue
		}
		settings := parseWorldSettingValues(content)
		restEnabled := settingEnabled(settings["RESTAPIEnabled"])
		rconEnabled := settingEnabled(settings["RCONEnabled"])
		if rconEnabled && status.RCONAvailable {
			status.RCONAvailable = probeRCON(instance) == nil
		}
		if guardianServiceHealthy(status, restEnabled, rconEnabled) {
			a.processMu.Lock()
			a.guardianFailures[instance.ID] = 0
			a.processMu.Unlock()
			continue
		}
		a.processMu.Lock()
		a.guardianFailures[instance.ID]++
		failures := a.guardianFailures[instance.ID]
		a.processMu.Unlock()
		if guardianFailureReached(failures, instance.GuardianFailureThreshold) {
			go a.recoverWithGuardian(instance, fmt.Sprintf("management endpoint failed %d consecutive checks", failures))
		}
	}
}

func (a *App) recoverWithGuardian(instance ServerInstance, reason string) {
	if a.guardianIsSuppressed(instance.ID) || !a.tryBeginOperation(instance.ID, "guardian") {
		return
	}
	defer a.endOperation(instance.ID)

	now := time.Now()
	a.processMu.Lock()
	attempts := a.guardianRestarts[instance.ID]
	allowed := guardianRestartAllowed(attempts, now, instance.GuardianMaxRestarts, guardianRestartWindow)
	if allowed {
		cutoff := now.Add(-guardianRestartWindow)
		active := make([]time.Time, 0, len(attempts)+1)
		for _, attempt := range attempts {
			if !attempt.Before(cutoff) {
				active = append(active, attempt)
			}
		}
		a.guardianRestarts[instance.ID] = append(active, now)
		a.guardianFailures[instance.ID] = 0
	}
	a.processMu.Unlock()
	if !allowed {
		if a.ctx != nil {
			a.emit("guardian:exhausted", instance.ID, reason)
		}
		a.notifyDiscord(instance.ID, "guardian", "Guardian 已停止自动恢复", reason)
		return
	}
	if a.ctx != nil {
		a.emit("guardian:recovering", instance.ID, reason)
	}
	status, _ := serverStatus(instance)
	if status.Running {
		_, _ = sendRCON(instance, "Save")
		if err := a.StopServer(instance.ID); err != nil {
			_ = a.ForceStopServer(instance.ID)
		}
		for attempt := 0; attempt < 30; attempt++ {
			time.Sleep(2 * time.Second)
			status, _ = serverStatus(instance)
			if !status.Running {
				break
			}
		}
	}
	time.Sleep(2 * time.Second)
	if err := a.StartServer(instance.ID); err != nil && a.ctx != nil {
		a.emit("guardian:failed", instance.ID, err.Error())
	}
}

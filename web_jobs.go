package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var longRunningWebRPCMethods = map[string]bool{
	"ApplyGamePreset": true, "ApplyOfficialPvPPreset": true, "ClearSteamCMDCache": true,
	"CreateBackup": true, "DeleteInstance": true, "DuplicateInstance": true, "ExportClientMods": true,
	"InspectSave": true, "InstallFrp": true, "InstallOrUpdateServer": true, "InstallSaveInspector": true,
	"ImportUploadedServer": true,
	"PerformManagedUpdate": true, "PruneBackups": true, "QuickSetup": true, "RestoreBackup": true,
	"SaveWorld": true, "StartGameEvent": true, "StopGameEvent": true,
	"UpdateAllExtensions": true, "UpdateExtension": true,
}

type webJobStatus struct {
	ID         string `json:"id"`
	Method     string `json:"method"`
	ServerID   string `json:"serverId"`
	State      string `json:"state"`
	CreatedAt  int64  `json:"createdAt"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	Result     any    `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
}

type webJobInvoker func(*App, string, []json.RawMessage) (any, error)

type webJobManager struct {
	mu              sync.RWMutex
	jobs            map[string]webJobStatus
	invoke          webJobInvoker
	persistencePath string
}

func newWebJobManager() *webJobManager {
	return &webJobManager{jobs: map[string]webJobStatus{}, invoke: invokeWebRPC}
}

func newPersistentWebJobManager(app *App) *webJobManager {
	manager := newWebJobManager()
	if app != nil && app.store != nil && strings.TrimSpace(app.store.path) != "" {
		manager.persistencePath = filepath.Join(filepath.Dir(app.store.path), "web-jobs.json")
	} else if base, err := appDataDir(); err == nil {
		manager.persistencePath = filepath.Join(base, "web-jobs.json")
	}
	manager.loadPersisted()
	return manager
}

type persistedWebJobs struct {
	Jobs []webJobStatus `json:"jobs"`
}

func persistedWebJob(job webJobStatus) webJobStatus {
	job.Result = nil
	return job
}

func (manager *webJobManager) persistLocked() error {
	if strings.TrimSpace(manager.persistencePath) == "" {
		return nil
	}
	jobs := make([]webJobStatus, 0, len(manager.jobs))
	for _, job := range manager.jobs {
		jobs = append(jobs, persistedWebJob(job))
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt > jobs[j].CreatedAt })
	data, err := json.MarshalIndent(persistedWebJobs{Jobs: jobs}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(manager.persistencePath), 0o700); err != nil {
		return err
	}
	temporary := manager.persistencePath + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, manager.persistencePath); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func (manager *webJobManager) loadPersisted() {
	if strings.TrimSpace(manager.persistencePath) == "" {
		return
	}
	data, err := os.ReadFile(manager.persistencePath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		return
	}
	var persisted persistedWebJobs
	if err := json.Unmarshal(data, &persisted); err != nil {
		corrupt := fmt.Sprintf("%s.corrupt-%d", manager.persistencePath, time.Now().Unix())
		_ = os.Rename(manager.persistencePath, corrupt)
		return
	}
	now := time.Now()
	for _, job := range persisted.Jobs {
		if strings.TrimSpace(job.ID) == "" {
			continue
		}
		job.Result = nil
		if job.State == "running" {
			job.State = "error"
			job.Error = "Agent 重启时该任务仍在运行，任务已中断，请检查服务器状态后重试"
			job.FinishedAt = now.UnixMilli()
		}
		manager.jobs[job.ID] = job
	}
	manager.pruneLocked(now)
	_ = manager.persistLocked()
}

func cloneRawArguments(arguments []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, len(arguments))
	for index, argument := range arguments {
		result[index] = append(json.RawMessage(nil), argument...)
	}
	return result
}

func (manager *webJobManager) pruneLocked(now time.Time) bool {
	cutoff := now.Add(-24 * time.Hour).UnixMilli()
	resultCutoff := now.Add(-10 * time.Minute).UnixMilli()
	changed := false
	for id, job := range manager.jobs {
		if job.FinishedAt > 0 && job.FinishedAt < cutoff {
			delete(manager.jobs, id)
			changed = true
		} else if job.FinishedAt > 0 && job.FinishedAt < resultCutoff && job.Result != nil {
			job.Result = nil
			manager.jobs[id] = job
			changed = true
		}
	}
	return changed
}

func (manager *webJobManager) conflictingRunningJobLocked(serverID string) (webJobStatus, bool) {
	for _, job := range manager.jobs {
		if job.State != "running" {
			continue
		}
		// Jobs without a server ID operate on shared launcher components, such
		// as SteamCMD or the save inspector. Keep them exclusive with all other
		// long-running jobs. Server-scoped jobs remain parallel across instances.
		if serverID == "" || job.ServerID == "" || job.ServerID == serverID {
			return job, true
		}
	}
	return webJobStatus{}, false
}

func (manager *webJobManager) start(app *App, method string, arguments []json.RawMessage, remoteIP string) (webJobStatus, error) {
	if !longRunningWebRPCMethods[method] || !webRPCMethods[method] {
		return webJobStatus{}, errors.New("method is not available as a background job")
	}
	id, err := randomHex(16)
	if err != nil {
		return webJobStatus{}, err
	}
	now := time.Now()
	job := webJobStatus{ID: id, Method: method, ServerID: auditServerID(arguments), State: "running", CreatedAt: now.UnixMilli(), StartedAt: now.UnixMilli()}
	manager.mu.Lock()
	manager.pruneLocked(now)
	if conflict, found := manager.conflictingRunningJobLocked(job.ServerID); found {
		manager.mu.Unlock()
		if job.ServerID == "" || conflict.ServerID == "" {
			return webJobStatus{}, fmt.Errorf("共享后台任务 %s 正在运行，请等待任务完成后重试", conflict.Method)
		}
		return webJobStatus{}, fmt.Errorf("此服务器正在执行后台任务 %s，请等待任务完成后重试", conflict.Method)
	}
	if len(manager.jobs) >= 512 {
		manager.mu.Unlock()
		return webJobStatus{}, errors.New("too many retained background jobs")
	}
	manager.jobs[id] = job
	if err := manager.persistLocked(); err != nil {
		delete(manager.jobs, id)
		manager.mu.Unlock()
		return webJobStatus{}, fmt.Errorf("persist background job: %w", err)
	}
	manager.mu.Unlock()
	arguments = cloneRawArguments(arguments)
	go func() {
		result, invokeErr := manager.invoke(app, method, arguments)
		finished := time.Now()
		manager.mu.RLock()
		completed := manager.jobs[id]
		manager.mu.RUnlock()
		completed.FinishedAt = finished.UnixMilli()
		if invokeErr != nil {
			completed.State, completed.Error = "error", invokeErr.Error()
		} else {
			completed.State, completed.Result = "completed", result
		}
		_ = app.appendAgentAudit(AgentAuditEntry{Time: finished.UTC().Format(time.RFC3339Nano), Method: method, ServerID: completed.ServerID, RemoteIP: remoteIP, Successful: invokeErr == nil, Error: auditError(invokeErr)})
		manager.mu.Lock()
		manager.jobs[id] = completed
		_ = manager.persistLocked()
		manager.mu.Unlock()
		app.emit("web:job", completed)
	}()
	return job, nil
}

func (manager *webJobManager) status(id string) (webJobStatus, bool) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	job, found := manager.jobs[id]
	return job, found
}

func (manager *webJobManager) list(serverID string, limit int) []webJobStatus {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	manager.mu.Lock()
	pruned := manager.pruneLocked(time.Now())
	result := make([]webJobStatus, 0, len(manager.jobs))
	for _, job := range manager.jobs {
		if serverID != "" && job.ServerID != serverID {
			continue
		}
		job.Result = nil
		result = append(result, job)
	}
	if pruned {
		_ = manager.persistLocked()
	}
	manager.mu.Unlock()
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

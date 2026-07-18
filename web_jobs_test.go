package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestWebJobManagerRunsMethodAfterReturningJob(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	started := make(chan struct{})
	release := make(chan struct{})
	manager := newWebJobManager()
	manager.invoke = func(*App, string, []json.RawMessage) (any, error) {
		close(started)
		<-release
		return map[string]string{"value": "completed"}, nil
	}
	app := &App{store: &Store{path: filepath.Join(home, "config.json")}}
	job, err := manager.start(app, "CreateBackup", []json.RawMessage{json.RawMessage(`"srv-job"`)}, "127.0.0.1")
	if err != nil || job.State != "running" || job.FinishedAt != 0 {
		t.Fatalf("started job = %#v, %v", job, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background method did not start")
	}
	if current, found := manager.status(job.ID); !found || current.State != "running" {
		t.Fatalf("running job status = %#v, %v", current, found)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, _ := manager.status(job.ID)
		if current.State == "completed" {
			if current.Result.(map[string]string)["value"] != "completed" || current.FinishedAt == 0 {
				t.Fatalf("completed job = %#v", current)
			}
			entries, auditErr := app.ListAgentAudit(10)
			if auditErr != nil || len(entries) != 1 || entries[0].Method != "CreateBackup" || entries[0].ServerID != "srv-job" || !entries[0].Successful {
				t.Fatalf("job audit = %#v, %v", entries, auditErr)
			}
			listed := manager.list("srv-job", 10)
			if len(listed) != 1 || listed[0].ID != job.ID || listed[0].Result != nil {
				t.Fatalf("listed jobs = %#v", listed)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background job did not complete")
}

func TestAgentBackgroundJobHTTPFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	app := &App{store: &Store{path: filepath.Join(home, "config.json")}}
	auth := newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	session, err := auth.createSession()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newAgentHTTPHandler(app, auth)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/ClearSteamCMDCache", bytes.NewBufferString(`{"args":[]}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: agentSessionCookie, Value: session})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("job start status = %d, body=%s", response.Code, response.Body.String())
	}
	var job webJobStatus
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil || job.ID == "" {
		t.Fatalf("job response = %#v, %v", job, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+job.ID, nil)
		statusRequest.AddCookie(&http.Cookie{Name: agentSessionCookie, Value: session})
		statusResponse := httptest.NewRecorder()
		handler.ServeHTTP(statusResponse, statusRequest)
		if statusResponse.Code != http.StatusOK {
			t.Fatalf("job status HTTP = %d, body=%s", statusResponse.Code, statusResponse.Body.String())
		}
		var current webJobStatus
		if err := json.NewDecoder(statusResponse.Body).Decode(&current); err != nil {
			t.Fatal(err)
		}
		if current.State == "completed" {
			listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?limit=10", nil)
			listRequest.AddCookie(&http.Cookie{Name: agentSessionCookie, Value: session})
			listResponse := httptest.NewRecorder()
			handler.ServeHTTP(listResponse, listRequest)
			if listResponse.Code != http.StatusOK {
				t.Fatalf("job list HTTP = %d, body=%s", listResponse.Code, listResponse.Body.String())
			}
			var listed []webJobStatus
			if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil || len(listed) != 1 || listed[0].ID != job.ID || listed[0].Result != nil {
				t.Fatalf("listed HTTP jobs = %#v, %v", listed, err)
			}
			return
		}
		if current.State == "error" {
			t.Fatalf("background HTTP job failed: %s", current.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background HTTP job did not complete")
}

func TestWebJobListDropsOldResultsAndExpiredJobs(t *testing.T) {
	now := time.Now()
	manager := newWebJobManager()
	manager.jobs["recent"] = webJobStatus{ID: "recent", State: "completed", CreatedAt: now.Add(-20 * time.Minute).UnixMilli(), FinishedAt: now.Add(-20 * time.Minute).UnixMilli(), Result: "large-result"}
	manager.jobs["expired"] = webJobStatus{ID: "expired", State: "completed", CreatedAt: now.Add(-25 * time.Hour).UnixMilli(), FinishedAt: now.Add(-25 * time.Hour).UnixMilli(), Result: "old-result"}
	listed := manager.list("", 100)
	if len(listed) != 1 || listed[0].ID != "recent" || listed[0].Result != nil {
		t.Fatalf("pruned jobs = %#v", listed)
	}
	if _, found := manager.status("expired"); found {
		t.Fatal("expired job was retained")
	}
}

func TestPersistentWebJobsSurviveAgentRestartWithoutPersistingResults(t *testing.T) {
	home := t.TempDir()
	app := &App{store: &Store{path: filepath.Join(home, "config.json")}}
	manager := newPersistentWebJobManager(app)
	manager.invoke = func(*App, string, []json.RawMessage) (any, error) {
		return map[string]string{"secret": "result-must-not-be-persisted"}, nil
	}
	job, err := manager.start(app, "CreateBackup", []json.RawMessage{json.RawMessage(`"preview-server"`)}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, _ := manager.status(job.ID)
		if current.State == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, found := manager.status(job.ID)
	if !found || current.State != "completed" || current.Result == nil {
		t.Fatalf("completed in-memory job = %#v, found=%v", current, found)
	}
	data, err := os.ReadFile(filepath.Join(home, "web-jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("result-must-not-be-persisted")) {
		t.Fatalf("persisted job exposed its result: %s", data)
	}
	restarted := newPersistentWebJobManager(app)
	restored, found := restarted.status(job.ID)
	if !found || restored.State != "completed" || restored.ServerID != "preview-server" || restored.Result != nil {
		t.Fatalf("restored job = %#v, found=%v", restored, found)
	}
}

func TestPersistentWebJobsMarkRunningTaskInterruptedAfterRestart(t *testing.T) {
	home := t.TempDir()
	app := &App{store: &Store{path: filepath.Join(home, "config.json")}}
	manager := newPersistentWebJobManager(app)
	manager.jobs["running-job"] = webJobStatus{
		ID: "running-job", Method: "InstallOrUpdateServer", ServerID: "srv-linux",
		State: "running", CreatedAt: time.Now().Add(-time.Minute).UnixMilli(), StartedAt: time.Now().Add(-time.Minute).UnixMilli(),
	}
	manager.mu.Lock()
	if err := manager.persistLocked(); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()

	restarted := newPersistentWebJobManager(app)
	restored, found := restarted.status("running-job")
	if !found || restored.State != "error" || restored.FinishedAt == 0 || !strings.Contains(restored.Error, "Agent 重启") {
		t.Fatalf("interrupted job = %#v, found=%v", restored, found)
	}
}

func TestWebJobsIsolateServersAndRejectConflictingTasks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	manager := newWebJobManager()
	started := make(chan string, 2)
	release := make(chan struct{})
	manager.invoke = func(_ *App, method string, arguments []json.RawMessage) (any, error) {
		started <- auditServerID(arguments) + ":" + method
		<-release
		return nil, nil
	}
	app := &App{store: &Store{path: filepath.Join(home, "config.json")}}
	first, err := manager.start(app, "CreateBackup", []json.RawMessage{json.RawMessage(`"srv-a"`)}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first background job did not start")
	}
	if _, err := manager.start(app, "RestoreBackup", []json.RawMessage{json.RawMessage(`"srv-a"`), json.RawMessage(`"backup"`)}, "127.0.0.1"); err == nil || !strings.Contains(err.Error(), "此服务器正在执行") {
		t.Fatalf("same-server conflict error = %v", err)
	}
	second, err := manager.start(app, "CreateBackup", []json.RawMessage{json.RawMessage(`"srv-b"`)}, "127.0.0.1")
	if err != nil {
		t.Fatalf("different server job was rejected: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("different-server background job did not start")
	}
	if _, err := manager.start(app, "ClearSteamCMDCache", nil, "127.0.0.1"); err == nil || !strings.Contains(err.Error(), "共享后台任务") {
		t.Fatalf("shared-task conflict error = %v", err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		firstStatus, _ := manager.status(first.ID)
		secondStatus, _ := manager.status(second.ID)
		if firstStatus.State == "completed" && secondStatus.State == "completed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("parallel server jobs did not complete")
}

func TestGlobalWebJobBlocksServerScopedJobs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", home)
	manager := newWebJobManager()
	started := make(chan struct{})
	release := make(chan struct{})
	manager.invoke = func(*App, string, []json.RawMessage) (any, error) {
		close(started)
		<-release
		return nil, nil
	}
	app := &App{store: &Store{path: filepath.Join(home, "config.json")}}
	job, err := manager.start(app, "ClearSteamCMDCache", nil, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("global job did not start")
	}
	if _, err := manager.start(app, "CreateBackup", []json.RawMessage{json.RawMessage(`"srv-a"`)}, "127.0.0.1"); err == nil || !strings.Contains(err.Error(), "共享后台任务") {
		t.Fatalf("server job was not blocked by global job: %v", err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, _ := manager.status(job.ID)
		if status.State == "completed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("global job did not complete")
}

func TestFrontendAndBackendLongRunningMethodListsMatch(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("frontend", "src", "platformApi.ts"))
	if err != nil {
		t.Fatal(err)
	}
	blockPattern := regexp.MustCompile(`(?s)const longRunningWebMethods = new Set\(\[(.*?)\]\);`)
	block := blockPattern.FindSubmatch(data)
	if len(block) != 2 {
		t.Fatal("frontend long-running method list was not found")
	}
	methodPattern := regexp.MustCompile(`'([A-Z][A-Za-z0-9_]*)'`)
	frontend := map[string]bool{}
	for _, match := range methodPattern.FindAllSubmatch(block[1], -1) {
		frontend[string(match[1])] = true
	}
	if len(frontend) != len(longRunningWebRPCMethods) {
		t.Fatalf("long-running method count frontend=%d backend=%d", len(frontend), len(longRunningWebRPCMethods))
	}
	for method := range longRunningWebRPCMethods {
		if !frontend[method] {
			t.Fatalf("frontend is missing background method %s", method)
		}
	}
}

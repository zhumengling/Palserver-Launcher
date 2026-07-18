package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	agentLoginFailureLimit = 5
	agentLoginWindow       = 5 * time.Minute
	agentLoginLockout      = time.Minute
	agentAuditMaxSize      = 5 << 20
)

var auditServerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type agentLoginAttempt struct {
	Failures    int
	WindowStart time.Time
	LockedUntil time.Time
}

func (auth *agentAuth) loginRetryAfter(client string, now time.Time) time.Duration {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	attempt := auth.attempts[client]
	if attempt.LockedUntil.After(now) {
		return attempt.LockedUntil.Sub(now)
	}
	if !attempt.LockedUntil.IsZero() || now.Sub(attempt.WindowStart) > agentLoginWindow {
		delete(auth.attempts, client)
	}
	return 0
}

func (auth *agentAuth) recordLoginFailure(client string, now time.Time) time.Duration {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	attempt := auth.attempts[client]
	if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) > agentLoginWindow {
		attempt = agentLoginAttempt{WindowStart: now}
	}
	attempt.Failures++
	if attempt.Failures >= agentLoginFailureLimit {
		attempt.LockedUntil = now.Add(agentLoginLockout)
	}
	auth.attempts[client] = attempt
	if attempt.LockedUntil.After(now) {
		return attempt.LockedUntil.Sub(now)
	}
	return 0
}

func (auth *agentAuth) recordLoginSuccess(client string) {
	auth.mu.Lock()
	delete(auth.attempts, client)
	auth.mu.Unlock()
}

func requestClientIP(request *http.Request) string {
	direct := requestDirectIP(request)
	if parsed := net.ParseIP(direct); parsed != nil && parsed.IsLoopback() {
		if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
			return forwarded
		}
	}
	return direct
}

func requestDirectIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return request.RemoteAddr
}

func agentRequestSecure(request *http.Request) bool {
	if request.TLS != nil {
		return true
	}
	direct := net.ParseIP(requestDirectIP(request))
	return direct != nil && direct.IsLoopback() && strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")), "https")
}

func sameOriginAgentRequest(request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
		return true
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, request.Host)
}

var webRPCMutatingMethods = map[string]bool{
	"Announce": true, "ApplyGamePreset": true, "ApplyLauncherUpdate": true, "ApplyOfficialPvPPreset": true,
	"BanHistoricalPlayer": true, "ClearSteamCMDCache": true, "CreateBackup": true, "DeleteInstance": true,
	"CreateDiagnosticBundle": true,
	"DeleteMaintenanceTask":  true, "DeleteMod": true, "DeleteOfficialWorkshopMod": true, "DuplicateInstance": true,
	"ExportClientMods": true, "ForceStopServer": true, "ImportExistingServer": true, "ImportUploadedServer": true, "ImportMods": true,
	"ImportOfficialWorkshopMod": true, "InstallFrp": true, "InstallOrUpdateServer": true, "InstallSaveInspector": true,
	"InstallServerModArchive": true, "InspectSave": true, "PerformManagedUpdate": true, "PlayerAction": true, "PruneBackups": true,
	"QuickSetup": true, "RestoreBackup": true, "RestorePluginsAfterSafeMode": true, "RunMaintenanceTask": true,
	"SaveDiscordWebhook": true, "SaveFrpSettings": true, "SaveInstance": true, "SaveMaintenanceTask": true,
	"SaveOfficialWorkshopRoot": true, "SaveWorld": true, "SaveWorldSettingsValues": true, "SelectInstance": true,
	"SendRCON": true, "SetOfficialWorkshopModEnabled": true, "SetPlayerNote": true, "SetPlayerWhitelist": true,
	"ShutdownServer": true, "StartFrp": true, "StartGameEvent": true, "StartServer": true,
	"StartServerSafeMode": true, "StopFrp": true, "StopGameEvent": true, "StopServer": true,
	"TestDiscordWebhook": true, "ToggleExtension": true, "ToggleMod": true, "UnbanPlayer": true,
	"UninstallServerMod": true, "UpdateAllExtensions": true, "UpdateExtension": true, "WriteWorldSettings": true,
}

func auditServerID(args []json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(args[0], &value) == nil {
		return safeAuditServerID(value)
	}
	var object struct {
		ID       string `json:"id"`
		ServerID string `json:"serverId"`
	}
	if json.Unmarshal(args[0], &object) == nil {
		if value = safeAuditServerID(object.ServerID); value != "" {
			return value
		}
		return safeAuditServerID(object.ID)
	}
	return ""
}

func safeAuditServerID(value string) string {
	value = strings.TrimSpace(value)
	if auditServerIDPattern.MatchString(value) {
		return value
	}
	return ""
}

func auditError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

func agentAuditPath() (string, error) {
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "audit", "web-agent.jsonl"), nil
}

func (a *App) appendAgentAudit(entry AgentAuditEntry) error {
	a.webAuditMu.Lock()
	defer a.webAuditMu.Unlock()
	path, err := agentAuditPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if info, statErr := os.Stat(path); statErr == nil && info.Size() >= agentAuditMaxSize {
		_ = os.Remove(path + ".1")
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encodeErr := json.NewEncoder(file).Encode(entry)
	closeErr := file.Close()
	return errors.Join(encodeErr, closeErr)
}

func (a *App) ListAgentAudit(limit int) ([]AgentAuditEntry, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	path, err := agentAuditPath()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return []AgentAuditEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries := make([]AgentAuditEntry, 0, limit)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	for scanner.Scan() {
		var entry AgentAuditEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		entries = append(entries, entry)
		if len(entries) > limit {
			entries = entries[len(entries)-limit:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	return entries, nil
}

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu     sync.RWMutex
	path   string
	config AppConfig
}

func NewStore() (*Store, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return nil, errors.New("LOCALAPPDATA is not available")
	}
	dir := filepath.Join(base, "palserver-launcher")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "config.json")}
	s.config.Language = "zh-CN"
	if data, err := os.ReadFile(s.path); err == nil {
		if err := json.Unmarshal(data, &s.config); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Snapshot() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, _ := json.Marshal(s.config)
	var copy AppConfig
	_ = json.Unmarshal(data, &copy)
	return copy
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Upsert(instance ServerInstance) (ServerInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if instance.ID == "" {
		instance.ID = newID()
	}
	instance = withDefaults(instance)
	found := false
	for i := range s.config.Instances {
		if s.config.Instances[i].ID == instance.ID {
			s.config.Instances[i] = instance
			found = true
			break
		}
	}
	if !found {
		s.config.Instances = append(s.config.Instances, instance)
	}
	if s.config.SelectedID == "" {
		s.config.SelectedID = instance.ID
	}
	return instance, s.saveLocked()
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.config.Instances[:0]
	for _, instance := range s.config.Instances {
		if instance.ID != id {
			filtered = append(filtered, instance)
		}
	}
	s.config.Instances = filtered
	if s.config.SelectedID == id {
		s.config.SelectedID = ""
		if len(filtered) > 0 {
			s.config.SelectedID = filtered[0].ID
		}
	}
	return s.saveLocked()
}

// DeleteServerData removes the instance and every launcher-owned record tied to it.
// Files on disk are handled by App.DeleteInstance so this method remains safe for
// the "remove record only" action.
func (s *Store) DeleteServerData(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	instances := s.config.Instances[:0]
	for _, instance := range s.config.Instances {
		if instance.ID != id {
			instances = append(instances, instance)
		}
	}
	s.config.Instances = instances
	if s.config.SelectedID == id {
		s.config.SelectedID = ""
		if len(instances) > 0 {
			s.config.SelectedID = instances[0].ID
		}
	}
	tasks := s.config.MaintenanceTasks[:0]
	for _, task := range s.config.MaintenanceTasks {
		if task.ServerID != id {
			tasks = append(tasks, task)
		}
	}
	s.config.MaintenanceTasks = tasks
	history := s.config.PlayerHistory[:0]
	for _, entry := range s.config.PlayerHistory {
		if entry.ServerID != id {
			history = append(history, entry)
		}
	}
	s.config.PlayerHistory = history
	events := s.config.ActiveEvents[:0]
	for _, event := range s.config.ActiveEvents {
		if event.ServerID != id {
			events = append(events, event)
		}
	}
	s.config.ActiveEvents = events
	webhooks := s.config.DiscordWebhooks[:0]
	for _, webhook := range s.config.DiscordWebhooks {
		if webhook.ServerID != id {
			webhooks = append(webhooks, webhook)
		}
	}
	s.config.DiscordWebhooks = webhooks
	return s.saveLocked()
}

func (s *Store) Select(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, instance := range s.config.Instances {
		if instance.ID == id {
			s.config.SelectedID = id
			return s.saveLocked()
		}
	}
	return errors.New("server instance not found")
}

func (s *Store) Find(id string) (ServerInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, instance := range s.config.Instances {
		if instance.ID == id {
			return instance, nil
		}
	}
	return ServerInstance{}, errors.New("server instance not found")
}

func (s *Store) UpsertMaintenanceTask(task MaintenanceTask) (MaintenanceTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task.ID == "" {
		task.ID = "task-" + strings.TrimPrefix(newID(), "srv-")
	}
	found := false
	for index := range s.config.MaintenanceTasks {
		if s.config.MaintenanceTasks[index].ID == task.ID {
			s.config.MaintenanceTasks[index] = task
			found = true
			break
		}
	}
	if !found {
		s.config.MaintenanceTasks = append(s.config.MaintenanceTasks, task)
	}
	return task, s.saveLocked()
}

func (s *Store) DeleteMaintenanceTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.config.MaintenanceTasks[:0]
	for _, task := range s.config.MaintenanceTasks {
		if task.ID != id {
			filtered = append(filtered, task)
		}
	}
	s.config.MaintenanceTasks = filtered
	return s.saveLocked()
}

func (s *Store) MaintenanceTasks(serverID string) []MaintenanceTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]MaintenanceTask, 0)
	for _, task := range s.config.MaintenanceTasks {
		if serverID == "" || task.ServerID == serverID {
			result = append(result, task)
		}
	}
	return result
}

func (s *Store) MaintenanceTask(id string) (MaintenanceTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, task := range s.config.MaintenanceTasks {
		if task.ID == id {
			return task, nil
		}
	}
	return MaintenanceTask{}, errors.New("maintenance task not found")
}

func (s *Store) ClaimDueMaintenanceTasks(now time.Time) ([]MaintenanceTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	claimed := make([]MaintenanceTask, 0)
	changed := false
	for index := range s.config.MaintenanceTasks {
		task := &s.config.MaintenanceTasks[index]
		if !task.Enabled {
			continue
		}
		if task.NextRun == 0 {
			task.NextRun = nextMaintenanceRun(*task, now).UnixMilli()
			changed = true
			continue
		}
		if task.NextRun > now.UnixMilli() {
			continue
		}
		task.LastRun = now.UnixMilli()
		task.NextRun = nextMaintenanceRun(*task, now).UnixMilli()
		task.LastStatus = "running"
		task.LastMessage = ""
		claimed = append(claimed, *task)
		changed = true
	}
	if changed {
		return claimed, s.saveLocked()
	}
	return claimed, nil
}

func (s *Store) CompleteMaintenanceTask(id, status, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.config.MaintenanceTasks {
		if s.config.MaintenanceTasks[index].ID == id {
			s.config.MaintenanceTasks[index].LastStatus = status
			s.config.MaintenanceTasks[index].LastMessage = message
			return s.saveLocked()
		}
	}
	return errors.New("maintenance task not found")
}

func (s *Store) MergePlayerHistory(serverID string, players []Player, now time.Time) ([]PlayerHistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	serverEntries := make([]PlayerHistoryEntry, 0)
	otherEntries := make([]PlayerHistoryEntry, 0)
	for _, entry := range s.config.PlayerHistory {
		if entry.ServerID == serverID {
			serverEntries = append(serverEntries, entry)
		} else {
			otherEntries = append(otherEntries, entry)
		}
	}
	serverEntries = mergePlayerHistory(serverEntries, players, now)
	for index := range serverEntries {
		serverEntries[index].ServerID = serverID
	}
	s.config.PlayerHistory = append(otherEntries, serverEntries...)
	return append([]PlayerHistoryEntry(nil), serverEntries...), s.saveLocked()
}

func (s *Store) PlayerHistory(serverID string) []PlayerHistoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]PlayerHistoryEntry, 0)
	for _, entry := range s.config.PlayerHistory {
		if serverID == "" || entry.ServerID == serverID {
			result = append(result, entry)
		}
	}
	return result
}

func (s *Store) UpdatePlayerHistory(serverID, userID string, update func(*PlayerHistoryEntry)) (PlayerHistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.config.PlayerHistory {
		entry := &s.config.PlayerHistory[index]
		if entry.ServerID == serverID && entry.UserID == userID {
			update(entry)
			return *entry, s.saveLocked()
		}
	}
	return PlayerHistoryEntry{}, errors.New("player history entry not found")
}

func (s *Store) SaveActiveEvent(event ActiveGameEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.config.ActiveEvents {
		if s.config.ActiveEvents[index].ServerID == event.ServerID {
			s.config.ActiveEvents[index] = event
			return s.saveLocked()
		}
	}
	s.config.ActiveEvents = append(s.config.ActiveEvents, event)
	return s.saveLocked()
}

func (s *Store) ActiveEvent(serverID string) (ActiveGameEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, event := range s.config.ActiveEvents {
		if event.ServerID == serverID {
			return event, true
		}
	}
	return ActiveGameEvent{}, false
}

func (s *Store) ActiveEvents() []ActiveGameEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ActiveGameEvent(nil), s.config.ActiveEvents...)
}

func (s *Store) DeleteActiveEvent(serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.config.ActiveEvents[:0]
	for _, event := range s.config.ActiveEvents {
		if event.ServerID != serverID {
			filtered = append(filtered, event)
		}
	}
	s.config.ActiveEvents = filtered
	return s.saveLocked()
}

func (s *Store) SaveDiscordWebhook(config DiscordWebhookConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.config.DiscordWebhooks {
		if s.config.DiscordWebhooks[index].ServerID == config.ServerID {
			s.config.DiscordWebhooks[index] = config
			return s.saveLocked()
		}
	}
	s.config.DiscordWebhooks = append(s.config.DiscordWebhooks, config)
	return s.saveLocked()
}

func (s *Store) DiscordWebhook(serverID string) (DiscordWebhookConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, config := range s.config.DiscordWebhooks {
		if config.ServerID == serverID {
			return config, true
		}
	}
	return DiscordWebhookConfig{}, false
}

func newID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return "srv-" + hex.EncodeToString(buf)
}

func withDefaults(instance ServerInstance) ServerInstance {
	instance.Name = strings.TrimSpace(instance.Name)
	instance.RootPath = filepath.Clean(strings.TrimSpace(instance.RootPath))
	if instance.PublicPort == 0 {
		instance.PublicPort = 8211
	}
	if instance.QueryPort == 0 {
		instance.QueryPort = 27015
	}
	if instance.RCONPort == 0 {
		instance.RCONPort = 25575
	}
	if instance.RESTPort == 0 {
		instance.RESTPort = 8212
	}
	if instance.Executable == "" && instance.RootPath != "" {
		instance.Executable = filepath.Join(instance.RootPath, "PalServer.exe")
	}
	if instance.IconID == "" {
		instance.IconID = "SheepBall"
	}
	if instance.BackupRetentionMode == "" {
		instance.BackupRetentionMode = "tiered"
	}
	if instance.BackupRetentionCount <= 0 {
		instance.BackupRetentionCount = 30
	}
	if instance.BackupRetentionDays <= 0 {
		instance.BackupRetentionDays = 30
	}
	if instance.UpdateWarnMinutes <= 0 {
		instance.UpdateWarnMinutes = 5
	}
	if instance.GuardianFailureThreshold <= 0 {
		instance.GuardianFailureThreshold = 3
	}
	if instance.GuardianCheckSeconds < 30 {
		instance.GuardianCheckSeconds = 60
	}
	if instance.GuardianMaxRestarts <= 0 {
		instance.GuardianMaxRestarts = 3
	}
	return instance
}

func safeJoin(root string, parts ...string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(append([]string{rootAbs}, parts...)...)
	joinedAbs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, joinedAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes server root")
	}
	return joinedAbs, nil
}

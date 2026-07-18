package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu       sync.RWMutex
	path     string
	config   AppConfig
	warnings []string
}

func NewStore() (*Store, error) {
	dir, err := appDataDir()
	if err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "config.json")}
	s.config.Language = "zh-CN"
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func decodeAppConfig(data []byte) (AppConfig, error) {
	var config AppConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return AppConfig{}, err
	}
	if strings.TrimSpace(config.Language) == "" {
		config.Language = "zh-CN"
	}
	config.StartupWarnings = nil
	return config, nil
}

func hydrateConfigSecrets(config *AppConfig) []string {
	warnings := make([]string, 0)
	for index := range config.Instances {
		instance := &config.Instances[index]
		if instance.EncryptedAdminPassword != "" {
			value, err := unprotectSecret(instance.EncryptedAdminPassword)
			if err != nil {
				instance.AdminPassword = ""
				warnings = append(warnings, fmt.Sprintf("服务器“%s”的管理员密码无法解密，请重新输入并保存", instance.Name))
			} else {
				instance.AdminPassword = value
			}
		}
		if instance.EncryptedServerPassword != "" {
			value, err := unprotectSecret(instance.EncryptedServerPassword)
			if err != nil {
				instance.ServerPassword = ""
				warnings = append(warnings, fmt.Sprintf("服务器“%s”的入服密码无法解密，请重新输入并保存", instance.Name))
			} else {
				instance.ServerPassword = value
			}
		}
	}
	return warnings
}

func configForPersistence(config AppConfig) (AppConfig, error) {
	config.StartupWarnings = nil
	config.Instances = append([]ServerInstance(nil), config.Instances...)
	for index := range config.Instances {
		instance := &config.Instances[index]
		if instance.AdminPassword != "" {
			encrypted, err := protectSecret(instance.AdminPassword)
			if err != nil {
				return AppConfig{}, fmt.Errorf("encrypt administrator password for %s: %w", instance.Name, err)
			}
			instance.EncryptedAdminPassword = encrypted
		}
		if instance.ServerPassword != "" {
			encrypted, err := protectSecret(instance.ServerPassword)
			if err != nil {
				return AppConfig{}, fmt.Errorf("encrypt server password for %s: %w", instance.Name, err)
			}
			instance.EncryptedServerPassword = encrypted
		}
		instance.AdminPassword = ""
		instance.ServerPassword = ""
	}
	return config, nil
}

func normalizeStoredConfig(config *AppConfig) bool {
	migrated := false
	if strings.TrimSpace(config.Language) == "" {
		config.Language = "zh-CN"
		migrated = true
	}
	config.StartupWarnings = nil
	for index := range config.Instances {
		if config.Instances[index].AdminPassword != "" && config.Instances[index].EncryptedAdminPassword == "" || config.Instances[index].ServerPassword != "" && config.Instances[index].EncryptedServerPassword == "" {
			migrated = true
		}
		updated := withDefaults(config.Instances[index])
		if updated != config.Instances[index] {
			migrated = true
		}
		config.Instances[index] = updated
	}
	return migrated
}

func configBackupPath(path string) string { return path + ".bak" }

func quarantineConfigFile(path string, now time.Time) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	candidate := fmt.Sprintf("%s.corrupt-%s", path, now.Format("20060102-150405"))
	for suffix := 2; ; suffix++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s.corrupt-%s-%d", path, now.Format("20060102-150405"), suffix)
	}
	if err := os.Rename(path, candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func writeAppConfig(path string, config AppConfig) error {
	config, err := configForPersistence(config)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func writeConfigBackup(path string, data []byte) error {
	config, err := decodeAppConfig(data)
	if err != nil {
		return nil
	}
	config, err = configForPersistence(config)
	if err != nil {
		return err
	}
	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	backup := configBackupPath(path)
	temporary := backup + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, backup); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func (s *Store) recoverConfig(mainErr error) error {
	backup := configBackupPath(s.path)
	backupData, backupReadErr := os.ReadFile(backup)
	if backupReadErr == nil {
		if recovered, decodeErr := decodeAppConfig(backupData); decodeErr == nil {
			quarantined, err := quarantineConfigFile(s.path, time.Now())
			if err != nil {
				return fmt.Errorf("quarantine corrupt config: %w", err)
			}
			normalizeStoredConfig(&recovered)
			s.warnings = append(s.warnings, hydrateConfigSecrets(&recovered)...)
			s.config = recovered
			if err := writeAppConfig(s.path, s.config); err != nil {
				return fmt.Errorf("restore config backup: %w", err)
			}
			message := "启动器配置损坏，已自动恢复上一份备份"
			if quarantined != "" {
				message += "；损坏文件已保留为 " + filepath.Base(quarantined)
			}
			s.warnings = append(s.warnings, message)
			return nil
		}
	}

	now := time.Now()
	quarantinedMain, quarantineMainErr := quarantineConfigFile(s.path, now)
	if quarantineMainErr != nil {
		return fmt.Errorf("quarantine corrupt config: %w", quarantineMainErr)
	}
	quarantinedBackup, quarantineBackupErr := quarantineConfigFile(backup, now)
	if quarantineBackupErr != nil {
		return fmt.Errorf("quarantine corrupt config backup: %w", quarantineBackupErr)
	}
	s.config = AppConfig{Language: "zh-CN"}
	if err := writeAppConfig(s.path, s.config); err != nil {
		return err
	}
	message := "启动器配置无法读取，已使用空白配置启动；原文件均已保留"
	if mainErr != nil {
		message += "（" + mainErr.Error() + "）"
	}
	if quarantinedMain != "" || quarantinedBackup != "" {
		names := make([]string, 0, 2)
		if quarantinedMain != "" {
			names = append(names, filepath.Base(quarantinedMain))
		}
		if quarantinedBackup != "" {
			names = append(names, filepath.Base(quarantinedBackup))
		}
		message += "：" + strings.Join(names, "、")
	}
	s.warnings = append(s.warnings, message)
	return nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		backupData, backupErr := os.ReadFile(configBackupPath(s.path))
		if os.IsNotExist(backupErr) {
			return nil
		}
		if backupErr != nil {
			return backupErr
		}
		recovered, decodeErr := decodeAppConfig(backupData)
		if decodeErr != nil {
			return s.recoverConfig(decodeErr)
		}
		normalizeStoredConfig(&recovered)
		s.warnings = append(s.warnings, hydrateConfigSecrets(&recovered)...)
		s.config = recovered
		if err := writeAppConfig(s.path, s.config); err != nil {
			return err
		}
		s.warnings = append(s.warnings, "启动器主配置缺失，已从上一份备份自动恢复")
		return nil
	}
	if err != nil {
		return err
	}
	config, decodeErr := decodeAppConfig(data)
	if decodeErr != nil {
		return s.recoverConfig(decodeErr)
	}
	migrated := normalizeStoredConfig(&config)
	s.warnings = append(s.warnings, hydrateConfigSecrets(&config)...)
	s.config = config
	if migrated {
		return s.saveLocked()
	}
	return nil
}

func (s *Store) Warnings() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.warnings...)
}

func (s *Store) AddWarning(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.warnings {
		if existing == message {
			return
		}
	}
	s.warnings = append(s.warnings, message)
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
	if current, err := os.ReadFile(s.path); err == nil {
		if err := writeConfigBackup(s.path, current); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeAppConfig(s.path, s.config)
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
	frpConfigs := s.config.FrpConfigs[:0]
	for _, config := range s.config.FrpConfigs {
		if config.ServerID != id {
			frpConfigs = append(frpConfigs, config)
		}
	}
	s.config.FrpConfigs = frpConfigs
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

func (s *Store) MarkMaintenanceTaskRunning(id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.config.MaintenanceTasks {
		if s.config.MaintenanceTasks[index].ID == id {
			s.config.MaintenanceTasks[index].LastRun = now.UnixMilli()
			s.config.MaintenanceTasks[index].LastStatus = "running"
			s.config.MaintenanceTasks[index].LastMessage = ""
			return s.saveLocked()
		}
	}
	return errors.New("maintenance task not found")
}

func (s *Store) RecoverInterruptedMaintenanceTasks(now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recovered := 0
	for index := range s.config.MaintenanceTasks {
		task := &s.config.MaintenanceTasks[index]
		if task.LastStatus != "running" {
			continue
		}
		task.LastStatus = "interrupted"
		task.LastMessage = "Agent 在任务完成前重启，任务已中断，请检查服务器状态后重试"
		if task.NextRun == 0 && task.Enabled {
			task.NextRun = nextMaintenanceRun(*task, now).UnixMilli()
		}
		recovered++
	}
	if recovered == 0 {
		return 0, nil
	}
	return recovered, s.saveLocked()
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

func (s *Store) SaveFrpConfig(config FrpConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.config.FrpConfigs {
		if s.config.FrpConfigs[index].ServerID == config.ServerID {
			s.config.FrpConfigs[index] = config
			return s.saveLocked()
		}
	}
	s.config.FrpConfigs = append(s.config.FrpConfigs, config)
	return s.saveLocked()
}

func (s *Store) FrpConfig(serverID string) (FrpConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, config := range s.config.FrpConfigs {
		if config.ServerID == serverID {
			return config, true
		}
	}
	return FrpConfig{}, false
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
		instance.Executable = defaultServerExecutable(instance.RootPath)
	}
	if instance.IconID == "" {
		instance.IconID = "SheepBall"
	}
	if instance.WorkerThreads < 0 {
		instance.WorkerThreads = 0
	}
	if instance.WorkerThreads > 256 {
		instance.WorkerThreads = 256
	}
	switch instance.ProcessPriority {
	case "normal", "above_normal", "high":
	default:
		instance.ProcessPriority = "above_normal"
	}
	switch instance.CPUAffinityMode {
	case "all", "auto":
	default:
		instance.CPUAffinityMode = "auto"
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

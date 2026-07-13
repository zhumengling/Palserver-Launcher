package main

import (
	"errors"
	"time"
)

var builtInGamePresets = []GamePreset{
	{ID: "casual", Name: "休闲", Description: "更快成长与更低惩罚", Values: map[string]string{"ExpRate": "2", "PalCaptureRate": "1.5", "CollectionDropRate": "1.5", "DeathPenalty": "None"}},
	{ID: "balanced", Name: "平衡", Description: "接近官方默认体验", Values: map[string]string{"ExpRate": "1", "PalCaptureRate": "1", "CollectionDropRate": "1", "DeathPenalty": "All"}},
	{ID: "pvp", Name: "PvP", Description: "开启玩家对战与友伤", Values: map[string]string{"bIsPvP": "True", "bEnableFriendlyFire": "True", "DeathPenalty": "All"}},
	{ID: "hardcore", Name: "硬核", Description: "较慢成长与完整死亡惩罚", Values: map[string]string{"ExpRate": "0.5", "PalCaptureRate": "0.7", "CollectionDropRate": "0.7", "DeathPenalty": "All"}},
	{ID: "performance", Name: "性能", Description: "降低实体密度和掉落物上限", Values: map[string]string{"PalSpawnNumRate": "0.7", "DropItemMaxNum": "1500", "BaseCampMaxNum": "64", "BaseCampWorkerMaxNum": "12"}},
}

var builtInGameEvents = []GamePreset{
	{ID: "double-xp", Name: "双倍经验", Description: "限时双倍经验", Values: map[string]string{"ExpRate": "2"}},
	{ID: "double-drops", Name: "双倍掉落", Description: "限时双倍采集与敌人掉落", Values: map[string]string{"CollectionDropRate": "2", "EnemyDropItemRate": "2"}},
	{ID: "capture-boost", Name: "捕获加成", Description: "限时提高捕获率", Values: map[string]string{"PalCaptureRate": "2"}},
}

func gamePresetByID(id string) (GamePreset, bool) {
	for _, preset := range append(append([]GamePreset{}, builtInGamePresets...), builtInGameEvents...) {
		if preset.ID == id {
			return preset, true
		}
	}
	return GamePreset{}, false
}

func eventExpired(event ActiveGameEvent, now time.Time) bool {
	return event.EndsAt > 0 && event.EndsAt <= now.UnixMilli()
}

func (a *App) ListGamePresets() []GamePreset { return append([]GamePreset(nil), builtInGamePresets...) }
func (a *App) ListGameEvents() []GamePreset  { return append([]GamePreset(nil), builtInGameEvents...) }

func (a *App) ApplyGamePreset(serverID, presetID string) error {
	preset, ok := gamePresetByID(presetID)
	if !ok {
		return errors.New("unknown game preset")
	}
	return a.applyWorldSettingsManaged(serverID, func(content string) (string, error) {
		return mergeWorldSettingValues(content, preset.Values)
	})
}

func (a *App) StartGameEvent(serverID, eventID string, durationMinutes int, customValues map[string]string) (ActiveGameEvent, error) {
	if _, active := a.store.ActiveEvent(serverID); active {
		return ActiveGameEvent{}, errors.New("another game event is already active")
	}
	definition, ok := gamePresetByID(eventID)
	if eventID == "custom" {
		definition = GamePreset{ID: "custom", Name: "自定义活动", Values: customValues}
		ok = len(customValues) > 0
	}
	if !ok || durationMinutes < 1 {
		return ActiveGameEvent{}, errors.New("valid event and duration are required")
	}
	original, err := a.ReadWorldSettings(serverID)
	if err != nil {
		return ActiveGameEvent{}, err
	}
	if err := a.applyWorldSettingsManaged(serverID, func(content string) (string, error) {
		return mergeWorldSettingValues(content, definition.Values)
	}); err != nil {
		return ActiveGameEvent{}, err
	}
	now := time.Now()
	event := ActiveGameEvent{ServerID: serverID, EventID: definition.ID, Name: definition.Name, StartedAt: now.UnixMilli(), EndsAt: now.Add(time.Duration(durationMinutes) * time.Minute).UnixMilli(), OriginalSettings: original, Values: definition.Values}
	return event, a.store.SaveActiveEvent(event)
}

func (a *App) GetActiveGameEvent(serverID string) ActiveGameEvent {
	event, _ := a.store.ActiveEvent(serverID)
	return event
}

func (a *App) StopGameEvent(serverID string) error {
	event, ok := a.store.ActiveEvent(serverID)
	if !ok {
		return errors.New("no active game event")
	}
	if err := a.applyWorldSettingsManaged(serverID, func(string) (string, error) { return event.OriginalSettings, nil }); err != nil {
		return err
	}
	return a.store.DeleteActiveEvent(serverID)
}

func (a *App) pollGameEvents(now time.Time) {
	for _, event := range a.store.ActiveEvents() {
		if eventExpired(event, now) && a.currentOperation(event.ServerID) == "" {
			go func(serverID string) { _ = a.StopGameEvent(serverID) }(event.ServerID)
		}
	}
}

func (a *App) applyWorldSettingsManaged(serverID string, transform func(string) (string, error)) error {
	if !a.tryBeginOperation(serverID, "settings") {
		return errors.New("server is busy")
	}
	defer a.endOperation(serverID)
	instance, err := a.store.Find(serverID)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	wasRunning := status.Running
	if wasRunning {
		_, _ = sendRCON(instance, "Save")
		if err := a.restartStopOnly(serverID); err != nil {
			return err
		}
	}
	content, err := a.ReadWorldSettings(serverID)
	if err != nil {
		return err
	}
	updated, err := transform(content)
	if err != nil {
		return err
	}
	if err := a.WriteWorldSettings(serverID, updated); err != nil {
		return err
	}
	if wasRunning {
		return a.StartServer(serverID)
	}
	return nil
}

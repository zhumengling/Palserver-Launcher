package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func playerIdentity(player Player) string {
	if strings.TrimSpace(player.UserID) != "" {
		return strings.TrimSpace(player.UserID)
	}
	return strings.TrimSpace(player.PlayerID)
}

func containsAlias(values []string, value string) bool {
	for _, item := range values {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

func mergePlayerHistory(existing []PlayerHistoryEntry, players []Player, now time.Time) []PlayerHistoryEntry {
	result := append([]PlayerHistoryEntry(nil), existing...)
	index := map[string]int{}
	for i := range result {
		result[i].Online = false
		if result[i].UserID != "" {
			index[result[i].UserID] = i
		}
		if result[i].PlayerID != "" {
			index[result[i].PlayerID] = i
		}
	}
	for _, player := range players {
		identity := playerIdentity(player)
		if identity == "" {
			continue
		}
		position, found := index[identity]
		if !found {
			result = append(result, PlayerHistoryEntry{UserID: player.UserID, PlayerID: player.PlayerID, FirstSeen: now.UnixMilli()})
			position = len(result) - 1
			index[identity] = position
		}
		entry := &result[position]
		wasOnline := existingOnline(existing, entry.UserID, entry.PlayerID)
		entry.UserID, entry.PlayerID = player.UserID, player.PlayerID
		entry.Name, entry.LastIP, entry.LastSeen, entry.Online = player.Name, player.IP, now.UnixMilli(), true
		if !wasOnline {
			entry.Visits++
		}
		if entry.FirstSeen == 0 {
			entry.FirstSeen = now.UnixMilli()
		}
		if player.Name != "" && !containsAlias(entry.Aliases, player.Name) {
			entry.Aliases = append(entry.Aliases, player.Name)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastSeen > result[j].LastSeen })
	return result
}

func existingOnline(entries []PlayerHistoryEntry, userID, playerID string) bool {
	for _, entry := range entries {
		if (userID != "" && entry.UserID == userID) || (playerID != "" && entry.PlayerID == playerID) {
			return entry.Online
		}
	}
	return false
}

func shouldKickForWhitelist(enforced bool, entry PlayerHistoryEntry) bool {
	return enforced && !entry.Whitelisted
}

func playerPresenceTransitions(before, after []PlayerHistoryEntry) (joined, left []PlayerHistoryEntry) {
	previous := map[string]bool{}
	for _, entry := range before {
		previous[entry.UserID+"\x00"+entry.PlayerID] = entry.Online
	}
	for _, entry := range after {
		key := entry.UserID + "\x00" + entry.PlayerID
		if entry.Online && !previous[key] {
			joined = append(joined, entry)
		}
		if !entry.Online && previous[key] {
			left = append(left, entry)
		}
	}
	return joined, left
}

func (a *App) pollPlayerHistory(now time.Time) {
	for _, instance := range a.store.Snapshot().Instances {
		status, _ := a.GetStatus(instance.ID)
		if !status.Running {
			continue
		}
		players, err := a.GetPlayers(instance.ID)
		if err != nil {
			continue
		}
		before := a.store.PlayerHistory(instance.ID)
		entries, err := a.store.MergePlayerHistory(instance.ID, players, now)
		if err != nil || !instance.WhitelistEnforced {
			if err != nil {
				continue
			}
		}
		joined, left := playerPresenceTransitions(before, entries)
		for _, entry := range joined {
			a.notifyDiscord(instance.ID, "player-join", "玩家加入", entry.Name)
		}
		for _, entry := range left {
			a.notifyDiscord(instance.ID, "player-leave", "玩家离开", entry.Name)
		}
		if !instance.WhitelistEnforced {
			continue
		}
		byID := map[string]PlayerHistoryEntry{}
		for _, entry := range entries {
			byID[entry.UserID] = entry
			byID[entry.PlayerID] = entry
		}
		for _, player := range players {
			entry := byID[playerIdentity(player)]
			if shouldKickForWhitelist(true, entry) {
				if _, kickErr := restPost(instance, "/kick", map[string]any{"userid": player.UserID, "message": "Whitelist only"}); kickErr == nil {
					a.invalidateOfficialCache(instance.ID, "players")
				}
			}
		}
	}
}

func (a *App) ListPlayerHistory(serverID string) []PlayerHistoryEntry {
	return a.store.PlayerHistory(serverID)
}

func (a *App) SetPlayerWhitelist(serverID, userID string, whitelisted bool) (PlayerHistoryEntry, error) {
	return a.store.UpdatePlayerHistory(serverID, userID, func(entry *PlayerHistoryEntry) { entry.Whitelisted = whitelisted })
}

func (a *App) SetPlayerNote(serverID, userID, note string) (PlayerHistoryEntry, error) {
	return a.store.UpdatePlayerHistory(serverID, userID, func(entry *PlayerHistoryEntry) { entry.Note = strings.TrimSpace(note) })
}

func (a *App) BanHistoricalPlayer(serverID, userID string) error {
	instance, err := a.store.Find(serverID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("user id is required")
	}
	status, _ := serverStatus(instance)
	if status.Running {
		if _, restErr := restPost(instance, "/ban", map[string]any{"userid": userID, "message": "Banned by Palserver Launcher"}); restErr == nil {
			a.invalidateOfficialCache(serverID, "players")
			_, _ = a.store.UpdatePlayerHistory(serverID, userID, func(entry *PlayerHistoryEntry) { entry.Banned = true })
			return nil
		}
	}
	path := filepath.Join(instance.RootPath, "Pal", "Saved", "SaveGames", "banlist.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, _ := os.ReadFile(path)
	for _, value := range parseBanList(string(data)) {
		if value == userID {
			return nil
		}
	}
	content := strings.TrimSpace(string(data))
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content+userID+"\n"), 0o600); err != nil {
		return err
	}
	_, _ = a.store.UpdatePlayerHistory(serverID, userID, func(entry *PlayerHistoryEntry) { entry.Banned = true })
	return nil
}

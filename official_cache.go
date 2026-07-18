package main

import (
	"errors"
	"strings"
	"time"
)

const (
	officialInfoCacheTTL     = 60 * time.Second
	officialMetricsCacheTTL  = 3 * time.Second
	officialPlayersCacheTTL  = 2 * time.Second
	officialSettingsCacheTTL = 30 * time.Second
	officialWorldCacheTTL    = 15 * time.Second
	officialErrorCacheTTL    = 2 * time.Second
)

type officialCacheEntry struct {
	value     any
	err       error
	sampledAt time.Time
	loading   chan struct{}
}

func officialCacheKey(serverID, endpoint string) string {
	return strings.TrimSpace(serverID) + "\x00" + endpoint
}

func (a *App) cachedOfficialValue(serverID, endpoint string, ttl time.Duration, fetch func() (any, error)) (any, error) {
	if ttl <= 0 {
		return fetch()
	}
	key := officialCacheKey(serverID, endpoint)
	for {
		a.officialCacheMu.Lock()
		if a.officialCache == nil {
			a.officialCache = map[string]*officialCacheEntry{}
		}
		entry := a.officialCache[key]
		if entry != nil && entry.loading != nil {
			waiting := entry.loading
			a.officialCacheMu.Unlock()
			<-waiting
			continue
		}
		if entry != nil && !entry.sampledAt.IsZero() {
			maximumAge := ttl
			if entry.err != nil && maximumAge > officialErrorCacheTTL {
				maximumAge = officialErrorCacheTTL
			}
			if time.Since(entry.sampledAt) <= maximumAge {
				value, err := entry.value, entry.err
				a.officialCacheMu.Unlock()
				return value, err
			}
		}
		if entry == nil {
			entry = &officialCacheEntry{}
			a.officialCache[key] = entry
		}
		entry.loading = make(chan struct{})
		loading := entry.loading
		a.officialCacheMu.Unlock()

		value, err := fetch()
		a.officialCacheMu.Lock()
		entry.value = value
		entry.err = err
		entry.sampledAt = time.Now()
		entry.loading = nil
		close(loading)
		a.officialCacheMu.Unlock()
		return value, err
	}
}

func cachedOfficial[T any](a *App, serverID, endpoint string, ttl time.Duration, fetch func() (T, error)) (T, error) {
	var zero T
	value, err := a.cachedOfficialValue(serverID, endpoint, ttl, func() (any, error) {
		return fetch()
	})
	if err != nil {
		return zero, err
	}
	typed, ok := value.(T)
	if !ok {
		return zero, errors.New("official REST cache returned an unexpected value type")
	}
	return typed, nil
}

func (a *App) cachedServerInfo(instance ServerInstance) (ServerInfo, error) {
	return cachedOfficial(a, instance.ID, "info", officialInfoCacheTTL, func() (ServerInfo, error) {
		return getOfficialServerInfo(instance)
	})
}

func (a *App) cachedServerMetrics(instance ServerInstance) (ServerMetrics, error) {
	return cachedOfficial(a, instance.ID, "metrics", officialMetricsCacheTTL, func() (ServerMetrics, error) {
		return getOfficialServerMetrics(instance)
	})
}

func (a *App) cachedServerPlayers(instance ServerInstance) ([]Player, error) {
	return cachedOfficial(a, instance.ID, "players", officialPlayersCacheTTL, func() ([]Player, error) {
		return getOfficialPlayers(instance)
	})
}

func (a *App) cachedServerSettings(instance ServerInstance) (ServerSettings, error) {
	return cachedOfficial(a, instance.ID, "settings", officialSettingsCacheTTL, func() (ServerSettings, error) {
		return getOfficialServerSettings(instance)
	})
}

func (a *App) cachedWorldSnapshot(instance ServerInstance) (WorldSnapshot, error) {
	return cachedOfficial(a, instance.ID, "game-data", officialWorldCacheTTL, func() (WorldSnapshot, error) {
		return getOfficialWorldSnapshot(instance)
	})
}

func (a *App) invalidateOfficialCache(serverID string, endpoints ...string) {
	serverID = strings.TrimSpace(serverID)
	a.officialCacheMu.Lock()
	defer a.officialCacheMu.Unlock()
	if len(endpoints) == 0 {
		prefix := serverID + "\x00"
		for key := range a.officialCache {
			if strings.HasPrefix(key, prefix) {
				delete(a.officialCache, key)
			}
		}
		return
	}
	for _, endpoint := range endpoints {
		delete(a.officialCache, officialCacheKey(serverID, endpoint))
	}
}

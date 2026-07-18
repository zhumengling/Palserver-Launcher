package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type ServerInfo struct {
	Version     string `json:"version"`
	ServerName  string `json:"servername"`
	Description string `json:"description"`
	WorldGUID   string `json:"worldguid"`
}

type ServerMetrics struct {
	ServerFPS        float64 `json:"serverfps"`
	CurrentPlayerNum int     `json:"currentplayernum"`
	ServerFrameTime  float64 `json:"serverframetime"`
	MaxPlayerNum     int     `json:"maxplayernum"`
	Uptime           int64   `json:"uptime"`
	BaseCampNum      int     `json:"basecampnum"`
	Days             int     `json:"days"`
}

type ServerSettingEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ServerSettings struct {
	Values  map[string]string    `json:"values"`
	Entries []ServerSettingEntry `json:"entries"`
}

type OfficialServerSettings = ServerSettings

type WorldActor struct {
	Type              string  `json:"Type"`
	InstanceID        string  `json:"InstanceID"`
	UnitType          string  `json:"UnitType"`
	NickName          string  `json:"NickName"`
	TrainerInstanceID string  `json:"TrainerInstanceID"`
	TrainerNickName   string  `json:"TrainerNickName"`
	TrainerClass      string  `json:"TrainerClass"`
	UserID            string  `json:"userid"`
	IP                string  `json:"ip"`
	Level             int     `json:"level"`
	HP                float64 `json:"HP"`
	MaxHP             float64 `json:"MaxHP"`
	GuildID           string  `json:"GuildID"`
	GuildName         string  `json:"GuildName"`
	Class             string  `json:"Class"`
	Action            string  `json:"Action"`
	AIAction          string  `json:"AI_Action"`
	LocationX         float64 `json:"LocationX"`
	LocationY         float64 `json:"LocationY"`
	LocationZ         float64 `json:"LocationZ"`
	RotationX         float64 `json:"RotationX"`
	RotationY         float64 `json:"RotationY"`
	RotationZ         float64 `json:"RotationZ"`
	Stage             string  `json:"Stage"`
	IsActive          bool    `json:"IsActive"`
}

type WorldSnapshot struct {
	Time              string       `json:"Time"`
	FPS               float64      `json:"FPS"`
	AverageFPS        float64      `json:"AverageFPS"`
	ActorData         []WorldActor `json:"ActorData"`
	Available         bool         `json:"available"`
	UnavailableReason string       `json:"unavailableReason"`
}

type officialPlayerList struct {
	Players []struct {
		Name          string  `json:"name"`
		AccountName   string  `json:"accountName"`
		PlayerID      string  `json:"playerId"`
		UserID        string  `json:"userId"`
		IP            string  `json:"ip"`
		Ping          float64 `json:"ping"`
		LocationX     float64 `json:"location_x"`
		LocationY     float64 `json:"location_y"`
		Level         int     `json:"level"`
		BuildingCount int     `json:"building_count"`
	} `json:"players"`
}

func restRequestJSON[T any](instance ServerInstance, method, endpoint string, body any) (T, error) {
	var result T
	data, err := restRequestBytes(instance, method, endpoint, body)
	if err != nil {
		return result, err
	}
	if len(data) == 0 {
		return result, nil
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("decode %s response: %w", endpoint, err)
	}
	return result, nil
}

func getOfficialServerInfo(instance ServerInstance) (ServerInfo, error) {
	return restRequestJSON[ServerInfo](instance, http.MethodGet, "/info", nil)
}

func getOfficialServerMetrics(instance ServerInstance) (ServerMetrics, error) {
	return restRequestJSON[ServerMetrics](instance, http.MethodGet, "/metrics", nil)
}

func getOfficialPlayers(instance ServerInstance) ([]Player, error) {
	payload, err := restRequestJSON[officialPlayerList](instance, http.MethodGet, "/players", nil)
	if err != nil {
		return nil, err
	}
	players := make([]Player, len(payload.Players))
	for index, item := range payload.Players {
		players[index] = Player{Name: item.Name, AccountName: item.AccountName, PlayerID: item.PlayerID, UserID: item.UserID, IP: item.IP, Ping: item.Ping, LocationX: item.LocationX, LocationY: item.LocationY, Level: item.Level, BuildingCount: item.BuildingCount}
	}
	return players, nil
}

func officialSettingValue(value any) string {
	switch typed := value.(type) {
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case string:
		return typed
	default:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	}
}

func getOfficialServerSettings(instance ServerInstance) (ServerSettings, error) {
	raw, err := restRequestJSON[map[string]any](instance, http.MethodGet, "/settings", nil)
	if err != nil {
		return ServerSettings{}, err
	}
	result := ServerSettings{Values: make(map[string]string, len(raw)), Entries: make([]ServerSettingEntry, 0, len(raw))}
	keys := make([]string, 0, len(raw))
	for key, value := range raw {
		result.Values[key] = officialSettingValue(value)
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result.Entries = append(result.Entries, ServerSettingEntry{Key: key, Value: result.Values[key]})
	}
	return result, nil
}

func getOfficialWorldSnapshot(instance ServerInstance) (WorldSnapshot, error) {
	snapshot, err := restRequestJSON[WorldSnapshot](instance, http.MethodGet, "/game-data", nil)
	if err != nil {
		var responseErr *restHTTPError
		if errors.As(err, &responseErr) && responseErr.StatusCode == http.StatusNotFound && strings.Contains(strings.ToLower(responseErr.Body), "gamedata api is not enabled") {
			return WorldSnapshot{UnavailableReason: "当前 Palworld 服务端未启用 GameData API，世界快照暂不可用。"}, nil
		}
		return WorldSnapshot{}, err
	}
	snapshot.Available = true
	return snapshot, nil
}

func saveWorldREST(instance ServerInstance) error {
	_, err := restPost(instance, "/save", nil)
	return err
}

func shutdownServerREST(instance ServerInstance, waitTime int, message string) error {
	if waitTime < 0 {
		return fmt.Errorf("wait time must be zero or greater")
	}
	_, err := restPost(instance, "/shutdown", map[string]any{"waittime": waitTime, "message": message})
	return err
}

func (a *App) GetServerInfo(id string) (ServerInfo, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return ServerInfo{}, err
	}
	return a.cachedServerInfo(instance)
}

func (a *App) GetServerMetrics(id string) (ServerMetrics, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return ServerMetrics{}, err
	}
	return a.cachedServerMetrics(instance)
}

func (a *App) GetServerSettings(id string) (ServerSettings, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return ServerSettings{}, err
	}
	return a.cachedServerSettings(instance)
}

func (a *App) GetWorldSnapshot(id string) (WorldSnapshot, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return WorldSnapshot{}, err
	}
	return a.cachedWorldSnapshot(instance)
}

func (a *App) ShutdownServer(id string, waitTime int, message string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	if waitTime < 0 {
		return fmt.Errorf("wait time must be zero or greater")
	}
	a.markExpectedStop(id)
	a.setGuardianSuppressed(id, true)
	if err := shutdownServerREST(instance, waitTime, message); err != nil {
		a.clearExpectedStop(id)
		a.setGuardianSuppressed(id, false)
		return err
	}
	return nil
}

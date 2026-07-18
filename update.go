package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNoUpdateAvailable = errors.New("server is already up to date")
	ErrPlayersOnline     = errors.New("players are online; update deferred")
	steamBuildInfoURL    = "https://api.steamcmd.net/v1/info/2394010"
	acfBuildPattern      = regexp.MustCompile(`(?i)"buildid"\s+"(\d+)"`)
)

func parseACFBuildID(content string) string {
	match := acfBuildPattern.FindStringSubmatch(content)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func parseRemoteBuildID(data []byte, branch string) (string, error) {
	var payload struct {
		Data map[string]struct {
			Depots struct {
				Branches map[string]struct {
					BuildID any `json:"buildid"`
				} `json:"branches"`
			} `json:"depots"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return "", err
	}
	if branch == "" {
		branch = "public"
	}
	value, ok := payload.Data["2394010"].Depots.Branches[branch]
	if !ok {
		return "", errors.New("Steam branch was not found")
	}
	buildID := strings.TrimSpace(fmt.Sprint(value.BuildID))
	if buildID == "" || buildID == "<nil>" {
		return "", errors.New("Steam build id was not found")
	}
	return buildID, nil
}

func localServerBuildID(instance ServerInstance) string {
	steamcmd := steamCMDExecutable(instance.SteamCMDPath)
	candidates := []string{
		filepath.Join(instance.RootPath, "steamapps", "appmanifest_2394010.acf"),
		filepath.Join(filepath.Dir(steamcmd), "steamapps", "appmanifest_2394010.acf"),
	}
	for _, path := range candidates {
		if data, err := os.ReadFile(path); err == nil {
			if buildID := parseACFBuildID(string(data)); buildID != "" {
				return buildID
			}
		}
	}
	return ""
}

func remoteServerBuildID(branch string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(steamBuildInfoURL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Steam build lookup: %s", response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	return parseRemoteBuildID(data, branch)
}

func (a *App) GetServerUpdateStatus(id string) (ServerUpdateStatus, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return ServerUpdateStatus{}, err
	}
	branch := "public"
	local := localServerBuildID(instance)
	remote, err := remoteServerBuildID(branch)
	if err != nil {
		return ServerUpdateStatus{}, err
	}
	return ServerUpdateStatus{Installed: local != "", UpdateAvailable: local == "" || local != remote, LocalBuildID: local, RemoteBuildID: remote, Branch: branch}, nil
}

func (a *App) PerformManagedUpdate(id string, force bool) error {
	if !a.tryBeginOperation(id, "update") {
		return errors.New("server is busy")
	}
	defer a.endOperation(id)
	a.notifyDiscord(id, "update", "服务器更新开始", "正在检查并安装更新")
	err := a.performServerUpdate(id, force)
	if err != nil {
		a.notifyDiscord(id, "update", "服务器更新未完成", err.Error())
		return err
	}
	a.notifyDiscord(id, "update", "服务器更新完成", "SteamCMD 更新已安装")
	return nil
}

func (a *App) performServerUpdate(id string, force bool) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	if !force {
		status, err := a.GetServerUpdateStatus(id)
		if err != nil {
			return err
		}
		if !status.UpdateAvailable {
			return ErrNoUpdateAvailable
		}
	}
	runtimeStatus, _ := serverStatus(instance)
	wasRunning := runtimeStatus.Running
	players := []Player{}
	if wasRunning {
		players, _ = a.GetPlayers(id)
		if instance.UpdateOnlyWhenEmpty && len(players) > 0 && !force {
			return ErrPlayersOnline
		}
	}
	if _, backupErr := a.createBackup(id); backupErr != nil && !os.IsNotExist(backupErr) && !errors.Is(backupErr, ErrSaveDirectoryNotFound) {
		return fmt.Errorf("pre-update backup: %w", backupErr)
	}
	if wasRunning && len(players) > 0 {
		minutes := instance.UpdateWarnMinutes
		if minutes > 30 {
			minutes = 30
		}
		for remaining := minutes; remaining > 0; remaining-- {
			if remaining == minutes || remaining == 5 || remaining == 1 {
				_, _ = restPost(instance, "/announce", map[string]any{"message": fmt.Sprintf("Server update in %d minute(s)", remaining)})
			}
			time.Sleep(time.Minute)
		}
	}
	if wasRunning {
		if err := a.restartStopOnly(id); err != nil {
			return err
		}
	}
	if err := a.installOrUpdateServer(id); err != nil {
		return err
	}
	if wasRunning {
		return a.StartServer(id)
	}
	return nil
}

func (a *App) restartStopOnly(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	_, _ = sendRCON(instance, "Save")
	if err := a.StopServer(id); err != nil {
		return err
	}
	for attempt := 0; attempt < 45; attempt++ {
		time.Sleep(2 * time.Second)
		status, _ := serverStatus(instance)
		if !status.Running {
			return nil
		}
	}
	return errors.New("server did not stop before update")
}

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var verifiedServerModCatalog = []ServerModCatalogEntry{
	{ID: "pal-evo", Name: "PalEvo", Version: "1", UpdatedAt: "2026-07-13 13:01", Description: "让帕鲁在达到指定等级后自动进化，并保留等级、个体值、被动技能和昵称；仅服务器端安装。", NexusURL: "https://www.nexusmods.com/palworld/mods/3679", Dependency: "Palworld Experimental UE4SS", Warning: "当前仍是 Beta，作者已在 Palworld 1.0 专用服务器测试；部分游戏内提示暂为葡萄牙语。", FolderName: "PalEvolution"},
	{ID: "better-server-commands", Name: "Better Server-Side Commands", Version: "1.1", UpdatedAt: "2026-07-13 08:53", Description: "为管理员和玩家增加聊天命令、传送点、发放道具/经验、生成帕鲁和管理功能。", NexusURL: "https://www.nexusmods.com/palworld/mods/3669", Dependency: "UE4SS（Palworld 1.0 兼容版）", Warning: "服务器管理权限较高，只应授予可信管理员。", FolderName: "PalServerCommands"},
	{ID: "palzones", Name: "PalZones", Version: "1.37", UpdatedAt: "2026-07-13 10:36", Description: "在地图上创建安全区、PvP 区域和不可破坏建筑区域，并支持自动重载 zones.json。", NexusURL: "https://www.nexusmods.com/palworld/mods/1838", Dependency: "UE4SS v3.0.1；服务器需要开启 PvP", Warning: "作者明确标注支持 Palworld v1.0.0.100427；需通过作者网页生成 zones.json。", FolderName: "PalZones"},
	{ID: "level-lock", Name: "Level Lock", Version: "2.1.1", UpdatedAt: "2026-07-13 05:37", Description: "按高塔首领进度限制玩家和可选帕鲁等级，支持全服共享或玩家独立进度。", NexusURL: "https://www.nexusmods.com/palworld/mods/3534", Dependency: "UE4SS；专用服务器建议使用作者指定的 Palworld experimental build", Warning: "安装到已有存档前应设置已击败高塔数量，并先创建存档备份。", FolderName: "LevelLock"},
	{ID: "rewards-engine", Name: "RewardsEngine", Version: "1.2.1", UpdatedAt: "2026-07-13 00:44", Description: "为服务器增加等级与活跃奖励系统，可按登录、采集、制作、PvP 和 NPC 击杀自动发放奖励。", NexusURL: "https://www.nexusmods.com/palworld/mods/3246", Dependency: "Palworld Experimental UE4SS", Warning: "仅支持服务器，不适用于单人游戏；安装后需要检查 config.lua 中的等级和奖励权限。", FolderName: "RewardsEngine"},
	{ID: "starter-kit", Name: "Starter Kit", Version: "2.3", UpdatedAt: "2026-07-12 03:03", Description: "为新玩家、每日签到或管理员发放可配置礼包，支持物品、食物和自动进入帕鲁终端的帕鲁。", NexusURL: "https://www.nexusmods.com/palworld/mods/2277", Dependency: "Palworld Experimental UE4SS", Warning: "支持 Windows、Linux、Proton/Wine 专用服务器；请在 kit.lua 中核对道具和帕鲁代码。", FolderName: "StarterKit"},
}

func sortedServerModCatalog() []ServerModCatalogEntry {
	result := append([]ServerModCatalogEntry(nil), verifiedServerModCatalog...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].UpdatedAt > result[j].UpdatedAt })
	return result
}

func serverModCatalogEntry(id string) (ServerModCatalogEntry, error) {
	for _, entry := range verifiedServerModCatalog {
		if entry.ID == id {
			return entry, nil
		}
	}
	return ServerModCatalogEntry{}, errors.New("unknown server mod")
}

func nexusURLForCatalog(id string) (string, error) {
	entry, err := serverModCatalogEntry(id)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(entry.NexusURL, "https://www.nexusmods.com/palworld/mods/") {
		return "", errors.New("invalid Nexus mod URL")
	}
	return entry.NexusURL, nil
}

func (a *App) OpenNexusModPage(catalogID string) error {
	url, err := nexusURLForCatalog(catalogID)
	if err != nil {
		return err
	}
	return openExternalURLPlatform(a, url)
}

func catalogModInstallState(modsRoot, folderName string) (installed, enabled bool, installedPath string) {
	enabledPath := filepath.Join(modsRoot, folderName)
	if info, err := os.Stat(enabledPath); err == nil && info.IsDir() {
		return true, true, enabledPath
	}
	disabledPath := enabledPath + ".disabled"
	if info, err := os.Stat(disabledPath); err == nil && info.IsDir() {
		return true, false, disabledPath
	}
	return false, false, ""
}

func (a *App) ListServerModCatalog(serverID string) ([]ServerModCatalogEntry, error) {
	instance, err := a.store.Find(serverID)
	if err != nil {
		return nil, err
	}
	root := ue4ssModsRoot(instance)
	result := sortedServerModCatalog()
	for index := range result {
		result[index].Installed, result[index].Enabled, result[index].InstalledPath = catalogModInstallState(root, result[index].FolderName)
		if result[index].InstalledPath != "" {
			if metadata, metadataErr := readServerModMetadata(result[index].InstalledPath); metadataErr == nil {
				result[index].InstalledVersion = metadata.Version
				result[index].InstalledUpdatedAt = metadata.UpdatedAt
			}
		}
	}
	return result, nil
}

func findCatalogModDirectory(root, folderName string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && strings.EqualFold(info.Name(), folderName) {
			found = path
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("压缩包中没有找到 %s 文件夹", folderName)
	}
	return found, nil
}

func setUE4SSModEnabled(modsRoot, folderName string, enabled bool) error {
	path := filepath.Join(modsRoot, "mods.txt")
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	prefix := strings.ToLower(folderName) + " :"
	found := false
	for index, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), prefix) {
			lines[index] = fmt.Sprintf("%s : %d", folderName, map[bool]int{false: 0, true: 1}[enabled])
			found = true
		}
	}
	if !found {
		lines = append(lines, fmt.Sprintf("%s : %d", folderName, map[bool]int{false: 0, true: 1}[enabled]))
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(strings.Join(lines, "\r\n"))+"\r\n"), 0o600)
}

func (a *App) InstallServerModArchive(serverID, catalogID, archivePath string) error {
	if !serverModsSupported() {
		return errors.New(serverModsUnsupportedReason())
	}
	if !a.tryBeginOperation(serverID, "mods") {
		return errors.New("server is busy")
	}
	defer a.endOperation(serverID)
	instance, err := a.store.Find(serverID)
	if err != nil {
		return err
	}
	status, _ := a.GetStatus(instance.ID)
	if status.Running {
		return errors.New("请先停止服务器再安装模组")
	}
	entry, err := serverModCatalogEntry(catalogID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(archivePath), ".zip") {
		return errors.New("目前仅支持 Nexus 下载的 ZIP 压缩包")
	}
	temp, err := os.MkdirTemp("", "palserver-nexus-mod-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err := unzipSafe(archivePath, temp); err != nil {
		return err
	}
	source, err := findCatalogModDirectory(temp, entry.FolderName)
	if err != nil {
		return err
	}
	modsRoot := ue4ssModsRoot(instance)
	destination := filepath.Join(modsRoot, entry.FolderName)
	if installed, _, installedPath := catalogModInstallState(modsRoot, entry.FolderName); installed {
		base, dataErr := appDataDir()
		if dataErr != nil {
			return dataErr
		}
		backup := filepath.Join(base, "mod-backups", serverID, entry.ID+"-"+time.Now().Format("20060102-150405"))
		if err := copyTree(installedPath, backup); err != nil {
			return err
		}
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
		if err := os.RemoveAll(destination + ".disabled"); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(modsRoot, 0o755); err != nil {
		return err
	}
	if err := copyTree(source, destination); err != nil {
		return err
	}
	metadata := serverModMetadata{CatalogID: entry.ID, Version: entry.Version, UpdatedAt: entry.UpdatedAt}
	if latest, ok := a.cachedNexusModInfo(entry.ID); ok {
		metadata.Version = latest.Version
		metadata.UpdatedAt = latest.UpdatedAt
	}
	if err := writeServerModMetadata(destination, metadata); err != nil {
		return err
	}
	return setUE4SSModEnabled(modsRoot, entry.FolderName, true)
}

func (a *App) UninstallServerMod(serverID, catalogID string) error {
	if !serverModsSupported() {
		return errors.New(serverModsUnsupportedReason())
	}
	instance, err := a.store.Find(serverID)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("请先停止服务器再卸载模组")
	}
	entry, err := serverModCatalogEntry(catalogID)
	if err != nil {
		return err
	}
	modsRoot := ue4ssModsRoot(instance)
	if err := os.RemoveAll(filepath.Join(modsRoot, entry.FolderName)); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(modsRoot, entry.FolderName) + ".disabled"); err != nil {
		return err
	}
	return setUE4SSModEnabled(modsRoot, entry.FolderName, false)
}

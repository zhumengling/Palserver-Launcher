package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func buildManagedInstance(base, name string) ServerInstance {
	return buildManagedInstanceAt(base, name, "")
}

func buildManagedInstanceAt(base, name, installRoot string) ServerInstance {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "我的帕鲁服务器"
	}
	root := filepath.Clean(strings.TrimSpace(installRoot))
	steamcmd := filepath.Join(base, "runtime", "steamcmd", "steamcmd.exe")
	if root == "." || root == "" {
		directory := safeDirectoryName(name)
		if directory == "" {
			directory = "palworld-server"
		}
		root = filepath.Join(base, "servers", directory)
	} else {
		steamcmd = filepath.Join(filepath.Dir(root), "PalserverRuntime", "steamcmd", "steamcmd.exe")
	}
	return withDefaults(ServerInstance{
		Name:            name,
		RootPath:        root,
		Executable:      filepath.Join(root, "PalServer.exe"),
		SteamCMDPath:    steamcmd,
		AdminPassword:   randomPassword(),
		Community:       true,
		PerformanceMode: true,
	})
}

func assignAvailablePorts(instance ServerInstance, existing []ServerInstance) ServerInstance {
	used := make(map[int]bool, len(existing)*4+4)
	for _, item := range existing {
		for _, port := range []int{item.PublicPort, item.RESTPort, item.RCONPort, item.QueryPort} {
			if port > 0 {
				used[port] = true
			}
		}
	}
	instance.PublicPort = nextAvailablePort(instance.PublicPort, used)
	instance.RESTPort = nextAvailablePort(instance.RESTPort, used)
	instance.RCONPort = nextAvailablePort(instance.RCONPort, used)
	instance.QueryPort = nextAvailablePort(instance.QueryPort, used)
	return instance
}

func validateServerInstancePorts(candidate ServerInstance, existing []ServerInstance) error {
	type namedPort struct {
		name string
		port int
	}
	ports := []namedPort{
		{name: "游戏端口", port: candidate.PublicPort},
		{name: "查询端口", port: candidate.QueryPort},
		{name: "RCON 端口", port: candidate.RCONPort},
		{name: "REST API 端口", port: candidate.RESTPort},
	}
	usedByCandidate := map[int]string{}
	for _, item := range ports {
		if err := validatePort(item.name, item.port); err != nil {
			return err
		}
		if previous := usedByCandidate[item.port]; previous != "" {
			return fmt.Errorf("%s和%s不能使用同一个端口 %d", previous, item.name, item.port)
		}
		usedByCandidate[item.port] = item.name
	}
	for _, instance := range existing {
		if instance.ID == candidate.ID {
			continue
		}
		otherPorts := map[int]string{
			instance.PublicPort: "游戏端口",
			instance.QueryPort:  "查询端口",
			instance.RCONPort:   "RCON 端口",
			instance.RESTPort:   "REST API 端口",
		}
		for _, item := range ports {
			if otherName := otherPorts[item.port]; otherName != "" {
				return fmt.Errorf("端口 %d 已被服务器“%s”的%s使用，请为“%s”设置其他%s", item.port, instance.Name, otherName, candidate.Name, item.name)
			}
		}
	}
	return nil
}

func nextAvailablePort(start int, used map[int]bool) int {
	if start < 1024 || start > 65535 {
		start = 1024
	}
	for port := start; port <= 65535; port++ {
		if !used[port] {
			used[port] = true
			return port
		}
	}
	for port := 1024; port < start; port++ {
		if !used[port] {
			used[port] = true
			return port
		}
	}
	return start
}

func safeDirectoryName(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_'):
			builder.WriteRune(r)
		case r <= unicode.MaxASCII && unicode.IsSpace(r):
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-_. ")
}

func validateInstallDirectory(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" || !filepath.IsAbs(path) {
		return errors.New("请选择完整的服务器安装目录")
	}
	if filepath.Clean(filepath.VolumeName(path)+string(os.PathSeparator)) == path {
		return errors.New("不能直接安装到磁盘根目录")
	}
	for _, character := range path {
		if character > unicode.MaxASCII {
			return errors.New("SteamCMD 安装目录不能包含中文，请选择例如 D:\\PalworldServers\\Server1 的英文目录")
		}
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return errors.New("安装位置不是目录")
	}
	return nil
}

func randomPassword() string {
	data := make([]byte, 12)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("Pal-%d", time.Now().UnixNano())
	}
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(data), "=")
}

func writeManagedWorldSettings(instance ServerInstance) error {
	path, err := worldSettingsPath(instance)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(Difficulty=None,DayTimeSpeedRate=1,NightTimeSpeedRate=1,ExpRate=1,PalCaptureRate=1,PalSpawnNumRate=1,DeathPenalty=Item,bIsMultiplay=True,bIsPvP=False,ServerPlayerMaxNum=32,bIsUseBackupSaveData=True)
`
	if defaults, readErr := os.ReadFile(filepath.Join(instance.RootPath, "DefaultPalWorldSettings.ini")); readErr == nil {
		content = string(defaults)
	}
	content, err = mergeWorldSettingValues(content, map[string]string{
		"ServerName": strings.ReplaceAll(instance.Name, `"`, ""), "AdminPassword": instance.AdminPassword,
		"ServerPassword": instance.ServerPassword, "PublicPort": strconv.Itoa(instance.PublicPort), "PublicIP": instance.PublicIP,
		"RCONEnabled": "True", "RCONPort": strconv.Itoa(instance.RCONPort), "RESTAPIEnabled": "True", "RESTAPIPort": strconv.Itoa(instance.RESTPort),
		"bIsMultiplay": "True", "bAllowClientMod": "True", "bIsUseBackupSaveData": "True",
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func (a *App) QuickSetup(name, installRoot string) (ServerInstance, error) {
	base, err := appDataDir()
	if err != nil {
		return ServerInstance{}, err
	}
	if err := validateInstallDirectory(installRoot); err != nil {
		return ServerInstance{}, err
	}
	instance := buildManagedInstanceAt(base, name, installRoot)
	instance = assignAvailablePorts(instance, a.store.Snapshot().Instances)
	for _, existing := range a.store.Snapshot().Instances {
		if strings.EqualFold(filepath.Clean(existing.RootPath), instance.RootPath) {
			return ServerInstance{}, errors.New("该目录已经由启动器管理，请选择其他目录")
		}
	}
	if _, err := os.Stat(instance.Executable); err == nil {
		return ServerInstance{}, errors.New("所选目录已经包含 PalServer.exe，请使用“导入已有服务器”")
	}
	progress := func(message string, percent int) {
		runtime.EventsEmit(a.ctx, "setup:progress", map[string]any{"message": message, "percent": percent})
	}
	progress("正在准备安装目录", 3)
	if err := os.MkdirAll(instance.RootPath, 0o755); err != nil {
		return ServerInstance{}, err
	}
	if err := ensureSteamCMD(instance.SteamCMDPath, progress); err != nil {
		return ServerInstance{}, err
	}
	progress("正在安装帕鲁专用服务器", 25)
	if err := a.installOrUpdate(instance, func(output steamCMDProgress) {
		progress(output.Message, output.Percent)
	}); err != nil {
		return ServerInstance{}, err
	}
	progress("正在生成服务器配置", 85)
	if err := writeManagedWorldSettings(instance); err != nil {
		return ServerInstance{}, err
	}
	progress("正在安装 PalDefender", 90)
	if _, err := downloadLatestRelease("Ultimeit/PalDefender", win64Path(instance), func(asset string) bool {
		return strings.HasSuffix(asset, ".zip") && strings.Contains(asset, "paldefender")
	}); err != nil {
		progress("PalDefender 自动安装失败，可稍后在插件页重试", 95)
	}
	stored, err := a.store.Upsert(instance)
	if err != nil {
		return ServerInstance{}, err
	}
	progress("服务器已准备完成", 100)
	return stored, nil
}

var settingValuePattern = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]*)=("[^"]*"|[^,)]+)`)

func (a *App) ImportExistingServer(root string) (ServerInstance, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" {
		return ServerInstance{}, errors.New("server directory is required")
	}
	executable := filepath.Join(root, "PalServer.exe")
	if _, err := os.Stat(executable); err != nil {
		return ServerInstance{}, errors.New("PalServer.exe was not found in the selected directory")
	}
	base, _ := appDataDir()
	instance := withDefaults(ServerInstance{
		Name:            filepath.Base(root),
		RootPath:        root,
		Executable:      executable,
		SteamCMDPath:    filepath.Join(base, "runtime", "steamcmd", "steamcmd.exe"),
		Community:       true,
		PerformanceMode: true,
	})
	if data, err := os.ReadFile(filepath.Join(root, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")); err == nil {
		values := map[string]string{}
		for _, match := range settingValuePattern.FindAllStringSubmatch(string(data), -1) {
			values[match[1]] = strings.Trim(match[2], `"`)
		}
		instance.Name = fallback(values["ServerName"], instance.Name)
		instance.PublicIP = values["PublicIP"]
		instance.AdminPassword = values["AdminPassword"]
		instance.ServerPassword = values["ServerPassword"]
		instance.PublicPort = parsePort(values["PublicPort"], instance.PublicPort)
		instance.QueryPort = parsePort(values["QueryPort"], instance.QueryPort)
		instance.RCONPort = parsePort(values["RCONPort"], instance.RCONPort)
		instance.RESTPort = parsePort(values["RESTAPIPort"], instance.RESTPort)
	}
	if instance.AdminPassword == "" {
		instance.AdminPassword = randomPassword()
	}
	instance = assignAvailablePorts(instance, a.store.Snapshot().Instances)
	if err := syncInstanceWorldSettings(instance); err != nil {
		return ServerInstance{}, fmt.Errorf("sync imported server settings: %w", err)
	}
	return a.store.Upsert(instance)
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func parsePort(value string, defaultValue int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 65535 {
		return defaultValue
	}
	return parsed
}

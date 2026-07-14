package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrSaveDirectoryNotFound = errors.New("save directory not found")

func missingSaveDirectoryError() error { return ErrSaveDirectoryNotFound }

func worldSettingsPath(instance ServerInstance) (string, error) {
	return safeJoin(instance.RootPath, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
}

func (a *App) ReadWorldSettings(id string) (string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return "", err
	}
	path, err := worldSettingsPath(instance)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "[/Script/Pal.PalGameWorldSettings]\nOptionSettings=()", nil
	}
	return string(data), err
}

func (a *App) WriteWorldSettings(id, content string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("stop the server before changing world settings")
	}
	return writeWorldSettingsFile(instance, content)
}

func writeWorldSettingsFile(instance ServerInstance, content string) error {
	path, err := worldSettingsPath(instance)
	if err != nil {
		return err
	}
	if !strings.Contains(content, "[/Script/Pal.PalGameWorldSettings]") || !strings.Contains(content, "OptionSettings=") {
		return errors.New("invalid PalWorldSettings.ini content")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func appDataDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("LOCALAPPDATA is not available")
	}
	dir := filepath.Join(base, "palserver-launcher")
	return dir, os.MkdirAll(dir, 0o755)
}

func copyTree(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func dirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func (a *App) ListBackups(id string) ([]BackupEntry, error) {
	dir, err := appDataDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dir, "backups", id)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []BackupEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]BackupEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, _ := entry.Info()
		result = append(result, BackupEntry{Name: entry.Name(), Path: filepath.Join(root, entry.Name()), CreatedAt: info.ModTime().UnixMilli(), Size: dirSize(filepath.Join(root, entry.Name()))})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}

func claimBackupDestination(root string, now time.Time) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	name := now.Format("20060102-150405.000")
	for suffix := 1; ; suffix++ {
		candidate := filepath.Join(root, name)
		if suffix > 1 {
			candidate = filepath.Join(root, fmt.Sprintf("%s-%d", name, suffix))
		}
		if err := os.Mkdir(candidate, 0o755); err == nil {
			return candidate, nil
		} else if !os.IsExist(err) {
			return "", err
		}
	}
}

func (a *App) CreateBackup(id string) (BackupEntry, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return BackupEntry{}, err
	}
	source, err := safeJoin(instance.RootPath, "Pal", "Saved", "SaveGames")
	if err != nil {
		return BackupEntry{}, err
	}
	if _, err := os.Stat(source); err != nil {
		return BackupEntry{}, missingSaveDirectoryError()
	}
	base, err := appDataDir()
	if err != nil {
		return BackupEntry{}, err
	}
	now := time.Now()
	destination, err := claimBackupDestination(filepath.Join(base, "backups", id), now)
	if err != nil {
		return BackupEntry{}, err
	}
	if err := copyTree(source, destination); err != nil {
		_ = os.RemoveAll(destination)
		return BackupEntry{}, err
	}
	entry := BackupEntry{Name: filepath.Base(destination), Path: destination, CreatedAt: now.UnixMilli(), Size: dirSize(destination)}
	_ = a.PruneBackups(id)
	a.notifyDiscord(id, "backup", "备份已完成", entry.Name)
	return entry, nil
}

func (a *App) RestoreBackup(id, backupPath string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("stop the server before restoring a backup")
	}
	base, _ := appDataDir()
	allowedRoot := filepath.Join(base, "backups", id)
	backupAbs, _ := filepath.Abs(backupPath)
	rel, relErr := filepath.Rel(allowedRoot, backupAbs)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("invalid backup path")
	}
	destination, _ := safeJoin(instance.RootPath, "Pal", "Saved", "SaveGames")
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	return copyTree(backupAbs, destination)
}

func win64Path(instance ServerInstance) string {
	return filepath.Join(instance.RootPath, "Pal", "Binaries", "Win64")
}

// ue4ssModsRoot supports both the current Palworld server layout used by
// post-1.0 mods (Win64\ue4ss\Mods) and older UE4SS packages that placed Mods
// directly below Win64.
func ue4ssModsRoot(instance ServerInstance) string {
	current := filepath.Join(win64Path(instance), "ue4ss", "Mods")
	if info, err := os.Stat(current); err == nil && info.IsDir() {
		return current
	}
	legacy := filepath.Join(win64Path(instance), "Mods")
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		return legacy
	}
	return current
}

func (a *App) ListExtensions(id string) ([]ExtensionStatus, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	base := win64Path(instance)
	extensions := []struct{ id, name, file, disabled, version string }{
		{"paldefender", "PalDefender", "PalDefender.dll", "PalDefender.disabled.dll", "palguard.version.txt"},
		{"ue4ss", "UE4SS", "UE4SS.dll", "UE4SS.disabled.dll", "ue4ss.version.txt"},
	}
	result := make([]ExtensionStatus, 0, len(extensions))
	for _, extension := range extensions {
		enabledPath, disabledPath := filepath.Join(base, extension.file), filepath.Join(base, extension.disabled)
		_, enabledErr := os.Stat(enabledPath)
		_, disabledErr := os.Stat(disabledPath)
		versionData, _ := os.ReadFile(filepath.Join(base, extension.version))
		result = append(result, ExtensionStatus{ID: extension.id, Name: extension.name, Installed: enabledErr == nil || disabledErr == nil, Enabled: enabledErr == nil, Version: strings.TrimSpace(string(versionData)), Path: enabledPath})
	}
	return result, nil
}

func (a *App) ToggleExtension(id, extensionID string, enabled bool) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("stop the server before changing extensions")
	}
	files := map[string][2]string{"paldefender": {"PalDefender.dll", "PalDefender.disabled.dll"}, "ue4ss": {"UE4SS.dll", "UE4SS.disabled.dll"}}
	pair, ok := files[extensionID]
	if !ok {
		return errors.New("unknown extension")
	}
	base := win64Path(instance)
	from, to := filepath.Join(base, pair[0]), filepath.Join(base, pair[1])
	if enabled {
		from, to = to, from
	}
	return os.Rename(from, to)
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Digest             string `json:"digest"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

func downloadLatestRelease(repo, destination string, matcher func(string) bool) (string, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("User-Agent", "palserver-launcher")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub release lookup failed: %s", resp.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	for _, asset := range release.Assets {
		if !matcher(strings.ToLower(asset.Name)) {
			continue
		}
		assetResp, err := http.Get(asset.BrowserDownloadURL)
		if err != nil {
			return "", err
		}
		defer assetResp.Body.Close()
		if assetResp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("download failed: %s", assetResp.Status)
		}
		temp, err := os.CreateTemp("", "palserver-extension-*.zip")
		if err != nil {
			return "", err
		}
		_, err = io.Copy(temp, assetResp.Body)
		_ = temp.Close()
		if err != nil {
			return "", err
		}
		defer os.Remove(temp.Name())
		if err := unzipSafe(temp.Name(), destination); err != nil {
			return "", err
		}
		return release.TagName, nil
	}
	return "", errors.New("compatible release asset not found")
}

func unzipSafe(archive, destination string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		clean := filepath.Clean(file.Name)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return errors.New("unsafe zip entry")
		}
		target := filepath.Join(destination, clean)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func (a *App) UpdateExtension(id, extensionID string) (string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return "", err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return "", errors.New("stop the server before updating extensions")
	}
	base := win64Path(instance)
	_ = os.MkdirAll(base, 0o755)
	var version string
	switch extensionID {
	case "paldefender":
		version, err = downloadLatestRelease("Ultimeit/PalDefender", base, func(name string) bool {
			return strings.HasSuffix(name, ".zip") && strings.Contains(name, "paldefender")
		})
	case "ue4ss":
		preserved := map[string][]byte{}
		for _, relative := range []string{"UE4SS-settings.ini", filepath.Join("Mods", "mods.txt"), filepath.Join("ue4ss", "Mods", "mods.txt")} {
			if data, readErr := os.ReadFile(filepath.Join(base, relative)); readErr == nil {
				preserved[relative] = data
			}
		}
		version, err = downloadLatestRelease("UE4SS-RE/RE-UE4SS", base, func(name string) bool {
			return strings.HasSuffix(name, ".zip") && strings.Contains(name, "ue4ss") && !strings.Contains(name, "zdev")
		})
		for relative, data := range preserved {
			_ = os.WriteFile(filepath.Join(base, relative), data, 0o600)
		}
	default:
		return "", errors.New("unknown extension")
	}
	if err != nil {
		return "", err
	}
	versionFile := map[string]string{"paldefender": "palguard.version.txt", "ue4ss": "ue4ss.version.txt"}[extensionID]
	if err := os.WriteFile(filepath.Join(base, versionFile), []byte(version+"\n"), 0o600); err != nil {
		return "", err
	}
	return version, nil
}

func modRoots(instance ServerInstance) map[string]string {
	return map[string]string{
		"lua":      ue4ssModsRoot(instance),
		"pak":      filepath.Join(instance.RootPath, "Pal", "Content", "Paks"),
		"paklogic": filepath.Join(instance.RootPath, "Pal", "Content", "Paks", "LogicMods"),
		"dll":      win64Path(instance),
	}
}

var ue4ssBuiltInMods = map[string]string{
	"ActorDumperMod":         "导出和检查 Unreal Actor，主要用于开发调试",
	"BPML_GenericFunctions":  "Blueprint Mod Loader 的通用函数库",
	"BPModLoaderMod":         "加载 Blueprint 与部分 LogicMods 的基础组件",
	"CheatManagerEnablerMod": "启用 Unreal CheatManager 调试接口",
	"ConsoleCommandsMod":     "提供对象查看和属性设置等控制台命令",
	"ConsoleEnablerMod":      "启用 Unreal Engine 控制台",
	"jsbLuaProfilerMod":      "UE4SS Lua 性能分析工具",
	"Keybinds":               "UE4SS 内置快捷键支持",
	"LineTraceMod":           "射线检测与物体定位调试工具",
	"shared":                 "UE4SS 模组共用运行库",
	"SplitScreenMod":         "本地分屏测试模组，专用服务器通常不需要",
}

func classifyMod(kind, name string) (origin, description string, system bool) {
	cleanName := strings.TrimSuffix(name, ".disabled")
	if kind == "lua" {
		if description, ok := ue4ssBuiltInMods[cleanName]; ok {
			return "ue4ss-system", description, true
		}
		return "ue4ss-lua", "通过 UE4SS 加载的 Lua 模组", false
	}
	switch kind {
	case "paklogic":
		return "logicmods", "通过游戏 LogicMods 目录加载的逻辑模组", false
	case "pak":
		return "pak", "游戏 Pak 内容模组", false
	case "dll":
		return "dll", "服务器 Win64 目录中的 DLL 扩展", false
	default:
		return "other", "其他扩展", false
	}
}

func (a *App) ListMods(id string) ([]ModEntry, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	result := []ModEntry{}
	for kind, root := range modRoots(instance) {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			lower := strings.ToLower(name)
			if kind == "pak" && !strings.Contains(lower, ".pak") {
				continue
			}
			if kind == "dll" && (!strings.Contains(lower, ".dll") || strings.HasPrefix(lower, "palserver") || strings.HasPrefix(lower, "ue4ss") || strings.HasPrefix(lower, "paldefender")) {
				continue
			}
			info, _ := entry.Info()
			origin, description, system := classifyMod(kind, name)
			result = append(result, ModEntry{Name: strings.TrimSuffix(name, ".disabled"), Kind: kind, Origin: origin, Description: description, System: system, Path: filepath.Join(root, name), Enabled: !strings.HasSuffix(lower, ".disabled"), Size: info.Size()})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind == result[j].Kind {
			return result[i].Name < result[j].Name
		}
		return result[i].Kind < result[j].Kind
	})
	return result, nil
}

func (a *App) ImportMods(id, kind string, sources []string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	root, ok := modRoots(instance)[kind]
	if !ok {
		return errors.New("unknown mod type")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.Base(source))
		if info.IsDir() {
			if err := copyTree(source, target); err != nil {
				return err
			}
		} else {
			if err := copyFile(source, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (a *App) ToggleMod(id, path string, enabled bool) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	valid := false
	pathAbs, _ := filepath.Abs(path)
	for _, root := range modRoots(instance) {
		rootAbs, _ := filepath.Abs(root)
		rel, _ := filepath.Rel(rootAbs, pathAbs)
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			valid = true
		}
	}
	if !valid {
		return errors.New("mod path is outside this server")
	}
	target := strings.TrimSuffix(pathAbs, ".disabled")
	if !enabled && !strings.HasSuffix(strings.ToLower(pathAbs), ".disabled") {
		target = pathAbs + ".disabled"
	}
	return os.Rename(pathAbs, target)
}

func (a *App) DeleteMod(id, path string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	pathAbs, _ := filepath.Abs(path)
	if _, ok := ue4ssBuiltInMods[strings.TrimSuffix(filepath.Base(pathAbs), ".disabled")]; ok {
		return errors.New("UE4SS built-in components cannot be deleted")
	}
	for _, root := range modRoots(instance) {
		rootAbs, _ := filepath.Abs(root)
		rel, _ := filepath.Rel(rootAbs, pathAbs)
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return os.RemoveAll(pathAbs)
		}
	}
	return errors.New("mod path is outside this server")
}

func (a *App) RunDiagnostics(id string) ([]DiagnosticResult, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	results := []DiagnosticResult{}
	if _, err := os.Stat(instance.Executable); err == nil {
		results = append(results, DiagnosticResult{"服务器程序", "ok", instance.Executable})
	} else {
		results = append(results, DiagnosticResult{"服务器程序", "error", err.Error()})
	}
	localBuild := localServerBuildID(instance)
	remoteBuild, buildErr := remoteServerBuildID("public")
	if buildErr == nil && localBuild != "" && localBuild == remoteBuild {
		results = append(results, DiagnosticResult{"Palworld 1.0 版本", "ok", "当前公开构建 " + localBuild})
	} else if buildErr == nil {
		results = append(results, DiagnosticResult{"Palworld 1.0 版本", "warn", fmt.Sprintf("本地 %s，最新 %s", fallback(localBuild, "未知"), remoteBuild)})
	}
	extensions, _ := a.ListExtensions(id)
	for _, extension := range extensions {
		status := "warn"
		if extension.Installed && extension.Enabled {
			status = "ok"
		}
		results = append(results, DiagnosticResult{extension.Name + " 插件", status, fallback(extension.Version, "版本未知")})
	}
	mods, _ := a.ListMods(id)
	userMods := 0
	for _, mod := range mods {
		if !mod.System {
			userMods++
		}
	}
	if userMods == 0 {
		results = append(results, DiagnosticResult{"1.0 模组兼容", "ok", "未发现第三方 Pak、LogicMods、Lua 或 DLL 模组"})
	} else {
		results = append(results, DiagnosticResult{"1.0 模组兼容", "warn", fmt.Sprintf("发现 %d 个第三方模组；官方要求逐个确认 1.0 兼容性", userMods)})
	}
	for name, port := range map[string]int{"REST": instance.RESTPort, "RCON": instance.RCONPort} {
		conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if dialErr == nil {
			conn.Close()
			results = append(results, DiagnosticResult{name + " port", "ok", fmt.Sprintf("127.0.0.1:%d", port)})
		} else {
			results = append(results, DiagnosticResult{name + " port", "warn", dialErr.Error()})
		}
	}
	if instance.PublicIP == "" {
		results = append(results, DiagnosticResult{"公网地址", "warn", "尚未配置公网 IP"})
	} else {
		results = append(results, DiagnosticResult{"公网地址", "info", fmt.Sprintf("%s:%d/UDP", instance.PublicIP, instance.PublicPort)})
	}
	results = append(results, DiagnosticResult{"FRP 转发", "info", "游戏连接使用 UDP 端口；请确认公网 UDP 转发到本机游戏端口，TCP 8211 不是客户端连接端口。"})
	return results, nil
}

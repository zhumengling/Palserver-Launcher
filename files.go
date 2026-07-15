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
	"syscall"
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

func copyTreeEntryIsReparsePoint(info os.FileInfo) bool {
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && attributes.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func copyTree(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || copyTreeEntryIsReparsePoint(info) {
			return fmt.Errorf("copy source contains a symlink or reparse point: %s", path)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("copy source contains a non-regular entry: %s", path)
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
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return errors.Join(err, in.Close())
		}
		_, copyErr := io.Copy(out, in)
		inCloseErr := in.Close()
		closeErr := out.Close()
		return errors.Join(copyErr, inCloseErr, closeErr)
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

func readableDirEntryInfo(entry os.DirEntry) (os.FileInfo, bool) {
	info, err := entry.Info()
	return info, err == nil && info != nil
}

func backupEntryFromDirEntry(root string, entry os.DirEntry) (BackupEntry, bool) {
	if !entry.IsDir() {
		return BackupEntry{}, false
	}
	info, ok := readableDirEntryInfo(entry)
	if !ok || !info.IsDir() {
		return BackupEntry{}, false
	}
	path := filepath.Join(root, entry.Name())
	return BackupEntry{Name: entry.Name(), Path: path, CreatedAt: info.ModTime().UnixMilli(), Size: dirSize(path)}, true
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
		if backup, ok := backupEntryFromDirEntry(root, entry); ok {
			result = append(result, backup)
		}
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
	status, _ := serverStatus(instance)
	if err := flushWorldSaveBeforeBackup(instance, status.Running); err != nil {
		return BackupEntry{}, fmt.Errorf("save world before backup: %w", err)
	}
	if status.Running {
		if err := waitForSaveFilesStable(source); err != nil {
			return BackupEntry{}, fmt.Errorf("wait for world save: %w", err)
		}
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
	destination, err := safeJoin(instance.RootPath, "Pal", "Saved", "SaveGames")
	if err != nil {
		return err
	}
	return restoreBackupTree(backupAbs, destination)
}

func nextRestoreSwapPath(destination string) (string, error) {
	base := filepath.Base(destination)
	parent := filepath.Dir(destination)
	for suffix := 1; ; suffix++ {
		candidate := filepath.Join(parent, fmt.Sprintf(".%s.restore-old-%d", base, suffix))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
}

func restoreBackupTree(source, destination string) error {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := copyTree(source, staging); err != nil {
		return err
	}

	previous := ""
	if _, err := os.Stat(destination); err == nil {
		previous, err = nextRestoreSwapPath(destination)
		if err != nil {
			return err
		}
		if err := os.Rename(destination, previous); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		if previous != "" {
			_ = os.Rename(previous, destination)
		}
		return err
	}
	if previous != "" {
		_ = os.RemoveAll(previous)
	}
	return nil
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

func palDefenderInstallationState(base string) (installed, enabled bool) {
	_, loaderErr := os.Stat(filepath.Join(base, "d3d9.dll"))
	_, enabledErr := os.Stat(filepath.Join(base, "PalDefender.dll"))
	_, disabledErr := os.Stat(filepath.Join(base, "PalDefender.disabled.dll"))
	loaderInstalled := loaderErr == nil
	enabledInstalled := enabledErr == nil
	disabledInstalled := disabledErr == nil
	return loaderInstalled && (enabledInstalled || disabledInstalled), loaderInstalled && enabledInstalled
}

func validateExtensionInstallation(base, extensionID string) error {
	switch extensionID {
	case "paldefender":
		installed, _ := palDefenderInstallationState(base)
		if !installed {
			return errors.New("PalDefender 安装不完整：缺少 PalDefender.dll 或 d3d9.dll")
		}
	case "ue4ss":
		if _, installed, _ := ue4ssInstallationState(base); !installed {
			return errors.New("UE4SS 安装不完整：缺少代理 DLL、核心 DLL 或设置文件")
		}
	default:
		return errors.New("unknown extension")
	}
	return nil
}

func (a *App) ListExtensions(id string) ([]ExtensionStatus, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	base := win64Path(instance)
	extensions := []struct{ id, name, version string }{
		{"paldefender", "PalDefender", "palguard.version.txt"},
		{"ue4ss", "UE4SS", "ue4ss.version.txt"},
	}
	result := make([]ExtensionStatus, 0, len(extensions))
	for _, extension := range extensions {
		installed, enabled := false, false
		actualPath := ""
		if extension.id == "paldefender" {
			installed, enabled = palDefenderInstallationState(base)
			actualPath = filepath.Join(base, "PalDefender.dll")
			if installed && !enabled {
				actualPath = filepath.Join(base, "PalDefender.disabled.dll")
			}
		} else {
			actualPath, installed, enabled = ue4ssInstallationState(base)
		}
		versionData, _ := os.ReadFile(filepath.Join(base, extension.version))
		status := ExtensionStatus{
			ID: extension.id, Name: extension.name, Installed: installed, Enabled: enabled,
			Version: strings.TrimSpace(string(versionData)), Path: actualPath,
		}
		if manifestPath, pathErr := extensionInstalledManifestPath(instance, extension.id); pathErr == nil {
			if manifest, manifestErr := readExtensionUpdateManifest(manifestPath); manifestErr == nil && manifest.ExtensionID == extension.id {
				status.InstalledAsset = manifest.Asset.Name
				status.InstalledUpdatedAt = manifest.Asset.UpdatedAt
				if status.Version == "" {
					status.Version = manifest.Version
				}
			}
		}
		if pending, pathErr := extensionPendingPath(instance, extension.id); pathErr == nil {
			if manifest, manifestErr := readExtensionUpdateManifest(filepath.Join(pending, "manifest.json")); manifestErr == nil && manifest.ExtensionID == extension.id {
				status.Pending = true
				status.PendingVersion = manifest.Version
			}
		}
		result = append(result, status)
	}
	return result, nil
}

func renameExtensionStateFileChanged(from, to string) (bool, error) {
	if _, err := os.Stat(to); err == nil {
		if _, fromErr := os.Stat(from); os.IsNotExist(fromErr) {
			return false, nil
		}
		return false, errors.New("both enabled and disabled extension files exist")
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.Rename(from, to); err != nil {
		return false, err
	}
	return true, nil
}

func renameExtensionStateFile(from, to string) error {
	_, err := renameExtensionStateFileChanged(from, to)
	return err
}

func toggleLegacyUE4SSState(base string, enabled bool, renameFn func(string, string) (bool, error)) error {
	if renameFn == nil {
		renameFn = renameExtensionStateFileChanged
	}
	coreFrom, coreTo := filepath.Join(base, "UE4SS.dll"), filepath.Join(base, "UE4SS.disabled.dll")
	proxyFrom, proxyTo := filepath.Join(base, "dwmapi.dll"), filepath.Join(base, "dwmapi.disabled.dll")
	if enabled {
		coreFrom, coreTo = coreTo, coreFrom
		proxyFrom, proxyTo = proxyTo, proxyFrom
	}
	coreChanged, err := renameFn(coreFrom, coreTo)
	if err != nil {
		return err
	}
	if _, err := os.Stat(proxyFrom); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := renameFn(proxyFrom, proxyTo); err != nil {
		if coreChanged {
			if _, rollbackErr := renameFn(coreTo, coreFrom); rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("rollback legacy UE4SS core state: %w", rollbackErr))
			}
		}
		return err
	}
	return nil
}

func (a *App) ToggleExtension(id, extensionID string, enabled bool) error {
	return a.toggleExtensionWith(id, extensionID, enabled, serverStatus)
}

func (a *App) toggleExtensionWith(id, extensionID string, enabled bool, statusFor func(ServerInstance) (RuntimeStatus, error)) error {
	a.serverStartMu.Lock()
	defer a.serverStartMu.Unlock()
	a.extensionStageMu.Lock()
	defer a.extensionStageMu.Unlock()
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	if err := validateExtensionID(extensionID); err != nil {
		return err
	}
	if statusFor == nil {
		statusFor = serverStatus
	}
	status, err := statusFor(instance)
	if err != nil {
		return err
	}
	if status.Running {
		return errors.New("stop the server before changing extensions")
	}
	base := win64Path(instance)
	switch extensionID {
	case "paldefender":
		from, to := filepath.Join(base, "PalDefender.dll"), filepath.Join(base, "PalDefender.disabled.dll")
		if enabled {
			from, to = to, from
		}
		return renameExtensionStateFile(from, to)
	case "ue4ss":
		nested := regularNonEmptyFileExists(filepath.Join(base, "ue4ss", "UE4SS.dll")) && regularNonEmptyFileExists(filepath.Join(base, "ue4ss", "UE4SS-settings.ini"))
		if nested {
			from, to := filepath.Join(base, "dwmapi.dll"), filepath.Join(base, "dwmapi.disabled.dll")
			if enabled {
				from, to = to, from
			}
			return renameExtensionStateFile(from, to)
		}
		return toggleLegacyUE4SSState(base, enabled, renameExtensionStateFileChanged)
	default:
		return errors.New("unknown extension")
	}
}

type githubReleaseAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
	UpdatedAt          string `json:"updated_at"`
}

type githubRelease struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	Body        string               `json:"body"`
	PublishedAt string               `json:"published_at"`
	Draft       bool                 `json:"draft"`
	Prerelease  bool                 `json:"prerelease"`
	Assets      []githubReleaseAsset `json:"assets"`
}

func createReleaseTempArchive(destination string) (*os.File, error) {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return nil, err
	}
	return os.CreateTemp(destination, "palserver-extension-*.zip")
}

func releaseDownloadClient() *http.Client { return &http.Client{Timeout: 10 * time.Minute} }

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
		assetResp, err := releaseDownloadClient().Get(asset.BrowserDownloadURL)
		if err != nil {
			return "", err
		}
		defer assetResp.Body.Close()
		if assetResp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("download failed: %s", assetResp.Status)
		}
		temp, err := createReleaseTempArchive(destination)
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
			info, ok := readableDirEntryInfo(entry)
			if !ok {
				continue
			}
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
	if host, hostErr := a.GetHostResources(); hostErr == nil {
		if host.MemoryTotalMB < 16*1024 {
			results = append(results, DiagnosticResult{"内存容量", "warn", fmt.Sprintf("%.0f GB；官方建议至少 16 GB，大型服务器建议 32 GB", host.MemoryTotalMB/1024)})
		} else if host.MemoryTotalMB < 32*1024 {
			results = append(results, DiagnosticResult{"内存容量", "info", fmt.Sprintf("%.0f GB；满足基础建议，32 GB 更适合多人服务器", host.MemoryTotalMB/1024)})
		} else {
			results = append(results, DiagnosticResult{"内存容量", "ok", fmt.Sprintf("%.0f GB", host.MemoryTotalMB/1024)})
		}
	}
	if media, mediaErr := serverStorageMediaType(instance.RootPath); mediaErr == nil {
		if strings.Contains(strings.ToLower(media), "ssd") || strings.Contains(strings.ToLower(media), "nvme") {
			results = append(results, DiagnosticResult{"存档磁盘", "ok", media})
		} else {
			results = append(results, DiagnosticResult{"存档磁盘", "warn", media + "；官方建议将存档放在 SSD，低性能磁盘可能损坏存档"})
		}
	}
	if listening, listenErr := udpPortListening(instance.PublicPort); listenErr == nil {
		status := "warn"
		detail := fmt.Sprintf("UDP %d 未监听（服务器停止时属正常）", instance.PublicPort)
		if listening {
			status, detail = "ok", fmt.Sprintf("UDP %d 正在监听", instance.PublicPort)
		}
		results = append(results, DiagnosticResult{"游戏 UDP 端口", status, detail})
	}
	if allowed, firewallErr := firewallAllowsUDP(instance.PublicPort); firewallErr == nil {
		status := "warn"
		detail := fmt.Sprintf("未检测到允许 UDP %d 的入站防火墙规则", instance.PublicPort)
		if allowed {
			status, detail = "ok", fmt.Sprintf("检测到允许 UDP %d 的入站防火墙规则", instance.PublicPort)
		}
		results = append(results, DiagnosticResult{"Windows 防火墙", status, detail})
	}
	if instance.Community && instance.PublicIP != "" {
		results = append(results, DiagnosticResult{"社区服本地回环", "info", "若同一局域网无法通过公网地址连接，请检查路由器是否支持 Hairpin NAT"})
	}
	results = append(results, DiagnosticResult{"RCON 兼容通道", "warn", "官方 1.0 文档已将 RCON 标记为 Deprecated；玩家管理优先使用 REST API"})
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

func serverStorageMediaType(root string) (string, error) {
	volume := strings.TrimSuffix(filepath.VolumeName(filepath.Clean(root)), `\`)
	if volume == "" {
		return "", errors.New("server drive was not found")
	}
	query := `$letter='` + strings.TrimSuffix(volume, `:`) + `'; $disk=Get-Partition -DriveLetter $letter -ErrorAction Stop | Get-Disk; ($disk.BusType.ToString() + ' / ' + $disk.MediaType.ToString())`
	output, err := newHiddenPowerShell(query).Output()
	return strings.TrimSpace(string(output)), err
}

func udpPortListening(port int) (bool, error) {
	query := fmt.Sprintf(`[bool](Get-NetUDPEndpoint -LocalPort %d -ErrorAction SilentlyContinue | Select-Object -First 1) | ConvertTo-Json -Compress`, port)
	output, err := newHiddenPowerShell(query).Output()
	return strings.EqualFold(strings.TrimSpace(string(output)), "true"), err
}

func firewallAllowsUDP(port int) (bool, error) {
	query := fmt.Sprintf(`$rules=Get-NetFirewallRule -Enabled True -Direction Inbound -Action Allow -ErrorAction Stop; [bool]($rules | Get-NetFirewallPortFilter | Where-Object { $_.Protocol -eq 'UDP' -and ($_.LocalPort -eq 'Any' -or $_.LocalPort -eq '%d') } | Select-Object -First 1) | ConvertTo-Json -Compress`, port)
	output, err := newHiddenPowerShell(query).Output()
	return strings.EqualFold(strings.TrimSpace(string(output)), "true"), err
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pluginCompatibilityBaseline struct {
	GameBuildID        string `json:"gameBuildId"`
	ServerVersion      string `json:"serverVersion"`
	PalDefenderVersion string `json:"palDefenderVersion"`
	UE4SSVersion       string `json:"ue4ssVersion"`
	RecordedAt         int64  `json:"recordedAt"`
}

type pluginCrashRecord struct {
	OccurredAt    int64  `json:"occurredAt"`
	Signature     string `json:"signature"`
	Summary       string `json:"summary"`
	PluginRelated bool   `json:"pluginRelated"`
}

type safeModeManifest struct {
	ActivatedAt           int64 `json:"activatedAt"`
	PalDefenderWasEnabled bool  `json:"palDefenderWasEnabled"`
	UE4SSWasEnabled       bool  `json:"ue4ssWasEnabled"`
}

const compatibilityLaunchMarker = "[launcher] ===== server launch "

func launcherCompatibilityPath(instance ServerInstance, name string) string {
	return filepath.Join(instance.RootPath, "launcher-logs", name)
}

func readJSONFile[T any](path string) (T, error) {
	var result T
	data, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	return result, nil
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return replaceFileData(path, append(data, '\n'), 0o600)
}

func analyzePluginCrash(logText string) pluginCrashRecord {
	trimmed := strings.TrimSpace(logText)
	lower := strings.ToLower(trimmed)
	record := pluginCrashRecord{OccurredAt: time.Now().UnixMilli(), Summary: trimmed}
	if len(record.Summary) > 4000 {
		record.Summary = record.Summary[len(record.Summary)-4000:]
	}
	switch {
	case strings.Contains(lower, "exception_access_violation") && strings.Contains(lower, "ue4ss"):
		record.Signature, record.PluginRelated = "UE4SS_ACCESS_VIOLATION", true
	case strings.Contains(lower, "exception_access_violation") && strings.Contains(lower, "paldefender"):
		record.Signature, record.PluginRelated = "PALDEFENDER_ACCESS_VIOLATION", true
	case strings.Contains(lower, "exception_access_violation"):
		record.Signature = "ACCESS_VIOLATION"
	case strings.Contains(lower, "fatal error") || strings.Contains(lower, "critical error"):
		record.Signature = "FATAL_ERROR"
	default:
		record.Signature = "PROCESS_EXIT"
	}
	return record
}

func readCompatibilityLog(instance ServerInstance) string {
	data, err := os.ReadFile(launcherCompatibilityPath(instance, "server.log"))
	if err != nil {
		return ""
	}
	const maximum = 64 * 1024
	if len(data) > maximum {
		data = data[len(data)-maximum:]
	}
	content := string(data)
	if marker := strings.LastIndex(content, compatibilityLaunchMarker); marker >= 0 {
		content = content[marker:]
	}
	return content
}

func recordPluginCrash(instance ServerInstance, detail string) pluginCrashRecord {
	logText := readCompatibilityLog(instance)
	if strings.TrimSpace(detail) != "" {
		logText += "\n" + detail
	}
	record := analyzePluginCrash(logText)
	_ = writeJSONFile(launcherCompatibilityPath(instance, "plugin-crash.json"), record)
	return record
}

func recordCompatibilityBaseline(instance ServerInstance) {
	status, err := serverStatus(instance)
	if err != nil || !status.Running {
		return
	}
	extensions := listExtensionStatuses(instance)
	palDefender := extensionByID(extensions, "paldefender")
	ue4ss := extensionByID(extensions, "ue4ss")
	baseline := pluginCompatibilityBaseline{
		GameBuildID: localServerBuildID(instance), ServerVersion: status.Version,
		PalDefenderVersion: palDefender.Version, UE4SSVersion: ue4ss.Version, RecordedAt: time.Now().UnixMilli(),
	}
	if writeJSONFile(launcherCompatibilityPath(instance, "plugin-baseline.json"), baseline) == nil {
		_ = os.Remove(launcherCompatibilityPath(instance, "plugin-crash.json"))
	}
}

func scheduleCompatibilityBaseline(instance ServerInstance) {
	go func() {
		time.Sleep(30 * time.Second)
		recordCompatibilityBaseline(instance)
	}()
}

func compatibilityIssue(severity, component, title, detail, action string) PluginCompatibilityIssue {
	return PluginCompatibilityIssue{Severity: severity, Component: component, Title: title, Detail: detail, Action: action}
}

func buildPluginCompatibilityReport(instance ServerInstance, extensions []ExtensionStatus, baseline pluginCompatibilityBaseline, crash pluginCrashRecord, safeMode SafeModeStatus) PluginCompatibilityReport {
	palDefender := extensionByID(extensions, "paldefender")
	ue4ss := extensionByID(extensions, "ue4ss")
	currentBuild := localServerBuildID(instance)
	issues := make([]PluginCompatibilityIssue, 0)
	for _, extension := range extensions {
		if extension.Installed {
			if err := validateExtensionInstallation(win64Path(instance), extension.ID); err != nil {
				issues = append(issues, compatibilityIssue("error", extension.Name, "插件安装不完整", err.Error(), "重新安装或更新该插件"))
			}
		}
		if extension.Pending {
			issues = append(issues, compatibilityIssue("warning", extension.Name, "插件更新等待应用", "已下载 "+extension.PendingVersion+"，下次启动前会应用", "停止并重新启动服务器"))
		}
	}
	pluginsEnabled := palDefender.Enabled || ue4ss.Enabled
	if baseline.GameBuildID != "" && currentBuild != "" && baseline.GameBuildID != currentBuild && pluginsEnabled {
		issues = append(issues, compatibilityIssue("warning", "Palworld", "游戏版本已变化", fmt.Sprintf("上次稳定 Build %s，当前 Build %s", baseline.GameBuildID, currentBuild), "先检查插件更新；启动失败时使用安全模式"))
	}
	if crash.Signature != "" && crash.PluginRelated {
		issues = append(issues, compatibilityIssue("critical", "UE4SS / PalDefender", "检测到插件相关崩溃", crash.Signature, "使用安全模式启动并逐个恢复插件"))
	} else if crash.Signature == "ACCESS_VIOLATION" && pluginsEnabled {
		issues = append(issues, compatibilityIssue("warning", "服务器进程", "检测到访问冲突", "崩溃日志包含 EXCEPTION_ACCESS_VIOLATION，且当前启用了核心插件", "优先尝试安全模式以排除插件冲突"))
	}
	if safeMode.Active {
		issues = append(issues, compatibilityIssue("info", "安全模式", "当前保留了安全模式状态", "核心插件保持停用，原启用状态已记录", "服务器停止后可恢复原插件状态"))
	}
	compatible, safeModeRecommended := true, false
	for _, issue := range issues {
		if issue.Severity == "critical" || issue.Severity == "error" {
			compatible = false
		}
		if issue.Severity == "critical" || issue.Title == "游戏版本已变化" || issue.Title == "检测到访问冲突" {
			safeModeRecommended = true
		}
	}
	return PluginCompatibilityReport{
		ServerID: instance.ID, CheckedAt: time.Now().UnixMilli(), GameBuildID: currentBuild, BaselineBuildID: baseline.GameBuildID,
		PalDefenderVersion: palDefender.Version, UE4SSVersion: ue4ss.Version, Compatible: compatible,
		SafeModeRecommended: safeModeRecommended, LastCrashSummary: crash.Summary, Issues: issues,
	}
}

func safeModeStatus(instance ServerInstance) SafeModeStatus {
	manifest, err := readJSONFile[safeModeManifest](launcherCompatibilityPath(instance, "safe-mode.json"))
	if err != nil {
		return SafeModeStatus{}
	}
	extensions := listExtensionStatuses(instance)
	palDefender := extensionByID(extensions, "paldefender")
	ue4ss := extensionByID(extensions, "ue4ss")
	return SafeModeStatus{
		Active: true, ActivatedAt: manifest.ActivatedAt, PalDefenderWasEnabled: manifest.PalDefenderWasEnabled, UE4SSWasEnabled: manifest.UE4SSWasEnabled,
		PalDefenderCurrentlyOff: !palDefender.Enabled, UE4SSCurrentlyOff: !ue4ss.Enabled,
	}
}

func (a *App) GetPluginCompatibility(id string) (PluginCompatibilityReport, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return PluginCompatibilityReport{}, err
	}
	baseline, _ := readJSONFile[pluginCompatibilityBaseline](launcherCompatibilityPath(instance, "plugin-baseline.json"))
	crash, _ := readJSONFile[pluginCrashRecord](launcherCompatibilityPath(instance, "plugin-crash.json"))
	return buildPluginCompatibilityReport(instance, listExtensionStatuses(instance), baseline, crash, safeModeStatus(instance)), nil
}

func (a *App) GetSafeModeStatus(id string) (SafeModeStatus, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return SafeModeStatus{}, err
	}
	return safeModeStatus(instance), nil
}

func (a *App) StartServerSafeMode(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("请先停止服务器再使用安全模式")
	}
	extensions := listExtensionStatuses(instance)
	palDefender := extensionByID(extensions, "paldefender")
	ue4ss := extensionByID(extensions, "ue4ss")
	if !palDefender.Installed && !ue4ss.Installed {
		return errors.New("没有检测到可由安全模式停用的核心插件")
	}
	manifest := safeModeManifest{ActivatedAt: time.Now().UnixMilli(), PalDefenderWasEnabled: palDefender.Enabled, UE4SSWasEnabled: ue4ss.Enabled}
	if err := writeJSONFile(launcherCompatibilityPath(instance, "safe-mode.json"), manifest); err != nil {
		return err
	}
	if palDefender.Enabled {
		if err := a.ToggleExtension(id, "paldefender", false); err != nil {
			_ = os.Remove(launcherCompatibilityPath(instance, "safe-mode.json"))
			return fmt.Errorf("停用 PalDefender：%w", err)
		}
	}
	if ue4ss.Enabled {
		if err := a.ToggleExtension(id, "ue4ss", false); err != nil {
			var rollbackErr error
			if palDefender.Enabled {
				rollbackErr = a.ToggleExtension(id, "paldefender", true)
			}
			if rollbackErr != nil {
				return errors.Join(fmt.Errorf("停用 UE4SS：%w", err), fmt.Errorf("恢复 PalDefender：%w", rollbackErr))
			}
			_ = os.Remove(launcherCompatibilityPath(instance, "safe-mode.json"))
			return fmt.Errorf("停用 UE4SS：%w", err)
		}
	}
	if err := a.StartServer(id); err != nil {
		recordPluginCrash(instance, err.Error())
		return fmt.Errorf("安全模式启动失败：%w", err)
	}
	return nil
}

func (a *App) RestorePluginsAfterSafeMode(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("请先停止服务器再恢复插件")
	}
	manifest, err := readJSONFile[safeModeManifest](launcherCompatibilityPath(instance, "safe-mode.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("没有需要恢复的安全模式状态")
		}
		return err
	}
	extensions := listExtensionStatuses(instance)
	palDefender := extensionByID(extensions, "paldefender")
	ue4ss := extensionByID(extensions, "ue4ss")
	if manifest.PalDefenderWasEnabled && palDefender.Installed && !palDefender.Enabled {
		if err := a.ToggleExtension(id, "paldefender", true); err != nil {
			return fmt.Errorf("恢复 PalDefender：%w", err)
		}
	}
	if manifest.UE4SSWasEnabled && ue4ss.Installed && !ue4ss.Enabled {
		if err := a.ToggleExtension(id, "ue4ss", true); err != nil {
			var rollbackErr error
			if manifest.PalDefenderWasEnabled && palDefender.Installed && !palDefender.Enabled {
				rollbackErr = a.ToggleExtension(id, "paldefender", false)
			}
			if rollbackErr != nil {
				return errors.Join(fmt.Errorf("恢复 UE4SS：%w", err), fmt.Errorf("重新停用 PalDefender：%w", rollbackErr))
			}
			return fmt.Errorf("恢复 UE4SS：%w", err)
		}
	}
	return os.Remove(launcherCompatibilityPath(instance, "safe-mode.json"))
}

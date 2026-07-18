package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

const linuxServerModsUnsupportedMessage = "Palworld 官方 1.0 文档目前仅支持在 Windows 专用服务器运行服务端模组，Linux 上已禁用 UE4SS、PalDefender、Workshop、Pak、Lua 与 LogicMods 部署"

func extensionByID(extensions []ExtensionStatus, id string) ExtensionStatus {
	for _, extension := range extensions {
		if extension.ID == id {
			return extension
		}
	}
	return ExtensionStatus{ID: id}
}

func capability(id, name, category string, available bool, detail, reason string) CapabilityStatus {
	state := "ready"
	if !available {
		state = "unavailable"
	} else if reason != "" {
		state = "warn"
	}
	return CapabilityStatus{ID: id, Name: name, Category: category, Available: available, State: state, Detail: detail, Reason: reason}
}

func buildServerCapabilityReport(instance ServerInstance, status RuntimeStatus, extensions []ExtensionStatus, rconAuthenticated bool) ServerCapabilityReport {
	palDefender := extensionByID(extensions, "paldefender")
	ue4ss := extensionByID(extensions, "ue4ss")
	restReady := status.Running && status.RESTAvailable
	rconReady := status.Running && rconAuthenticated
	palDefenderReady := extensionStatusSupported(palDefender) && palDefender.Installed && palDefender.Enabled
	pluginCommandsReady := rconReady && palDefenderReady

	capabilities := []CapabilityStatus{
		capability("process", "服务器进程", "内核", status.Running, fmt.Sprintf("PID %d", status.PID), map[bool]string{true: "", false: "服务器尚未运行"}[status.Running]),
		capability("rest", "官方 REST API", "管理通道", restReady, "玩家、公告、保存与关服的官方通道", map[bool]string{true: "", false: "需要运行服务器并启用 RESTAPIEnabled"}[restReady]),
		capability("rcon", "RCON 兼容通道", "管理通道", rconReady, "PalDefender 管理命令使用此通道", map[bool]string{true: "", false: "需要启用 RCON，且管理员密码必须正确"}[rconReady]),
		capability("paldefender", "PalDefender", "插件", palDefenderReady, palDefender.Version, map[bool]string{true: "", false: extensionCapabilityReason(palDefender, "需要安装并启用 PalDefender")}[palDefenderReady]),
		capability("ue4ss", "UE4SS", "插件", extensionStatusSupported(ue4ss) && ue4ss.Installed && ue4ss.Enabled, ue4ss.Version, map[bool]string{true: "", false: extensionCapabilityReason(ue4ss, "未安装或已停用；仅影响 UE4SS 模组")}[extensionStatusSupported(ue4ss) && ue4ss.Installed && ue4ss.Enabled]),
		capability("player-list", "在线玩家列表", "玩家管理", restReady, "官方 /players", map[bool]string{true: "", false: "官方 REST API 不可用"}[restReady]),
		capability("player-moderation", "踢出与封禁", "玩家管理", restReady, "官方 /kick 与 /ban", map[bool]string{true: "", false: "官方 REST API 不可用"}[restReady]),
		capability("rcon-admin", "管理员与 IP 封禁", "玩家管理", pluginCommandsReady, "PalDefender RCON 命令", map[bool]string{true: "", false: "需要 PalDefender 与可认证的 RCON"}[pluginCommandsReady]),
		capability("player-rewards", "道具、经验与普通帕鲁", "玩家奖励", pluginCommandsReady, "PalDefender give/givepal 命令", map[bool]string{true: "", false: "需要 PalDefender 与可认证的 RCON"}[pluginCommandsReady]),
		capability("custom-pal", "高级自定义帕鲁", "玩家奖励", pluginCommandsReady, "PalTemplate + givepal_j", customPalCapabilityReason(pluginCommandsReady, palDefender.Version)),
		capability("announce", "服务器公告", "官方运维", restReady, "官方 /announce", map[bool]string{true: "", false: "官方 REST API 不可用"}[restReady]),
		capability("save-world", "立即保存", "官方运维", restReady, "官方 /save", map[bool]string{true: "", false: "官方 REST API 不可用"}[restReady]),
		capability("graceful-shutdown", "优雅关服", "官方运维", restReady || rconReady, "优先官方 REST，RCON 作为兼容回退", map[bool]string{true: "", false: "REST 与 RCON 均不可用"}[restReady || rconReady]),
		capability("world-snapshot", "世界实时快照", "官方运维", restReady, "官方 /game-data；服务端还需启用 GameData API", map[bool]string{true: "需要在 PalDefender REST 配置中启用 GameData API", false: "官方 REST API 不可用"}[restReady]),
		capability("server-mods", "服务端模组部署", "插件", serverModsSupported(), "官方 Workshop、Pak、Lua、LogicMods 与原生扩展", map[bool]string{true: "", false: serverModsUnsupportedReason()}[serverModsSupported()]),
		capability("safe-mode", "插件安全模式", "恢复", coreExtensionSupported("paldefender") && (palDefender.Installed || ue4ss.Installed), "临时停用 UE4SS 与 PalDefender 后启动", map[bool]string{true: "", false: extensionCapabilityReason(palDefender, "没有检测到可停用的核心插件")}[coreExtensionSupported("paldefender") && (palDefender.Installed || ue4ss.Installed)]),
	}
	return ServerCapabilityReport{
		ServerID: instance.ID, Platform: runtime.GOOS, CheckedAt: time.Now().UnixMilli(), Running: status.Running, ServerVersion: status.Version,
		PalDefenderVersion: palDefender.Version, UE4SSVersion: ue4ss.Version, Capabilities: capabilities,
	}
}

func extensionCapabilityReason(extension ExtensionStatus, fallback string) string {
	if !extensionStatusSupported(extension) && extension.UnsupportedReason != "" {
		return extension.UnsupportedReason
	}
	return fallback
}

func customPalCapabilityReason(ready bool, version string) string {
	if !ready {
		return "需要 PalDefender 与可认证的 RCON"
	}
	if strings.TrimSpace(version) == "" {
		return "PalDefender 版本未知，请确认其支持 givepal_j 文件模板命令"
	}
	return ""
}

func (a *App) GetServerCapabilities(id string) (ServerCapabilityReport, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return ServerCapabilityReport{}, err
	}
	status, err := a.cachedRuntimeStatus(instance)
	if err != nil {
		return ServerCapabilityReport{}, err
	}
	extensions, err := a.ListExtensions(id)
	if err != nil {
		return ServerCapabilityReport{}, err
	}
	rconAuthenticated := false
	if status.Running && status.RCONAvailable {
		rconAuthenticated = probeRCONWithTimeout(instance, 900*time.Millisecond) == nil
	}
	report := buildServerCapabilityReport(instance, status, extensions, rconAuthenticated)
	return normalizeCapabilityReportPlatform(report, a.reportedPlatformName()), nil
}

func normalizeCapabilityReportPlatform(report ServerCapabilityReport, platform string) ServerCapabilityReport {
	report.Platform = platform
	if platform != "linux" {
		return report
	}
	blocked := map[string]bool{
		"paldefender": true, "ue4ss": true, "rcon-admin": true, "player-rewards": true,
		"custom-pal": true, "server-mods": true, "safe-mode": true,
	}
	for index := range report.Capabilities {
		if !blocked[report.Capabilities[index].ID] {
			continue
		}
		report.Capabilities[index].Available = false
		report.Capabilities[index].State = "unavailable"
		report.Capabilities[index].Reason = linuxServerModsUnsupportedMessage
	}
	return report
}

func findCapability(report ServerCapabilityReport, id string) CapabilityStatus {
	for _, item := range report.Capabilities {
		if item.ID == id {
			return item
		}
	}
	return CapabilityStatus{ID: id, State: "unavailable", Reason: "启动器未识别此能力"}
}

func ensurePluginCommandsReady(instance ServerInstance) error {
	if !coreExtensionSupported("paldefender") {
		return errors.New(coreExtensionUnsupportedReason("paldefender"))
	}
	installed, enabled := palDefenderInstallationState(win64Path(instance))
	if !installed || !enabled {
		return fmt.Errorf("此操作需要安装并启用 PalDefender")
	}
	if err := probeRCONWithTimeout(instance, 1500*time.Millisecond); err != nil {
		return fmt.Errorf("此操作需要可认证的 RCON：%w", err)
	}
	return nil
}

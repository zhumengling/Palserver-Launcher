//go:build windows

package main

import (
	"os"
	"runtime"
)

func (a *App) GetAgentPreflight() AgentPreflightReport {
	reported := a.reportedPlatformName()
	simulated := reported != runtime.GOOS
	status := "ok"
	detail := "Windows 桌面端使用当前用户权限，不需要 Linux systemd 部署自检"
	if simulated {
		status = "warning"
		detail = "当前在 Windows 开发机模拟 Linux 网页能力；真实目录权限、systemd 和 /proc 检查需要在 Linux Agent 上执行"
	}
	return AgentPreflightReport{
		OK: true, Platform: reported, HostPlatform: runtime.GOOS, Architecture: runtime.GOARCH,
		User: os.Getenv("USERNAME"), SimulatedPlatform: simulated,
		Checks: []AgentPreflightCheck{{Name: "开发预览平台", Status: status, Detail: detail}},
	}
}

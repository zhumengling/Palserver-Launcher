//go:build linux

package main

import "path/filepath"

func (a *App) GetAgentPreflight() AgentPreflightReport {
	base, err := appDataDir()
	if err != nil {
		return AgentPreflightReport{Platform: "linux", HostPlatform: "linux", Architecture: "amd64", Checks: []AgentPreflightCheck{{Name: "Agent 数据目录", Status: "error", Detail: err.Error()}}}
	}
	report, _ := runLinuxAgentSelfTest(base, a.agentAuthPath(filepath.Join(base, "admin-auth.json")))
	return report
}

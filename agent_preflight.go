package main

import (
	"path/filepath"
	"runtime"
	"strings"
)

type AgentPreflightCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type AgentPreflightReport struct {
	OK                bool                  `json:"ok"`
	Platform          string                `json:"platform"`
	HostPlatform      string                `json:"hostPlatform"`
	Architecture      string                `json:"architecture"`
	User              string                `json:"user"`
	DataDir           string                `json:"dataDir"`
	Home              string                `json:"home"`
	AllowedRoots      []string              `json:"allowedRoots"`
	SimulatedPlatform bool                  `json:"simulatedPlatform"`
	Checks            []AgentPreflightCheck `json:"checks"`
}

func (a *App) setReportedPlatform(platform string) {
	a.platformMu.Lock()
	a.reportedPlatform = platform
	a.platformMu.Unlock()
}

func (a *App) reportedPlatformName() string {
	a.platformMu.RLock()
	platform := a.reportedPlatform
	a.platformMu.RUnlock()
	if platform == "" {
		return runtime.GOOS
	}
	return platform
}

func (a *App) setAgentAuthFile(path string) {
	a.platformMu.Lock()
	a.agentAuthFile = filepath.Clean(strings.TrimSpace(path))
	a.platformMu.Unlock()
}

func (a *App) agentAuthPath(defaultPath string) string {
	a.platformMu.RLock()
	path := a.agentAuthFile
	a.platformMu.RUnlock()
	if path == "" || path == "." {
		return defaultPath
	}
	return path
}

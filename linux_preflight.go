//go:build linux

package main

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

func writableDirectorySelfTest(path string, mode os.FileMode) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" || !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	probe, err := os.CreateTemp(path, ".palserver-self-test-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func runLinuxAgentSelfTest(dataDir, authFile string) (AgentPreflightReport, error) {
	current, _ := user.Current()
	report := AgentPreflightReport{
		Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		HostPlatform: runtime.GOOS,
		DataDir:      filepath.Clean(dataDir), Home: filepath.Clean(os.Getenv("HOME")),
		AllowedRoots: configuredLinuxServerRoots(),
	}
	if current != nil {
		report.User = current.Username
	}
	failed := make([]error, 0)
	check := func(name, detail string, err error) {
		status := "ok"
		if err != nil {
			status, detail = "error", err.Error()
			failed = append(failed, errors.New(name+": "+err.Error()))
		}
		report.Checks = append(report.Checks, AgentPreflightCheck{Name: name, Status: status, Detail: detail})
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		check("运行平台", "Linux amd64", errors.New("Palworld Linux Agent requires linux/amd64"))
	} else {
		check("运行平台", "linux/amd64", nil)
	}
	check("Agent 数据目录", report.DataDir, writableDirectorySelfTest(report.DataDir, 0o700))
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		check("SteamCMD HOME", "", errors.New("HOME is not configured"))
	} else {
		check("SteamCMD HOME", home, writableDirectorySelfTest(home, 0o750))
	}
	authDir := filepath.Dir(filepath.Clean(authFile))
	check("认证目录", authDir, writableDirectorySelfTest(authDir, 0o700))
	for _, root := range report.AllowedRoots {
		check("服务器根目录", root, writableDirectorySelfTest(root, 0o750))
	}
	if len(report.AllowedRoots) == 0 {
		report.Checks = append(report.Checks, AgentPreflightCheck{Name: "服务器根目录限制", Status: "warning", Detail: "PALSERVER_ALLOWED_SERVER_ROOTS 未设置，手动运行模式允许管理任意绝对路径"})
	}
	_, procErr := os.ReadFile("/proc/self/stat")
	check("Linux /proc", "/proc/self/stat 可读", procErr)
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		check("文件句柄限制", "", err)
	} else if limit.Cur < 4096 {
		report.Checks = append(report.Checks, AgentPreflightCheck{Name: "文件句柄限制", Status: "warning", Detail: "当前限制低于 4096；systemd 服务会设置为 1048576"})
	} else {
		check("文件句柄限制", "当前上限满足运行要求", nil)
	}
	report.OK = len(failed) == 0
	return report, errors.Join(failed...)
}

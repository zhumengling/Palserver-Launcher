package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// OS-reported usable memory is normally slightly lower than the physical
	// 8/16 GB installed capacity because firmware and devices reserve a part.
	setupMinimumMemoryMB = 15 * 512
	setupRecommendedMB   = 15 * 1024
	setupMinimumDisk     = int64(12 << 30)
)

func evaluateSetupEnvironment(platform string, cpuCores int, memoryTotalMB float64, diskFreeBytes int64, pathValid bool, pathMessage string) SetupEnvironment {
	report := SetupEnvironment{
		Platform: platform, CPUCores: cpuCores, MemoryTotalMB: memoryTotalMB, DiskFreeBytes: diskFreeBytes,
		PathValid: pathValid, PathMessage: pathMessage, CPURecommended: cpuCores >= 4,
		MemoryMinimum: memoryTotalMB >= setupMinimumMemoryMB, MemoryRecommended: memoryTotalMB >= setupRecommendedMB,
		DiskMinimum: diskFreeBytes >= setupMinimumDisk,
	}
	if !report.CPURecommended {
		report.Warnings = append(report.Warnings, fmt.Sprintf("当前 %d 核；Palworld 官方建议至少 4 核", cpuCores))
	}
	if !report.MemoryMinimum {
		report.Warnings = append(report.Warnings, fmt.Sprintf("当前 %.1f GB 内存；官方说明 8 GB 仅可勉强启动，推荐至少 16 GB", memoryTotalMB/1024))
	} else if !report.MemoryRecommended {
		report.Warnings = append(report.Warnings, fmt.Sprintf("当前 %.1f GB 内存；可以启动，但官方推荐至少 16 GB", memoryTotalMB/1024))
	}
	if !report.DiskMinimum {
		report.Warnings = append(report.Warnings, fmt.Sprintf("安装磁盘仅剩 %.1f GB；启动器建议至少预留 12 GB 用于程序、更新和备份", float64(diskFreeBytes)/(1<<30)))
	}
	if !pathValid && strings.TrimSpace(pathMessage) != "" {
		report.Warnings = append(report.Warnings, pathMessage)
	}
	report.CanInstall = report.PathValid && report.MemoryMinimum && report.DiskMinimum
	return report
}

func existingSetupProbePath(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("安装路径的现有父项不是目录：%s", current)
			}
			return current, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("找不到可用的安装磁盘")
		}
		current = parent
	}
}

func (a *App) GetSetupEnvironment(installRoot string) (SetupEnvironment, error) {
	base, err := appDataDir()
	if err != nil {
		return SetupEnvironment{}, fmt.Errorf("读取 Agent 数据目录：%w", err)
	}
	installRoot = resolveManagedInstallRoot(base, installRoot, runtime.GOOS)
	host, err := a.GetHostResources()
	if err != nil {
		return SetupEnvironment{}, fmt.Errorf("读取主机资源：%w", err)
	}
	pathValid, pathMessage := true, "安装目录有效"
	if runtime.GOOS == "linux" {
		pathMessage = "Linux 服务器将自动安装到：" + installRoot
	}
	if err := validateInstallDirectory(installRoot); err != nil {
		pathValid, pathMessage = false, err.Error()
	}
	diskFree := int64(0)
	if pathValid {
		probe, probeErr := existingSetupProbePath(installRoot)
		if probeErr != nil {
			pathValid, pathMessage = false, probeErr.Error()
		} else if diskFree, err = setupDiskFreeBytes(probe); err != nil {
			return SetupEnvironment{}, fmt.Errorf("读取安装磁盘空间：%w", err)
		}
	}
	return evaluateSetupEnvironment(runtime.GOOS, runtime.NumCPU(), host.MemoryTotalMB, diskFree, pathValid, pathMessage), nil
}

package main

import (
	"embed"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed resources/Engine.*.ini
var engineConfigs embed.FS

const (
	performanceEngineBackupSuffix = ".palserver-backup"
	performanceEngineBlockBegin   = "; BEGIN PALSERVER LAUNCHER PERFORMANCE SETTINGS"
	performanceEngineBlockEnd     = "; END PALSERVER LAUNCHER PERFORMANCE SETTINGS"
)

func performanceEngineConfig(enabled bool) []byte {
	name := "resources/Engine.standard.ini"
	if enabled {
		name = "resources/Engine.optimized.ini"
	}
	data, _ := engineConfigs.ReadFile(name)
	return data
}

func performanceLaunchArgs(instance ServerInstance) []string {
	if !instance.LegacyPerformanceFlags {
		return nil
	}
	args := []string{"-useperfthreads", "-NoAsyncLoadingThread", "-UseMultithreadForDS"}
	if instance.WorkerThreads > 0 {
		args = append(args, "-NumberOfWorkerThreadsServer="+strconv.Itoa(instance.WorkerThreads))
	}
	return args
}

func managedPerformanceEngineSettings() string {
	optimized := string(performanceEngineConfig(true))
	section := "[/script/onlinesubsystemutils.ipnetdriver]"
	index := strings.Index(strings.ToLower(optimized), section)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(optimized[index:])
}

func normalizedEngineConfig(content string) string {
	return strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
}

func stripManagedPerformanceBlock(content string) string {
	for {
		start := strings.Index(content, performanceEngineBlockBegin)
		if start < 0 {
			return content
		}
		removeStart := start
		if start >= 2 && content[start-2:start] == "\n\n" {
			removeStart = start - 2
		}
		endOffset := strings.Index(content[start:], performanceEngineBlockEnd)
		if endOffset < 0 {
			return content[:removeStart]
		}
		end := start + endOffset + len(performanceEngineBlockEnd)
		if end < len(content) && content[end] == '\r' {
			end++
		}
		if end < len(content) && content[end] == '\n' {
			end++
		}
		content = content[:removeStart] + content[end:]
	}
}

func mergePerformanceEngineConfig(existing []byte, enabled bool) []byte {
	content := string(existing)
	if normalizedEngineConfig(content) == normalizedEngineConfig(string(performanceEngineConfig(true))) {
		content = string(performanceEngineConfig(false))
	}
	content = stripManagedPerformanceBlock(content)
	if !enabled {
		return []byte(content)
	}
	separator := "\n\n"
	if strings.HasSuffix(content, "\n") {
		separator = ""
	}
	if content == "" {
		separator = ""
	}
	managed := performanceEngineBlockBegin + "\n" + managedPerformanceEngineSettings() + "\n" + performanceEngineBlockEnd + "\n"
	return []byte(content + separator + managed)
}

func applyPerformanceConfig(instance ServerInstance) error {
	path := filepath.Join(instance.RootPath, "Pal", "Saved", "Config", serverConfigDirectoryName(), "Engine.ini")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		existing = performanceEngineConfig(false)
	} else if _, backupErr := os.Stat(path + performanceEngineBackupSuffix); os.IsNotExist(backupErr) {
		if backupErr := os.WriteFile(path+performanceEngineBackupSuffix, existing, 0o600); backupErr != nil {
			return backupErr
		}
	}
	return os.WriteFile(path, mergePerformanceEngineConfig(existing, instance.PerformanceMode), 0o600)
}

func memoryUsagePercent(total, free float64) float64 {
	if total <= 0 {
		return 0
	}
	return (total - free) / total * 100
}

func (a *App) GetHostResources() (HostResources, error) {
	return defaultProcessRuntime.HostResources()
}

func (a *App) GetServerSize(id string) (int64, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return 0, err
	}
	return dirSize(instance.RootPath), nil
}

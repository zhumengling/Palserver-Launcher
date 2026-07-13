package main

import (
	"embed"
	"encoding/json"
	"os"
	"path/filepath"
)

//go:embed resources/Engine.*.ini
var engineConfigs embed.FS

func performanceEngineConfig(enabled bool) []byte {
	name := "resources/Engine.standard.ini"
	if enabled {
		name = "resources/Engine.optimized.ini"
	}
	data, _ := engineConfigs.ReadFile(name)
	return data
}

func applyPerformanceConfig(instance ServerInstance) error {
	path := filepath.Join(instance.RootPath, "Pal", "Saved", "Config", "WindowsServer", "Engine.ini")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, performanceEngineConfig(instance.PerformanceMode), 0o600)
}

func memoryUsagePercent(total, free float64) float64 {
	if total <= 0 {
		return 0
	}
	return (total - free) / total * 100
}

func (a *App) GetHostResources() (HostResources, error) {
	query := `$os=Get-CimInstance Win32_OperatingSystem; $cpu=(Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average; [pscustomobject]@{CPU=$cpu;TotalMemoryMB=$os.TotalVisibleMemorySize/1024;FreeMemoryMB=$os.FreePhysicalMemory/1024} | ConvertTo-Json -Compress`
	output, err := newHiddenPowerShell(query).Output()
	if err != nil {
		return HostResources{}, err
	}
	var raw struct {
		CPU, TotalMemoryMB, FreeMemoryMB float64
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return HostResources{}, err
	}
	return HostResources{
		CPUPercent:    raw.CPU,
		MemoryPercent: memoryUsagePercent(raw.TotalMemoryMB, raw.FreeMemoryMB),
		MemoryUsedMB:  raw.TotalMemoryMB - raw.FreeMemoryMB,
		MemoryTotalMB: raw.TotalMemoryMB,
	}, nil
}

func (a *App) GetServerSize(id string) (int64, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return 0, err
	}
	return dirSize(instance.RootPath), nil
}

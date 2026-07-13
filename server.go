package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

type processInfo struct {
	ProcessID      int     `json:"ProcessId"`
	ExecutablePath string  `json:"ExecutablePath"`
	CPU            float64 `json:"CPU"`
	WorkingSet     float64 `json:"WorkingSet64"`
}

func hiddenServerSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW | syscall.CREATE_NEW_PROCESS_GROUP, HideWindow: true}
}

func serverLaunchExecutable(instance ServerInstance) string {
	shipping := filepath.Join(instance.RootPath, "Pal", "Binaries", "Win64", "PalServer-Win64-Shipping.exe")
	if _, err := os.Stat(shipping); err == nil {
		return shipping
	}
	return instance.Executable
}

func newHiddenPowerShell(query string) *exec.Cmd {
	command := exec.Command("powershell", "-NoProfile", "-Command", query)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return command
}

func serverStatus(instance ServerInstance) (RuntimeStatus, error) {
	status := RuntimeStatus{}
	query := `$target='` + strings.ReplaceAll(filepath.Clean(instance.Executable), "'", "''") + `'; Get-CimInstance Win32_Process | Where-Object { $_.Name -in @('PalServer.exe','PalServer-Win64-Shipping-Cmd.exe','PalServer-Win64-Shipping.exe') -and ($_.ExecutablePath -eq $target -or $_.ExecutablePath -like ((Split-Path $target -Parent)+'*')) } | Select-Object -First 1 ProcessId,ExecutablePath | ConvertTo-Json -Compress`
	out, _ := newHiddenPowerShell(query).Output()
	if len(strings.TrimSpace(string(out))) > 0 {
		var info processInfo
		if json.Unmarshal(out, &info) == nil && info.ProcessID > 0 {
			status.Running = true
			status.PID = info.ProcessID
			metricQuery := fmt.Sprintf(`$p=Get-Process -Id %d -ErrorAction SilentlyContinue; if($p){[pscustomobject]@{CPU=$p.CPU;WorkingSet64=$p.WorkingSet64;StartTime=$p.StartTime.ToFileTimeUtc()}|ConvertTo-Json -Compress}`, info.ProcessID)
			metricOut, _ := newHiddenPowerShell(metricQuery).Output()
			var metrics struct {
				CPU, WorkingSet64 float64
				StartTime         int64
			}
			if json.Unmarshal(metricOut, &metrics) == nil {
				status.CPU = metrics.CPU
				status.MemoryMB = metrics.WorkingSet64 / 1024 / 1024
				if metrics.StartTime > 0 {
					unix := (metrics.StartTime - 116444736000000000) / 10000000
					status.Uptime = time.Now().Unix() - unix
				}
			}
		}
	}
	if status.Running {
		if info, err := restInfo(instance); err == nil {
			status.RESTAvailable = true
			status.Version = fmt.Sprint(info["version"])
		}
		if metrics, err := restGet(instance, "/metrics"); err == nil {
			status.FPS = number(metrics["serverfps"])
			status.FrameTime = number(metrics["serverframetime"])
			status.Players = int(number(metrics["currentplayernum"]))
			status.MaxPlayers = int(number(metrics["maxplayernum"]))
		}
		if _, err := sendRCON(instance, "Info"); err == nil {
			status.RCONAvailable = true
		}
	}
	return status, nil
}

func number(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	}
	return 0
}

func (a *App) GetStatus(id string) (RuntimeStatus, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return RuntimeStatus{}, err
	}
	return serverStatus(instance)
}

func (a *App) StartServer(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("server is already running")
	}
	a.setGuardianSuppressed(id, false)
	if _, err := os.Stat(instance.Executable); err != nil {
		return fmt.Errorf("server executable not found: %w", err)
	}
	if err := applyPerformanceConfig(instance); err != nil {
		return fmt.Errorf("apply Engine.ini: %w", err)
	}
	args := []string{
		fmt.Sprintf("-RCONPort=%d", instance.RCONPort),
		fmt.Sprintf("-port=%d", instance.PublicPort),
		fmt.Sprintf("-publicport=%d", instance.PublicPort),
		fmt.Sprintf("-QueryPort=%d", instance.QueryPort),
	}
	if instance.PublicIP != "" {
		args = append(args, "-publicip="+instance.PublicIP)
	}
	if instance.Community {
		args = append(args, "-publiclobby")
	}
	if instance.PerformanceMode {
		args = append(args, "-useperfthreads", "-NoAsyncLoadingThread", "-UseMultithreadForDS")
	}
	// PalServer.exe is a small launcher that creates the visible
	// PalServer-Win64-Shipping-Cmd.exe console. Start the non-console Shipping
	// binary directly when it is available so no child console is created.
	launchPath := serverLaunchExecutable(instance)
	cmd := exec.Command(launchPath, args...)
	cmd.Dir = instance.RootPath
	// Keep the dedicated server attached to the launcher without creating a
	// visible console window. Its stdout/stderr are mirrored into server.log,
	// which is what the in-app console reads.
	cmd.SysProcAttr = hiddenServerSysProcAttr()
	logDir := filepath.Join(instance.RootPath, "launcher-logs")
	_ = os.MkdirAll(logDir, 0o755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	a.scheduleAutoRestart(instance)
	runtime.EventsEmit(a.ctx, "server:started", id, cmd.Process.Pid)
	a.notifyDiscord(id, "start", "服务器已启动", instance.Name)
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		runtime.EventsEmit(a.ctx, "server:exited", id, fmt.Sprint(err))
		a.handleServerExit(instance, err)
	}()
	return nil
}

func (a *App) StopServer(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	a.markExpectedStop(id)
	a.setGuardianSuppressed(id, true)
	if status, statusErr := serverStatus(instance); statusErr == nil && !status.Running {
		a.clearExpectedStop(id)
		a.setGuardianSuppressed(id, false)
		return nil
	}
	if _, err := restPost(instance, "/shutdown", map[string]any{"waittime": 5, "message": "Server maintenance"}); err == nil {
		a.notifyDiscord(id, "stop", "服务器正在停止", instance.Name)
		return nil
	}
	if _, err := sendRCON(instance, "Shutdown 5 Server maintenance"); err == nil {
		a.notifyDiscord(id, "stop", "服务器正在停止", instance.Name)
		return nil
	}
	// PalDefender may stop REST before the game process exits. Ask Windows to
	// terminate the process tree without /F, then wait for the process to exit.
	if status, statusErr := serverStatus(instance); statusErr == nil && status.Running {
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(status.PID), "/T").Run()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			current, _ := serverStatus(instance)
			if !current.Running {
				a.notifyDiscord(id, "stop", "服务器已停止", instance.Name)
				return nil
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	a.clearExpectedStop(id)
	a.setGuardianSuppressed(id, false)
	return errors.New("graceful shutdown failed; use force stop")
}

func (a *App) ForceStopServer(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, err := serverStatus(instance)
	if err != nil || !status.Running {
		return errors.New("server is not running")
	}
	a.markExpectedStop(id)
	a.setGuardianSuppressed(id, true)
	if err := exec.Command("taskkill", "/PID", strconv.Itoa(status.PID), "/T", "/F").Run(); err != nil {
		a.clearExpectedStop(id)
		a.setGuardianSuppressed(id, false)
		return err
	}
	return nil
}

func restartDelay(hours int) time.Duration {
	if hours <= 0 {
		return 0
	}
	return time.Duration(hours) * time.Hour
}

func (a *App) markExpectedStop(id string) {
	a.processMu.Lock()
	defer a.processMu.Unlock()
	a.expectedStops[id] = true
	if cancel := a.restartCancels[id]; cancel != nil {
		close(cancel)
		delete(a.restartCancels, id)
	}
}

func (a *App) clearExpectedStop(id string) {
	a.processMu.Lock()
	defer a.processMu.Unlock()
	delete(a.expectedStops, id)
}

func (a *App) consumeExpectedStop(id string) bool {
	a.processMu.Lock()
	defer a.processMu.Unlock()
	expected := a.expectedStops[id]
	delete(a.expectedStops, id)
	return expected
}

func (a *App) scheduleAutoRestart(instance ServerInstance) {
	delay := restartDelay(instance.AutoRestartHours)
	a.processMu.Lock()
	if previous := a.restartCancels[instance.ID]; previous != nil {
		close(previous)
	}
	if delay == 0 {
		delete(a.restartCancels, instance.ID)
		a.processMu.Unlock()
		return
	}
	cancel := make(chan struct{})
	a.restartCancels[instance.ID] = cancel
	a.processMu.Unlock()
	go func() {
		select {
		case <-time.After(delay):
			_, _ = sendRCON(instance, "Save")
			a.markExpectedStop(instance.ID)
			if _, err := restPost(instance, "/shutdown", map[string]any{"waittime": 5, "message": "Scheduled restart"}); err != nil {
				_, _ = sendRCON(instance, "Shutdown 5 Scheduled restart")
			}
			for i := 0; i < 30; i++ {
				time.Sleep(2 * time.Second)
				status, _ := serverStatus(instance)
				if !status.Running {
					time.Sleep(time.Second)
					_ = a.StartServer(instance.ID)
					return
				}
			}
		case <-cancel:
		}
	}()
}

func (a *App) handleServerExit(instance ServerInstance, waitErr error) {
	if a.consumeExpectedStop(instance.ID) {
		return
	}
	a.notifyDiscord(instance.ID, "crash", "服务器异常退出", fmt.Sprint(waitErr))
	if !instance.CrashRestart && !instance.GuardianEnabled {
		return
	}
	go func() {
		time.Sleep(5 * time.Second)
		a.recoverWithGuardian(instance, "server process exited")
	}()
}

func (a *App) InstallOrUpdateServer(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	if instance.SteamCMDPath == "" {
		base, _ := appDataDir()
		instance.SteamCMDPath = filepath.Join(base, "runtime", "steamcmd", "steamcmd.exe")
	}
	instance.SteamCMDPath = steamCMDExecutable(instance.SteamCMDPath)
	if err := ensureSteamCMD(instance.SteamCMDPath, func(message string, percent int) {
		runtime.EventsEmit(a.ctx, "install:progress", map[string]any{"message": message, "percent": percent})
	}); err != nil {
		return err
	}
	return a.installOrUpdate(instance, func(progress steamCMDProgress) { runtime.EventsEmit(a.ctx, "install:progress", id, progress) })
}

func steamCMDExecutable(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return filepath.Join(path, "steamcmd.exe")
	}
	return path
}

func (a *App) installOrUpdate(instance ServerInstance, onProgress func(steamCMDProgress)) error {
	return runSteamCMD(instance, func(progress steamCMDProgress) {
		if onProgress != nil {
			onProgress(progress)
		}
	})
}

func formatSteamCMDError(waitErr error, lines []string) error {
	detail := ""
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if strings.Contains(strings.ToLower(line), "error") || strings.Contains(strings.ToLower(line), "failed") {
			detail = line
			break
		}
	}
	combined := strings.ToLower(strings.Join(lines, "\n"))
	if strings.Contains(combined, "missing configuration") {
		return errors.New("SteamCMD 安装失败：Steam AppInfo 配置未加载，启动器已刷新缓存，请再次点击安装；详细日志位于服务器目录\\launcher-logs\\steamcmd.log")
	}
	if detail != "" {
		return fmt.Errorf("SteamCMD 安装失败 (%v): %s", waitErr, detail)
	}
	return fmt.Errorf("SteamCMD 安装失败: %w", waitErr)
}

func (a *App) GetConsoleLog(id string, lines int) (string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return "", err
	}
	paths := []string{
		filepath.Join(instance.RootPath, "launcher-logs", "server.log"),
		filepath.Join(instance.RootPath, "Pal", "Binaries", "Win64", "PalDefender", "Logs"),
	}
	var newest string
	var newestTime time.Time
	for _, p := range paths {
		info, statErr := os.Stat(p)
		if statErr != nil {
			continue
		}
		if !info.IsDir() {
			if info.ModTime().After(newestTime) {
				newest, newestTime = p, info.ModTime()
			}
			continue
		}
		entries, _ := os.ReadDir(p)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			entryInfo, _ := entry.Info()
			if entryInfo != nil && entryInfo.ModTime().After(newestTime) {
				newest, newestTime = filepath.Join(p, entry.Name()), entryInfo.ModTime()
			}
		}
	}
	if newest == "" {
		return "", nil
	}
	data, err := os.ReadFile(newest)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(data), "\n")
	if lines > 0 && len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n"), nil
}

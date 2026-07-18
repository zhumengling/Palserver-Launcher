package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func serverChildProcessRunning(instance ServerInstance) bool {
	process, found, err := defaultProcessRuntime.FindServerProcess(instance)
	return err == nil && found && process.PID > 0 && !usesPalServerWrapper(process.Path)
}

func waitForServerChild(instance ServerInstance, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if serverChildProcessRunning(instance) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(400 * time.Millisecond)
	}
}

func rconListenerMatchesProcess(running bool, processID, listenerOwnerPID int) bool {
	return running && processID > 0 && listenerOwnerPID == processID
}

func serverProcessRootPattern(executable string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(executable)), "*")
}

func serverStatus(instance ServerInstance) (RuntimeStatus, error) {
	process, found, processErr := defaultProcessRuntime.FindServerProcess(instance)
	if processErr != nil {
		return RuntimeStatus{}, processErr
	}
	return serverStatusFromProcess(instance, process, found), nil
}

func serverStatusFromProcessBase(instance ServerInstance, process serverProcessSnapshot, found bool) RuntimeStatus {
	status := RuntimeStatus{}
	if found {
		status.Running, status.PID = true, process.PID
		status.CPU, status.MemoryMB = process.CPUPercent, process.MemoryMB
		if !process.StartedAt.IsZero() {
			status.Uptime = max(0, time.Now().Unix()-process.StartedAt.Unix())
		}
		ownerPID, listening, _ := defaultProcessRuntime.TCPListenerOwner(instance.RCONPort)
		status.RCONAvailable = rconListenerMatchesProcess(status.Running, status.PID, ownerPID) && listening
	}
	return status
}

type serverInfoResult struct {
	value ServerInfo
	err   error
}

type serverMetricsResult struct {
	value ServerMetrics
	err   error
}

func applyOfficialStatus(status RuntimeStatus, info ServerInfo, infoErr error, metrics ServerMetrics, metricsErr error) RuntimeStatus {
	if infoErr == nil {
		status.RESTAvailable = true
		status.Version = info.Version
	}
	if metricsErr == nil {
		status.FPS = metrics.ServerFPS
		status.FrameTime = metrics.ServerFrameTime
		status.Players = metrics.CurrentPlayerNum
		status.MaxPlayers = metrics.MaxPlayerNum
		status.BaseCampNum = metrics.BaseCampNum
		status.WorldDays = metrics.Days
		if metrics.Uptime > 0 {
			status.Uptime = metrics.Uptime
		}
	}
	return status
}

func fetchServerOfficialStatus(info func() (ServerInfo, error), metrics func() (ServerMetrics, error)) (serverInfoResult, serverMetricsResult) {
	infoChannel := make(chan serverInfoResult, 1)
	metricsChannel := make(chan serverMetricsResult, 1)
	go func() {
		value, err := info()
		infoChannel <- serverInfoResult{value: value, err: err}
	}()
	go func() {
		value, err := metrics()
		metricsChannel <- serverMetricsResult{value: value, err: err}
	}()
	return <-infoChannel, <-metricsChannel
}

func serverStatusFromProcess(instance ServerInstance, process serverProcessSnapshot, found bool) RuntimeStatus {
	status := serverStatusFromProcessBase(instance, process, found)
	if !status.Running {
		return status
	}
	info, metrics := fetchServerOfficialStatus(
		func() (ServerInfo, error) { return getOfficialServerInfo(instance) },
		func() (ServerMetrics, error) { return getOfficialServerMetrics(instance) },
	)
	return applyOfficialStatus(status, info.value, info.err, metrics.value, metrics.err)
}

func (a *App) cachedStatusFromProcess(instance ServerInstance, process serverProcessSnapshot, found bool) RuntimeStatus {
	status := serverStatusFromProcessBase(instance, process, found)
	if !status.Running {
		return status
	}
	info, metrics := fetchServerOfficialStatus(
		func() (ServerInfo, error) { return a.cachedServerInfo(instance) },
		func() (ServerMetrics, error) { return a.cachedServerMetrics(instance) },
	)
	return applyOfficialStatus(status, info.value, info.err, metrics.value, metrics.err)
}

func (a *App) cachedRuntimeStatus(instance ServerInstance) (RuntimeStatus, error) {
	process, found, processErr := defaultProcessRuntime.FindServerProcess(instance)
	if processErr != nil {
		return RuntimeStatus{}, processErr
	}
	return a.cachedStatusFromProcess(instance, process, found), nil
}

func (a *App) GetStatus(id string) (RuntimeStatus, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return RuntimeStatus{}, err
	}
	status, cached := a.cachedServerStatus(id, 4*time.Second)
	if !cached {
		status, err = a.cachedRuntimeStatus(instance)
		if err != nil {
			return RuntimeStatus{}, err
		}
		a.cacheServerStatus(id, status, time.Now())
	}
	starting, stopping := a.serverTransitionFlags(id)
	return applyRuntimeState(status, a.currentOperation(id), starting, stopping, time.Now()), nil
}

func applyRuntimeState(status RuntimeStatus, operation string, starting, stopping bool, checkedAt time.Time) RuntimeStatus {
	status.CheckedAt = checkedAt.UnixMilli()
	switch operation {
	case "update", "install":
		status.State, status.StateMessage = "updating", "正在更新服务器程序"
	case "backup":
		status.State, status.StateMessage = "backing_up", "正在保存并复制服务器存档"
	case "restore":
		status.State, status.StateMessage = "restoring", "正在校验并恢复服务器存档"
	case "save-inspector":
		status.State, status.StateMessage = "inspecting", "正在备份并解析服务器存档"
	case "duplicate":
		status.State, status.StateMessage = "duplicating", "正在复制服务器程序与存档"
	case "delete":
		status.State, status.StateMessage = "deleting", "正在删除服务器数据"
	case "restart", "guardian":
		status.State, status.StateMessage = "restarting", "正在停止并重新启动服务器"
	default:
		switch {
		case stopping && status.Running:
			status.State, status.StateMessage = "stopping", "已发送关服指令，正在等待服务器进程退出"
		case starting:
			status.State, status.StateMessage = "starting", "正在启动服务器并等待游戏进程稳定"
		case status.Running && !status.RESTAvailable:
			status.State, status.StateMessage = "degraded", "服务器进程正在运行，但官方 REST API 暂不可用"
		case status.Running:
			status.State, status.StateMessage = "running", "服务器运行正常"
		default:
			status.State, status.StateMessage = "stopped", "服务器已停止"
		}
	}
	return status
}

func (a *App) setServerStarting(id string, starting bool) {
	a.processMu.Lock()
	defer a.processMu.Unlock()
	if a.startingServers == nil {
		a.startingServers = map[string]bool{}
	}
	if starting {
		a.startingServers[id] = true
	} else {
		delete(a.startingServers, id)
	}
}

func (a *App) serverTransitionFlags(id string) (starting, stopping bool) {
	a.processMu.Lock()
	defer a.processMu.Unlock()
	return a.startingServers[id], a.expectedStops[id]
}

func prepareServerBeforeLaunch(instance ServerInstance) error {
	if err := applyPendingExtensionUpdates(instance); err != nil {
		return fmt.Errorf("apply pending extension updates: %w", err)
	}
	return nil
}

func (a *App) StartServer(id string) error {
	a.serverStartMu.Lock()
	defer a.serverStartMu.Unlock()
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	if err := validateManagedServerRoot(instance.RootPath); err != nil {
		return err
	}
	if err := validatePlatformServerExecutable(instance); err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("server is already running")
	}
	a.invalidateOfficialCache(id)
	a.setServerStarting(id, true)
	defer a.setServerStarting(id, false)
	runningInstances := make([]ServerInstance, 0)
	for _, other := range a.store.Snapshot().Instances {
		if other.ID == instance.ID {
			continue
		}
		if otherStatus, statusErr := serverStatus(other); statusErr == nil && otherStatus.Running {
			runningInstances = append(runningInstances, other)
		}
	}
	if err := validateServerInstancePorts(instance, runningInstances); err != nil {
		return fmt.Errorf("无法启动服务器：%w", err)
	}
	a.setGuardianSuppressed(id, false)
	if _, err := os.Stat(instance.Executable); err != nil {
		return fmt.Errorf("server executable not found: %w", err)
	}
	a.extensionStageMu.Lock()
	prepareErr := prepareServerBeforeLaunch(instance)
	a.extensionStageMu.Unlock()
	if prepareErr != nil {
		return prepareErr
	}
	if err := ensureDirectXRuntime(nil); err != nil {
		return fmt.Errorf("无法启动服务器：%w", err)
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
	args = append(args, performanceLaunchArgs(instance)...)
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
	logPath := filepath.Join(logDir, "server.log")
	_ = rotateLogFile(logPath, managedLogMaxBytes, managedLogBackups)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(logFile, "%s%s =====\n", compatibilityLaunchMarker, time.Now().Format(time.RFC3339))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	if usesPalServerWrapper(launchPath) {
		// PalServer.exe only launches the actual Shipping child process and exits.
		// Waiting on this wrapper would report a false startup failure, so verify
		// the child through the instance process detector instead.
		if !waitForServerChild(instance, 10*time.Second) {
			go func() { _ = <-exited; _ = logFile.Close() }()
			logTail := readLogTail(logFile.Name())
			failure := serverStartupFailure(nil, logTail)
			if recordPluginCrash(instance, logTail).PluginRelated {
				return fmt.Errorf("%w 检测到 UE4SS/PalDefender 相关崩溃，请在插件页使用安全模式启动", failure)
			}
			return failure
		}
	} else {
		select {
		case waitErr := <-exited:
			_ = logFile.Close()
			logTail := readLogTail(logFile.Name())
			failure := serverStartupFailure(waitErr, logTail)
			if recordPluginCrash(instance, fmt.Sprint(waitErr)+"\n"+logTail).PluginRelated {
				return fmt.Errorf("%w 检测到 UE4SS/PalDefender 相关崩溃，请在插件页使用安全模式启动", failure)
			}
			return failure
		case <-time.After(3 * time.Second):
		}
	}
	a.onServerStarted(id, time.Now())
	actualPID := cmd.Process.Pid
	if process, found, _ := defaultProcessRuntime.FindServerProcess(instance); found {
		actualPID = process.PID
	}
	watcher := "cmd"
	if usesPalServerWrapper(launchPath) {
		watcher = "monitor"
	}
	a.registerObservedProcess(id, actualPID, watcher)
	if tuningErr := a.rebalanceServerProcesses(); tuningErr != nil {
		appendProcessTuningWarning(instance, tuningErr)
	}
	a.scheduleAutoRestart(instance)
	scheduleCompatibilityBaseline(instance)
	a.emit("server:started", id, actualPID)
	a.notifyDiscord(id, "start", "服务器已启动", instance.Name)
	if usesPalServerWrapper(launchPath) {
		go func() {
			_ = <-exited
			_ = logFile.Close()
			if tuningErr := a.rebalanceServerProcesses(); tuningErr != nil {
				appendProcessTuningWarning(instance, tuningErr)
			}
		}()
	} else {
		go func() {
			err := <-exited
			_ = logFile.Close()
			if tuningErr := a.rebalanceServerProcesses(); tuningErr != nil {
				appendProcessTuningWarning(instance, tuningErr)
			}
			a.emit("server:exited", id, fmt.Sprint(err))
			a.handleServerExit(instance, err)
		}()
	}
	return nil
}

func readLogTail(path string) string {
	data, err := readFileTail(path, 1600)
	if err != nil || len(data) == 0 {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func serverStartupFailure(waitErr error, logTail string) error {
	message := "服务器进程在启动后立即退出。请先检查 DirectX 运行库；启动器会在创建或启动服务器时自动修复缺失组件。"
	if logTail != "" {
		message += " 最近日志：" + logTail
	}
	if waitErr != nil {
		message += " 进程错误：" + waitErr.Error()
	}
	return errors.New(message)
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
		a.rebalanceAfterServerExit(instance)
		a.notifyDiscord(id, "stop", "服务器正在停止", instance.Name)
		return nil
	}
	if _, err := sendRCON(instance, "Shutdown 5 Server maintenance"); err == nil {
		a.rebalanceAfterServerExit(instance)
		a.notifyDiscord(id, "stop", "服务器正在停止", instance.Name)
		return nil
	}
	// PalDefender may stop REST before the game process exits. Ask Windows to
	// terminate the process tree without /F, then wait for the process to exit.
	if status, statusErr := serverStatus(instance); statusErr == nil && status.Running {
		_ = terminateProcessTree(status.PID, false)
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			current, _ := serverStatus(instance)
			if !current.Running {
				if tuningErr := a.rebalanceServerProcesses(); tuningErr != nil {
					appendProcessTuningWarning(instance, tuningErr)
				}
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
	if _, stopErr := restPost(instance, "/stop", nil); stopErr == nil {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			current, _ := serverStatus(instance)
			if !current.Running {
				a.rebalanceAfterServerExit(instance)
				return nil
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	if err := terminateProcessTree(status.PID, true); err != nil {
		a.clearExpectedStop(id)
		a.setGuardianSuppressed(id, false)
		return err
	}
	a.rebalanceAfterServerExit(instance)
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
	a.clearObservedProcess(instance.ID)
	if a.consumeExpectedStop(instance.ID) {
		return
	}
	recordPluginCrash(instance, fmt.Sprint(waitErr))
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
	if !a.tryBeginOperation(id, "install") {
		return errors.New("server is busy")
	}
	defer a.endOperation(id)
	return a.installOrUpdateServer(id)
}

func (a *App) installOrUpdateServer(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	if err := validateManagedServerRoot(instance.RootPath); err != nil {
		return err
	}
	if instance.SteamCMDPath == "" {
		base, _ := appDataDir()
		instance.SteamCMDPath = defaultSteamCMDExecutable(base)
	}
	instance.SteamCMDPath = steamCMDExecutable(instance.SteamCMDPath)
	if err := ensureSteamCMD(instance.SteamCMDPath, func(message string, percent int) {
		a.emit("install:progress", map[string]any{"message": message, "percent": percent})
	}); err != nil {
		return err
	}
	if err := a.installOrUpdate(instance, func(progress steamCMDProgress) { a.emit("install:progress", id, progress) }); err != nil {
		return err
	}
	return validateInstalledServerExecutable(instance)
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
	return readLogLines(newest, lines)
}

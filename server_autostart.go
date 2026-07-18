package main

import (
	"fmt"
	"time"
)

const automaticServerStartDelay = 2 * time.Second

func shouldAutomaticallyStart(instance ServerInstance, status RuntimeStatus, statusErr error) bool {
	return instance.StartOnBoot && statusErr == nil && !status.Running
}

func (a *App) reportAutomaticStartFailure(instance ServerInstance, err error) {
	message := fmt.Sprintf("服务器“%s”自动启动失败：%v", instance.Name, err)
	a.store.AddWarning(message)
	a.emit("server:status-error", instance.ID, message)
	a.notifyDiscord(instance.ID, "crash", "服务器自动启动失败", err.Error())
}

func (a *App) startAutomaticServers() {
	// Let the process monitor and HTTP/Wails bridge finish starting before
	// beginning potentially expensive game-server launches.
	time.Sleep(time.Second)
	instances := a.store.Snapshot().Instances
	for index, instance := range instances {
		if !instance.StartOnBoot {
			continue
		}
		status, statusErr := serverStatus(instance)
		if statusErr != nil {
			a.reportAutomaticStartFailure(instance, statusErr)
			continue
		}
		if !shouldAutomaticallyStart(instance, status, nil) {
			continue
		}
		if !a.tryBeginOperation(instance.ID, "autostart") {
			a.reportAutomaticStartFailure(instance, fmt.Errorf("服务器正在执行其他任务"))
			continue
		}
		startErr := a.StartServer(instance.ID)
		a.endOperation(instance.ID)
		if startErr != nil {
			a.reportAutomaticStartFailure(instance, startErr)
		}
		if index < len(instances)-1 {
			time.Sleep(automaticServerStartDelay)
		}
	}
}

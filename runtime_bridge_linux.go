//go:build linux

package main

import "errors"

func emitPlatformEvent(*App, AgentEvent) {}

func chooseDirectoryPlatform(*App, string) (string, error) {
	return "", errors.New("Linux 网页模式请直接填写服务器路径")
}

func chooseFilesPlatform(*App, string) ([]string, error) {
	return nil, errors.New("Linux 网页模式请使用文件上传")
}

func openPathPlatform(string, bool) error {
	return errors.New("Linux 网页模式不能在服务器上打开桌面文件管理器")
}

func openExternalURLPlatform(*App, string) error {
	return errors.New("请在浏览器中打开该链接")
}

func quitApplicationPlatform(a *App) {
	if a.quit != nil {
		go a.quit()
	}
}

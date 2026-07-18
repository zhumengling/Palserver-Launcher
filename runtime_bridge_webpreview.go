//go:build webpreview && windows

package main

import "errors"

// The local web preview runs on Windows during development, but it does not
// have a Wails lifecycle context. Keep its bridge browser-safe so background
// events are delivered only through the shared SSE event hub instead of
// calling Wails runtime APIs with an invalid context.
func emitPlatformEvent(*App, AgentEvent) {}

func chooseDirectoryPlatform(*App, string) (string, error) {
	return "", errors.New("网页预览模式请直接填写服务器路径")
}

func chooseFilesPlatform(*App, string) ([]string, error) {
	return nil, errors.New("网页预览模式不支持桌面文件选择器")
}

func openPathPlatform(string, bool) error {
	return errors.New("网页预览模式不能打开服务器上的桌面文件管理器")
}

func openExternalURLPlatform(*App, string) error {
	return errors.New("请在浏览器中打开该链接")
}

func quitApplicationPlatform(a *App) {
	if a.quit != nil {
		go a.quit()
	}
}

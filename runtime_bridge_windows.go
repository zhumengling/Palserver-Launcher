//go:build windows && !webpreview

package main

import (
	"errors"
	"os/exec"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func emitPlatformEvent(a *App, event AgentEvent) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event.Name, event.Args...)
	}
}

func chooseDirectoryPlatform(a *App, title string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

func chooseFilesPlatform(a *App, title string) ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

func openPathPlatform(path string, directory bool) error {
	args := []string{path}
	if !directory {
		args = []string{"/select," + filepath.Clean(path)}
	}
	if err := exec.Command("explorer.exe", args...).Start(); err != nil {
		return errors.New("failed to open Explorer: " + err.Error())
	}
	return nil
}

func openExternalURLPlatform(a *App, url string) error {
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

func quitApplicationPlatform(a *App) {
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

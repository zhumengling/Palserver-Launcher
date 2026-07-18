//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func serverConfigDirectoryName() string { return "WindowsServer" }

func autoInstallCoreExtensionID() string { return "paldefender" }

func coreExtensionSupported(string) bool { return true }

func coreExtensionUnsupportedReason(string) string { return "" }

func serverModsSupported() bool { return true }

func serverModsUnsupportedReason() string { return "" }

func validatePlatformInstallPath(path string) error {
	for _, character := range path {
		if character > 127 {
			return errors.New("SteamCMD 安装目录不能包含中文，请选择例如 D:\\PalworldServers\\Server1 的英文目录")
		}
	}
	return nil
}

func validateManagedServerRoot(string) error { return nil }

func validatePlatformServerExecutable(instance ServerInstance) error {
	if strings.TrimSpace(instance.Executable) == "" || !filepath.IsAbs(instance.Executable) {
		return errors.New("服务器程序必须使用完整路径")
	}
	if !pathWithinAllowedRoots(instance.Executable, []string{instance.RootPath}) {
		return errors.New("服务器程序必须位于服务器目录内")
	}
	return nil
}

func validateInstalledServerExecutable(instance ServerInstance) error {
	paths := []string{
		instance.Executable,
		filepath.Join(instance.RootPath, "Pal", "Binaries", "Win64", "PalServer-Win64-Shipping.exe"),
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return errors.New("SteamCMD completed but the Windows server executable is missing: " + path)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return errors.New("SteamCMD created an invalid Windows server executable: " + path)
		}
	}
	return nil
}

func defaultServerExecutable(root string) string { return filepath.Join(root, "PalServer.exe") }

func defaultSteamCMDExecutable(base string) string {
	return filepath.Join(base, "runtime", "steamcmd", "steamcmd.exe")
}

func isolatedSteamCMDExecutable(serverRoot string) string {
	return filepath.Join(filepath.Dir(serverRoot), "PalserverRuntime", "steamcmd", "steamcmd.exe")
}

func steamCMDExecutable(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return filepath.Join(path, "steamcmd.exe")
	}
	return path
}

func serverBinaryRoot(instance ServerInstance) string {
	return filepath.Join(instance.RootPath, "Pal", "Binaries", "Win64")
}

func serverLaunchExecutable(instance ServerInstance) string {
	shipping := filepath.Join(serverBinaryRoot(instance), "PalServer-Win64-Shipping.exe")
	if _, err := os.Stat(shipping); err == nil {
		return shipping
	}
	return instance.Executable
}

func usesPalServerWrapper(path string) bool {
	return strings.EqualFold(filepath.Base(filepath.Clean(path)), "PalServer.exe")
}

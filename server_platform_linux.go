//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func serverConfigDirectoryName() string { return "LinuxServer" }

func autoInstallCoreExtensionID() string { return "" }

func coreExtensionSupported(string) bool { return false }

func coreExtensionUnsupportedReason(string) string {
	return serverModsUnsupportedReason()
}

func serverModsSupported() bool { return false }

func serverModsUnsupportedReason() string {
	return linuxServerModsUnsupportedMessage
}

func configuredLinuxServerRoots() []string {
	configured := strings.TrimSpace(os.Getenv("PALSERVER_ALLOWED_SERVER_ROOTS"))
	if configured == "" {
		return nil
	}
	result := make([]string, 0)
	for _, root := range filepath.SplitList(configured) {
		if root = strings.TrimSpace(root); root != "" && filepath.IsAbs(root) {
			result = append(result, filepath.Clean(root))
		}
	}
	return result
}

func validateLinuxManagedServerRoot(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" || !filepath.IsAbs(path) {
		return errors.New("Linux 服务器目录必须是绝对路径")
	}
	roots := configuredLinuxServerRoots()
	if len(roots) > 0 {
		allowed, err := resolvedPathWithinAllowedRoots(path, roots)
		if err != nil {
			return errors.New("无法确认 Linux 服务器目录的真实路径：" + err.Error())
		}
		if !allowed {
			return errors.New("当前 systemd 服务只允许管理以下目录中的服务器：" + strings.Join(roots, "、"))
		}
	}
	return nil
}

func validatePlatformInstallPath(path string) error { return validateLinuxManagedServerRoot(path) }

func validateManagedServerRoot(path string) error { return validateLinuxManagedServerRoot(path) }

func validatePlatformServerExecutable(instance ServerInstance) error {
	if strings.TrimSpace(instance.Executable) == "" {
		return errors.New("Linux 服务器启动脚本不能为空")
	}
	allowed, err := resolvedPathWithinAllowedRoots(instance.Executable, []string{instance.RootPath})
	if err != nil {
		return errors.New("无法确认 Linux 服务器启动脚本的真实路径：" + err.Error())
	}
	if !allowed {
		return errors.New("Linux 服务器启动脚本必须位于服务器目录内")
	}
	return nil
}

func validateInstalledServerExecutable(instance ServerInstance) error {
	paths := []string{
		instance.Executable,
		filepath.Join(instance.RootPath, "Pal", "Binaries", "Linux", "PalServer-Linux-Shipping"),
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return errors.New("SteamCMD completed but the Linux server executable is missing: " + path)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return errors.New("SteamCMD created an invalid Linux server executable: " + path)
		}
		if info.Mode().Perm()&0o111 == 0 {
			if err := os.Chmod(path, info.Mode().Perm()|0o700); err != nil {
				return errors.New("cannot make the Linux server executable runnable: " + err.Error())
			}
		}
	}
	return nil
}

func defaultServerExecutable(root string) string { return filepath.Join(root, "PalServer.sh") }

func defaultSteamCMDExecutable(base string) string {
	return filepath.Join(base, "runtime", "steamcmd", "steamcmd.sh")
}

func isolatedSteamCMDExecutable(serverRoot string) string {
	return filepath.Join(filepath.Dir(serverRoot), "PalserverRuntime", "steamcmd", "steamcmd.sh")
}

func steamCMDExecutable(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return filepath.Join(path, "steamcmd.sh")
	}
	return path
}

func serverBinaryRoot(instance ServerInstance) string {
	return filepath.Join(instance.RootPath, "Pal", "Binaries", "Linux")
}

func serverLaunchExecutable(instance ServerInstance) string {
	return instance.Executable
}

func usesPalServerWrapper(path string) bool {
	return filepath.Base(filepath.Clean(path)) == "PalServer.sh"
}

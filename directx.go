package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var directXLegacyRuntimeFiles = []string{
	"d3dcompiler_43.dll",
	"d3dx9_43.dll",
	"xinput1_3.dll",
	"xaudio2_7.dll",
}

func missingDirectXRuntimeFiles(systemDirectory string, fileExists func(string) bool) []string {
	missing := make([]string, 0, len(directXLegacyRuntimeFiles))
	for _, name := range directXLegacyRuntimeFiles {
		if !fileExists(filepath.Join(systemDirectory, name)) {
			missing = append(missing, name)
		}
	}
	return missing
}

func defaultDirectXRepairDataPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Downloads", "DirectX_Repair_v4.4.0.36953", "DirectX_Repair", "Data")
}

func directXSystemDirectory() string {
	root := strings.TrimSpace(os.Getenv("WINDIR"))
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32")
}

func missingDirectXRuntime() []string {
	return missingDirectXRuntimeFiles(directXSystemDirectory(), func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	})
}

func directXRepairExecutable(path string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(path))
	if strings.EqualFold(filepath.Base(root), "Data") {
		root = filepath.Dir(root)
	}
	if root == "." || root == "" {
		return "", errors.New("DirectX repair directory is required")
	}
	dataPath := filepath.Join(root, "Data")
	if info, err := os.Stat(dataPath); err != nil || !info.IsDir() {
		return "", errors.New("DirectX repair Data directory was not found")
	}
	executable := filepath.Join(root, "DirectX Repair.exe")
	if info, err := os.Stat(executable); err != nil || info.IsDir() {
		return "", errors.New("DirectX Repair.exe was not found")
	}
	return executable, nil
}

func directXRepairDirectories() []string {
	candidates := make([]string, 0, 4)
	if configured := strings.TrimSpace(os.Getenv("PALSERVER_DIRECTX_REPAIR_DIR")); configured != "" {
		candidates = append(candidates, configured)
	}
	if base, err := appDataDir(); err == nil {
		candidates = append(candidates, filepath.Join(base, "runtime", "directx-repair"))
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "DirectX_Repair"))
	}
	if downloadsPath := defaultDirectXRepairDataPath(); downloadsPath != "" {
		candidates = append(candidates, downloadsPath)
	}
	return candidates
}

func locateDirectXRepair() (string, string, error) {
	for _, candidate := range directXRepairDirectories() {
		executable, err := directXRepairExecutable(candidate)
		if err == nil {
			return filepath.Dir(executable), executable, nil
		}
	}
	return "", "", fmt.Errorf("未找到 DirectX Repair 及 Data 包；请将其放到启动器目录的 DirectX_Repair 文件夹，或设置 PALSERVER_DIRECTX_REPAIR_DIR")
}

func directXRepairCachePath() (string, error) {
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "runtime", "directx-repair"), nil
}

func prepareDirectXRepair(source string) (string, error) {
	cache, err := directXRepairCachePath()
	if err != nil {
		return "", err
	}
	if executable, executableErr := directXRepairExecutable(cache); executableErr == nil {
		return executable, nil
	}
	if err := copyTree(source, cache); err != nil {
		return "", fmt.Errorf("copy DirectX repair data: %w", err)
	}
	executable, err := directXRepairExecutable(cache)
	if err != nil {
		return "", err
	}
	settings := "[DirectX Repair]\nLanguage=Auto\nFormStyle=Simple\n"
	if err := os.WriteFile(filepath.Join(cache, "Settings.ini"), []byte(settings), 0o600); err != nil {
		return "", err
	}
	return executable, nil
}

func powerShellQuoted(value string) string { return strings.ReplaceAll(value, "'", "''") }

func runDirectXRepair(executable string) error {
	script := fmt.Sprintf("$process = Start-Process -FilePath '%s' -WorkingDirectory '%s' -Verb RunAs -PassThru; $process.WaitForExit(); exit $process.ExitCode", powerShellQuoted(executable), powerShellQuoted(filepath.Dir(executable)))
	command := exec.Command("powershell", "-NoProfile", "-Command", script)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("DirectX repair did not complete: %w %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ensureDirectXRuntime(progress func(string)) error {
	missing := missingDirectXRuntime()
	if len(missing) == 0 {
		return nil
	}
	if progress != nil {
		progress("正在修复缺失的 DirectX 组件")
	}
	source, _, err := locateDirectXRepair()
	if err != nil {
		return fmt.Errorf("缺少 DirectX 运行库（%s）：%w", strings.Join(missing, "、"), err)
	}
	if progress != nil {
		progress("正在准备 DirectX 修复组件")
	}
	executable, err := prepareDirectXRepair(source)
	if err != nil {
		return err
	}
	if progress != nil {
		progress("正在以管理员权限修复 DirectX")
	}
	if err := runDirectXRepair(executable); err != nil {
		return err
	}
	if missing = missingDirectXRuntime(); len(missing) > 0 {
		return fmt.Errorf("DirectX 修复完成后仍缺少组件：%s，请重启 Windows 后重试", strings.Join(missing, "、"))
	}
	return nil
}

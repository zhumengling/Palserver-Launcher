package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// steamcmdBootstrap is the official SteamCMD bootstrap archive pinned at build time.
// SteamCMD itself still self-updates when it starts.
//
//go:embed resources/steamcmd/steamcmd.zip
var steamcmdBootstrap []byte

const steamcmdBootstrapSHA256 = "7669B170DEE42DB8EE2273775ED7DFB2D173BDBA1B849F70D2C7B379290BCE13"

var steamCMDProgressPattern = regexp.MustCompile(`(?i)progress:\s*([0-9]+(?:\.[0-9]+)?)`)
var steamCMDBytesPattern = regexp.MustCompile(`(?i)Downloading update \(([0-9,]+) of ([0-9,]+) KB\)`)

type steamCMDProgress struct {
	Message string
	Percent int
}

func steamCMDArchiveHash() string {
	digest := sha256.Sum256(steamcmdBootstrap)
	return strings.ToUpper(hex.EncodeToString(digest[:]))
}

func ensureSteamCMD(path string, progress func(string, int)) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if progress != nil {
		progress("正在释放内置 SteamCMD", 10)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if steamCMDArchiveHash() != steamcmdBootstrapSHA256 {
		return errors.New("内置 SteamCMD 组件校验失败")
	}
	reader, err := zip.NewReader(bytes.NewReader(steamcmdBootstrap), int64(len(steamcmdBootstrap)))
	if err != nil {
		return fmt.Errorf("读取内置 SteamCMD: %w", err)
	}
	for _, file := range reader.File {
		clean := filepath.Clean(file.Name)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return errors.New("内置 SteamCMD 组件包含不安全路径")
		}
		target := filepath.Join(filepath.Dir(path), clean)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		input, err := file.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		input.Close()
		output.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("内置 SteamCMD 释放后找不到 steamcmd.exe: %w", err)
	}
	if progress != nil {
		progress("SteamCMD 已准备完成", 18)
	}
	return nil
}

func steamCMDScript(root string) string {
	root = strings.ReplaceAll(filepath.Clean(root), `"`, `\"`)
	return strings.Join([]string{
		"@ShutdownOnFailedCommand 1",
		"@NoPromptForPassword 1",
		`force_install_dir "` + root + `"`,
		"login anonymous",
		// SteamCMD can have a stale or empty app cache after its self-update.
		// Refreshing AppInfo avoids the misleading exit status 7 / Missing configuration.
		"app_info_update 1",
		"app_update 2394010 validate",
		"quit",
	}, "\r\n") + "\r\n"
}

func parseSteamCMDLine(line string) steamCMDProgress {
	line = strings.TrimSpace(line)
	if line == "" {
		return steamCMDProgress{}
	}
	if match := steamCMDProgressPattern.FindStringSubmatch(line); len(match) == 2 {
		value, _ := strconv.ParseFloat(match[1], 64)
		return steamCMDProgress{Message: fmt.Sprintf("正在下载服务器文件 %.0f%%", value), Percent: 20 + int(value*0.65)}
	}
	if match := steamCMDBytesPattern.FindStringSubmatch(line); len(match) == 3 {
		current, _ := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", ""), 64)
		total, _ := strconv.ParseFloat(strings.ReplaceAll(match[2], ",", ""), 64)
		percent := 20
		if total > 0 {
			percent += int(current / total * 15)
		}
		return steamCMDProgress{Message: "正在更新 SteamCMD 组件", Percent: percent}
	}
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "connecting anonymously"):
		return steamCMDProgress{Message: "正在连接 Steam", Percent: 25}
	case strings.Contains(lower, "loading steam api"):
		return steamCMDProgress{Message: "正在加载 Steam 服务", Percent: 22}
	case strings.Contains(lower, "success! app") || strings.Contains(lower, "fully installed"):
		return steamCMDProgress{Message: "服务器文件下载完成", Percent: 82}
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		return steamCMDProgress{Message: "Steam 安装服务返回错误", Percent: 0}
	default:
		return steamCMDProgress{Message: "正在准备服务器文件", Percent: 30}
	}
}

func runSteamCMD(instance ServerInstance, onProgress func(steamCMDProgress)) error {
	steamcmd := steamCMDExecutable(instance.SteamCMDPath)
	if err := ensureSteamCMD(steamcmd, nil); err != nil {
		return err
	}
	script, err := os.CreateTemp(filepath.Dir(steamcmd), "palserver-steamcmd-*.txt")
	if err != nil {
		return err
	}
	scriptPath := script.Name()
	defer os.Remove(scriptPath)
	if _, err := script.WriteString(steamCMDScript(instance.RootPath)); err != nil {
		script.Close()
		return err
	}
	if err := script.Close(); err != nil {
		return err
	}
	command := exec.Command(steamcmd, "+runscript", scriptPath)
	command.Dir = filepath.Dir(steamcmd)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	_ = os.MkdirAll(filepath.Join(instance.RootPath, "launcher-logs"), 0o755)
	logFile, _ := os.OpenFile(filepath.Join(instance.RootPath, "launcher-logs", "steamcmd.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if logFile != nil {
		defer logFile.Close()
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	command.Stderr = command.Stdout
	if err := command.Start(); err != nil {
		return err
	}
	lines := make([]string, 0, 20)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if logFile != nil {
			_, _ = fmt.Fprintln(logFile, line)
		}
		lines = append(lines, line)
		if len(lines) > 20 {
			lines = lines[len(lines)-20:]
		}
		if onProgress != nil {
			onProgress(parseSteamCMDLine(line))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取 SteamCMD 输出失败: %w", err)
	}
	if err := command.Wait(); err != nil {
		return formatSteamCMDError(err, lines)
	}
	return nil
}

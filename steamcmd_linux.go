//go:build linux

package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const steamCMDLinuxBootstrapURL = "https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz"

var steamCMDProgressPattern = regexp.MustCompile(`(?i)progress:\s*([0-9]+(?:\.[0-9]+)?)`)
var steamCMDBytesPattern = regexp.MustCompile(`(?i)Downloading update \(([0-9,]+) of ([0-9,]+) KB\)`)

type steamCMDProgress struct {
	Message string
	Percent int
}

func ensureSteamCMD(path string, progress func(string, int)) error {
	return ensureLinuxSteamCMDFrom(path, steamCMDLinuxBootstrapURL, &http.Client{Timeout: 10 * time.Minute}, progress)
}

func ensureLinuxSteamCMDFrom(path, bootstrapURL string, client *http.Client, progress func(string, int)) error {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return nil
	}
	if progress != nil {
		progress("正在下载 Linux SteamCMD", 10)
	}
	request, err := http.NewRequest(http.MethodGet, bootstrapURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "palserver-launcher-linux")
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download Linux SteamCMD: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download Linux SteamCMD: %s", response.Status)
	}
	if response.ContentLength > 0 && response.ContentLength > 128<<20 {
		return errors.New("Linux SteamCMD archive is unexpectedly large")
	}
	limited := io.LimitReader(response.Body, (128<<20)+1)
	gzipReader, err := gzip.NewReader(limited)
	if err != nil {
		return fmt.Errorf("read Linux SteamCMD archive: %w", err)
	}
	defer gzipReader.Close()
	root := filepath.Dir(path)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return errors.New("Linux SteamCMD archive contains an unsafe path")
		}
		target := filepath.Join(root, clean)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > 128<<20 {
				return errors.New("Linux SteamCMD archive contains an invalid entry")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(output, io.LimitReader(tarReader, header.Size+1))
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if written != header.Size {
				return errors.New("Linux SteamCMD archive entry size mismatch")
			}
		default:
			return errors.New("Linux SteamCMD archive contains an unsupported entry type")
		}
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("Linux SteamCMD executable was not installed: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
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
		"app_info_update 1",
		"app_update 2394010 validate",
		"quit",
	}, "\n") + "\n"
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
		_ = script.Close()
		return err
	}
	if err := script.Close(); err != nil {
		return err
	}
	command := exec.Command(steamcmd, "+runscript", scriptPath)
	command.Dir = filepath.Dir(steamcmd)
	command.SysProcAttr = hiddenServerSysProcAttr()
	_ = os.MkdirAll(filepath.Join(instance.RootPath, "launcher-logs"), 0o755)
	logPath := filepath.Join(instance.RootPath, "launcher-logs", "steamcmd.log")
	_ = rotateLogFile(logPath, managedLogMaxBytes, managedLogBackups)
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
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

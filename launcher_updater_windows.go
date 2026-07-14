//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const launcherUpdaterWait = 2 * time.Minute

func runLauncherUpdaterFromArgs(args []string) (bool, int) {
	options, handled, err := parseLauncherUpdaterArgs(args)
	if !handled {
		return false, 0
	}
	if err != nil {
		writeLauncherUpdateError("parse updater arguments: " + err.Error())
		return true, 2
	}
	if err := applyAndRestartLauncher(options); err != nil {
		if !options.Elevated && isLauncherPermissionError(err) {
			elevatedOptions := options
			elevatedOptions.PID = os.Getpid()
			elevatedOptions.Elevated = true
			if elevateErr := launchElevatedUpdater(os.Args[0], elevatedOptions); elevateErr == nil {
				return true, 0
			} else {
				err = errors.Join(err, elevateErr)
			}
		}
		if restartErr := restartExistingLauncher(options.Target); restartErr != nil {
			err = errors.Join(err, fmt.Errorf("restart previous launcher: %w", restartErr))
		}
		writeLauncherUpdateError(err.Error())
		return true, 1
	}
	return true, 0
}

func launchUpdaterProcess(helper string, options launcherUpdaterOptions) error {
	command := exec.Command(helper,
		"--apply-launcher-update", "--target", options.Target, "--replacement", options.Replacement, "--pid", strconv.Itoa(options.PID),
	)
	command.Dir = filepath.Dir(helper)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, HideWindow: true}
	return command.Start()
}

func waitForLauncherExit(pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, uint32(timeout/time.Millisecond))
	if err != nil {
		return err
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return errors.New("timed out waiting for the launcher to exit")
	}
	return nil
}

func applyAndRestartLauncher(options launcherUpdaterOptions) error {
	if err := waitForLauncherExit(options.PID, launcherUpdaterWait); err != nil {
		return err
	}
	backup, err := replaceLauncherExecutable(options.Target, options.Replacement)
	if err != nil {
		return fmt.Errorf("replace launcher executable: %w", err)
	}
	if err := startAndVerifyLauncher(options.Target); err != nil {
		rollbackErr := restoreLauncherBackup(options.Target, backup)
		return errors.Join(fmt.Errorf("restart updated launcher: %w", err), rollbackErr)
	}
	_ = os.Remove(backup)
	return nil
}

func restartExistingLauncher(target string) error {
	if _, err := os.Stat(target); err != nil {
		return err
	}
	command := exec.Command(target)
	command.Dir = filepath.Dir(target)
	return command.Start()
}

func startAndVerifyLauncher(target string) error {
	command := exec.Command(target)
	command.Dir = filepath.Dir(target)
	if err := command.Start(); err != nil {
		return err
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(command.Process.Pid))
	_ = command.Process.Release()
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 2500)
	if err != nil {
		return err
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return nil
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return err
	}
	return fmt.Errorf("updated launcher exited early with code %d", exitCode)
}

func restoreLauncherBackup(target, backup string) error {
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(backup, target)
}

func isLauncherPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}

func launchElevatedUpdater(helper string, options launcherUpdaterOptions) error {
	arguments := strings.Join([]string{
		quoteWindowsArg("--apply-launcher-update"), quoteWindowsArg("--target"), quoteWindowsArg(options.Target),
		quoteWindowsArg("--replacement"), quoteWindowsArg(options.Replacement), quoteWindowsArg("--pid"), quoteWindowsArg(strconv.Itoa(options.PID)), quoteWindowsArg("--elevated"),
	}, " ")
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(helper)
	params, _ := windows.UTF16PtrFromString(arguments)
	directory, _ := windows.UTF16PtrFromString(filepath.Dir(helper))
	return windows.ShellExecute(0, verb, file, params, directory, windows.SW_HIDE)
}

func writeLauncherUpdateError(message string) {
	base, err := appDataDir()
	if err != nil {
		return
	}
	path := filepath.Join(base, "updates", "update-error.log")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	content := time.Now().Format(time.RFC3339) + " " + message + "\r\n"
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = file.WriteString(content)
		_ = file.Close()
	}
}

func quoteWindowsArg(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\v\"") {
		return value
	}
	var quoted strings.Builder
	quoted.WriteByte('"')
	backslashes := 0
	for _, char := range value {
		switch char {
		case '\\':
			backslashes++
		case '"':
			quoted.WriteString(strings.Repeat("\\", backslashes*2+1))
			quoted.WriteRune(char)
			backslashes = 0
		default:
			quoted.WriteString(strings.Repeat("\\", backslashes))
			backslashes = 0
			quoted.WriteRune(char)
		}
	}
	quoted.WriteString(strings.Repeat("\\", backslashes*2))
	quoted.WriteByte('"')
	return quoted.String()
}

func replaceLauncherExecutable(target, replacement string) (string, error) {
	if filepath.Clean(target) == filepath.Clean(replacement) {
		return "", errors.New("launcher replacement must differ from the target")
	}
	staging := target + ".new"
	backup := target + ".old"
	_ = os.Remove(staging)
	if err := copyLauncherFile(replacement, staging); err != nil {
		return "", err
	}
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		_ = os.Remove(staging)
		return "", err
	}
	if err := os.Rename(staging, target); err != nil {
		rollbackErr := os.Rename(backup, target)
		_ = os.Remove(staging)
		if rollbackErr != nil {
			return "", errors.Join(err, rollbackErr)
		}
		return "", err
	}
	return backup, nil
}

func copyLauncherFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	return errors.Join(copyErr, syncErr, closeErr)
}

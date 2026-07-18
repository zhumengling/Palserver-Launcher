//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func waitForLauncherProcess(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("timed out waiting for Linux agent to exit")
}

func runLauncherUpdaterFromArgs(args []string) (bool, int) {
	options, handled, err := parseLauncherUpdaterArgs(args)
	if !handled {
		return false, 0
	}
	if err != nil {
		return true, 2
	}
	if err := waitForLauncherProcess(options.PID, 60*time.Second); err != nil {
		return true, 3
	}
	backup, err := replaceLauncherExecutable(options.Target, options.Replacement)
	if err != nil {
		return true, 4
	}
	if os.Getenv("INVOCATION_ID") == "" {
		command := exec.Command(options.Target)
		command.Dir = filepath.Dir(options.Target)
		command.SysProcAttr = hiddenServerSysProcAttr()
		if err := command.Start(); err != nil {
			_ = os.Rename(options.Target, options.Replacement)
			_ = os.Rename(backup, options.Target)
			return true, 5
		}
	}
	_ = os.Remove(args[0])
	return true, 0
}

func launchUpdaterProcess(helper string, options launcherUpdaterOptions) error {
	// A helper started from a systemd service remains in the service cgroup and
	// may be killed as soon as the main process exits. Linux can atomically
	// replace a running executable, so install and validate the new binary in
	// the current process and then let Restart=always start it.
	if os.Getenv("INVOCATION_ID") != "" {
		if err := replaceLauncherForService(options.Target, options.Replacement); err != nil {
			return err
		}
		_ = os.Remove(helper)
		return nil
	}
	command := exec.Command(helper,
		"--apply-launcher-update",
		"--target", options.Target,
		"--replacement", options.Replacement,
		"--pid", strconv.Itoa(options.PID),
	)
	command.Dir = filepath.Dir(helper)
	command.SysProcAttr = hiddenServerSysProcAttr()
	return command.Start()
}

func replaceLauncherForService(target, replacement string) error {
	backup, err := replaceLauncherExecutable(target, replacement)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, target, "--version")
	command.Dir = filepath.Dir(target)
	command.SysProcAttr = hiddenServerSysProcAttr()
	if output, startErr := command.CombinedOutput(); startErr != nil || ctx.Err() != nil {
		rollbackErr := restoreLinuxLauncherBackup(target, backup, replacement)
		if ctx.Err() != nil {
			startErr = ctx.Err()
		}
		return errors.Join(fmt.Errorf("validate updated Linux agent: %w: %s", startErr, string(output)), rollbackErr)
	}
	return nil
}

func restoreLinuxLauncherBackup(target, backup, failedReplacement string) error {
	if err := os.Rename(target, failedReplacement); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(backup, target)
}

func quoteWindowsArg(value string) string { return value }

func replaceLauncherExecutable(target, replacement string) (string, error) {
	target, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	replacement, err = filepath.Abs(filepath.Clean(replacement))
	if err != nil {
		return "", err
	}
	if target == replacement {
		return "", errors.New("replacement path matches the running agent")
	}
	if info, err := os.Stat(replacement); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("Linux agent replacement is not a regular file")
	}
	backup := target + ".previous"
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		return "", fmt.Errorf("backup current Linux agent: %w", err)
	}
	if err := os.Rename(replacement, target); err != nil {
		rollbackErr := os.Rename(backup, target)
		return "", errors.Join(fmt.Errorf("install Linux agent update: %w", err), rollbackErr)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		_ = os.Rename(target, replacement)
		_ = os.Rename(backup, target)
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

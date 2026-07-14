//go:build !windows

package main

import "errors"

func runLauncherUpdaterFromArgs(args []string) (bool, int) {
	_, handled, err := parseLauncherUpdaterArgs(args)
	if !handled {
		return false, 0
	}
	if err != nil {
		return true, 2
	}
	return true, 1
}

func launchUpdaterProcess(helper string, options launcherUpdaterOptions) error {
	return errors.New("launcher self-update is only supported on Windows")
}

func quoteWindowsArg(value string) string { return value }

func replaceLauncherExecutable(target, replacement string) (string, error) {
	return "", errors.New("launcher self-update is only supported on Windows")
}

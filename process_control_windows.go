//go:build windows

package main

import (
	"errors"
	"os/exec"
	"strconv"
)

func terminateProcessTree(pid int, force bool) error {
	if pid <= 0 {
		return errors.New("invalid process id")
	}
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	return exec.Command("taskkill", args...).Run()
}

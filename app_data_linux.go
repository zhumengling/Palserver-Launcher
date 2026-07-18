//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
)

func appDataDir() (string, error) {
	if configured := os.Getenv("PALSERVER_LAUNCHER_HOME"); configured != "" {
		dir, err := filepath.Abs(configured)
		if err != nil {
			return "", err
		}
		return dir, os.MkdirAll(dir, 0o700)
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", errors.New("cannot resolve Linux home directory")
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "palserver-launcher")
	return dir, os.MkdirAll(dir, 0o700)
}

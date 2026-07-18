//go:build windows

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
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("LOCALAPPDATA is not available")
	}
	dir := filepath.Join(base, "palserver-launcher")
	return dir, os.MkdirAll(dir, 0o755)
}

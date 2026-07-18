//go:build windows

package main

import (
	"path/filepath"
	"strings"
)

func launcherReleaseAssetMatches(name string) bool {
	lower := strings.ToLower(name)
	return lower == "palserver-launcher.exe" || strings.HasPrefix(lower, "palserver-launcher-") && strings.HasSuffix(lower, "-windows-amd64.exe")
}

func launcherReleaseAssetMissingMessage() string {
	return "Windows amd64 launcher executable was not found in the release"
}

func launcherUpdatePaths(base, version string) (download, helper string, err error) {
	normalized, err := normalizeLauncherVersion(version)
	if err != nil {
		return "", "", err
	}
	root := filepath.Join(base, "updates", normalized)
	return filepath.Join(root, "palserver-launcher-update.exe"), filepath.Join(root, "palserver-launcher-updater.exe"), nil
}

func prepareLauncherReplacement(downloadPath string) (string, error) { return downloadPath, nil }

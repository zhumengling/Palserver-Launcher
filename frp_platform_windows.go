//go:build windows

package main

import "strings"

func frpReleaseAssetMatches(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "frp_") && strings.HasSuffix(lower, "windows_amd64.zip")
}

func frpExecutableName() string { return "frpc.exe" }

func extractFRPExecutable(archivePath, destination string) error {
	return extractNamedExecutable(archivePath, destination, frpExecutableName())
}

//go:build windows

package main

import "strings"

func saveInspectorReleaseAssetMatches(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "pst_") && strings.HasSuffix(lower, "windows_x86_64.zip")
}

func saveInspectorAssetMissingMessage() string {
	return "Windows x86_64 save inspector asset was not found"
}

func saveInspectorExecutableName() string { return "sav_cli.exe" }

func extractSaveInspectorExecutable(archivePath, destination string) error {
	return extractNamedExecutable(archivePath, destination, saveInspectorExecutableName())
}

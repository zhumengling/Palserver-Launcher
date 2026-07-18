package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const diagnosticLogTailBytes int64 = 1 << 20

var (
	diagnosticIPv4Pattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	diagnosticWindowsUserPath = regexp.MustCompile(`(?i)[A-Z]:\\Users\\[^\\\s]+`)
	diagnosticLinuxUserPath   = regexp.MustCompile(`/home/[^/\s]+`)
)

type diagnosticBundleSummary struct {
	GeneratedAt              string                    `json:"generatedAt"`
	LauncherVersion          string                    `json:"launcherVersion"`
	Platform                 string                    `json:"platform"`
	Architecture             string                    `json:"architecture"`
	Instance                 ServerInstance            `json:"instance"`
	Status                   RuntimeStatus             `json:"status"`
	StatusError              string                    `json:"statusError,omitempty"`
	Host                     HostResources             `json:"host"`
	HostError                string                    `json:"hostError,omitempty"`
	Capabilities             ServerCapabilityReport    `json:"capabilities"`
	CapabilitiesError        string                    `json:"capabilitiesError,omitempty"`
	PluginCompatibility      PluginCompatibilityReport `json:"pluginCompatibility"`
	PluginCompatibilityError string                    `json:"pluginCompatibilityError,omitempty"`
}

func diagnosticSafeInstance(instance ServerInstance) ServerInstance {
	result := instance
	result.RootPath = ""
	result.Executable = ""
	result.SteamCMDPath = ""
	result.PublicIP = ""
	result.AdminPassword = ""
	result.ServerPassword = ""
	result.EncryptedAdminPassword = ""
	result.EncryptedServerPassword = ""
	return result
}

func redactDiagnosticText(value string, instance ServerInstance) string {
	replacements := []struct{ old, new string }{
		{instance.AdminPassword, "[admin-password-redacted]"},
		{instance.ServerPassword, "[server-password-redacted]"},
		{instance.PublicIP, "[public-ip-redacted]"},
		{instance.RootPath, "[server-root]"},
		{instance.Executable, "[server-executable]"},
		{instance.SteamCMDPath, "[steamcmd]"},
	}
	if base, err := appDataDir(); err == nil {
		replacements = append(replacements, struct{ old, new string }{base, "[launcher-data]"})
	}
	for _, replacement := range replacements {
		if strings.TrimSpace(replacement.old) != "" {
			value = strings.ReplaceAll(value, replacement.old, replacement.new)
		}
	}
	value = diagnosticWindowsUserPath.ReplaceAllString(value, `[user-profile]`)
	value = diagnosticLinuxUserPath.ReplaceAllString(value, `[user-profile]`)
	return diagnosticIPv4Pattern.ReplaceAllString(value, `[ip-redacted]`)
}

func redactPluginCompatibility(report PluginCompatibilityReport, instance ServerInstance) PluginCompatibilityReport {
	report.LastCrashSummary = redactDiagnosticText(report.LastCrashSummary, instance)
	for index := range report.Issues {
		report.Issues[index].Detail = redactDiagnosticText(report.Issues[index].Detail, instance)
		report.Issues[index].Action = redactDiagnosticText(report.Issues[index].Action, instance)
	}
	return report
}

func diagnosticBundleFiles(app *App, id string) (map[string][]byte, error) {
	instance, err := app.store.Find(id)
	if err != nil {
		return nil, err
	}
	summary := diagnosticBundleSummary{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), LauncherVersion: LauncherVersion,
		Platform: runtime.GOOS, Architecture: runtime.GOARCH, Instance: diagnosticSafeInstance(instance),
	}
	summary.Status, err = app.GetStatus(id)
	if err != nil {
		summary.StatusError = redactDiagnosticText(err.Error(), instance)
	}
	summary.Host, err = app.GetHostResources()
	if err != nil {
		summary.HostError = redactDiagnosticText(err.Error(), instance)
	}
	summary.Capabilities, err = app.GetServerCapabilities(id)
	if err != nil {
		summary.CapabilitiesError = redactDiagnosticText(err.Error(), instance)
	}
	summary.PluginCompatibility, err = app.GetPluginCompatibility(id)
	if err != nil {
		summary.PluginCompatibilityError = redactDiagnosticText(err.Error(), instance)
	} else {
		summary.PluginCompatibility = redactPluginCompatibility(summary.PluginCompatibility, instance)
	}
	files := map[string][]byte{}
	summaryData, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return nil, err
	}
	files["summary.json"] = append(summaryData, '\n')
	files["README.txt"] = []byte("Palserver Launcher diagnostic bundle\nSensitive passwords, public IP addresses and user profile paths are redacted.\nThe bundle does not contain save files.\n")
	if worldSettings, readErr := app.ReadWorldSettings(id); readErr == nil {
		files["config/PalWorldSettings.redacted.ini"] = []byte(redactDiagnosticText(worldSettings, instance))
	}
	logs := map[string]string{
		"logs/server.log":   filepath.Join(instance.RootPath, "launcher-logs", "server.log"),
		"logs/steamcmd.log": filepath.Join(instance.RootPath, "launcher-logs", "steamcmd.log"),
	}
	if frpRoot, rootErr := frpServerRoot(id); rootErr == nil {
		logs["logs/frpc.log"] = filepath.Join(frpRoot, "frpc.log")
	}
	for name, path := range logs {
		data, readErr := readFileTail(path, diagnosticLogTailBytes)
		if readErr == nil && len(data) > 0 {
			files[name] = []byte(redactDiagnosticText(string(data), instance))
		}
	}
	return files, nil
}

func writeDiagnosticZIP(writer io.Writer, files map[string][]byte) error {
	archive := zip.NewWriter(writer)
	normalized := make(map[string][]byte, len(files))
	names := make([]string, 0, len(files))
	for name, data := range files {
		clean := filepath.ToSlash(filepath.Clean(name))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
			return errors.Join(errors.New("invalid diagnostic entry name"), archive.Close())
		}
		if _, exists := normalized[clean]; exists {
			return errors.Join(errors.New("duplicate diagnostic entry name"), archive.Close())
		}
		normalized[clean] = data
		names = append(names, clean)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return errors.Join(err, archive.Close())
		}
		if _, err := entry.Write(normalized[name]); err != nil {
			return errors.Join(err, archive.Close())
		}
	}
	return archive.Close()
}

func writeDiagnosticBundle(writer io.Writer, app *App, id string) error {
	files, err := diagnosticBundleFiles(app, id)
	if err != nil {
		return err
	}
	return writeDiagnosticZIP(writer, files)
}

func (a *App) CreateDiagnosticBundle(id string) (string, error) {
	if _, err := a.store.Find(id); err != nil {
		return "", err
	}
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(base, "exports", id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, "palserver-diagnostic-"+time.Now().Format("20060102-150405.000000000")+".zip")
	temporary, err := os.CreateTemp(directory, ".diagnostic-*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	writeErr := writeDiagnosticBundle(temporary, a, id)
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", fmt.Errorf("save diagnostic bundle: %w", err)
	}
	return path, nil
}

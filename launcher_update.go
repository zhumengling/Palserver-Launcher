package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const LauncherVersion = "0.1.1"

const launcherReleaseEndpoint = "https://api.github.com/repos/zhumengling/Palserver-Launcher/releases/latest"

var launcherVersionPattern = regexp.MustCompile(`^[vV]?(\d+)(?:\.(\d+))?(?:\.(\d+))?$`)

type launcherReleaseAsset struct {
	Name   string
	URL    string
	Digest string
	Size   int64
}

type launcherUpdaterOptions struct {
	Target      string
	Replacement string
	PID         int
	Elevated    bool
}

func normalizeLauncherVersion(value string) (string, error) {
	matches := launcherVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if matches == nil {
		return "", fmt.Errorf("invalid version %q", value)
	}
	parts := make([]int, 3)
	for index := range parts {
		if matches[index+1] == "" {
			continue
		}
		part, err := strconv.Atoi(matches[index+1])
		if err != nil {
			return "", fmt.Errorf("invalid version %q: %w", value, err)
		}
		parts[index] = part
	}
	return fmt.Sprintf("%d.%d.%d", parts[0], parts[1], parts[2]), nil
}

func compareLauncherVersions(current, latest string) (int, error) {
	currentNormalized, err := normalizeLauncherVersion(current)
	if err != nil {
		return 0, err
	}
	latestNormalized, err := normalizeLauncherVersion(latest)
	if err != nil {
		return 0, err
	}
	currentParts := strings.Split(currentNormalized, ".")
	latestParts := strings.Split(latestNormalized, ".")
	for index := 0; index < 3; index++ {
		currentPart, _ := strconv.Atoi(currentParts[index])
		latestPart, _ := strconv.Atoi(latestParts[index])
		if currentPart < latestPart {
			return -1, nil
		}
		if currentPart > latestPart {
			return 1, nil
		}
	}
	return 0, nil
}

func selectLauncherReleaseAsset(release githubRelease) (launcherReleaseAsset, error) {
	if release.Draft || release.Prerelease {
		return launcherReleaseAsset{}, errors.New("latest GitHub release is not a stable release")
	}
	if _, err := normalizeLauncherVersion(release.TagName); err != nil {
		return launcherReleaseAsset{}, err
	}
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.HasPrefix(name, "palserver-launcher-") && strings.HasSuffix(name, "-windows-amd64.exe") {
			return launcherReleaseAsset{Name: asset.Name, URL: asset.BrowserDownloadURL, Digest: asset.Digest, Size: asset.Size}, nil
		}
	}
	return launcherReleaseAsset{}, errors.New("Windows amd64 launcher executable was not found in the release")
}

func buildLauncherUpdateInfo(release githubRelease, currentVersion string) (LauncherUpdateInfo, launcherReleaseAsset, error) {
	asset, err := selectLauncherReleaseAsset(release)
	if err != nil {
		return LauncherUpdateInfo{}, launcherReleaseAsset{}, err
	}
	current, err := normalizeLauncherVersion(currentVersion)
	if err != nil {
		return LauncherUpdateInfo{}, launcherReleaseAsset{}, err
	}
	latest, err := normalizeLauncherVersion(release.TagName)
	if err != nil {
		return LauncherUpdateInfo{}, launcherReleaseAsset{}, err
	}
	comparison, err := compareLauncherVersions(current, latest)
	if err != nil {
		return LauncherUpdateInfo{}, launcherReleaseAsset{}, err
	}
	title := strings.TrimSpace(release.Name)
	if title == "" {
		title = "Palserver Launcher " + latest
	}
	return LauncherUpdateInfo{
		CurrentVersion: current, LatestVersion: latest, Title: title, Notes: strings.TrimSpace(release.Body),
		PublishedAt: release.PublishedAt, AssetName: asset.Name, AssetSize: asset.Size, UpdateAvailable: comparison < 0,
	}, asset, nil
}

func fetchLauncherRelease(client *http.Client, endpoint string) (githubRelease, error) {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRelease{}, err
	}
	request.Header.Set("User-Agent", "palserver-launcher/"+LauncherVersion)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := client.Do(request)
	if err != nil {
		return githubRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub release lookup failed: %s", response.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	return release, nil
}

type launcherProgressWriter struct {
	written int64
	total   int64
	notify  func(downloaded, total int64)
}

func (writer *launcherProgressWriter) Write(data []byte) (int, error) {
	writer.written += int64(len(data))
	if writer.notify != nil {
		writer.notify(writer.written, writer.total)
	}
	return len(data), nil
}

func downloadLauncherAsset(client *http.Client, asset launcherReleaseAsset, destination string, progress func(downloaded, total int64)) error {
	digest := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(asset.Digest)), "sha256:")
	if len(digest) != sha256.Size*2 {
		return errors.New("launcher release has no valid SHA-256 digest")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return errors.New("launcher release has no valid SHA-256 digest")
	}
	request, err := http.NewRequest(http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "palserver-launcher/"+LauncherVersion)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("launcher download failed: %s", response.Status)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary := destination + ".download"
	_ = os.Remove(temporary)
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	total := asset.Size
	if total <= 0 {
		total = response.ContentLength
	}
	progressWriter := &launcherProgressWriter{total: total, notify: progress}
	_, copyErr := io.Copy(file, io.TeeReader(response.Body, progressWriter))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if asset.Size > 0 && progressWriter.written != asset.Size {
		_ = os.Remove(temporary)
		return fmt.Errorf("launcher download size mismatch: got %d, want %d", progressWriter.written, asset.Size)
	}
	if err := verifyLauncherSHA256File(temporary, asset.Digest); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	_ = os.Remove(destination)
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if progress != nil {
		progress(progressWriter.written, total)
	}
	return nil
}

func parseLauncherUpdaterArgs(args []string) (launcherUpdaterOptions, bool, error) {
	modeIndex := -1
	for index, argument := range args {
		if argument == "--apply-launcher-update" {
			modeIndex = index
			break
		}
	}
	if modeIndex < 0 {
		return launcherUpdaterOptions{}, false, nil
	}
	flags := flag.NewFlagSet("launcher-updater", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options launcherUpdaterOptions
	flags.StringVar(&options.Target, "target", "", "launcher executable to replace")
	flags.StringVar(&options.Replacement, "replacement", "", "downloaded replacement executable")
	flags.IntVar(&options.PID, "pid", 0, "launcher process to wait for")
	flags.BoolVar(&options.Elevated, "elevated", false, "updater is running elevated")
	if err := flags.Parse(args[modeIndex+1:]); err != nil {
		return launcherUpdaterOptions{}, true, err
	}
	if strings.TrimSpace(options.Target) == "" || strings.TrimSpace(options.Replacement) == "" || options.PID <= 0 {
		return launcherUpdaterOptions{}, true, errors.New("invalid launcher updater arguments")
	}
	return options, true, nil
}

func launcherUpdatePaths(base, version string) (download, helper string, err error) {
	normalized, err := normalizeLauncherVersion(version)
	if err != nil {
		return "", "", err
	}
	root := filepath.Join(base, "updates", normalized)
	return filepath.Join(root, "palserver-launcher-update.exe"), filepath.Join(root, "palserver-launcher-updater.exe"), nil
}

func (a *App) GetLauncherVersion() string { return LauncherVersion }

func (a *App) CheckLauncherUpdate() (LauncherUpdateInfo, error) {
	release, err := fetchLauncherRelease(&http.Client{Timeout: 30 * time.Second}, launcherReleaseEndpoint)
	if err != nil {
		return LauncherUpdateInfo{}, err
	}
	info, _, err := buildLauncherUpdateInfo(release, LauncherVersion)
	return info, err
}

func (a *App) emitLauncherUpdateProgress(message string, percent int, downloaded, total int64) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "launcher:update-progress", LauncherUpdateProgress{
		Message: message, Percent: percent, Downloaded: downloaded, Total: total,
	})
}

func (a *App) ApplyLauncherUpdate() (returnErr error) {
	a.launcherUpdateMu.Lock()
	if a.launcherUpdating {
		a.launcherUpdateMu.Unlock()
		return errors.New("launcher update is already in progress")
	}
	a.launcherUpdating = true
	a.launcherUpdateMu.Unlock()
	defer func() {
		if returnErr != nil {
			a.launcherUpdateMu.Lock()
			a.launcherUpdating = false
			a.launcherUpdateMu.Unlock()
		}
	}()

	a.emitLauncherUpdateProgress("正在确认最新版本", 2, 0, 0)
	release, err := fetchLauncherRelease(&http.Client{Timeout: 30 * time.Second}, launcherReleaseEndpoint)
	if err != nil {
		return err
	}
	info, asset, err := buildLauncherUpdateInfo(release, LauncherVersion)
	if err != nil {
		return err
	}
	if !info.UpdateAvailable {
		return errors.New("current launcher is already up to date")
	}
	base, err := appDataDir()
	if err != nil {
		return err
	}
	downloadPath, helperPath, err := launcherUpdatePaths(base, info.LatestVersion)
	if err != nil {
		return err
	}
	a.emitLauncherUpdateProgress("正在下载新版本", 5, 0, asset.Size)
	downloadClient := &http.Client{Timeout: 30 * time.Minute}
	if err := downloadLauncherAsset(downloadClient, asset, downloadPath, func(downloaded, total int64) {
		percent := 50
		if total > 0 {
			percent = 5 + int(downloaded*85/total)
			if percent > 90 {
				percent = 90
			}
		}
		a.emitLauncherUpdateProgress("正在下载新版本", percent, downloaded, total)
	}); err != nil {
		return err
	}
	a.emitLauncherUpdateProgress("校验完成，正在准备替换", 94, asset.Size, asset.Size)
	currentExecutable, err := os.Executable()
	if err != nil {
		return err
	}
	currentExecutable, err = filepath.Abs(currentExecutable)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(helperPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(helperPath)
	if err := copyLauncherFile(currentExecutable, helperPath); err != nil {
		return fmt.Errorf("prepare launcher updater: %w", err)
	}
	if err := launchUpdaterProcess(helperPath, launcherUpdaterOptions{Target: currentExecutable, Replacement: downloadPath, PID: os.Getpid()}); err != nil {
		return fmt.Errorf("start launcher updater: %w", err)
	}
	a.emitLauncherUpdateProgress("下载完成，启动器即将重启", 100, asset.Size, asset.Size)
	go func() {
		time.Sleep(500 * time.Millisecond)
		wailsruntime.Quit(a.ctx)
	}()
	return nil
}

func verifyLauncherSHA256File(path, expected string) error {
	expected = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(expected)), "sha256:")
	if len(expected) != sha256.Size*2 {
		return errors.New("launcher release has no valid SHA-256 digest")
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return errors.New("launcher release has no valid SHA-256 digest")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return errors.New("launcher download checksum mismatch")
	}
	return nil
}

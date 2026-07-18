package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type saveInspectorAsset struct {
	Name, URL, Digest string
	Size              int64
}

func selectSaveInspectorAsset(release githubRelease) (saveInspectorAsset, error) {
	for _, asset := range release.Assets {
		if saveInspectorReleaseAssetMatches(asset.Name) {
			return saveInspectorAsset{Name: asset.Name, URL: asset.BrowserDownloadURL, Digest: asset.Digest, Size: asset.Size}, nil
		}
	}
	return saveInspectorAsset{}, errors.New(saveInspectorAssetMissingMessage())
}

func saveInspectorRoot() (string, error) {
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(base, "tools", "save-inspector")
	return root, os.MkdirAll(root, 0o755)
}

func (a *App) GetSaveInspectorStatus() SaveInspectorStatus {
	root, err := saveInspectorRoot()
	if err != nil {
		return SaveInspectorStatus{}
	}
	versionData, _ := os.ReadFile(filepath.Join(root, "version.txt"))
	path := filepath.Join(root, saveInspectorExecutableName())
	_, statErr := os.Stat(path)
	return SaveInspectorStatus{Installed: statErr == nil, Version: strings.TrimSpace(string(versionData)), Path: path}
}

func (a *App) InstallSaveInspector() (SaveInspectorStatus, error) {
	request, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/zaigie/palworld-server-tool/releases/latest", nil)
	request.Header.Set("User-Agent", "palserver-launcher")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return SaveInspectorStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return SaveInspectorStatus{}, fmt.Errorf("save inspector release lookup: %s", response.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return SaveInspectorStatus{}, err
	}
	asset, err := selectSaveInspectorAsset(release)
	if err != nil {
		return SaveInspectorStatus{}, err
	}
	download, err := (&http.Client{Timeout: 10 * time.Minute}).Get(asset.URL)
	if err != nil {
		return SaveInspectorStatus{}, err
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusOK {
		return SaveInspectorStatus{}, fmt.Errorf("save inspector download: %s", download.Status)
	}
	temporary, err := os.CreateTemp("", "pal-save-inspector-*.zip")
	if err != nil {
		return SaveInspectorStatus{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, download.Body); err != nil {
		temporary.Close()
		return SaveInspectorStatus{}, err
	}
	if err := temporary.Close(); err != nil {
		return SaveInspectorStatus{}, err
	}
	if err := verifySHA256File(temporaryPath, asset.Digest); err != nil {
		return SaveInspectorStatus{}, err
	}
	root, err := saveInspectorRoot()
	if err != nil {
		return SaveInspectorStatus{}, err
	}
	if err := extractSaveInspectorExecutable(temporaryPath, root); err != nil {
		return SaveInspectorStatus{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "version.txt"), []byte(release.TagName), 0o600); err != nil {
		return SaveInspectorStatus{}, err
	}
	return a.GetSaveInspectorStatus(), nil
}

func findLevelSave(instance ServerInstance) (string, error) {
	root := filepath.Join(instance.RootPath, "Pal", "Saved", "SaveGames")
	var newest string
	var newestTime time.Time
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && strings.EqualFold(info.Name(), "backup") {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.EqualFold(info.Name(), "Level.sav") && info.ModTime().After(newestTime) {
			newest, newestTime = path, info.ModTime()
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if newest == "" {
		return "", errors.New("Level.sav was not found")
	}
	return newest, nil
}

func randomInspectorToken() string {
	value := make([]byte, 24)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}

func (a *App) InspectSave(serverID string) (SaveInspectionResult, error) {
	if !a.tryBeginOperation(serverID, "save-inspector") {
		return SaveInspectionResult{}, errors.New("server is busy")
	}
	defer a.endOperation(serverID)
	instance, err := a.store.Find(serverID)
	if err != nil {
		return SaveInspectionResult{}, err
	}
	status := a.GetSaveInspectorStatus()
	if !status.Installed {
		if status, err = a.InstallSaveInspector(); err != nil {
			return SaveInspectionResult{}, err
		}
	}
	if runtimeStatus, _ := serverStatus(instance); runtimeStatus.Running {
		_, _ = sendRCON(instance, "Save")
		time.Sleep(2 * time.Second)
	}
	if _, err := a.createBackup(serverID); err != nil {
		return SaveInspectionResult{}, fmt.Errorf("pre-inspection backup: %w", err)
	}
	levelPath, err := findLevelSave(instance)
	if err != nil {
		return SaveInspectionResult{}, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return SaveInspectionResult{}, err
	}
	token := randomInspectorToken()
	result := SaveInspectionResult{ServerID: serverID, LevelPath: levelPath, ParsedAt: time.Now().UnixMilli()}
	var resultMu sync.Mutex
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.Header.Get("Authorization"), token) {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, 128<<20)
		var records []map[string]any
		if err := json.NewDecoder(request.Body).Decode(&records); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		resultMu.Lock()
		if strings.HasSuffix(request.URL.Path, "/player") {
			result.Players = records
		} else if strings.HasSuffix(request.URL.Path, "/guild") {
			result.Guilds = records
		}
		resultMu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"success":true}`))
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	requestURL := "http://" + listener.Addr().String() + "/api/"
	command := exec.CommandContext(ctx, status.Path, "-f", levelPath, "--request", requestURL, "--token", token)
	command.SysProcAttr = hiddenServerSysProcAttr()
	output, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return SaveInspectionResult{}, errors.New("save inspection timed out")
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 1000 {
			message = message[len(message)-1000:]
		}
		return SaveInspectionResult{}, fmt.Errorf("save inspector failed: %s", message)
	}
	resultMu.Lock()
	defer resultMu.Unlock()
	if result.Players == nil && result.Guilds == nil {
		return SaveInspectionResult{}, errors.New("save inspector returned no data")
	}
	return result, nil
}

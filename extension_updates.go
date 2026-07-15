package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const githubAPIBase = "https://api.github.com"

type extensionReleaseSource struct {
	ID       string
	Endpoint string
}

type extensionReleaseInfo struct {
	ExtensionID string
	Version     string
	Asset       githubReleaseAsset
	PublishedAt string
}

const extensionUpdateManifestSchema = 1

const (
	extensionArchiveMaxEntries          = 4096
	extensionArchiveMaxFileSize         = 256 << 20
	extensionArchiveMaxTotalSize        = 1 << 30
	extensionAssetMaxDownloadSize int64 = 512 << 20
)

type extensionUpdateAsset struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updatedAt"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type extensionUpdateManifest struct {
	Schema       int                  `json:"schema"`
	ExtensionID  string               `json:"extensionId"`
	Version      string               `json:"version"`
	Asset        extensionUpdateAsset `json:"asset"`
	DownloadedAt time.Time            `json:"downloadedAt"`
	Layout       string               `json:"layout"`
}

func validateExtensionID(extensionID string) error {
	switch extensionID {
	case "paldefender", "ue4ss":
		return nil
	default:
		return errors.New("unknown extension")
	}
}

func extensionLayout(extensionID string) (string, error) {
	switch extensionID {
	case "paldefender":
		return "paldefender-root-v1", nil
	case "ue4ss":
		return "ue4ss-nested-v1", nil
	default:
		return "", errors.New("unknown extension")
	}
}

func extensionLauncherPath(instance ServerInstance, extensionID string, parts ...string) (string, error) {
	if err := validateExtensionID(extensionID); err != nil {
		return "", err
	}
	base := filepath.Join(win64Path(instance), ".palserver-launcher")
	return filepath.Join(append([]string{base}, parts...)...), nil
}

func extensionStageRoot(instance ServerInstance, extensionID string) (string, error) {
	return extensionLauncherPath(instance, extensionID, "staged", extensionID)
}

func extensionPendingPath(instance ServerInstance, extensionID string) (string, error) {
	root, err := extensionStageRoot(instance, extensionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "pending"), nil
}

func requireNonEmptyRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("required payload file is empty or not regular: %s", filepath.Base(path))
	}
	return nil
}

func validateRegularPayloadTree(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("extension payload is not a directory")
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || info.IsDir() || info.Mode().IsRegular() {
			return nil
		}
		return fmt.Errorf("extension payload contains a non-regular entry: %s", path)
	})
}

func validateStagedExtension(extensionID, payload string) error {
	if err := validateExtensionID(extensionID); err != nil {
		return err
	}
	if err := validateRegularPayloadTree(payload); err != nil {
		return err
	}
	switch extensionID {
	case "paldefender":
		entries, err := os.ReadDir(payload)
		if err != nil {
			return err
		}
		if len(entries) != 2 {
			return errors.New("PalDefender payload must contain exactly PalDefender.dll and d3d9.dll")
		}
		seen := map[string]bool{}
		for _, entry := range entries {
			if entry.IsDir() {
				return errors.New("PalDefender payload contains an unexpected directory")
			}
			seen[entry.Name()] = true
		}
		if !seen["PalDefender.dll"] || !seen["d3d9.dll"] {
			return errors.New("PalDefender payload must contain exactly PalDefender.dll and d3d9.dll")
		}
		for _, name := range []string{"PalDefender.dll", "d3d9.dll"} {
			if err := requireNonEmptyRegularFile(filepath.Join(payload, name)); err != nil {
				return err
			}
		}
	case "ue4ss":
		entries, err := os.ReadDir(payload)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			switch strings.ToLower(entry.Name()) {
			case "ue4ss.dll", "ue4ss.disabled.dll", "ue4ss-settings.ini", "mods":
				return fmt.Errorf("UE4SS payload contains legacy root layout entry %s", entry.Name())
			}
		}
		for _, relative := range []string{"dwmapi.dll", filepath.Join("ue4ss", "UE4SS.dll"), filepath.Join("ue4ss", "UE4SS-settings.ini")} {
			if err := requireNonEmptyRegularFile(filepath.Join(payload, relative)); err != nil {
				return fmt.Errorf("invalid UE4SS payload: %w", err)
			}
		}
	}
	return nil
}

func writeJSONExclusive(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func stageExtensionPayload(instance ServerInstance, info extensionReleaseInfo, extractedDir string) (manifest extensionUpdateManifest, err error) {
	if err := validateExtensionID(info.ExtensionID); err != nil {
		return extensionUpdateManifest{}, err
	}
	if strings.TrimSpace(info.Version) == "" {
		return extensionUpdateManifest{}, errors.New("release version is empty")
	}
	if err := validateStagedExtension(info.ExtensionID, extractedDir); err != nil {
		return extensionUpdateManifest{}, err
	}
	layout, err := extensionLayout(info.ExtensionID)
	if err != nil {
		return extensionUpdateManifest{}, err
	}
	root, err := extensionStageRoot(instance, info.ExtensionID)
	if err != nil {
		return extensionUpdateManifest{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return extensionUpdateManifest{}, err
	}
	incoming := filepath.Join(root, "incoming")
	previous := filepath.Join(root, "previous")
	pending := filepath.Join(root, "pending")
	if err := os.RemoveAll(incoming); err != nil {
		return extensionUpdateManifest{}, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(incoming)
		}
	}()
	if err := os.Mkdir(incoming, 0o755); err != nil {
		return extensionUpdateManifest{}, err
	}
	if err := copyTree(extractedDir, filepath.Join(incoming, "payload")); err != nil {
		return extensionUpdateManifest{}, err
	}
	manifest = extensionUpdateManifest{
		Schema:      extensionUpdateManifestSchema,
		ExtensionID: info.ExtensionID,
		Version:     strings.TrimSpace(info.Version),
		Asset: extensionUpdateAsset{
			ID: info.Asset.ID, Name: info.Asset.Name, UpdatedAt: info.Asset.UpdatedAt,
			Digest: info.Asset.Digest, Size: info.Asset.Size,
		},
		DownloadedAt: time.Now().UTC(),
		Layout:       layout,
	}
	if err := writeJSONExclusive(filepath.Join(incoming, "manifest.json"), manifest); err != nil {
		return extensionUpdateManifest{}, err
	}
	if err := validateStagedExtension(info.ExtensionID, filepath.Join(incoming, "payload")); err != nil {
		return extensionUpdateManifest{}, err
	}
	if err := os.RemoveAll(previous); err != nil {
		return extensionUpdateManifest{}, err
	}
	hadPending := false
	if _, statErr := os.Stat(pending); statErr == nil {
		hadPending = true
		if err := os.Rename(pending, previous); err != nil {
			return extensionUpdateManifest{}, err
		}
	} else if !os.IsNotExist(statErr) {
		return extensionUpdateManifest{}, statErr
	}
	if err := os.Rename(incoming, pending); err != nil {
		if hadPending {
			if restoreErr := os.Rename(previous, pending); restoreErr != nil {
				return extensionUpdateManifest{}, errors.Join(err, fmt.Errorf("restore previous pending update: %w", restoreErr))
			}
		}
		return extensionUpdateManifest{}, err
	}
	_ = os.RemoveAll(previous)
	return manifest, nil
}

func downloadExtensionAsset(client *http.Client, info extensionReleaseInfo, destination string) (err error) {
	return downloadExtensionAssetWithLimit(client, info, destination, extensionAssetMaxDownloadSize)
}

func downloadExtensionAssetWithLimit(client *http.Client, info extensionReleaseInfo, destination string, limit int64) (err error) {
	if err := validateExtensionID(info.ExtensionID); err != nil {
		return err
	}
	if limit <= 0 {
		return errors.New("extension asset download limit must be positive")
	}
	if info.Asset.Size < 0 {
		return errors.New("release asset size is invalid")
	}
	if info.Asset.Size > limit {
		return fmt.Errorf("release asset is too large: %d bytes exceeds %d byte limit", info.Asset.Size, limit)
	}
	if strings.TrimSpace(info.Asset.BrowserDownloadURL) == "" {
		return errors.New("release asset download URL is empty")
	}
	if client == nil {
		client = releaseDownloadClient()
	}
	request, err := http.NewRequest(http.MethodGet, info.Asset.BrowserDownloadURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "palserver-launcher/"+LauncherVersion)
	request.Header.Set("Accept", "application/octet-stream")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", response.Status)
	}
	readLimit := limit
	if info.Asset.Size > 0 && info.Asset.Size < readLimit {
		readLimit = info.Asset.Size
	}
	if response.ContentLength > readLimit {
		return fmt.Errorf("download response is too large: %d bytes exceeds %d byte limit", response.ContentLength, readLimit)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(destination)
		}
	}()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, readLimit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > readLimit {
		if info.Asset.Size > 0 && readLimit == info.Asset.Size {
			return fmt.Errorf("download exceeds declared size: got more than %d bytes", info.Asset.Size)
		}
		return fmt.Errorf("download exceeds %d byte limit", limit)
	}
	if info.Asset.Size > 0 && written != info.Asset.Size {
		return fmt.Errorf("download size mismatch: got %d, want %d", written, info.Asset.Size)
	}
	digest := strings.TrimSpace(info.Asset.Digest)
	if digest != "" {
		algorithm, expected, ok := strings.Cut(digest, ":")
		if !ok || !strings.EqualFold(algorithm, "sha256") {
			return fmt.Errorf("unsupported release asset digest %q", digest)
		}
		expectedBytes, decodeErr := hex.DecodeString(expected)
		if decodeErr != nil || len(expectedBytes) != sha256.Size {
			return fmt.Errorf("invalid release asset digest %q", digest)
		}
		if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), hex.EncodeToString(expectedBytes)) {
			return errors.New("download SHA-256 mismatch")
		}
	}
	remove = false
	return nil
}

type extensionArchiveEntry struct {
	file      *zip.File
	clean     string
	directory bool
}

type extensionArchiveSeenEntry struct {
	directory bool
	explicit  bool
}

func windowsReservedArchiveName(component string) bool {
	base := component
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$", "CONIN$", "CONOUT$", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

func validateExtensionArchiveName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\\') || strings.ContainsRune(name, ':') {
		return "", fmt.Errorf("unsafe zip entry %q", name)
	}
	if strings.HasPrefix(name, "/") || path.IsAbs(name) {
		return "", fmt.Errorf("unsafe zip entry %q", name)
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" {
		return "", fmt.Errorf("unsafe zip entry %q", name)
	}
	components := strings.Split(trimmed, "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || windowsReservedArchiveName(component) {
			return "", fmt.Errorf("unsafe zip entry %q", name)
		}
	}
	clean := path.Clean(trimmed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe zip entry %q", name)
	}
	return clean, nil
}

func preflightExtensionArchive(reader *zip.ReadCloser) ([]extensionArchiveEntry, error) {
	if len(reader.File) > extensionArchiveMaxEntries {
		return nil, fmt.Errorf("extension archive has too many entries: %d", len(reader.File))
	}
	entries := make([]extensionArchiveEntry, 0, len(reader.File))
	seen := map[string]extensionArchiveSeenEntry{}
	var total uint64
	for _, file := range reader.File {
		clean, err := validateExtensionArchiveName(file.Name)
		if err != nil {
			return nil, err
		}
		mode := file.Mode()
		directory := strings.HasSuffix(file.Name, "/") || mode.IsDir()
		if mode&os.ModeSymlink != 0 || (!directory && !mode.IsRegular()) || (directory && mode.Type() != 0 && !mode.IsDir()) {
			return nil, fmt.Errorf("extension archive contains non-regular entry %q", file.Name)
		}
		if directory && file.UncompressedSize64 != 0 {
			return nil, fmt.Errorf("extension archive directory has data %q", file.Name)
		}
		if file.UncompressedSize64 > extensionArchiveMaxFileSize {
			return nil, fmt.Errorf("extension archive entry is too large: %q", file.Name)
		}
		if total > extensionArchiveMaxTotalSize-file.UncompressedSize64 {
			return nil, errors.New("extension archive expands beyond the total size limit")
		}
		total += file.UncompressedSize64

		components := strings.Split(clean, "/")
		for index := 1; index < len(components); index++ {
			ancestor := strings.ToLower(strings.Join(components[:index], "/"))
			if existing, ok := seen[ancestor]; ok && !existing.directory {
				return nil, fmt.Errorf("extension archive path collides with a file: %q", file.Name)
			}
			if _, ok := seen[ancestor]; !ok {
				seen[ancestor] = extensionArchiveSeenEntry{directory: true}
			}
		}
		canonical := strings.ToLower(clean)
		if existing, ok := seen[canonical]; ok {
			if !directory || !existing.directory || existing.explicit {
				return nil, fmt.Errorf("extension archive contains a duplicate path %q", file.Name)
			}
			seen[canonical] = extensionArchiveSeenEntry{directory: true, explicit: true}
		} else {
			seen[canonical] = extensionArchiveSeenEntry{directory: directory, explicit: true}
		}
		entries = append(entries, extensionArchiveEntry{file: file, clean: clean, directory: directory})
	}
	return entries, nil
}

func extractExtensionArchive(archive, destination string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	entries, err := preflightExtensionArchive(reader)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		target := filepath.Join(destination, filepath.FromSlash(entry.clean))
		if entry.directory {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := entry.file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = in.Close()
			return err
		}
		expected := int64(entry.file.UncompressedSize64)
		written, copyErr := io.Copy(out, io.LimitReader(in, expected+1))
		inCloseErr := in.Close()
		outCloseErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inCloseErr != nil {
			return inCloseErr
		}
		if outCloseErr != nil {
			return outCloseErr
		}
		if written != expected {
			return fmt.Errorf("extension archive entry size mismatch for %q", entry.file.Name)
		}
	}
	return nil
}

type extensionApplyOps struct {
	AfterMutation func(step string) error
}

func (ops extensionApplyOps) afterMutation(step string) error {
	if ops.AfterMutation == nil {
		return nil
	}
	return ops.AfterMutation(step)
}

type extensionBackupEntry struct {
	Path       string `json:"path"`
	Existed    bool   `json:"existed"`
	Directory  bool   `json:"directory"`
	BackupPath string `json:"backupPath,omitempty"`
}

type extensionBackupManifest struct {
	Schema      int                    `json:"schema"`
	ExtensionID string                 `json:"extensionId"`
	Transaction string                 `json:"transaction"`
	CreatedAt   time.Time              `json:"createdAt"`
	Entries     []extensionBackupEntry `json:"entries"`
}

func extensionInstalledManifestPath(instance ServerInstance, extensionID string) (string, error) {
	return extensionLauncherPath(instance, extensionID, "manifests", extensionID+".json")
}

func extensionManagedPaths(extensionID string) ([]string, error) {
	if err := validateExtensionID(extensionID); err != nil {
		return nil, err
	}
	if extensionID == "paldefender" {
		return []string{
			"PalDefender.dll",
			"PalDefender.disabled.dll",
			"d3d9.dll",
			"PalDefender",
			"d3d9_config.json",
			"palguard.version.txt",
			filepath.Join(".palserver-launcher", "manifests", "paldefender.json"),
		}, nil
	}
	return []string{
		"dwmapi.dll",
		"dwmapi.disabled.dll",
		"ue4ss",
		"UE4SS.dll",
		"UE4SS.disabled.dll",
		"UE4SS-settings.ini",
		"Mods",
		"ue4ss.version.txt",
		filepath.Join(".palserver-launcher", "manifests", "ue4ss.json"),
	}, nil
}

func copyRegularFileExclusive(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func createExtensionBackup(instance ServerInstance, extensionID string) (backupPath string, manifest extensionBackupManifest, err error) {
	managed, err := extensionManagedPaths(extensionID)
	if err != nil {
		return "", extensionBackupManifest{}, err
	}
	root, err := extensionLauncherPath(instance, extensionID, "backups", extensionID)
	if err != nil {
		return "", extensionBackupManifest{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", extensionBackupManifest{}, err
	}
	transaction := time.Now().UTC().Format("20060102T150405.000000000Z")
	for suffix := 0; ; suffix++ {
		name := transaction
		if suffix > 0 {
			name = fmt.Sprintf("%s-%d", transaction, suffix)
		}
		backupPath = filepath.Join(root, name)
		if err := os.Mkdir(backupPath, 0o755); err == nil {
			transaction = name
			break
		} else if !os.IsExist(err) {
			return "", extensionBackupManifest{}, err
		}
	}
	remove := true
	defer func() {
		if remove {
			_ = os.RemoveAll(backupPath)
		}
	}()
	manifest = extensionBackupManifest{
		Schema: 1, ExtensionID: extensionID, Transaction: transaction, CreatedAt: time.Now().UTC(),
		Entries: make([]extensionBackupEntry, 0, len(managed)),
	}
	base := win64Path(instance)
	for index, relative := range managed {
		entry := extensionBackupEntry{Path: relative}
		live := filepath.Join(base, relative)
		info, statErr := os.Lstat(live)
		if os.IsNotExist(statErr) {
			manifest.Entries = append(manifest.Entries, entry)
			continue
		}
		if statErr != nil {
			return "", extensionBackupManifest{}, statErr
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return "", extensionBackupManifest{}, fmt.Errorf("managed extension path is not regular: %s", live)
		}
		entry.Existed = true
		entry.Directory = info.IsDir()
		entry.BackupPath = filepath.Join("files", fmt.Sprintf("%03d", index))
		backupTarget := filepath.Join(backupPath, entry.BackupPath)
		if entry.Directory {
			if err := copyTree(live, backupTarget); err != nil {
				return "", extensionBackupManifest{}, err
			}
		} else if err := copyRegularFileExclusive(live, backupTarget); err != nil {
			return "", extensionBackupManifest{}, err
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	if err := writeJSONExclusive(filepath.Join(backupPath, "manifest.json"), manifest); err != nil {
		return "", extensionBackupManifest{}, err
	}
	remove = false
	return backupPath, manifest, nil
}

func readExtensionBackupManifest(backupPath string) (extensionBackupManifest, error) {
	data, err := os.ReadFile(filepath.Join(backupPath, "manifest.json"))
	if err != nil {
		return extensionBackupManifest{}, err
	}
	var manifest extensionBackupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return extensionBackupManifest{}, err
	}
	if manifest.Schema != 1 || manifest.Transaction == "" {
		return extensionBackupManifest{}, errors.New("invalid extension backup manifest")
	}
	managed, err := extensionManagedPaths(manifest.ExtensionID)
	if err != nil {
		return extensionBackupManifest{}, err
	}
	if len(manifest.Entries) != len(managed) {
		return extensionBackupManifest{}, errors.New("extension backup manifest does not cover all managed paths")
	}
	for index := range managed {
		if filepath.Clean(manifest.Entries[index].Path) != filepath.Clean(managed[index]) {
			return extensionBackupManifest{}, errors.New("extension backup manifest contains an unexpected managed path")
		}
		if manifest.Entries[index].Existed && filepath.Clean(manifest.Entries[index].BackupPath) != filepath.Join("files", fmt.Sprintf("%03d", index)) {
			return extensionBackupManifest{}, errors.New("extension backup manifest contains an invalid backup path")
		}
	}
	return manifest, nil
}

func restoreExtensionBackup(instance ServerInstance, extensionID, backupPath string) error {
	manifest, err := readExtensionBackupManifest(backupPath)
	if err != nil {
		return err
	}
	if manifest.ExtensionID != extensionID {
		return errors.New("extension backup belongs to another extension")
	}
	base := win64Path(instance)
	var restoreErr error
	for _, entry := range manifest.Entries {
		if err := os.RemoveAll(filepath.Join(base, entry.Path)); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("remove managed path %s: %w", entry.Path, err))
		}
	}
	for _, entry := range manifest.Entries {
		if !entry.Existed {
			continue
		}
		source := filepath.Join(backupPath, entry.BackupPath)
		destination := filepath.Join(base, entry.Path)
		if entry.Directory {
			if err := copyTree(source, destination); err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("restore directory %s: %w", entry.Path, err))
			}
		} else if err := copyRegularFileExclusive(source, destination); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore file %s: %w", entry.Path, err))
		}
	}
	return restoreErr
}

func readExtensionUpdateManifest(path string) (extensionUpdateManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return extensionUpdateManifest{}, err
	}
	var manifest extensionUpdateManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return extensionUpdateManifest{}, err
	}
	layout, err := extensionLayout(manifest.ExtensionID)
	if err != nil {
		return extensionUpdateManifest{}, err
	}
	if manifest.Schema != extensionUpdateManifestSchema || strings.TrimSpace(manifest.Version) == "" || manifest.Layout != layout {
		return extensionUpdateManifest{}, errors.New("invalid extension update manifest")
	}
	return manifest, nil
}

func replaceFileData(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp := path + ".incoming"
	if err := os.Remove(temp); err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(temp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func replaceFileFromSource(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return replaceFileData(destination, data, 0o600)
}

func migratePalDefenderConfig(input []byte) ([]byte, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(input, &values); err != nil {
		return nil, err
	}
	delete(values, "blockTowerBossCapture")
	updated, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(updated, '\n'), nil
}

func applyPalDefenderPayload(instance ServerInstance, payload string, ops extensionApplyOps) error {
	base := win64Path(instance)
	_, enabledErr := os.Stat(filepath.Join(base, "PalDefender.dll"))
	_, disabledErr := os.Stat(filepath.Join(base, "PalDefender.disabled.dll"))
	enabled := !(os.IsNotExist(enabledErr) && disabledErr == nil)
	if err := replaceFileFromSource(filepath.Join(payload, "d3d9.dll"), filepath.Join(base, "d3d9.dll")); err != nil {
		return err
	}
	if err := ops.afterMutation("paldefender-loader"); err != nil {
		return err
	}
	pluginTarget := filepath.Join(base, "PalDefender.dll")
	if !enabled {
		pluginTarget = filepath.Join(base, "PalDefender.disabled.dll")
	}
	for _, name := range []string{"PalDefender.dll", "PalDefender.disabled.dll"} {
		if err := os.Remove(filepath.Join(base, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := replaceFileFromSource(filepath.Join(payload, "PalDefender.dll"), pluginTarget); err != nil {
		return err
	}
	if err := ops.afterMutation("paldefender-plugin"); err != nil {
		return err
	}
	dataRoot := filepath.Join(base, "PalDefender")
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return err
	}
	configPath := filepath.Join(dataRoot, "Config.json")
	if input, err := os.ReadFile(configPath); err == nil {
		updated, migrateErr := migratePalDefenderConfig(input)
		if migrateErr != nil {
			return migrateErr
		}
		if err := replaceFileData(configPath, updated, 0o600); err != nil {
			return err
		}
		if err := ops.afterMutation("paldefender-config"); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func iniValues(data []byte) map[string]string {
	values := map[string]string{}
	section := ""
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			values[section+"\x00"+strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	return values
}

func mergeUE4SSSettings(baseline, existing []byte) []byte {
	oldValues := iniValues(existing)
	forced := map[string]string{
		"buseuobjectarraycache": "false",
		"consoleenabled":        "0",
		"guiconsoleenabled":     "0",
		"guiconsolevisible":     "0",
	}
	seenForced := map[string]bool{}
	section := ""
	lines := strings.Split(strings.ReplaceAll(string(baseline), "\r\n", "\n"), "\n")
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		lowerKey := strings.ToLower(key)
		if migrated, exists := oldValues[section+"\x00"+lowerKey]; exists {
			value = migrated
		}
		if safe, exists := forced[lowerKey]; exists {
			value = safe
			seenForced[lowerKey] = true
		}
		lines[index] = key + " = " + value
	}
	if !seenForced["buseuobjectarraycache"] {
		lines = append(lines, "[General]", "bUseUObjectArrayCache = false")
	}
	debugAdded := false
	for _, key := range []string{"consoleenabled", "guiconsoleenabled", "guiconsolevisible"} {
		if seenForced[key] {
			continue
		}
		if !debugAdded {
			lines = append(lines, "[Debug]")
			debugAdded = true
		}
		name := map[string]string{
			"consoleenabled": "ConsoleEnabled", "guiconsoleenabled": "GuiConsoleEnabled", "guiconsolevisible": "GuiConsoleVisible",
		}[key]
		lines = append(lines, name+" = 0")
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n")
}

type ue4ssCustomMod struct {
	name    string
	source  string
	enabled string
}

func ue4ssModIsBuiltIn(name string) bool {
	name = strings.TrimSuffix(name, ".disabled")
	for builtIn := range ue4ssBuiltInMods {
		if strings.EqualFold(name, builtIn) {
			return true
		}
	}
	return false
}

func parseUE4SSModsState(data []byte) map[string]string {
	states := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name != "" && (value == "0" || value == "1") {
			states[strings.ToLower(name)] = value
		}
	}
	return states
}

func collectUE4SSCustomMods(root string, mods map[string]ue4ssCustomMod) error {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	stateData, _ := os.ReadFile(filepath.Join(root, "mods.txt"))
	states := parseUE4SSModsState(stateData)
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), "mods.txt") {
			continue
		}
		logicalName := strings.TrimSuffix(entry.Name(), ".disabled")
		if ue4ssModIsBuiltIn(logicalName) {
			continue
		}
		enabled := states[strings.ToLower(logicalName)]
		if enabled == "" {
			enabled = "1"
		}
		mods[strings.ToLower(logicalName)] = ue4ssCustomMod{name: entry.Name(), source: filepath.Join(root, entry.Name()), enabled: enabled}
	}
	return nil
}

func copyUE4SSCustomMod(mod ue4ssCustomMod, destination string) error {
	info, err := os.Lstat(mod.source)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(destination)
	if err == nil {
		logical := strings.ToLower(strings.TrimSuffix(mod.name, ".disabled"))
		for _, entry := range entries {
			if strings.ToLower(strings.TrimSuffix(entry.Name(), ".disabled")) == logical {
				if err := os.RemoveAll(filepath.Join(destination, entry.Name())); err != nil {
					return err
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	target := filepath.Join(destination, mod.name)
	if info.IsDir() {
		return copyTree(mod.source, target)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("custom UE4SS mod is not regular: %s", mod.source)
	}
	return copyRegularFileExclusive(mod.source, target)
}

func mergeUE4SSModsState(baseline []byte, mods []ue4ssCustomMod) []byte {
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(baseline), "\r\n", "\n"), "\n"), "\n")
	lineByName := map[string]int{}
	for index, line := range lines {
		if name, _, ok := strings.Cut(line, ":"); ok {
			lineByName[strings.ToLower(strings.TrimSpace(name))] = index
		}
	}
	for _, mod := range mods {
		logical := strings.TrimSuffix(mod.name, ".disabled")
		line := logical + " : " + mod.enabled
		if index, ok := lineByName[strings.ToLower(logical)]; ok {
			lines[index] = line
		} else {
			lineByName[strings.ToLower(logical)] = len(lines)
			lines = append(lines, line)
		}
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n")
}

func regularNonEmptyFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func ue4ssInstallationState(base string) (actualPath string, installed, enabled bool) {
	nestedCore := regularNonEmptyFileExists(filepath.Join(base, "ue4ss", "UE4SS.dll"))
	nestedSettings := regularNonEmptyFileExists(filepath.Join(base, "ue4ss", "UE4SS-settings.ini"))
	proxyEnabled := regularNonEmptyFileExists(filepath.Join(base, "dwmapi.dll"))
	proxyDisabled := regularNonEmptyFileExists(filepath.Join(base, "dwmapi.disabled.dll"))
	if nestedCore && nestedSettings && (proxyEnabled || proxyDisabled) {
		if proxyEnabled {
			return filepath.Join(base, "dwmapi.dll"), true, true
		}
		return filepath.Join(base, "dwmapi.disabled.dll"), true, false
	}
	legacyEnabled := regularNonEmptyFileExists(filepath.Join(base, "UE4SS.dll"))
	legacyDisabled := regularNonEmptyFileExists(filepath.Join(base, "UE4SS.disabled.dll"))
	if (legacyEnabled || legacyDisabled) && (proxyEnabled || proxyDisabled) {
		if legacyDisabled || proxyDisabled {
			return filepath.Join(base, "UE4SS.disabled.dll"), true, false
		}
		return filepath.Join(base, "UE4SS.dll"), true, true
	}
	return filepath.Join(base, "ue4ss", "UE4SS.dll"), false, false
}

func applyUE4SSPayload(instance ServerInstance, payload, workspace string, ops extensionApplyOps) error {
	base := win64Path(instance)
	_, installed, enabled := ue4ssInstallationState(base)
	if !installed {
		enabled = true
	}
	prepared := filepath.Join(workspace, "ue4ss")
	if err := copyTree(filepath.Join(payload, "ue4ss"), prepared); err != nil {
		return err
	}
	settingsPath := filepath.Join(prepared, "UE4SS-settings.ini")
	baselineSettings, err := os.ReadFile(settingsPath)
	if err != nil {
		return err
	}
	existingSettings := []byte(nil)
	for _, candidate := range []string{filepath.Join(base, "ue4ss", "UE4SS-settings.ini"), filepath.Join(base, "UE4SS-settings.ini")} {
		if data, readErr := os.ReadFile(candidate); readErr == nil {
			existingSettings = data
			break
		} else if !os.IsNotExist(readErr) {
			return readErr
		}
	}
	if err := replaceFileData(settingsPath, mergeUE4SSSettings(baselineSettings, existingSettings), 0o600); err != nil {
		return err
	}
	preparedMods := filepath.Join(prepared, "Mods")
	if err := os.MkdirAll(preparedMods, 0o755); err != nil {
		return err
	}
	packageMods := map[string]bool{}
	if entries, readErr := os.ReadDir(preparedMods); readErr == nil {
		for _, entry := range entries {
			if !strings.EqualFold(entry.Name(), "mods.txt") {
				packageMods[strings.ToLower(strings.TrimSuffix(entry.Name(), ".disabled"))] = true
			}
		}
	} else {
		return readErr
	}
	custom := map[string]ue4ssCustomMod{}
	if err := collectUE4SSCustomMods(filepath.Join(base, "Mods"), custom); err != nil {
		return err
	}
	if err := collectUE4SSCustomMods(filepath.Join(base, "ue4ss", "Mods"), custom); err != nil {
		return err
	}
	keys := make([]string, 0, len(custom))
	for key := range custom {
		if packageMods[key] {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	mods := make([]ue4ssCustomMod, 0, len(keys))
	for _, key := range keys {
		mod := custom[key]
		if err := copyUE4SSCustomMod(mod, preparedMods); err != nil {
			return err
		}
		mods = append(mods, mod)
	}
	baselineMods, readErr := os.ReadFile(filepath.Join(preparedMods, "mods.txt"))
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if err := replaceFileData(filepath.Join(preparedMods, "mods.txt"), mergeUE4SSModsState(baselineMods, mods), 0o600); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(base, "ue4ss")); err != nil {
		return err
	}
	if err := os.Rename(prepared, filepath.Join(base, "ue4ss")); err != nil {
		return err
	}
	if err := ops.afterMutation("ue4ss-directory"); err != nil {
		return err
	}
	for _, name := range []string{"dwmapi.dll", "dwmapi.disabled.dll"} {
		if err := os.Remove(filepath.Join(base, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	proxyTarget := filepath.Join(base, "dwmapi.dll")
	if !enabled {
		proxyTarget = filepath.Join(base, "dwmapi.disabled.dll")
	}
	if err := replaceFileFromSource(filepath.Join(payload, "dwmapi.dll"), proxyTarget); err != nil {
		return err
	}
	if err := ops.afterMutation("ue4ss-proxy"); err != nil {
		return err
	}
	for _, legacy := range []string{"UE4SS.dll", "UE4SS.disabled.dll", "UE4SS-settings.ini", "Mods"} {
		if err := os.RemoveAll(filepath.Join(base, legacy)); err != nil {
			return err
		}
	}
	return ops.afterMutation("ue4ss-legacy-cleanup")
}

func extensionVersionMarker(extensionID string) (string, error) {
	switch extensionID {
	case "paldefender":
		return "palguard.version.txt", nil
	case "ue4ss":
		return "ue4ss.version.txt", nil
	default:
		return "", errors.New("unknown extension")
	}
}

func applyPendingExtensionUpdate(instance ServerInstance, extensionID string) error {
	return applyPendingExtensionUpdateWithOps(instance, extensionID, extensionApplyOps{})
}

func applyPendingExtensionUpdateWithOps(instance ServerInstance, extensionID string, ops extensionApplyOps) error {
	pending, err := extensionPendingPath(instance, extensionID)
	if err != nil {
		return err
	}
	manifest, err := readExtensionUpdateManifest(filepath.Join(pending, "manifest.json"))
	if err != nil {
		return err
	}
	if manifest.ExtensionID != extensionID {
		return errors.New("pending extension manifest ID mismatch")
	}
	payload := filepath.Join(pending, "payload")
	if err := validateStagedExtension(extensionID, payload); err != nil {
		return err
	}
	backupPath, backupManifest, err := createExtensionBackup(instance, extensionID)
	if err != nil {
		return err
	}
	workspaceRoot, err := extensionLauncherPath(instance, extensionID, "apply", extensionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		return err
	}
	workspace := filepath.Join(workspaceRoot, backupManifest.Transaction)
	if err := os.RemoveAll(workspace); err != nil {
		return err
	}
	if err := os.Mkdir(workspace, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	applyErr := error(nil)
	switch extensionID {
	case "paldefender":
		applyErr = applyPalDefenderPayload(instance, payload, ops)
	case "ue4ss":
		applyErr = applyUE4SSPayload(instance, payload, workspace, ops)
	default:
		applyErr = errors.New("unknown extension")
	}
	if applyErr == nil {
		applyErr = validateExtensionInstallation(win64Path(instance), extensionID)
	}
	if applyErr == nil {
		marker, markerErr := extensionVersionMarker(extensionID)
		if markerErr != nil {
			applyErr = markerErr
		} else {
			applyErr = replaceFileData(filepath.Join(win64Path(instance), marker), []byte(manifest.Version+"\n"), 0o600)
			if applyErr == nil {
				applyErr = ops.afterMutation("version-marker")
			}
		}
	}
	if applyErr == nil {
		installedManifest, pathErr := extensionInstalledManifestPath(instance, extensionID)
		if pathErr != nil {
			applyErr = pathErr
		} else {
			data, marshalErr := json.MarshalIndent(manifest, "", "  ")
			if marshalErr != nil {
				applyErr = marshalErr
			} else {
				applyErr = replaceFileData(installedManifest, append(data, '\n'), 0o600)
				if applyErr == nil {
					applyErr = ops.afterMutation("install-manifest")
				}
			}
		}
	}
	if applyErr == nil {
		applyErr = os.RemoveAll(pending)
	}
	if applyErr != nil {
		rollbackErr := restoreExtensionBackup(instance, extensionID, backupPath)
		if rollbackErr != nil {
			return errors.Join(applyErr, fmt.Errorf("rollback extension update: %w", rollbackErr))
		}
		return applyErr
	}
	pruneExtensionBackups(instance, extensionID, 3)
	return nil
}

func pruneExtensionBackups(instance ServerInstance, extensionID string, keep int) {
	root, err := extensionLauncherPath(instance, extensionID, "backups", extensionID)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	directories := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(directories)))
	if len(directories) <= keep {
		return
	}
	for _, name := range directories[keep:] {
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}

func applyPendingExtensionUpdates(instance ServerInstance) error {
	for _, extensionID := range []string{"paldefender", "ue4ss"} {
		pending, err := extensionPendingPath(instance, extensionID)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(pending, "manifest.json")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := applyPendingExtensionUpdate(instance, extensionID); err != nil {
			return fmt.Errorf("apply %s update: %w", extensionID, err)
		}
	}
	return nil
}

func downloadAndExtractExtension(instance ServerInstance, info extensionReleaseInfo, client *http.Client) (string, func(), error) {
	root, err := extensionStageRoot(instance, info.ExtensionID)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", nil, err
	}
	work, err := os.MkdirTemp(root, "download-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(work) }
	archive := filepath.Join(work, "asset.zip")
	if err := downloadExtensionAsset(client, info, archive); err != nil {
		cleanup()
		return "", nil, err
	}
	extracted := filepath.Join(work, "extracted")
	if err := extractExtensionArchive(archive, extracted); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("extract extension asset: %w", err)
	}
	return extracted, cleanup, nil
}

func downloadAndStageExtension(instance ServerInstance, info extensionReleaseInfo, client *http.Client) (extensionUpdateManifest, error) {
	extracted, cleanup, err := downloadAndExtractExtension(instance, info, client)
	if err != nil {
		return extensionUpdateManifest{}, err
	}
	defer cleanup()
	return stageExtensionPayload(instance, info, extracted)
}

func installLatestExtensionForInstance(instance ServerInstance, extensionID string, client *http.Client, sourceFor func(string) (extensionReleaseSource, error)) (ExtensionUpdateResult, error) {
	if err := validateExtensionID(extensionID); err != nil {
		return ExtensionUpdateResult{}, err
	}
	if client == nil {
		client = releaseDownloadClient()
	}
	if sourceFor == nil {
		sourceFor = extensionReleaseSourceFor
	}
	source, err := sourceFor(extensionID)
	if err != nil {
		return ExtensionUpdateResult{}, err
	}
	if source.ID == "" {
		source.ID = extensionID
	}
	if source.ID != extensionID {
		return ExtensionUpdateResult{}, errors.New("extension release source ID mismatch")
	}
	latest, err := fetchExtensionRelease(client, source)
	if err != nil {
		return ExtensionUpdateResult{}, err
	}
	manifest, err := downloadAndStageExtension(instance, latest, client)
	if err != nil {
		return ExtensionUpdateResult{}, err
	}
	result := ExtensionUpdateResult{
		ExtensionID: extensionID,
		Version:     manifest.Version,
		Pending:     true,
		Message:     "已下载，等待应用",
	}
	if err := applyPendingExtensionUpdate(instance, extensionID); err != nil {
		return result, fmt.Errorf("apply staged %s update: %w", extensionID, err)
	}
	result.Pending = false
	result.Message = "更新已应用"
	return result, nil
}

type extensionUpdateDependencies struct {
	Client    *http.Client
	SourceFor func(string) (extensionReleaseSource, error)
	StatusFor func(ServerInstance) (RuntimeStatus, error)
	ApplyOps  extensionApplyOps
}

func (dependencies extensionUpdateDependencies) normalized() extensionUpdateDependencies {
	if dependencies.Client == nil {
		dependencies.Client = releaseDownloadClient()
	}
	if dependencies.SourceFor == nil {
		dependencies.SourceFor = extensionReleaseSourceFor
	}
	if dependencies.StatusFor == nil {
		dependencies.StatusFor = serverStatus
	}
	return dependencies
}

func sameExtensionUpdateTarget(staged, current ServerInstance) bool {
	return strings.EqualFold(filepath.Clean(staged.RootPath), filepath.Clean(current.RootPath)) &&
		strings.EqualFold(filepath.Clean(staged.Executable), filepath.Clean(current.Executable))
}

func (a *App) updateExtensionWith(id, extensionID string, dependencies extensionUpdateDependencies) (ExtensionUpdateResult, error) {
	if err := validateExtensionID(extensionID); err != nil {
		return ExtensionUpdateResult{}, err
	}
	instance, err := a.store.Find(id)
	if err != nil {
		return ExtensionUpdateResult{}, err
	}
	dependencies = dependencies.normalized()
	source, err := dependencies.SourceFor(extensionID)
	if err != nil {
		return ExtensionUpdateResult{}, err
	}
	if source.ID == "" {
		source.ID = extensionID
	}
	if source.ID != extensionID {
		return ExtensionUpdateResult{}, errors.New("extension release source ID mismatch")
	}
	latest, err := fetchExtensionRelease(dependencies.Client, source)
	if err != nil {
		return ExtensionUpdateResult{}, err
	}
	extracted, cleanup, err := downloadAndExtractExtension(instance, latest, dependencies.Client)
	if err != nil {
		return ExtensionUpdateResult{}, err
	}
	defer cleanup()
	a.serverStartMu.Lock()
	defer a.serverStartMu.Unlock()
	a.extensionStageMu.Lock()
	defer a.extensionStageMu.Unlock()
	manifest, err := stageExtensionPayload(instance, latest, extracted)
	if err != nil {
		return ExtensionUpdateResult{}, err
	}
	result := ExtensionUpdateResult{ExtensionID: extensionID, Version: manifest.Version, Pending: true, Message: "已下载，等待服务器停止或下次启动时应用"}
	current, err := a.store.Find(id)
	if err != nil {
		return result, err
	}
	if !sameExtensionUpdateTarget(instance, current) {
		return result, errors.New("server instance paths changed while staging extension update; pending update was retained")
	}
	status, err := dependencies.StatusFor(current)
	if err != nil {
		return result, err
	}
	if status.Running {
		return result, nil
	}
	if err := applyPendingExtensionUpdateWithOps(current, extensionID, dependencies.ApplyOps); err != nil {
		return result, err
	}
	result.Pending = false
	result.Message = "更新已应用"
	return result, nil
}

func (a *App) UpdateExtension(id, extensionID string) (ExtensionUpdateResult, error) {
	return a.updateExtensionWith(id, extensionID, extensionUpdateDependencies{})
}

func (a *App) updateAllExtensionsWith(id string, dependencies extensionUpdateDependencies) ([]ExtensionUpdateResult, error) {
	statuses, err := a.ListExtensions(id)
	if err != nil {
		return nil, err
	}
	dependencies = dependencies.normalized()
	candidates := make([]ExtensionStatus, 0, len(statuses))
	for _, status := range statuses {
		if !status.Installed || status.Pending {
			continue
		}
		candidates = append(candidates, status)
	}
	checked := checkExtensionUpdatesWith(candidates, dependencies.Client, dependencies.SourceFor)
	checkErrors := make([]error, 0)
	for _, status := range checked {
		if status.UpdateCheckError == "" {
			continue
		}
		checkErrors = append(checkErrors, fmt.Errorf("%s (%s): %s", status.Name, status.ID, status.UpdateCheckError))
	}
	if checkErr := errors.Join(checkErrors...); checkErr != nil {
		return nil, fmt.Errorf("check extension updates: %w", checkErr)
	}
	results := make([]ExtensionUpdateResult, 0, len(checked))
	for _, status := range checked {
		if !status.UpdateAvailable || status.UpdateCheckError != "" {
			continue
		}
		result, err := a.updateExtensionWith(id, status.ID, dependencies)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (a *App) UpdateAllExtensions(id string) ([]ExtensionUpdateResult, error) {
	return a.updateAllExtensionsWith(id, extensionUpdateDependencies{})
}

func extensionReleaseSourceFor(extensionID string) (extensionReleaseSource, error) {
	switch extensionID {
	case "paldefender":
		return extensionReleaseSource{ID: extensionID, Endpoint: githubAPIBase + "/repos/Ultimeit/PalDefender/releases/latest"}, nil
	case "ue4ss":
		return extensionReleaseSource{ID: extensionID, Endpoint: githubAPIBase + "/repos/UE4SS-RE/RE-UE4SS/releases/tags/experimental-latest"}, nil
	default:
		return extensionReleaseSource{}, errors.New("unknown extension")
	}
}

func selectExtensionAsset(extensionID string, release githubRelease) (githubReleaseAsset, string, error) {
	if extensionID != "paldefender" && extensionID != "ue4ss" {
		return githubReleaseAsset{}, "", errors.New("unknown extension")
	}
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		switch extensionID {
		case "paldefender":
			if strings.EqualFold(asset.Name, "PalDefender.zip") {
				return asset, release.TagName, nil
			}
		case "ue4ss":
			if strings.HasPrefix(name, "ue4ss_v") && strings.HasSuffix(name, ".zip") {
				version := asset.Name[len("UE4SS_") : len(asset.Name)-len(".zip")]
				return asset, version, nil
			}
		}
	}
	return githubReleaseAsset{}, "", errors.New("compatible release asset not found")
}

func fetchExtensionRelease(client *http.Client, source extensionReleaseSource) (extensionReleaseInfo, error) {
	request, err := http.NewRequest(http.MethodGet, source.Endpoint, nil)
	if err != nil {
		return extensionReleaseInfo{}, err
	}
	request.Header.Set("User-Agent", "palserver-launcher/"+LauncherVersion)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := client.Do(request)
	if err != nil {
		return extensionReleaseInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return extensionReleaseInfo{}, fmt.Errorf("GitHub release lookup failed: %s", response.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
		return extensionReleaseInfo{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	asset, version, err := selectExtensionAsset(source.ID, release)
	if err != nil {
		return extensionReleaseInfo{}, err
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return extensionReleaseInfo{}, errors.New("release version is empty")
	}
	return extensionReleaseInfo{
		ExtensionID: source.ID,
		Version:     version,
		Asset:       asset,
		PublishedAt: release.PublishedAt,
	}, nil
}

func extensionUpdateAvailable(local ExtensionStatus, latest extensionReleaseInfo) bool {
	switch latest.ExtensionID {
	case "paldefender", "ue4ss":
	default:
		return false
	}
	if !local.Installed {
		return false
	}
	localVersion := strings.TrimSpace(local.Version)
	installedAsset := strings.TrimSpace(local.InstalledAsset)
	installedUpdatedAt := strings.TrimSpace(local.InstalledUpdatedAt)
	if localVersion == "" || installedAsset == "" || installedUpdatedAt == "" {
		return true
	}
	latestAsset := strings.TrimSpace(latest.Asset.Name)
	latestUpdatedAt := strings.TrimSpace(latest.Asset.UpdatedAt)
	switch latest.ExtensionID {
	case "paldefender":
		return !strings.EqualFold(localVersion, latest.Version) ||
			!strings.EqualFold(installedAsset, latestAsset) ||
			installedUpdatedAt != latestUpdatedAt
	case "ue4ss":
		if strings.HasPrefix(strings.ToLower(localVersion), "ue4ss_") && strings.HasSuffix(strings.ToLower(localVersion), ".zip") {
			localVersion = localVersion[len("UE4SS_") : len(localVersion)-len(".zip")]
		}
		return !strings.EqualFold(localVersion, latest.Version) ||
			!strings.EqualFold(installedAsset, latestAsset) ||
			installedUpdatedAt != latestUpdatedAt
	}
	return false
}

func checkExtensionUpdatesWith(local []ExtensionStatus, client *http.Client, sourceFor func(string) (extensionReleaseSource, error)) []ExtensionStatus {
	statuses := append([]ExtensionStatus(nil), local...)
	type checkResult struct {
		index  int
		latest extensionReleaseInfo
		err    error
	}
	results := make(chan checkResult, len(statuses))
	var wait sync.WaitGroup
	for index := range statuses {
		status := statuses[index]
		status.LatestVersion = ""
		status.LatestAsset = ""
		status.LatestUpdatedAt = ""
		status.UpdateAvailable = false
		status.UpdateCheckError = ""
		statuses[index] = status
		wait.Add(1)
		go func(index int, extensionID string) {
			defer wait.Done()
			source, err := sourceFor(extensionID)
			if err != nil {
				results <- checkResult{index: index, err: err}
				return
			}
			if source.ID == "" {
				source.ID = extensionID
			}
			latest, err := fetchExtensionRelease(client, source)
			results <- checkResult{index: index, latest: latest, err: err}
		}(index, status.ID)
	}
	wait.Wait()
	close(results)
	for result := range results {
		status := statuses[result.index]
		if result.err != nil {
			status.UpdateCheckError = result.err.Error()
			statuses[result.index] = status
			continue
		}
		status.LatestVersion = result.latest.Version
		status.LatestAsset = result.latest.Asset.Name
		status.LatestUpdatedAt = result.latest.Asset.UpdatedAt
		if status.LatestUpdatedAt == "" {
			status.LatestUpdatedAt = result.latest.PublishedAt
		}
		status.UpdateAvailable = extensionUpdateAvailable(status, result.latest)
		statuses[result.index] = status
	}
	return statuses
}

func (a *App) CheckExtensionUpdates(id string) ([]ExtensionStatus, error) {
	local, err := a.ListExtensions(id)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	return checkExtensionUpdatesWith(local, client, extensionReleaseSourceFor), nil
}

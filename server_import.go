package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	serverImportMaxExtractedBytes = int64(64 << 30)
	serverImportMaxExtractedFiles = 200000
)

// ServerImportResult is deliberately about the operation, not the Agent's
// filesystem.  The browser only needs to show what was detected and which new
// managed instance was created; absolute Linux paths never leave the Agent.
type ServerImportResult struct {
	Instance ServerInstance `json:"instance"`
	Format   string         `json:"format"`
	Detected string         `json:"detected"`
	Name     string         `json:"detectedName"`
}

type serverImportLayout struct {
	SaveRoot     string
	RawWorld     string
	Settings     string
	Detected     string
	DetectedName string
}

func validServerImportID(id string) bool {
	if id == "" || len(id) > 80 || filepath.Base(id) != id {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'f') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func serverImportDirectory(id string) (string, error) {
	if !validServerImportID(id) {
		return "", errors.New("无效的导入任务")
	}
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(base, "web-imports", id)
	if ok, err := resolvedPathWithinAllowedRoots(root, []string{filepath.Join(base, "web-imports")}); err != nil || !ok {
		if err != nil {
			return "", err
		}
		return "", errors.New("导入文件路径不安全")
	}
	return root, nil
}

func serverImportFilePath(id string) (string, error) {
	return serverImportDirectory(id)
}

func cleanupAbandonedServerImports() error {
	base, err := appDataDir()
	if err != nil {
		return err
	}
	root := filepath.Join(base, "web-imports")
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	return os.MkdirAll(root, 0o700)
}

// saveWebServerImport keeps an upload on the Agent until the background job
// finishes. Unlike mod uploads, it preserves webkitRelativePath so a user can
// choose a local folder without exposing or typing a remote path.
func saveWebServerImport(request *http.Request) (string, error) {
	if err := request.ParseMultipartForm(webUploadMemory); err != nil {
		return "", fmt.Errorf("解析服务器导入：%w", err)
	}
	form := request.MultipartForm
	defer form.RemoveAll()
	headers := form.File["files"]
	if len(headers) == 0 {
		return "", errors.New("请选择服务器 ZIP 文件或服务器文件夹")
	}
	if len(headers) > webServerImportMaxFileCount {
		return "", fmt.Errorf("导入文件过多，最多 %d 个文件", webServerImportMaxFileCount)
	}
	paths := form.Value["paths"]
	if len(paths) != 0 && len(paths) != len(headers) {
		return "", errors.New("导入文件路径信息不完整，请重新选择")
	}
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	root, err := serverImportDirectory(id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	for index, header := range headers {
		relative := header.Filename
		if len(paths) > 0 {
			relative = paths[index]
		}
		relative, err = cleanServerImportRelativePath(relative)
		if err != nil {
			cleanup()
			return "", err
		}
		destination := filepath.Join(root, filepath.FromSlash(relative))
		if !pathWithinAllowedRoots(destination, []string{root}) || destination == root {
			cleanup()
			return "", errors.New("导入文件路径不安全")
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			cleanup()
			return "", err
		}
		if err := copyWebUploadWithLimit(header, destination, webServerImportMaxFile); err != nil {
			cleanup()
			return "", err
		}
	}
	return id, nil
}

func cleanServerImportRelativePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || strings.ContainsRune(value, '\x00') || strings.Contains(value, ":") {
		return "", errors.New("导入文件包含无效路径")
	}
	clean := path.Clean(strings.TrimPrefix(value, "./"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", errors.New("导入文件包含不安全路径")
	}
	return clean, nil
}

func archiveImportName(path string) string {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return "tar.gz"
	}
	if strings.HasSuffix(lower, ".zip") {
		return "zip"
	}
	return ""
}

func prepareServerImportRoot(staged string) (root, format string, cleanup func(), err error) {
	cleanup = func() {}
	entries, err := os.ReadDir(staged)
	if err != nil {
		return "", "", cleanup, err
	}
	archivePath := ""
	if len(entries) == 1 && !entries[0].IsDir() {
		archivePath = filepath.Join(staged, entries[0].Name())
	}
	format = archiveImportName(archivePath)
	if format == "" {
		return staged, "folder", cleanup, nil
	}
	extracted, err := os.MkdirTemp(staged, "extracted-")
	if err != nil {
		return "", "", cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(extracted) }
	switch format {
	case "zip":
		err = extractServerImportZIP(archivePath, extracted)
	case "tar.gz":
		err = extractServerImportTarGZ(archivePath, extracted)
	}
	if err != nil {
		cleanup()
		return "", "", func() {}, err
	}
	return extracted, format, cleanup, nil
}

func extractServerImportZIP(archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("读取 ZIP：%w", err)
	}
	defer reader.Close()
	var total int64
	files := 0
	for _, entry := range reader.File {
		if files >= serverImportMaxExtractedFiles {
			return errors.New("ZIP 文件包含过多条目")
		}
		if entry.FileInfo().IsDir() && path.Clean(strings.ReplaceAll(entry.Name, "\\", "/")) == "." {
			continue
		}
		rel, err := cleanServerImportRelativePath(entry.Name)
		if err != nil {
			return err
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return errors.New("ZIP 不允许包含符号链接")
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		files++
		if entry.UncompressedSize64 > uint64(serverImportMaxExtractedBytes) || total > serverImportMaxExtractedBytes-int64(entry.UncompressedSize64) {
			return errors.New("解压后的导入文件过大")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		in, err := entry.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = in.Close()
			return err
		}
		written, copyErr := io.Copy(out, io.LimitReader(in, serverImportMaxExtractedBytes-total+1))
		_ = in.Close()
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		if written > serverImportMaxExtractedBytes-total {
			return errors.New("解压后的导入文件过大")
		}
		total += written
	}
	return nil
}

func extractServerImportTarGZ(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("读取 tar.gz：%w", err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var total int64
	files := 0
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
		if header.Typeflag == tar.TypeDir && path.Clean(strings.ReplaceAll(header.Name, "\\", "/")) == "." {
			continue
		}
		rel, err := cleanServerImportRelativePath(header.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			files++
			if files > serverImportMaxExtractedFiles || header.Size < 0 || header.Size > serverImportMaxExtractedBytes || total > serverImportMaxExtractedBytes-header.Size {
				return errors.New("解压后的导入文件过大")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(out, io.LimitReader(reader, header.Size+1))
			closeErr := out.Close()
			if copyErr != nil || closeErr != nil {
				return errors.Join(copyErr, closeErr)
			}
			if written != header.Size {
				return errors.New("tar.gz 文件大小不一致")
			}
			total += written
		default:
			return errors.New("tar.gz 不允许包含链接或特殊文件")
		}
	}
	return nil
}

func detectServerImportLayout(root string) (serverImportLayout, error) {
	layout := serverImportLayout{}
	err := filepath.Walk(root, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("导入目录不能包含符号链接")
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		originalParts := strings.Split(filepath.ToSlash(rel), "/")
		parts := make([]string, len(originalParts))
		for index := range originalParts {
			parts[index] = strings.ToLower(originalParts[index])
		}
		if len(parts) == 0 {
			return nil
		}
		for index := 0; index+1 < len(parts); index++ {
			if parts[index] == "pal" && parts[index+1] == "saved" {
				layout.SaveRoot = filepath.Join(root, filepath.FromSlash(strings.Join(originalParts[:index+2], "/")))
			}
			if parts[index] == "saved" && parts[index+1] == "savegames" {
				layout.SaveRoot = filepath.Join(root, filepath.FromSlash(strings.Join(originalParts[:index+1], "/")))
			}
		}
		for index, part := range parts {
			if part == "savegames" {
				candidate := filepath.Dir(filepath.Join(root, filepath.FromSlash(strings.Join(originalParts[:index+1], "/"))))
				if layout.SaveRoot == "" || len(candidate) < len(layout.SaveRoot) {
					layout.SaveRoot = candidate
				}
			}
		}
		if parts[len(parts)-1] == "palworldsettings.ini" {
			layout.Settings = filePath
			if data, readErr := os.ReadFile(filePath); readErr == nil {
				values := parseWorldSettingValues(string(data))
				layout.DetectedName = strings.TrimSpace(values["ServerName"])
			}
		}
		if parts[len(parts)-1] == "level.sav" && layout.SaveRoot == "" {
			layout.RawWorld = filepath.Dir(filePath)
			for index, part := range parts {
				if part == "savegames" {
					layout.SaveRoot = filepath.Dir(filepath.Join(root, filepath.FromSlash(strings.Join(originalParts[:index+1], "/"))))
				}
			}
		}
		return nil
	})
	if err != nil {
		return serverImportLayout{}, err
	}
	switch {
	case layout.SaveRoot != "" && layout.Settings != "":
		layout.Detected = "服务器存档 + 世界设置"
	case layout.RawWorld != "" && layout.Settings != "":
		layout.Detected = "单个世界存档 + 世界设置"
	case layout.SaveRoot != "":
		layout.Detected = "服务器存档"
	case layout.RawWorld != "":
		layout.Detected = "单个世界存档"
	case layout.Settings != "":
		layout.Detected = "世界设置"
	default:
		return serverImportLayout{}, errors.New("未识别出 Palworld 存档。请上传包含 Pal/Saved、Saved/SaveGames 或 PalWorldSettings.ini 的文件夹/压缩包")
	}
	return layout, nil
}

func copyServerImportData(instance ServerInstance, layout serverImportLayout) error {
	targetSaved := filepath.Join(instance.RootPath, "Pal", "Saved")
	if layout.SaveRoot != "" {
		sourceSaveGames, err := caseInsensitiveChildDirectory(layout.SaveRoot, "SaveGames")
		if err != nil {
			return fmt.Errorf("识别出的 Saved 目录缺少 SaveGames：%w", err)
		}
		if err := copyTree(sourceSaveGames, filepath.Join(targetSaved, "SaveGames")); err != nil {
			return fmt.Errorf("复制存档：%w", err)
		}
	}
	if layout.SaveRoot == "" && layout.RawWorld != "" {
		targetWorld := filepath.Join(targetSaved, "SaveGames", "0", filepath.Base(layout.RawWorld))
		if err := copyTree(layout.RawWorld, targetWorld); err != nil {
			return fmt.Errorf("复制单个世界存档：%w", err)
		}
	}
	if layout.Settings != "" {
		target := filepath.Join(targetSaved, "Config", serverConfigDirectoryName(), "PalWorldSettings.ini")
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := copyFile(layout.Settings, target); err != nil {
			return fmt.Errorf("复制世界设置：%w", err)
		}
	}
	return nil
}

func caseInsensitiveChildDirectory(parent, name string) (string, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			return filepath.Join(parent, entry.Name()), nil
		}
	}
	return "", os.ErrNotExist
}

// ImportUploadedServer always installs a fresh server runtime first. Only
// portable Palworld data is copied; Windows binaries, UE4SS DLLs and other
// platform-specific files are intentionally ignored on Linux.
func (a *App) ImportUploadedServer(uploadID, requestedName string) (ServerImportResult, error) {
	staged, err := serverImportFilePath(uploadID)
	if err != nil {
		return ServerImportResult{}, err
	}
	defer os.RemoveAll(staged)
	root, format, cleanup, err := prepareServerImportRoot(staged)
	if err != nil {
		return ServerImportResult{}, err
	}
	defer cleanup()
	layout, err := detectServerImportLayout(root)
	if err != nil {
		return ServerImportResult{}, err
	}
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = layout.DetectedName
	}
	if name == "" {
		name = "导入的帕鲁服务器"
	}
	if runtime.GOOS != "linux" {
		return ServerImportResult{}, errors.New("网页 Agent 的服务器导入仅支持 Linux Agent；Windows 请使用桌面端导入")
	}
	instance, err := a.QuickSetup(name, "")
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("安装全新的 Linux 服务器失败：%w", err)
	}
	completed := false
	defer func() {
		if !completed {
			_ = a.DeleteInstance(instance.ID, true)
		}
	}()
	if err := copyServerImportData(instance, layout); err != nil {
		return ServerImportResult{}, err
	}
	if layout.Settings != "" {
		if data, readErr := os.ReadFile(layout.Settings); readErr == nil {
			values := parseWorldSettingValues(string(data))
			if password := strings.TrimSpace(values["AdminPassword"]); password != "" {
				instance.AdminPassword = password
			}
			if password := strings.TrimSpace(values["ServerPassword"]); password != "" {
				instance.ServerPassword = password
			}
		}
	}
	if err := syncInstanceWorldSettings(instance); err != nil {
		return ServerImportResult{}, err
	}
	stored, err := a.store.Upsert(instance)
	if err != nil {
		return ServerImportResult{}, err
	}
	completed = true
	return ServerImportResult{Instance: stored, Format: format, Detected: layout.Detected, Name: layout.DetectedName}, nil
}

func marshalServerImportJobArgs(uploadID, name string) ([]json.RawMessage, error) {
	first, err := json.Marshal(uploadID)
	if err != nil {
		return nil, err
	}
	second, err := json.Marshal(name)
	if err != nil {
		return nil, err
	}
	return []json.RawMessage{first, second}, nil
}

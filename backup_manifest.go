package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	backupManifestName    = ".palserver-backup.json"
	backupManifestMaxSize = 64 << 20
)

type backupManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type backupManifest struct {
	Version   int                  `json:"version"`
	ServerID  string               `json:"serverId"`
	CreatedAt int64                `json:"createdAt"`
	Files     []backupManifestFile `json:"files"`
}

func validBackupManifestPath(value string) bool {
	if strings.TrimSpace(value) == "" || strings.Contains(value, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	return clean != "." && clean != ".." && !filepath.IsAbs(clean) && filepath.VolumeName(clean) == "" && !strings.HasPrefix(clean, ".."+string(os.PathSeparator)) && filepath.ToSlash(clean) == value
}

func copyFileWithSHA256(source, destination string, mode os.FileMode) (int64, string, error) {
	input, err := os.Open(source)
	if err != nil {
		return 0, "", err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return 0, "", errors.Join(err, input.Close())
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), input)
	inputCloseErr := input.Close()
	outputCloseErr := output.Close()
	if err := errors.Join(copyErr, inputCloseErr, outputCloseErr); err != nil {
		return 0, "", err
	}
	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func copyBackupTree(source, destination string) ([]backupManifestFile, error) {
	files := make([]backupManifestFile, 0)
	err := filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 || copyTreeEntryIsReparsePoint(info) {
			return fmt.Errorf("backup source contains a symlink or reparse point: %s", path)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("backup source contains a non-regular entry: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		relative = filepath.ToSlash(relative)
		if relative == backupManifestName {
			return errors.New("save data contains the reserved backup manifest filename")
		}
		if !validBackupManifestPath(relative) {
			return fmt.Errorf("backup source contains an unsafe relative path: %s", relative)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		written, digest, err := copyFileWithSHA256(path, target, info.Mode())
		if err != nil {
			return err
		}
		if written != info.Size() {
			return fmt.Errorf("backup file changed while copying: %s", relative)
		}
		files = append(files, backupManifestFile{Path: relative, Size: written, SHA256: digest})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func writeBackupManifest(root, serverID string, createdAt int64, files []backupManifestFile) error {
	manifest := backupManifest{Version: 1, ServerID: serverID, CreatedAt: createdAt, Files: files}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > backupManifestMaxSize {
		return errors.New("backup manifest is unexpectedly large")
	}
	path := filepath.Join(root, backupManifestName)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func readBackupManifest(root string) (backupManifest, bool, error) {
	path := filepath.Join(root, backupManifestName)
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return backupManifest{}, false, nil
	}
	if err != nil {
		return backupManifest{}, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, backupManifestMaxSize+1))
	if err != nil {
		return backupManifest{}, true, err
	}
	if len(data) > backupManifestMaxSize {
		return backupManifest{}, true, errors.New("backup manifest is too large")
	}
	var manifest backupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return backupManifest{}, true, fmt.Errorf("decode backup manifest: %w", err)
	}
	if manifest.Version != 1 {
		return backupManifest{}, true, fmt.Errorf("unsupported backup manifest version %d", manifest.Version)
	}
	return manifest, true, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyBackupManifest(root string) (backupManifest, bool, error) {
	manifest, found, err := readBackupManifest(root)
	if err != nil || !found {
		return manifest, found, err
	}
	expected := make(map[string]backupManifestFile, len(manifest.Files))
	for _, entry := range manifest.Files {
		if !validBackupManifestPath(entry.Path) || entry.Size < 0 || len(entry.SHA256) != sha256.Size*2 {
			return backupManifest{}, true, fmt.Errorf("backup manifest contains an invalid file entry: %s", entry.Path)
		}
		if _, err := hex.DecodeString(entry.SHA256); err != nil {
			return backupManifest{}, true, fmt.Errorf("backup manifest contains an invalid digest for %s", entry.Path)
		}
		if _, duplicate := expected[entry.Path]; duplicate {
			return backupManifest{}, true, fmt.Errorf("backup manifest contains duplicate path %s", entry.Path)
		}
		expected[entry.Path] = entry
	}
	seen := make(map[string]bool, len(expected))
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 || copyTreeEntryIsReparsePoint(info) {
			return fmt.Errorf("backup contains a symlink or reparse point: %s", path)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup contains a non-regular entry: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == backupManifestName {
			return nil
		}
		entry, ok := expected[relative]
		if !ok {
			return fmt.Errorf("backup contains an unlisted file: %s", relative)
		}
		if info.Size() != entry.Size {
			return fmt.Errorf("backup file size mismatch: %s", relative)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if !strings.EqualFold(digest, entry.SHA256) {
			return fmt.Errorf("backup file checksum mismatch: %s", relative)
		}
		seen[relative] = true
		return nil
	})
	if err != nil {
		return backupManifest{}, true, err
	}
	for path := range expected {
		if !seen[path] {
			return backupManifest{}, true, fmt.Errorf("backup file is missing: %s", path)
		}
	}
	return manifest, true, nil
}

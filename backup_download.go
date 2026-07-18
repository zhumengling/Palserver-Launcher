package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func validBackupPathComponent(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && value != ".." && !filepath.IsAbs(value) && filepath.VolumeName(value) == "" && filepath.Base(value) == value
}

func backupDownloadSource(instanceID, name string) (string, error) {
	instanceID = strings.TrimSpace(instanceID)
	name = strings.TrimSpace(name)
	if !validBackupPathComponent(instanceID) {
		return "", errors.New("invalid server id")
	}
	if !validBackupPathComponent(name) {
		return "", errors.New("invalid backup name")
	}
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(base, "backups", instanceID)
	path := filepath.Join(root, name)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("backup path is outside this server")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || copyTreeEntryIsReparsePoint(info) {
		return "", errors.New("backup is a symbolic link or reparse point")
	}
	if !info.IsDir() {
		return "", errors.New("backup is not a directory")
	}
	return path, nil
}

func (a *App) officialBackupDownloadSource(instanceID, name string) (string, error) {
	instanceID = strings.TrimSpace(instanceID)
	name = strings.TrimSpace(name)
	if !validBackupPathComponent(instanceID) {
		return "", errors.New("invalid server id")
	}
	if !validBackupPathComponent(name) {
		return "", errors.New("invalid backup name")
	}
	backups, err := a.ListOfficialBackups(instanceID)
	if err != nil {
		return "", err
	}
	for _, backup := range backups {
		if backup.Name != name {
			continue
		}
		info, err := os.Lstat(backup.Path)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || copyTreeEntryIsReparsePoint(info) {
			return "", errors.New("official backup is not a safe directory")
		}
		return backup.Path, nil
	}
	return "", errors.New("official backup was not found")
}

func validateBackupZIPSource(source string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || copyTreeEntryIsReparsePoint(info) {
			return fmt.Errorf("backup contains a symbolic link or reparse point: %s", filepath.Base(path))
		}
		if !entry.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("backup contains a non-regular entry: %s", filepath.Base(path))
		}
		return nil
	})
}

func writeBackupZIPContents(writer io.Writer, source string) error {
	archive := zip.NewWriter(writer)
	walkErr := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == source {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if entry.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		output, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := input.Close()
		return errors.Join(copyErr, closeErr)
	})
	return errors.Join(walkErr, archive.Close())
}

func writeBackupZIP(writer io.Writer, source string) error {
	if err := validateBackupZIPSource(source); err != nil {
		return err
	}
	return writeBackupZIPContents(writer, source)
}

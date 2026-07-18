package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	managedLogMaxBytes     int64 = 32 << 20
	managedLogReadMaxBytes int64 = 4 << 20
	managedLogBackups            = 3
)

func rotateLogFile(path string, maximumBytes int64, backups int) error {
	if maximumBytes < 1 {
		return errors.New("log rotation size must be positive")
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < maximumBytes {
		return nil
	}
	if backups < 1 {
		return os.Remove(path)
	}
	for index := backups; index >= 2; index-- {
		source := path + "." + strconv.Itoa(index-1)
		destination := path + "." + strconv.Itoa(index)
		_ = os.Remove(destination)
		if err := os.Rename(source, destination); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	first := path + ".1"
	_ = os.Remove(first)
	return os.Rename(path, first)
}

func readFileTail(path string, maximumBytes int64) ([]byte, error) {
	if maximumBytes < 1 {
		return nil, errors.New("tail size must be positive")
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return []byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := max(int64(0), info.Size()-maximumBytes)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	if err != nil {
		return nil, err
	}
	if start > 0 {
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			data = data[newline+1:]
		}
	}
	return data, nil
}

func readLogLines(path string, lines int) (string, error) {
	data, err := readFileTail(filepath.Clean(path), managedLogReadMaxBytes)
	if err != nil || len(data) == 0 {
		return "", err
	}
	content := strings.TrimSuffix(string(data), "\n")
	content = strings.TrimSuffix(content, "\r")
	parts := strings.Split(content, "\n")
	if lines > 0 && len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n"), nil
}

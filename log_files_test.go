package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotateLogFileKeepsConfiguredGenerations(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "server.log")
	for name, content := range map[string]string{
		"server.log": "current-log", "server.log.1": "previous-one", "server.log.2": "previous-two",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := rotateLogFile(path, 4, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("active log still exists after rotation: %v", err)
	}
	first, _ := os.ReadFile(path + ".1")
	second, _ := os.ReadFile(path + ".2")
	if string(first) != "current-log" || string(second) != "previous-one" {
		t.Fatalf("rotated logs = %q / %q", first, second)
	}
}

func TestRotateLogFileLeavesSmallLogUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "small.log")
	if err := os.WriteFile(path, []byte("small"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateLogFile(path, 1024, 3); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "small" {
		t.Fatalf("small log = %q, %v", data, err)
	}
}

func TestReadLogLinesReadsBoundedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.log")
	prefix := strings.Repeat("old-data-without-reading-all\n", 200000)
	if err := os.WriteFile(path, []byte(prefix+"last-one\nlast-two\nlast-three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := readLogLines(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result != "last-two\nlast-three" {
		t.Fatalf("log tail = %q", result)
	}
}

func TestReadLogLinesMissingFileIsEmpty(t *testing.T) {
	result, err := readLogLines(filepath.Join(t.TempDir(), "missing.log"), 100)
	if err != nil || result != "" {
		t.Fatalf("missing log = %q, %v", result, err)
	}
}

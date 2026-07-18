package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func multipartUploadRequest(t *testing.T, files map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, content := range files {
		part, err := writer.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "/upload", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestSaveWebMultipartFilesStagesAndCleansUploads(t *testing.T) {
	t.Setenv("PALSERVER_LAUNCHER_HOME", t.TempDir())
	request := multipartUploadRequest(t, map[string]string{"example.pak": "pak-data", "config.lua": "lua-data"})
	paths, cleanup, err := saveWebMultipartFiles(request, "files", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("staged paths = %d, want 2", len(paths))
	}
	root := filepath.Dir(paths[0])
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			t.Fatalf("staged upload %s data=%q err=%v", path, data, err)
		}
	}
	cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("upload staging directory still exists: %v", err)
	}
}

func TestSaveWebMultipartFilesRejectsUnsafeOrDuplicateNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../escape.pak", `folder\escape.pak`} {
		if validWebUploadName(name) {
			t.Fatalf("unsafe upload name was accepted: %q", name)
		}
	}
	t.Setenv("PALSERVER_LAUNCHER_HOME", t.TempDir())
	request := multipartUploadRequest(t, map[string]string{"one.pak": "1", "ONE.PAK": "2"})
	if _, _, err := saveWebMultipartFiles(request, "files", 4); err == nil {
		t.Fatal("case-insensitive duplicate upload names were accepted")
	}
}

func TestSaveWebMultipartFilesEnforcesFileCount(t *testing.T) {
	t.Setenv("PALSERVER_LAUNCHER_HOME", t.TempDir())
	request := multipartUploadRequest(t, map[string]string{"one.pak": "1", "two.pak": "2"})
	if _, _, err := saveWebMultipartFiles(request, "files", 1); err == nil {
		t.Fatal("upload file count limit was not enforced")
	}
}

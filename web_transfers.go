package main

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	webUploadMaxBody      int64 = 512 << 20
	webUploadMaxFile      int64 = 512 << 20
	webUploadMemory             = 16 << 20
	webUploadMaxFileCount       = 128
)

// Server imports are intentionally larger than mod uploads.  A browser is
// uploading a save from another computer, so the Agent must not require the
// user to type a path that only exists on the Agent host.
const (
	webServerImportMaxBody      int64 = 32 << 30
	webServerImportMaxFile      int64 = 32 << 30
	webServerImportMaxFileCount       = 20000
)

func validWebUploadName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && len(name) <= 255 && !strings.ContainsAny(name, "/\\\x00") && name != "." && name != ".."
}

func copyWebUpload(header *multipart.FileHeader, destination string) error {
	return copyWebUploadWithLimit(header, destination, webUploadMaxFile)
}

func copyWebUploadWithLimit(header *multipart.FileHeader, destination string, maximum int64) error {
	if header.Size < 0 || header.Size > maximum {
		return errors.New("uploaded file is too large")
	}
	input, err := header.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, maximum+1))
	closeErr := output.Close()
	if written > maximum {
		_ = os.Remove(destination)
		return errors.New("uploaded file is too large")
	}
	if joined := errors.Join(copyErr, closeErr); joined != nil {
		_ = os.Remove(destination)
		return joined
	}
	if header.Size >= 0 && written != header.Size {
		_ = os.Remove(destination)
		return errors.New("uploaded file size mismatch")
	}
	return nil
}

func saveWebMultipartFiles(request *http.Request, field string, maximum int) (paths []string, cleanup func(), err error) {
	if maximum < 1 || maximum > webUploadMaxFileCount {
		maximum = webUploadMaxFileCount
	}
	if err := request.ParseMultipartForm(webUploadMemory); err != nil {
		return nil, func() {}, fmt.Errorf("parse upload: %w", err)
	}
	removeMultipart := func() {
		if request.MultipartForm != nil {
			_ = request.MultipartForm.RemoveAll()
		}
	}
	headers := request.MultipartForm.File[field]
	if len(headers) == 0 {
		removeMultipart()
		return nil, func() {}, errors.New("no files were uploaded")
	}
	if len(headers) > maximum {
		removeMultipart()
		return nil, func() {}, fmt.Errorf("too many uploaded files; maximum is %d", maximum)
	}
	base, err := appDataDir()
	if err != nil {
		removeMultipart()
		return nil, func() {}, err
	}
	uploadRoot := filepath.Join(base, "web-uploads")
	if err := os.MkdirAll(uploadRoot, 0o700); err != nil {
		removeMultipart()
		return nil, func() {}, err
	}
	temporary, err := os.MkdirTemp(uploadRoot, "upload-*")
	if err != nil {
		removeMultipart()
		return nil, func() {}, err
	}
	cleanup = func() {
		removeMultipart()
		_ = os.RemoveAll(temporary)
	}
	seen := map[string]bool{}
	paths = make([]string, 0, len(headers))
	for _, header := range headers {
		name := strings.TrimSpace(header.Filename)
		if !validWebUploadName(name) {
			cleanup()
			return nil, func() {}, errors.New("uploaded file has an unsafe name")
		}
		key := strings.ToLower(name)
		if seen[key] {
			cleanup()
			return nil, func() {}, errors.New("uploaded files contain duplicate names")
		}
		seen[key] = true
		destination := filepath.Join(temporary, name)
		if err := copyWebUpload(header, destination); err != nil {
			cleanup()
			return nil, func() {}, err
		}
		paths = append(paths, destination)
	}
	return paths, cleanup, nil
}

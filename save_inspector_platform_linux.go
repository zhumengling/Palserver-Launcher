//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func saveInspectorReleaseAssetMatches(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "pst_") && strings.HasSuffix(lower, "linux_x86_64.tar.gz")
}

func saveInspectorAssetMissingMessage() string {
	return "Linux x86_64 save inspector asset was not found"
}

func saveInspectorExecutableName() string { return "sav_cli" }

func extractSaveInspectorExecutable(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != saveInspectorExecutableName() {
			continue
		}
		if header.Size < 1 || header.Size > 256<<20 {
			return errors.New("Linux save inspector executable has an invalid size")
		}
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		target := filepath.Join(destination, saveInspectorExecutableName())
		output, err := os.OpenFile(target+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(tarReader, header.Size+1))
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != header.Size {
			return errors.New("Linux save inspector executable size mismatch")
		}
		return os.Rename(target+".tmp", target)
	}
	return errors.New("sav_cli was not found in Linux save inspector archive")
}

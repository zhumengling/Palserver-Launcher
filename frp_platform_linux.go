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

func frpReleaseAssetMatches(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "frp_") && strings.HasSuffix(lower, "linux_amd64.tar.gz")
}

func frpExecutableName() string { return "frpc" }

func extractFRPExecutable(archivePath, destination string) error {
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
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "frpc" {
			continue
		}
		if header.Size < 1 || header.Size > 256<<20 {
			return errors.New("FRP Linux executable has an invalid size")
		}
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		target := filepath.Join(destination, frpExecutableName())
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
			return errors.New("FRP Linux executable size mismatch")
		}
		return os.Rename(target+".tmp", target)
	}
	return errors.New("frpc was not found in FRP Linux archive")
}

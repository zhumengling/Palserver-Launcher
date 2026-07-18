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

func launcherReleaseAssetMatches(name string) bool {
	return strings.EqualFold(name, "palserver-agent-linux-amd64.tar.gz")
}

func launcherReleaseAssetMissingMessage() string {
	return "Linux amd64 agent bundle was not found in the release"
}

func launcherUpdatePaths(base, version string) (download, helper string, err error) {
	normalized, err := normalizeLauncherVersion(version)
	if err != nil {
		return "", "", err
	}
	root := filepath.Join(base, "updates", normalized)
	return filepath.Join(root, "palserver-agent-linux-amd64.tar.gz"), filepath.Join(root, "pal-agent-updater"), nil
}

func prepareLauncherReplacement(downloadPath string) (string, error) {
	file, err := os.Open(downloadPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	replacement := filepath.Join(filepath.Dir(downloadPath), "pal-agent-replacement")
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return "", nextErr
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(filepath.Clean(header.Name)) != "pal-agent" {
			continue
		}
		if header.Size < 1 || header.Size > 256<<20 {
			return "", errors.New("Linux agent replacement has an invalid size")
		}
		output, err := os.OpenFile(replacement+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			return "", err
		}
		written, copyErr := io.Copy(output, io.LimitReader(tarReader, header.Size+1))
		syncErr := output.Sync()
		closeErr := output.Close()
		if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
			return "", err
		}
		if written != header.Size {
			return "", errors.New("Linux agent replacement size mismatch")
		}
		_ = os.Remove(replacement)
		if err := os.Rename(replacement+".tmp", replacement); err != nil {
			return "", err
		}
		return replacement, nil
	}
	return "", errors.New("pal-agent was not found in Linux release bundle")
}

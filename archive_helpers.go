package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func verifySHA256(data []byte, expected string) error {
	expected = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(expected)), "sha256:")
	if expected == "" {
		return errors.New("release asset has no SHA-256 digest")
	}
	actual := sha256.Sum256(data)
	if hex.EncodeToString(actual[:]) != expected {
		return errors.New("download checksum mismatch")
	}
	return nil
}

func verifySHA256File(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	expected = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(expected)), "sha256:")
	if expected == "" || hex.EncodeToString(hash.Sum(nil)) != expected {
		return errors.New("download checksum mismatch")
	}
	return nil
}

func extractNamedExecutable(archivePath, destination, name string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if !strings.EqualFold(filepath.Base(file.Name), name) {
			continue
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		temporary := filepath.Join(destination, name+".tmp")
		out, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		inCloseErr := in.Close()
		closeErr := out.Close()
		if joined := errors.Join(copyErr, inCloseErr, closeErr); joined != nil {
			return joined
		}
		return os.Rename(temporary, filepath.Join(destination, name))
	}
	return errors.New(name + " was not found in release archive")
}

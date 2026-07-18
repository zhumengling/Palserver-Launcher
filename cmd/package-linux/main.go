package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type bundleEntry struct {
	Source string
	Name   string
	Mode   int64
}

type packageOptions struct {
	Agent   string
	Install string
	Remove  string
	Service string
	Readme  string
	Output  string
}

func bundleEntries(options packageOptions) []bundleEntry {
	return []bundleEntry{
		{Source: options.Agent, Name: "pal-agent", Mode: 0o755},
		{Source: options.Install, Name: "install.sh", Mode: 0o755},
		{Source: options.Remove, Name: "uninstall.sh", Mode: 0o755},
		{Source: options.Service, Name: "palserver-agent.service", Mode: 0o644},
		{Source: options.Readme, Name: "README-linux.md", Mode: 0o644},
	}
}

func validateBundleEntry(entry bundleEntry) (os.FileInfo, error) {
	if entry.Source == "" || entry.Name == "" || filepath.Base(entry.Name) != entry.Name {
		return nil, errors.New("invalid Linux bundle entry")
	}
	info, err := os.Lstat(entry.Source)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Source, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Linux bundle source is not a regular file: %s", entry.Source)
	}
	return info, nil
}

func writeBundleArchive(writer io.Writer, entries []bundleEntry) error {
	gzipWriter, err := gzip.NewWriterLevel(writer, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 3
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		info, err := validateBundleEntry(entry)
		if err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return err
		}
		header := &tar.Header{
			Name: entry.Name, Mode: entry.Mode, Size: info.Size(), Typeflag: tar.TypeReg,
			ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Time{}, ChangeTime: time.Time{},
			Uid: 0, Gid: 0, Uname: "root", Gname: "root", Format: tar.FormatPAX,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return err
		}
		file, err := os.Open(entry.Source)
		if err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return err
		}
		written, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return err
		}
		if written != info.Size() {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return fmt.Errorf("Linux bundle source changed while packaging: %s", entry.Source)
		}
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return err
	}
	return gzipWriter.Close()
}

func fileDigest(path string) (string, error) {
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

func replaceBundleFile(temporary, output string) error {
	backup := output + ".previous"
	_ = os.Remove(backup)
	hadPrevious := false
	if _, err := os.Stat(output); err == nil {
		if err := os.Rename(output, backup); err != nil {
			return fmt.Errorf("move previous Linux bundle: %w", err)
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(temporary, output); err != nil {
		if hadPrevious {
			_ = os.Rename(backup, output)
		}
		return err
	}
	if hadPrevious {
		_ = os.Remove(backup)
	}
	return nil
}

func buildLinuxBundle(options packageOptions) error {
	if options.Output == "" {
		return errors.New("Linux bundle output path is required")
	}
	entries := bundleEntries(options)
	for _, entry := range entries {
		if _, err := validateBundleEntry(entry); err != nil {
			return err
		}
	}
	output, err := filepath.Abs(options.Output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".palserver-linux-bundle-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := writeBundleArchive(temporary, entries); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceBundleFile(temporaryPath, output); err != nil {
		return err
	}
	digest, err := fileDigest(output)
	if err != nil {
		return err
	}
	checksum := fmt.Sprintf("%s  %s\n", digest, filepath.Base(output))
	return os.WriteFile(output+".sha256", []byte(checksum), 0o644)
}

func main() {
	options := packageOptions{}
	flag.StringVar(&options.Agent, "agent", filepath.Join("build", "bin", "pal-agent-linux-amd64"), "compiled Linux amd64 Agent")
	flag.StringVar(&options.Install, "install", filepath.Join("deploy", "linux", "install.sh"), "Linux install script")
	flag.StringVar(&options.Remove, "uninstall", filepath.Join("deploy", "linux", "uninstall.sh"), "Linux uninstall script")
	flag.StringVar(&options.Service, "service", filepath.Join("deploy", "linux", "palserver-agent.service"), "systemd service file")
	flag.StringVar(&options.Readme, "readme", filepath.Join("docs", "linux-server.md"), "Linux deployment documentation")
	flag.StringVar(&options.Output, "output", filepath.Join("build", "palserver-agent-linux-amd64.tar.gz"), "output tar.gz path")
	flag.Parse()
	if err := buildLinuxBundle(options); err != nil {
		fmt.Fprintln(os.Stderr, "package Linux Agent:", err)
		os.Exit(1)
	}
	fmt.Println(options.Output)
}

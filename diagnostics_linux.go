//go:build linux

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func firewallDiagnosticName() string { return "Linux 防火墙" }

func serverStorageMediaType(root string) (string, error) {
	output, err := exec.Command("findmnt", "-no", "SOURCE,FSTYPE", "-T", root).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("findmnt: %w", err)
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return "", errors.New("storage device was not found")
	}
	return detail + "（请确认底层设备为 SSD/NVMe）", nil
}

func linuxUDPTableContains(path string, port int) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		parts := strings.Split(fields[1], ":")
		if len(parts) != 2 {
			continue
		}
		value, _ := strconv.ParseInt(parts[1], 16, 32)
		if int(value) == port {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func udpPortListening(port int) (bool, error) {
	listening, err := linuxUDPTableContains("/proc/net/udp", port)
	if err == nil && listening {
		return true, nil
	}
	listening6, err6 := linuxUDPTableContains("/proc/net/udp6", port)
	if err6 == nil {
		return listening6, nil
	}
	return false, errors.Join(err, err6)
}

func firewallAllowsUDP(port int) (bool, error) {
	if path, err := exec.LookPath("ufw"); err == nil {
		output, commandErr := exec.Command(path, "status").CombinedOutput()
		if commandErr != nil {
			return false, commandErr
		}
		text := strings.ToLower(string(output))
		return strings.Contains(text, fmt.Sprintf("%d/udp", port)) && strings.Contains(text, "allow"), nil
	}
	if path, err := exec.LookPath("nft"); err == nil {
		output, commandErr := exec.Command(path, "list", "ruleset").CombinedOutput()
		if commandErr != nil {
			return false, commandErr
		}
		text := strings.ToLower(string(output))
		return strings.Contains(text, "udp dport "+strconv.Itoa(port)) && (strings.Contains(text, "accept") || strings.Contains(text, "allow")), nil
	}
	return false, errors.New("未检测到 ufw 或 nftables")
}

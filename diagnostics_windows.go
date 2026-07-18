//go:build windows

package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func firewallDiagnosticName() string { return "Windows 防火墙" }

func serverStorageMediaType(root string) (string, error) {
	volume := strings.TrimSuffix(filepath.VolumeName(filepath.Clean(root)), `\`)
	if volume == "" {
		return "", errors.New("server drive was not found")
	}
	query := `$letter='` + strings.TrimSuffix(volume, `:`) + `'; $disk=Get-Partition -DriveLetter $letter -ErrorAction Stop | Get-Disk; ($disk.BusType.ToString() + ' / ' + $disk.MediaType.ToString())`
	output, err := newHiddenPowerShell(query).Output()
	return strings.TrimSpace(string(output)), err
}

func udpPortListening(port int) (bool, error) {
	query := fmt.Sprintf(`[bool](Get-NetUDPEndpoint -LocalPort %d -ErrorAction SilentlyContinue | Select-Object -First 1) | ConvertTo-Json -Compress`, port)
	output, err := newHiddenPowerShell(query).Output()
	return strings.EqualFold(strings.TrimSpace(string(output)), "true"), err
}

func firewallAllowsUDP(port int) (bool, error) {
	query := fmt.Sprintf(`$rules=Get-NetFirewallRule -Enabled True -Direction Inbound -Action Allow -ErrorAction Stop; [bool]($rules | Get-NetFirewallPortFilter | Where-Object { $_.Protocol -eq 'UDP' -and ($_.LocalPort -eq 'Any' -or $_.LocalPort -eq '%d') } | Select-Object -First 1) | ConvertTo-Json -Compress`, port)
	output, err := newHiddenPowerShell(query).Output()
	return strings.EqualFold(strings.TrimSpace(string(output)), "true"), err
}

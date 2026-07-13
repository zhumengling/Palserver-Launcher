package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var frpProxyNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var frpHostnamePattern = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

type frpRuntimeClaim struct {
	ServerID   string
	ServerName string
	Settings   FrpSettings
}

type frpRemoteBinding struct {
	Protocol string
	Port     int
}

func frpRemoteBindings(settings FrpSettings) []frpRemoteBinding {
	bindings := []frpRemoteBinding{{Protocol: "udp", Port: settings.RemoteGamePort}}
	if settings.QueryEnabled {
		bindings = append(bindings, frpRemoteBinding{Protocol: "udp", Port: settings.RemoteQueryPort})
	}
	if settings.RCONEnabled {
		bindings = append(bindings, frpRemoteBinding{Protocol: "tcp", Port: settings.RemoteRCONPort})
	}
	if settings.RESTEnabled {
		bindings = append(bindings, frpRemoteBinding{Protocol: "tcp", Port: settings.RemoteRESTPort})
	}
	return bindings
}

func sameFrps(left, right FrpSettings) bool {
	return strings.EqualFold(strings.TrimSpace(left.ServerAddress), strings.TrimSpace(right.ServerAddress)) && left.ServerPort == right.ServerPort
}

func validateFrpRuntimeClaim(candidate frpRuntimeClaim, running []frpRuntimeClaim) error {
	for _, active := range running {
		if active.ServerID == candidate.ServerID || !sameFrps(candidate.Settings, active.Settings) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(candidate.Settings.ProxyName), strings.TrimSpace(active.Settings.ProxyName)) {
			return fmt.Errorf("服务器“%s”正在同一 FRPS 使用代理名称 %s，请更换代理名称或先停止它", active.ServerName, candidate.Settings.ProxyName)
		}
		for _, wanted := range frpRemoteBindings(candidate.Settings) {
			for _, occupied := range frpRemoteBindings(active.Settings) {
				if wanted.Protocol == occupied.Protocol && wanted.Port == occupied.Port {
					return fmt.Errorf("服务器“%s”正在同一 FRPS 使用远程端口 %d/%s，请停止它或更换端口", active.ServerName, wanted.Port, strings.ToUpper(wanted.Protocol))
				}
			}
		}
	}
	return nil
}

func defaultFrpSettings(instance ServerInstance) FrpSettings {
	suffix := strings.TrimPrefix(instance.ID, "srv-")
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix == "" {
		suffix = "server"
	}
	return FrpSettings{
		ServerID: instance.ID, ServerPort: 7000, ProxyName: "pal-" + suffix,
		RemoteGamePort: instance.PublicPort, RemoteQueryPort: instance.QueryPort,
		RemoteRCONPort: instance.RCONPort, RemoteRESTPort: instance.RESTPort,
	}
}

func frpSettingsFromConfig(config FrpConfig) FrpSettings {
	return FrpSettings{
		ServerID: config.ServerID, ServerAddress: config.ServerAddress, ServerPort: config.ServerPort,
		TokenConfigured: config.EncryptedToken != "", ProxyName: config.ProxyName, RemoteGamePort: config.RemoteGamePort,
		QueryEnabled: config.QueryEnabled, RemoteQueryPort: config.RemoteQueryPort,
		RCONEnabled: config.RCONEnabled, RemoteRCONPort: config.RemoteRCONPort,
		RESTEnabled: config.RESTEnabled, RemoteRESTPort: config.RemoteRESTPort, AutoStart: config.AutoStart,
	}
}

func validatePort(name string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", name)
	}
	return nil
}

func validateFrpSettings(settings FrpSettings) error {
	address := strings.TrimSpace(settings.ServerAddress)
	if address == "" || (net.ParseIP(address) == nil && !frpHostnamePattern.MatchString(address)) {
		return errors.New("FRP server address is invalid")
	}
	if err := validatePort("FRP server port", settings.ServerPort); err != nil {
		return err
	}
	if !frpProxyNamePattern.MatchString(settings.ProxyName) {
		return errors.New("proxy name may only contain letters, numbers, underscores, and hyphens")
	}
	if err := validatePort("remote game port", settings.RemoteGamePort); err != nil {
		return err
	}
	udpPorts := map[int]bool{settings.RemoteGamePort: true}
	if settings.QueryEnabled {
		if err := validatePort("remote query port", settings.RemoteQueryPort); err != nil {
			return err
		}
		if udpPorts[settings.RemoteQueryPort] {
			return errors.New("UDP remote ports must be unique")
		}
	}
	tcpPorts := map[int]bool{}
	if settings.RCONEnabled {
		if err := validatePort("remote RCON port", settings.RemoteRCONPort); err != nil {
			return err
		}
		tcpPorts[settings.RemoteRCONPort] = true
	}
	if settings.RESTEnabled {
		if err := validatePort("remote REST port", settings.RemoteRESTPort); err != nil {
			return err
		}
		if tcpPorts[settings.RemoteRESTPort] {
			return errors.New("TCP remote ports must be unique")
		}
	}
	return nil
}

func frpProxyBlock(name, protocol string, localPort, remotePort int) string {
	return fmt.Sprintf("\n[[proxies]]\nname = %s\ntype = %s\nlocalIP = \"127.0.0.1\"\nlocalPort = %d\nremotePort = %d\n", strconv.Quote(name), strconv.Quote(protocol), localPort, remotePort)
}

func buildFrpcConfig(instance ServerInstance, settings FrpSettings, token string) (string, error) {
	if err := validateFrpSettings(settings); err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", errors.New("FRP authentication token is required")
	}
	var content strings.Builder
	fmt.Fprintf(&content, "serverAddr = %s\nserverPort = %d\nloginFailExit = true\nauth.method = \"token\"\nauth.token = %s\n", strconv.Quote(strings.TrimSpace(settings.ServerAddress)), settings.ServerPort, strconv.Quote(token))
	content.WriteString(frpProxyBlock(settings.ProxyName+"-game", "udp", instance.PublicPort, settings.RemoteGamePort))
	if settings.QueryEnabled {
		content.WriteString(frpProxyBlock(settings.ProxyName+"-query", "udp", instance.QueryPort, settings.RemoteQueryPort))
	}
	if settings.RCONEnabled {
		content.WriteString(frpProxyBlock(settings.ProxyName+"-rcon", "tcp", instance.RCONPort, settings.RemoteRCONPort))
	}
	if settings.RESTEnabled {
		content.WriteString(frpProxyBlock(settings.ProxyName+"-rest", "tcp", instance.RESTPort, settings.RemoteRESTPort))
	}
	return content.String(), nil
}

func selectFRPReleaseAsset(release githubRelease) (name, url, digest string, err error) {
	for _, asset := range release.Assets {
		lower := strings.ToLower(asset.Name)
		if strings.HasSuffix(lower, "windows_amd64.zip") && strings.HasPrefix(lower, "frp_") {
			return asset.Name, asset.BrowserDownloadURL, asset.Digest, nil
		}
	}
	return "", "", "", errors.New("FRP Windows amd64 release asset was not found")
}

func frpRoot() (string, error) {
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(base, "runtime", "frp")
	return root, os.MkdirAll(root, 0o755)
}

func frpExecutable() (string, error) {
	root, err := frpRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "frpc.exe"), nil
}

func frpServerRoot(serverID string) (string, error) {
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(base, "frp", serverID)
	return root, os.MkdirAll(root, 0o700)
}

func (a *App) GetFrpSettings(serverID string) (FrpSettings, error) {
	instance, err := a.store.Find(serverID)
	if err != nil {
		return FrpSettings{}, err
	}
	config, ok := a.store.FrpConfig(serverID)
	if !ok {
		return defaultFrpSettings(instance), nil
	}
	return frpSettingsFromConfig(config), nil
}

func (a *App) SaveFrpSettings(settings FrpSettings, token string) (FrpSettings, error) {
	if _, err := a.store.Find(settings.ServerID); err != nil {
		return FrpSettings{}, err
	}
	if err := validateFrpSettings(settings); err != nil {
		return FrpSettings{}, err
	}
	existing, _ := a.store.FrpConfig(settings.ServerID)
	encryptedToken := existing.EncryptedToken
	if strings.TrimSpace(token) != "" {
		var err error
		encryptedToken, err = protectSecret(strings.TrimSpace(token))
		if err != nil {
			return FrpSettings{}, err
		}
	}
	config := FrpConfig{
		ServerID: settings.ServerID, ServerAddress: strings.TrimSpace(settings.ServerAddress), ServerPort: settings.ServerPort,
		EncryptedToken: encryptedToken, ProxyName: settings.ProxyName, RemoteGamePort: settings.RemoteGamePort,
		QueryEnabled: settings.QueryEnabled, RemoteQueryPort: settings.RemoteQueryPort,
		RCONEnabled: settings.RCONEnabled, RemoteRCONPort: settings.RemoteRCONPort,
		RESTEnabled: settings.RESTEnabled, RemoteRESTPort: settings.RemoteRESTPort, AutoStart: settings.AutoStart,
	}
	if err := a.store.SaveFrpConfig(config); err != nil {
		return FrpSettings{}, err
	}
	return frpSettingsFromConfig(config), nil
}

func (a *App) GetFrpStatus(serverID string) (FrpStatus, error) {
	settings, err := a.GetFrpSettings(serverID)
	if err != nil {
		return FrpStatus{}, err
	}
	executable, err := frpExecutable()
	if err != nil {
		return FrpStatus{}, err
	}
	status := FrpStatus{Path: executable, Settings: settings}
	if _, err := os.Stat(executable); err == nil {
		status.Installed = true
		root := filepath.Dir(executable)
		if data, readErr := os.ReadFile(filepath.Join(root, "version.txt")); readErr == nil {
			status.Version = strings.TrimSpace(string(data))
		}
	}
	a.processMu.Lock()
	if command := a.frpProcesses[serverID]; command != nil && command.Process != nil && command.ProcessState == nil {
		status.Running = true
		status.PID = command.Process.Pid
	}
	a.processMu.Unlock()
	return status, nil
}

func (a *App) InstallFrp() (FrpStatus, error) {
	request, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/fatedier/frp/releases/latest", nil)
	request.Header.Set("User-Agent", "palserver-launcher")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return FrpStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return FrpStatus{}, fmt.Errorf("FRP release lookup failed: %s", response.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return FrpStatus{}, err
	}
	_, url, digest, err := selectFRPReleaseAsset(release)
	if err != nil {
		return FrpStatus{}, err
	}
	download, err := (&http.Client{Timeout: 10 * time.Minute}).Get(url)
	if err != nil {
		return FrpStatus{}, err
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusOK {
		return FrpStatus{}, fmt.Errorf("FRP download failed: %s", download.Status)
	}
	temporary, err := os.CreateTemp("", "palserver-frp-*.zip")
	if err != nil {
		return FrpStatus{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, download.Body); err != nil {
		_ = temporary.Close()
		return FrpStatus{}, err
	}
	if err := temporary.Close(); err != nil {
		return FrpStatus{}, err
	}
	data, err := os.ReadFile(temporaryPath)
	if err != nil {
		return FrpStatus{}, err
	}
	if err := verifySHA256(data, digest); err != nil {
		return FrpStatus{}, err
	}
	root, err := frpRoot()
	if err != nil {
		return FrpStatus{}, err
	}
	if err := extractNamedExecutable(temporaryPath, root, "frpc.exe"); err != nil {
		return FrpStatus{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "version.txt"), []byte(release.TagName), 0o600); err != nil {
		return FrpStatus{}, err
	}
	return FrpStatus{Installed: true, Version: release.TagName, Path: filepath.Join(root, "frpc.exe")}, nil
}

func (a *App) StartFrp(serverID string) error {
	instance, err := a.store.Find(serverID)
	if err != nil {
		return err
	}
	config, ok := a.store.FrpConfig(serverID)
	if !ok {
		return errors.New("save FRP settings before starting the client")
	}
	settings := frpSettingsFromConfig(config)
	if err := validateFrpSettings(settings); err != nil {
		return err
	}
	token, err := unprotectSecret(config.EncryptedToken)
	if err != nil {
		return errors.New("FRP authentication token is not configured")
	}
	content, err := buildFrpcConfig(instance, settings, token)
	if err != nil {
		return err
	}
	executable, err := frpExecutable()
	if err != nil {
		return err
	}
	if _, err := os.Stat(executable); err != nil {
		return errors.New("FRP client is not installed")
	}
	claim := frpRuntimeClaim{ServerID: serverID, ServerName: instance.Name, Settings: settings}
	a.processMu.Lock()
	if running := a.frpProcesses[serverID]; running != nil && running.Process != nil && running.ProcessState == nil {
		a.processMu.Unlock()
		return nil
	}
	if _, starting := a.frpClaims[serverID]; starting {
		a.processMu.Unlock()
		return nil
	}
	runningClaims := make([]frpRuntimeClaim, 0, len(a.frpClaims))
	for id, active := range a.frpClaims {
		if id != serverID {
			runningClaims = append(runningClaims, active)
		}
	}
	if err := validateFrpRuntimeClaim(claim, runningClaims); err != nil {
		a.processMu.Unlock()
		return err
	}
	// Reserve the FRPS bindings before creating the process so two simultaneous
	// start requests cannot pass validation with the same remote port.
	a.frpClaims[serverID] = claim
	a.processMu.Unlock()
	keepClaim := false
	defer func() {
		if keepClaim {
			return
		}
		a.processMu.Lock()
		if active, ok := a.frpClaims[serverID]; ok && active.ServerID == claim.ServerID {
			delete(a.frpClaims, serverID)
		}
		a.processMu.Unlock()
	}()
	root, err := frpServerRoot(serverID)
	if err != nil {
		return err
	}
	configPath := filepath.Join(root, "frpc.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(root, "frpc.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	command := exec.Command(executable, "-c", configPath)
	command.Dir = filepath.Dir(executable)
	command.SysProcAttr = hiddenServerSysProcAttr()
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	a.processMu.Lock()
	a.frpProcesses[serverID] = command
	a.processMu.Unlock()
	keepClaim = true
	go func() {
		_ = command.Wait()
		_ = logFile.Close()
		a.processMu.Lock()
		if a.frpProcesses[serverID] == command {
			delete(a.frpProcesses, serverID)
			delete(a.frpClaims, serverID)
		}
		a.processMu.Unlock()
		_ = os.Remove(configPath)
	}()
	return nil
}

func (a *App) StopFrp(serverID string) error {
	a.processMu.Lock()
	command := a.frpProcesses[serverID]
	a.processMu.Unlock()
	if command == nil || command.Process == nil {
		return nil
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T").Run()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.processMu.Lock()
		running := a.frpProcesses[serverID] == command
		a.processMu.Unlock()
		if !running {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return command.Process.Kill()
}

func (a *App) GetFrpLog(serverID string, lines int) (string, error) {
	root, err := frpServerRoot(serverID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(root, "frpc.log"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(data), "\n")
	if lines > 0 && len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n"), nil
}

func (a *App) startAutomaticFrpClients() {
	for _, config := range a.store.Snapshot().FrpConfigs {
		if config.AutoStart {
			_ = a.StartFrp(config.ServerID)
		}
	}
}

func (a *App) stopAllFrpClients() {
	a.processMu.Lock()
	ids := make([]string, 0, len(a.frpProcesses))
	for id := range a.frpProcesses {
		ids = append(ids, id)
	}
	a.processMu.Unlock()
	for _, id := range ids {
		_ = a.StopFrp(id)
	}
}

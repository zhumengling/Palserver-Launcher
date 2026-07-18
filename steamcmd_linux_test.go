//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func linuxSteamCMDArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range entries {
		data := []byte(content)
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o700, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func TestLinuxEnsureSteamCMDDownloadsOfficialArchiveLayout(t *testing.T) {
	archive := linuxSteamCMDArchive(t, map[string]string{
		"steamcmd.sh":          "#!/bin/sh\nexit 0\n",
		"linux32/steamcmd":     "runtime",
		"package/steam_client": "client",
	})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/gzip")
		writer.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		_, _ = writer.Write(archive)
	}))
	defer server.Close()
	root := t.TempDir()
	path := filepath.Join(root, "steamcmd.sh")
	progress := make([]int, 0)
	if err := ensureLinuxSteamCMDFrom(path, server.URL, server.Client(), func(_ string, percent int) { progress = append(progress, percent) }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("steamcmd executable = %#v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(root, "linux32", "steamcmd")); err != nil {
		t.Fatal(err)
	}
	if len(progress) != 2 || progress[0] != 10 || progress[1] != 18 {
		t.Fatalf("bootstrap progress = %v", progress)
	}
	if err := ensureLinuxSteamCMDFrom(path, server.URL, server.Client(), nil); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("existing SteamCMD triggered %d downloads", requests.Load())
	}
}

func TestLinuxEnsureSteamCMDRejectsArchiveTraversal(t *testing.T) {
	archive := linuxSteamCMDArchive(t, map[string]string{"../escape": "unsafe"})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { _, _ = writer.Write(archive) }))
	defer server.Close()
	root := t.TempDir()
	err := ensureLinuxSteamCMDFrom(filepath.Join(root, "steamcmd.sh"), server.URL, server.Client(), nil)
	if err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("archive traversal error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape")); !os.IsNotExist(err) {
		t.Fatalf("archive wrote outside destination: %v", err)
	}
}

type linuxQuickSetupRuntime struct{}

func (linuxQuickSetupRuntime) FindServerProcess(ServerInstance) (serverProcessSnapshot, bool, error) {
	return serverProcessSnapshot{}, false, nil
}
func (linuxQuickSetupRuntime) HostResources() (HostResources, error) {
	return HostResources{MemoryTotalMB: 32 * 1024}, nil
}
func (linuxQuickSetupRuntime) TCPListenerOwner(int) (int, bool, error) { return 0, false, nil }

func writeFakeLinuxSteamCMD(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
if [ "$1" != "+runscript" ]; then
  echo "unexpected arguments" >&2
  exit 2
fi
install_root=$(sed -n 's/^force_install_dir "\(.*\)"$/\1/p' "$2")
if [ -z "$install_root" ]; then
  echo "missing force_install_dir" >&2
  exit 3
fi
mkdir -p "$install_root/Pal/Binaries/Linux"
cat > "$install_root/PalServer.sh" <<'EOF'
#!/bin/sh
exec "$(dirname "$0")/Pal/Binaries/Linux/PalServer-Linux-Shipping" "$@"
EOF
cat > "$install_root/Pal/Binaries/Linux/PalServer-Linux-Shipping" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 700 "$install_root/PalServer.sh" "$install_root/Pal/Binaries/Linux/PalServer-Linux-Shipping"
cat > "$install_root/DefaultPalWorldSettings.ini" <<'EOF'
[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Default Palworld Server",PublicPort=8211)
EOF
echo "progress: 100"
echo "Success! App '2394010' fully installed."
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxQuickSetupInstallsServerAndWritesLinuxConfiguration(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "agent-data")
	installRoot := filepath.Join(root, "servers", "linux-e2e")
	t.Setenv("PALSERVER_LAUNCHER_HOME", data)
	t.Setenv("PALSERVER_ALLOWED_SERVER_ROOTS", root)
	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = linuxQuickSetupRuntime{}
	defer func() { defaultProcessRuntime = previousRuntime }()
	base, err := appDataDir()
	if err != nil {
		t.Fatal(err)
	}
	instanceTemplate := buildManagedInstanceAt(base, "Linux E2E", installRoot)
	writeFakeLinuxSteamCMD(t, instanceTemplate.SteamCMDPath)
	app := NewApp()
	instance, err := app.QuickSetup("Linux E2E", installRoot)
	if err != nil {
		t.Fatal(err)
	}
	if instance.ID == "" || instance.Executable != filepath.Join(installRoot, "PalServer.sh") {
		t.Fatalf("installed instance = %#v", instance)
	}
	if err := validateInstalledServerExecutable(instance); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(installRoot, "Pal", "Saved", "Config", "LinuxServer", "PalWorldSettings.ini")
	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`ServerName="Linux E2E"`, "RESTAPIEnabled=True", "RCONEnabled=True", "bIsMultiplay=True"} {
		if !bytes.Contains(settings, []byte(expected)) {
			t.Fatalf("Linux settings missing %q: %s", expected, settings)
		}
	}
	if stored, err := app.store.Find(instance.ID); err != nil || stored.RootPath != installRoot {
		t.Fatalf("stored instance = %#v, %v", stored, err)
	}
	logData, err := os.ReadFile(filepath.Join(installRoot, "launcher-logs", "steamcmd.log"))
	if err != nil || !bytes.Contains(logData, []byte("2394010")) {
		t.Fatalf("SteamCMD log = %q, %v", logData, err)
	}
	if _, err := os.Stat(filepath.Join(installRoot, "Pal", "Binaries", "Linux", "PalDefender.dll")); !os.IsNotExist(err) {
		t.Fatalf("Linux quick setup installed a Windows plugin: %v", err)
	}
}

func TestLinuxQuickSetupWithoutPathUsesAgentManagedServerDirectory(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "agent-data")
	t.Setenv("PALSERVER_LAUNCHER_HOME", data)
	t.Setenv("PALSERVER_ALLOWED_SERVER_ROOTS", filepath.Join(data, "servers"))
	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = linuxQuickSetupRuntime{}
	defer func() { defaultProcessRuntime = previousRuntime }()
	base, err := appDataDir()
	if err != nil {
		t.Fatal(err)
	}
	writeFakeLinuxSteamCMD(t, defaultSteamCMDExecutable(base))
	app := NewApp()
	instance, err := app.QuickSetup("Linux Auto", "")
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(data, "servers", "Linux-Auto")
	if instance.RootPath != wantRoot || instance.Executable != filepath.Join(wantRoot, "PalServer.sh") {
		t.Fatalf("automatic Linux instance = %#v", instance)
	}
	if instance.SteamCMDPath != defaultSteamCMDExecutable(base) {
		t.Fatalf("automatic Linux SteamCMD path = %q", instance.SteamCMDPath)
	}
}

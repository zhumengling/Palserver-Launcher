package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func findSaveGameRoot(serverRoot string) (string, error) {
	zero := filepath.Join(serverRoot, "Pal", "Saved", "SaveGames", "0")
	entries, err := os.ReadDir(zero)
	if err != nil {
		return "", err
	}
	directories := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, entry.Name())
		}
	}
	if len(directories) == 0 {
		return "", errors.New("world save directory was not found")
	}
	sort.Strings(directories)
	return filepath.Join(zero, directories[0]), nil
}

func discoverOfficialBackups(saveRoot string) ([]BackupEntry, error) {
	root := filepath.Join(saveRoot, "backup", "world")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []BackupEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]BackupEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, _ := entry.Info()
		path := filepath.Join(root, entry.Name())
		result = append(result, BackupEntry{Name: entry.Name(), Path: path, CreatedAt: info.ModTime().UnixMilli(), Size: dirSize(path)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}

func parseBanList(content string) []string {
	lines := strings.FieldsFunc(content, func(r rune) bool { return r == '\r' || r == '\n' })
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if value := strings.TrimSpace(line); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func shouldExportPak(name string) bool {
	switch strings.ToLower(name) {
	case "pal-windowsserver.pak", "pal-windowsserver.ucas", "pal-windowsserver.utoc", "global.ucas", "global.utoc":
		return false
	default:
		return true
	}
}

func (a *App) Announce(id, message string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return errors.New("announcement message is required")
	}
	_, err = restPost(instance, "/announce", map[string]any{"message": message})
	return err
}

func (a *App) ListBans(id string) ([]string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(instance.RootPath, "Pal", "Saved", "SaveGames", "banlist.txt")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseBanList(string(data)), nil
}

func (a *App) UnbanPlayer(id, userID string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("user id is required")
	}
	_, err = restPost(instance, "/unban", map[string]any{"userid": userID})
	return err
}

func (a *App) GetServerPaths(id string) (map[string]string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	saveRoot, _ := findSaveGameRoot(instance.RootPath)
	return map[string]string{
		"server":      instance.RootPath,
		"saved":       filepath.Join(instance.RootPath, "Pal", "Saved"),
		"world":       saveRoot,
		"config":      filepath.Join(instance.RootPath, "Pal", "Saved", "Config", "WindowsServer"),
		"logs":        filepath.Join(win64Path(instance), "PalDefender", "Logs"),
		"paldefender": filepath.Join(win64Path(instance), "PalDefender", "Config.json"),
	}, nil
}

func (a *App) OpenServerPath(id, kind string) error {
	paths, err := a.GetServerPaths(id)
	if err != nil {
		return err
	}
	path, ok := paths[kind]
	if !ok || path == "" {
		return errors.New("server path is not available")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return a.OpenPath(path)
}

func (a *App) ListOfficialBackups(id string) ([]BackupEntry, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	saveRoot, err := findSaveGameRoot(instance.RootPath)
	if err != nil {
		return []BackupEntry{}, nil
	}
	return discoverOfficialBackups(saveRoot)
}

func (a *App) OpenOfficialBackup(id, backupPath string) error {
	backups, err := a.ListOfficialBackups(id)
	if err != nil {
		return err
	}
	for _, backup := range backups {
		if filepath.Clean(backup.Path) == filepath.Clean(backupPath) {
			return a.OpenPath(backup.Path)
		}
	}
	return errors.New("official backup was not found")
}

func (a *App) ClearSteamCMDCache() error {
	base, err := appDataDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(base, "runtime", "steamcmd", "steamapps"))
}

func (a *App) ExportClientMods(id string) (string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return "", err
	}
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	destination := filepath.Join(base, "exports", id, "clientside-mods")
	if err := os.RemoveAll(destination); err != nil {
		return "", err
	}
	luaSource := modRoots(instance)["lua"]
	if _, err := os.Stat(luaSource); err == nil {
		if err := copyTree(luaSource, filepath.Join(destination, "Pal", "Binaries", "Win64", "Mods")); err != nil {
			return "", err
		}
	}
	pakSource := modRoots(instance)["pak"]
	pakDestination := filepath.Join(destination, "Pal", "Content", "Paks")
	if entries, err := os.ReadDir(pakSource); err == nil {
		for _, entry := range entries {
			if !shouldExportPak(entry.Name()) {
				continue
			}
			source := filepath.Join(pakSource, entry.Name())
			target := filepath.Join(pakDestination, entry.Name())
			if entry.IsDir() {
				err = copyTree(source, target)
			} else {
				err = copyFile(source, target)
			}
			if err != nil {
				return "", err
			}
		}
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", err
	}
	_ = a.OpenPath(destination)
	return destination, nil
}

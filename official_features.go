package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func flushWorldSaveBeforeBackup(instance ServerInstance, running bool) error {
	if !running {
		return nil
	}
	_, err := restPost(instance, "/save", nil)
	return err
}

func (a *App) SaveWorld(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, err := serverStatus(instance)
	if err != nil {
		return err
	}
	if !status.Running {
		return errors.New("server is not running")
	}
	return flushWorldSaveBeforeBackup(instance, true)
}

func waitForSaveFilesStable(root string) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	snapshot := func() (string, error) {
		parts := []string{}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			parts = append(parts, path+":"+strconv.FormatInt(info.Size(), 10)+":"+strconv.FormatInt(info.ModTime().UnixNano(), 10))
			return nil
		})
		sort.Strings(parts)
		return strings.Join(parts, "|"), err
	}
	previous, err := snapshot()
	if err != nil {
		return err
	}
	stable, deadline := 0, time.Now().Add(5*time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		current, err := snapshot()
		if err != nil {
			return err
		}
		if current == previous {
			stable++
		} else {
			previous, stable = current, 0
		}
		if stable >= 2 {
			return nil
		}
	}
	return errors.New("save files did not become stable")
}

func officialWorkshopRoot(instance ServerInstance) string {
	return filepath.Join(instance.RootPath, "Mods", "Workshop")
}

func palModSettingsPath(instance ServerInstance) string {
	return filepath.Join(instance.RootPath, "Mods", "PalModSettings.ini")
}

func parseOfficialWorkshopMod(path string) (OfficialWorkshopMod, error) {
	data, err := os.ReadFile(filepath.Join(path, "Info.json"))
	if err != nil {
		return OfficialWorkshopMod{}, err
	}
	var raw struct {
		PackageName  string          `json:"PackageName"`
		Version      string          `json:"Version"`
		Dependencies json.RawMessage `json:"Dependencies"`
		InstallRules []struct {
			IsServer bool `json:"IsServer"`
		} `json:"InstallRules"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return OfficialWorkshopMod{}, err
	}
	if strings.TrimSpace(raw.PackageName) == "" {
		return OfficialWorkshopMod{}, errors.New("Info.json does not contain PackageName")
	}
	dependencies := parseWorkshopDependencies(raw.Dependencies)
	serverCompatible := false
	for _, rule := range raw.InstallRules {
		serverCompatible = serverCompatible || rule.IsServer
	}
	return OfficialWorkshopMod{Name: filepath.Base(path), PackageName: raw.PackageName, Version: raw.Version, Dependencies: dependencies, ServerCompatible: serverCompatible, Path: path, Size: dirSize(path)}, nil
}

func parseWorkshopDependencies(data json.RawMessage) []string {
	var names []string
	_ = json.Unmarshal(data, &names)
	if len(names) == 0 && len(data) > 0 {
		var objects []struct {
			PackageName string `json:"PackageName"`
		}
		if json.Unmarshal(data, &objects) == nil {
			for _, item := range objects {
				if item.PackageName != "" {
					names = append(names, item.PackageName)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

func mergePalModSettings(content string, active []string, workshopRoot string) string {
	active = append([]string(nil), active...)
	sort.Strings(active)
	start := strings.Index(strings.ToLower(content), "[palmodsettings]")
	current := ""
	prefix, suffix := strings.TrimRight(content, "\r\n"), ""
	if start >= 0 {
		prefix = content[:start]
		rest := content[start+len("[PalModSettings]"):]
		next := strings.Index(rest, "\n[")
		if next < 0 {
			current = rest
		} else {
			current, suffix = rest[:next], rest[next+1:]
		}
	}
	preserved := []string{}
	existingRoot := ""
	for _, line := range strings.Split(current, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "bGlobalEnableMod="), strings.HasPrefix(trimmed, "ActiveModList="):
			continue
		case strings.HasPrefix(trimmed, "WorkshopRootDir="):
			existingRoot = strings.TrimSpace(strings.TrimPrefix(trimmed, "WorkshopRootDir="))
		default:
			if trimmed != "" {
				preserved = append(preserved, line)
			}
		}
	}
	if strings.TrimSpace(workshopRoot) == "" {
		workshopRoot = existingRoot
	}
	lines := []string{"[PalModSettings]", "bGlobalEnableMod=true"}
	for _, packageName := range active {
		lines = append(lines, "ActiveModList="+packageName)
	}
	if strings.TrimSpace(workshopRoot) != "" {
		lines = append(lines, "WorkshopRootDir="+strings.TrimSpace(workshopRoot))
	}
	lines = append(lines, preserved...)
	section := strings.Join(lines, "\n") + "\n"
	if start < 0 {
		return prefix + "\n\n" + section
	}
	return prefix + section + suffix
}

func (a *App) ListOfficialWorkshopMods(id string) ([]OfficialWorkshopMod, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	settings, _ := os.ReadFile(palModSettingsPath(instance))
	active := map[string]bool{}
	for _, line := range strings.Split(string(settings), "\n") {
		if name := strings.TrimSpace(strings.TrimPrefix(line, "ActiveModList=")); strings.HasPrefix(strings.TrimSpace(line), "ActiveModList=") {
			active[name] = true
		}
	}
	root, rootErr := a.GetOfficialWorkshopRoot(id)
	if rootErr != nil {
		return nil, rootErr
	}
	if root == "" {
		root = officialWorkshopRoot(instance)
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []OfficialWorkshopMod{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]OfficialWorkshopMod, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mod, parseErr := parseOfficialWorkshopMod(filepath.Join(root, entry.Name()))
		if parseErr != nil {
			continue
		}
		mod.Enabled = active[mod.PackageName]
		_, deployedErr := os.Stat(filepath.Join(instance.RootPath, "Mods", "ManagedMods", mod.PackageName, "InstallManifest.json"))
		mod.Deployed = deployedErr == nil
		result = append(result, mod)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PackageName < result[j].PackageName })
	return result, nil
}

func (a *App) ImportOfficialWorkshopMod(id, source string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("stop the server before installing official Workshop mods")
	}
	mod, err := parseOfficialWorkshopMod(source)
	if err != nil {
		return err
	}
	if !mod.ServerCompatible {
		return errors.New("this Workshop mod is not marked as server compatible")
	}
	root, rootErr := a.GetOfficialWorkshopRoot(id)
	if rootErr != nil {
		return rootErr
	}
	if root == "" {
		root = officialWorkshopRoot(instance)
	}
	target := filepath.Join(root, filepath.Base(filepath.Clean(source)))
	sourceAbs, _ := filepath.Abs(source)
	targetAbs, _ := filepath.Abs(target)
	mods, err := a.ListOfficialWorkshopMods(id)
	if err != nil {
		return err
	}
	previousPackage := ""
	if previous, previousErr := parseOfficialWorkshopMod(target); previousErr == nil {
		previousPackage = previous.PackageName
	}
	candidate := make([]OfficialWorkshopMod, 0, len(mods)+1)
	for _, item := range mods {
		if !strings.EqualFold(item.Path, target) && item.PackageName != previousPackage {
			candidate = append(candidate, item)
		}
	}
	candidate = append(candidate, mod)
	active := make([]string, 0, len(mods))
	for _, item := range candidate {
		if item.Enabled || item.PackageName == mod.PackageName {
			active = append(active, item.PackageName)
		}
	}
	if err := validateWorkshopDependencies(candidate, active); err != nil {
		return err
	}
	if strings.EqualFold(sourceAbs, targetAbs) {
		return writePalModSettings(instance, active, "")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	incoming := target + ".incoming-" + newID()
	if err := copyTree(source, incoming); err != nil {
		return err
	}
	backup := target + ".previous-" + newID()
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			_ = os.RemoveAll(incoming)
			return err
		}
	}
	if err := os.Rename(incoming, target); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, target)
		}
		_ = os.RemoveAll(incoming)
		return err
	}
	if err := writePalModSettings(instance, active, ""); err != nil {
		_ = os.RemoveAll(target)
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, target)
		}
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func (a *App) SetOfficialWorkshopModEnabled(id, packageName string, enabled bool) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("stop the server before changing official Workshop mods")
	}
	mods, err := a.ListOfficialWorkshopMods(id)
	if err != nil {
		return err
	}
	active := make([]string, 0, len(mods))
	found := false
	for _, item := range mods {
		isActive := item.Enabled
		if item.PackageName == packageName {
			isActive, found = enabled, true
		}
		if isActive {
			active = append(active, item.PackageName)
		}
	}
	if !found {
		return errors.New("official Workshop mod was not found")
	}
	if err := validateWorkshopDependencies(mods, active); err != nil {
		return err
	}
	return writePalModSettings(instance, active, "")
}

func (a *App) DeleteOfficialWorkshopMod(id, path string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("stop the server before removing official Workshop mods")
	}
	configuredRoot, rootErr := a.GetOfficialWorkshopRoot(id)
	if rootErr != nil {
		return rootErr
	}
	if configuredRoot == "" {
		configuredRoot = officialWorkshopRoot(instance)
	}
	root, _ := filepath.Abs(configuredRoot)
	target, _ := filepath.Abs(path)
	rel, relErr := filepath.Rel(root, target)
	if relErr != nil || rel == "." || filepath.Dir(rel) != "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("Workshop mod path is outside this server")
	}
	mod, err := parseOfficialWorkshopMod(target)
	if err != nil {
		return err
	}
	mods, err := a.ListOfficialWorkshopMods(id)
	if err != nil {
		return err
	}
	active := make([]string, 0, len(mods))
	for _, item := range mods {
		if item.Enabled && item.PackageName != mod.PackageName {
			active = append(active, item.PackageName)
		}
	}
	if err := validateWorkshopDependencies(mods, active); err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return writePalModSettings(instance, active, "")
}

func writePalModSettings(instance ServerInstance, active []string, workshopRoot string) error {
	path := palModSettingsPath(instance)
	content, _ := os.ReadFile(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(mergePalModSettings(string(content), active, workshopRoot)), 0o600)
}

func validateWorkshopDependencies(mods []OfficialWorkshopMod, active []string) error {
	enabled := map[string]bool{}
	for _, packageName := range active {
		enabled[packageName] = true
	}
	for _, mod := range mods {
		if !enabled[mod.PackageName] {
			continue
		}
		for _, dependency := range mod.Dependencies {
			if !enabled[dependency] {
				return errors.New("official Workshop mod " + mod.PackageName + " requires " + dependency)
			}
		}
	}
	return nil
}

func (a *App) GetOfficialWorkshopRoot(id string) (string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(palModSettingsPath(instance))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "WorkshopRootDir=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "WorkshopRootDir=")), nil
		}
	}
	return "", nil
}

func (a *App) SaveOfficialWorkshopRoot(id, workshopRoot string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("stop the server before changing Workshop root")
	}
	// A new root can contain a different Workshop collection. Clear the active
	// list rather than loading package names that may not exist under it.
	return writePalModSettings(instance, nil, strings.TrimSpace(workshopRoot))
}

func officialPvPPresetUpdates() map[string]string {
	return map[string]string{
		"bIsPvP": "True", "bEnablePlayerToPlayerDamage": "True", "bEnableDefenseOtherGuildPlayer": "True", "bBuildAreaLimit": "True",
		"bExistPlayerAfterLogout": "True", "bEnableFastTravel": "True", "bEnableFastTravelOnlyBaseCamp": "True",
		"bAllowEnhanceStat_Health": "False", "bAllowEnhanceStat_Attack": "False",
		"DenyTechnologyList": `(SkillUnlock_JetDragon,SkillUnlock_IceHorse,SkillUnlock_IceHorse_Dark,SkillUnlock_SaintCentaur,SkillUnlock_BlackCentaur,SkillUnlock_DarkMechaDragon,SkillUnlock_PoseidonOrca,GrapplingGun,GrapplingGun2,GrapplingGun3,GrapplingGun4,GrapplingGun5,GuildChest)`,
		"GuildPlayerMaxNum":  "4", "BaseCampMaxNumInGuild": "2", "MaxBuildingLimitNum": "1000",
		"BlockRespawnTime": "5.0", "RespawnPenaltyDurationThreshold": "1800.0", "RespawnPenaltyTimeScale": "2.0",
		"bAdditionalDropItemWhenPlayerKillingInPvPMode": "True", "AdditionalDropItemWhenPlayerKillingInPvPMode": `PlayerDropItem("Champion's Emblem")`, "AdditionalDropItemNumWhenPlayerKillingInPvPMode": "1.0",
		"bDisplayPvPItemNumOnWorldMap_BaseCamp": "True", "bDisplayPvPItemNumOnWorldMap_Player": "True",
	}
}

func officialWorldSettingRange(key string) [2]float64 {
	switch key {
	case "ServerReplicatePawnCullDistance":
		return [2]float64{5000, 15000}
	case "BaseCampWorkerMaxNum":
		return [2]float64{0, 50}
	case "BaseCampMaxNumInGuild":
		return [2]float64{0, 10}
	default:
		return [2]float64{0, 0}
	}
}

func validateOfficialWorldSettings(updates map[string]string) error {
	for key, value := range updates {
		rangeLimit := officialWorldSettingRange(key)
		if rangeLimit[0] == 0 && rangeLimit[1] == 0 {
			continue
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || parsed < rangeLimit[0] || parsed > rangeLimit[1] {
			return errors.New(key + " is outside the official range")
		}
	}
	return nil
}

func (a *App) ApplyOfficialPvPPreset(id string) error {
	return a.SaveWorldSettingsValues(id, officialPvPPresetUpdates())
}

func buildPerformanceAdvice(status RuntimeStatus, values map[string]string) []PerformanceAdvice {
	result := []PerformanceAdvice{}
	if !status.Running {
		return result
	}
	if status.FPS > 0 && status.FPS < 45 || status.FrameTime > 25 {
		result = append(result, PerformanceAdvice{"warn", "服务器帧时间偏高", "当前 FPS 或帧时间表明服务器负载偏高，优先降低据点、工作帕鲁和同步范围。", ""})
	}
	if status.BaseCampNum >= 8 {
		result = append(result, PerformanceAdvice{"warn", "据点数量较多", "官方说明更多据点会增加处理负载。", "BaseCampMaxNumInGuild"})
	}
	if values["BaseCampWorkerMaxNum"] == "50" {
		result = append(result, PerformanceAdvice{"info", "工作帕鲁达到上限", "每个据点 50 只工作帕鲁会显著增加 AI 负载。", "BaseCampWorkerMaxNum"})
	}
	if values["PalSpawnNumRate"] != "" && values["PalSpawnNumRate"] != "1" {
		result = append(result, PerformanceAdvice{"info", "帕鲁生成倍率已调整", "官方说明 PalSpawnNumRate 会影响性能。", "PalSpawnNumRate"})
	}
	if values["PhysicsActiveDropItemMaxNum"] != "" && values["PhysicsActiveDropItemMaxNum"] != "-1" && values["PhysicsActiveDropItemMaxNum"] != "0" {
		result = append(result, PerformanceAdvice{"info", "物理掉落物会占用 CPU", "降低物理掉落物上限可减少物理模拟。", "PhysicsActiveDropItemMaxNum"})
	}
	return result
}

func (a *App) GetPerformanceAdvice(id string) ([]PerformanceAdvice, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	status, err := serverStatus(instance)
	if err != nil {
		return nil, err
	}
	values, err := a.GetWorldSettingsValues(id)
	if err != nil {
		return nil, err
	}
	return buildPerformanceAdvice(status, values), nil
}

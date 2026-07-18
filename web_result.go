package main

// sanitizeAgentWebResult removes Agent-host filesystem details from responses.
// A browser may run on a completely different computer, so Linux paths are
// neither useful input nor part of the public management model.
func sanitizeAgentWebResult(method string, value any) any {
	sanitizeInstance := func(instance ServerInstance) ServerInstance {
		instance.RootPath = ""
		instance.Executable = ""
		instance.SteamCMDPath = ""
		return instance
	}
	switch result := value.(type) {
	case AppConfig:
		for index := range result.Instances {
			result.Instances[index] = sanitizeInstance(result.Instances[index])
		}
		return result
	case ServerInstance:
		return sanitizeInstance(result)
	case ServerImportResult:
		result.Instance = sanitizeInstance(result.Instance)
		return result
	case []BackupEntry:
		for index := range result {
			result[index].Path = result[index].Name
		}
		return result
	case BackupEntry:
		result.Path = result.Name
		return result
	case []ExtensionStatus:
		for index := range result {
			result[index].Path = ""
		}
		return result
	case []ModEntry:
		for index := range result {
			result[index].Path = ""
		}
		return result
	case []OfficialWorkshopMod:
		for index := range result {
			result[index].Path = ""
		}
		return result
	case []ServerModCatalogEntry:
		for index := range result {
			result[index].InstalledPath = ""
		}
		return result
	case SaveInspectionResult:
		result.LevelPath = ""
		return result
	case SaveInspectorStatus:
		result.Path = ""
		return result
	case FrpStatus:
		result.Path = ""
		return result
	case map[string]string:
		if method == "GetServerPaths" {
			return map[string]string{}
		}
	}
	return value
}

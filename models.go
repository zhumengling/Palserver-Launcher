package main

type ServerInstance struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	RootPath                 string `json:"rootPath"`
	Executable               string `json:"executable"`
	SteamCMDPath             string `json:"steamCmdPath"`
	PublicIP                 string `json:"publicIp"`
	PublicPort               int    `json:"publicPort"`
	QueryPort                int    `json:"queryPort"`
	RCONPort                 int    `json:"rconPort"`
	RESTPort                 int    `json:"restPort"`
	AdminPassword            string `json:"adminPassword"`
	ServerPassword           string `json:"serverPassword"`
	Community                bool   `json:"community"`
	PerformanceMode          bool   `json:"performanceMode"`
	IconID                   string `json:"iconId"`
	AutoRestartHours         int    `json:"autoRestartHours"`
	CrashRestart             bool   `json:"crashRestart"`
	GuardianEnabled          bool   `json:"guardianEnabled"`
	GuardianFailureThreshold int    `json:"guardianFailureThreshold"`
	GuardianCheckSeconds     int    `json:"guardianCheckSeconds"`
	GuardianMaxRestarts      int    `json:"guardianMaxRestarts"`
	WhitelistEnforced        bool   `json:"whitelistEnforced"`
	BackupRetentionMode      string `json:"backupRetentionMode"`
	BackupRetentionCount     int    `json:"backupRetentionCount"`
	BackupRetentionDays      int    `json:"backupRetentionDays"`
	UpdateOnlyWhenEmpty      bool   `json:"updateOnlyWhenEmpty"`
	UpdateWarnMinutes        int    `json:"updateWarnMinutes"`
}

type BackupRetentionPolicy struct {
	Mode  string `json:"mode"`
	Count int    `json:"count"`
	Days  int    `json:"days"`
}

type ServerUpdateStatus struct {
	Installed       bool   `json:"installed"`
	UpdateAvailable bool   `json:"updateAvailable"`
	LocalBuildID    string `json:"localBuildId"`
	RemoteBuildID   string `json:"remoteBuildId"`
	Branch          string `json:"branch"`
}

type AppConfig struct {
	Instances        []ServerInstance       `json:"instances"`
	MaintenanceTasks []MaintenanceTask      `json:"maintenanceTasks"`
	PlayerHistory    []PlayerHistoryEntry   `json:"playerHistory"`
	ActiveEvents     []ActiveGameEvent      `json:"activeEvents"`
	DiscordWebhooks  []DiscordWebhookConfig `json:"discordWebhooks"`
	SelectedID       string                 `json:"selectedId"`
	Language         string                 `json:"language"`
}

type DiscordWebhookConfig struct {
	ServerID     string   `json:"serverId"`
	Enabled      bool     `json:"enabled"`
	EncryptedURL string   `json:"encryptedUrl"`
	Events       []string `json:"events"`
}

type DiscordWebhookSettings struct {
	ServerID   string   `json:"serverId"`
	Enabled    bool     `json:"enabled"`
	Configured bool     `json:"configured"`
	Events     []string `json:"events"`
}

type SaveInspectionResult struct {
	ServerID  string           `json:"serverId"`
	LevelPath string           `json:"levelPath"`
	ParsedAt  int64            `json:"parsedAt"`
	Players   []map[string]any `json:"players"`
	Guilds    []map[string]any `json:"guilds"`
}

type SaveInspectorStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Path      string `json:"path"`
}

type GamePreset struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Values      map[string]string `json:"values"`
}

type ActiveGameEvent struct {
	ServerID         string            `json:"serverId"`
	EventID          string            `json:"eventId"`
	Name             string            `json:"name"`
	StartedAt        int64             `json:"startedAt"`
	EndsAt           int64             `json:"endsAt"`
	OriginalSettings string            `json:"originalSettings"`
	Values           map[string]string `json:"values"`
}

type PlayerHistoryEntry struct {
	ServerID    string   `json:"serverId"`
	UserID      string   `json:"userId"`
	PlayerID    string   `json:"playerId"`
	SteamID     string   `json:"steamId"`
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases"`
	FirstSeen   int64    `json:"firstSeen"`
	LastSeen    int64    `json:"lastSeen"`
	Visits      int      `json:"visits"`
	LastIP      string   `json:"lastIp"`
	Online      bool     `json:"online"`
	Whitelisted bool     `json:"whitelisted"`
	Note        string   `json:"note"`
	Banned      bool     `json:"banned"`
}

type MaintenanceTask struct {
	ID              string `json:"id"`
	ServerID        string `json:"serverId"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Enabled         bool   `json:"enabled"`
	Schedule        string `json:"schedule"`
	IntervalMinutes int    `json:"intervalMinutes"`
	DailyTime       string `json:"dailyTime"`
	LastRun         int64  `json:"lastRun"`
	NextRun         int64  `json:"nextRun"`
	LastStatus      string `json:"lastStatus"`
	LastMessage     string `json:"lastMessage"`
}

type RuntimeStatus struct {
	Running       bool    `json:"running"`
	PID           int     `json:"pid"`
	Version       string  `json:"version"`
	Players       int     `json:"players"`
	MaxPlayers    int     `json:"maxPlayers"`
	FPS           float64 `json:"fps"`
	FrameTime     float64 `json:"frameTime"`
	Uptime        int64   `json:"uptime"`
	CPU           float64 `json:"cpu"`
	MemoryMB      float64 `json:"memoryMb"`
	RESTAvailable bool    `json:"restAvailable"`
	RCONAvailable bool    `json:"rconAvailable"`
}

type HostResources struct {
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryPercent float64 `json:"memoryPercent"`
	MemoryUsedMB  float64 `json:"memoryUsedMb"`
	MemoryTotalMB float64 `json:"memoryTotalMb"`
}

type Player struct {
	Name        string  `json:"name"`
	AccountName string  `json:"accountName"`
	PlayerID    string  `json:"playerId"`
	UserID      string  `json:"userId"`
	IP          string  `json:"ip"`
	Ping        float64 `json:"ping"`
	LocationX   float64 `json:"locationX"`
	LocationY   float64 `json:"locationY"`
	Level       int     `json:"level"`
}

type BackupEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	CreatedAt int64  `json:"createdAt"`
	Size      int64  `json:"size"`
}

type ExtensionStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Version   string `json:"version"`
	Path      string `json:"path"`
}

type ModEntry struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Origin      string `json:"origin"`
	Description string `json:"description"`
	System      bool   `json:"system"`
	Path        string `json:"path"`
	Enabled     bool   `json:"enabled"`
	Size        int64  `json:"size"`
}

type ServerModCatalogEntry struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Version            string `json:"version"`
	UpdatedAt          string `json:"updatedAt"`
	Description        string `json:"description"`
	NexusURL           string `json:"nexusUrl"`
	Dependency         string `json:"dependency"`
	Warning            string `json:"warning"`
	FolderName         string `json:"folderName"`
	Installed          bool   `json:"installed"`
	Enabled            bool   `json:"enabled"`
	InstalledPath      string `json:"installedPath"`
	InstalledVersion   string `json:"installedVersion"`
	InstalledUpdatedAt string `json:"installedUpdatedAt"`
	LatestVersion      string `json:"latestVersion"`
	LatestUpdatedAt    string `json:"latestUpdatedAt"`
	UpdateAvailable    bool   `json:"updateAvailable"`
	UpdateCheckError   string `json:"updateCheckError"`
}

type DiagnosticResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type ActionRequest struct {
	Action string `json:"action"`
	UserID string `json:"userId"`
	Value  string `json:"value"`
	Amount int    `json:"amount"`
}

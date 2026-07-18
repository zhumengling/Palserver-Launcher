export namespace main {
	
	export class PalSoulBonuses {
	    health: number;
	    attack: number;
	    defense: number;
	    craftSpeed: number;
	
	    static createFrom(source: any = {}) {
	        return new PalSoulBonuses(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.health = source["health"];
	        this.attack = source["attack"];
	        this.defense = source["defense"];
	        this.craftSpeed = source["craftSpeed"];
	    }
	}
	export class PalIVs {
	    health: number;
	    attackMelee: number;
	    attackShot: number;
	    defense: number;
	
	    static createFrom(source: any = {}) {
	        return new PalIVs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.health = source["health"];
	        this.attackMelee = source["attackMelee"];
	        this.attackShot = source["attackShot"];
	        this.defense = source["defense"];
	    }
	}
	export class PalGrantOptions {
	    custom: boolean;
	    level: number;
	    gender: string;
	    nickname: string;
	    shiny: boolean;
	    partnerSkillLevel: number;
	    activeSkills: string[];
	    passives: string[];
	    ivs: PalIVs;
	    palSouls: PalSoulBonuses;
	
	    static createFrom(source: any = {}) {
	        return new PalGrantOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.custom = source["custom"];
	        this.level = source["level"];
	        this.gender = source["gender"];
	        this.nickname = source["nickname"];
	        this.shiny = source["shiny"];
	        this.partnerSkillLevel = source["partnerSkillLevel"];
	        this.activeSkills = source["activeSkills"];
	        this.passives = source["passives"];
	        this.ivs = this.convertValues(source["ivs"], PalIVs);
	        this.palSouls = this.convertValues(source["palSouls"], PalSoulBonuses);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ActionRequest {
	    action: string;
	    userId: string;
	    value: string;
	    amount: number;
	    extra: string;
	    pal?: PalGrantOptions;
	
	    static createFrom(source: any = {}) {
	        return new ActionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.userId = source["userId"];
	        this.value = source["value"];
	        this.amount = source["amount"];
	        this.extra = source["extra"];
	        this.pal = this.convertValues(source["pal"], PalGrantOptions);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ActiveGameEvent {
	    serverId: string;
	    eventId: string;
	    name: string;
	    state: string;
	    durationMinutes: number;
	    startedAt: number;
	    endsAt: number;
	    originalSettings: string;
	    values: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ActiveGameEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.eventId = source["eventId"];
	        this.name = source["name"];
	        this.state = source["state"];
	        this.durationMinutes = source["durationMinutes"];
	        this.startedAt = source["startedAt"];
	        this.endsAt = source["endsAt"];
	        this.originalSettings = source["originalSettings"];
	        this.values = source["values"];
	    }
	}
	export class AgentAuditEntry {
	    time: string;
	    method: string;
	    serverId: string;
	    remoteIp: string;
	    successful: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentAuditEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.method = source["method"];
	        this.serverId = source["serverId"];
	        this.remoteIp = source["remoteIp"];
	        this.successful = source["successful"];
	        this.error = source["error"];
	    }
	}
	export class AgentPreflightCheck {
	    name: string;
	    status: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentPreflightCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	    }
	}
	export class AgentPreflightReport {
	    ok: boolean;
	    platform: string;
	    hostPlatform: string;
	    architecture: string;
	    user: string;
	    dataDir: string;
	    home: string;
	    allowedRoots: string[];
	    simulatedPlatform: boolean;
	    checks: AgentPreflightCheck[];
	
	    static createFrom(source: any = {}) {
	        return new AgentPreflightReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.platform = source["platform"];
	        this.hostPlatform = source["hostPlatform"];
	        this.architecture = source["architecture"];
	        this.user = source["user"];
	        this.dataDir = source["dataDir"];
	        this.home = source["home"];
	        this.allowedRoots = source["allowedRoots"];
	        this.simulatedPlatform = source["simulatedPlatform"];
	        this.checks = this.convertValues(source["checks"], AgentPreflightCheck);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FrpConfig {
	    serverId: string;
	    serverAddress: string;
	    serverPort: number;
	    encryptedToken: string;
	    proxyName: string;
	    remoteGamePort: number;
	    queryEnabled: boolean;
	    remoteQueryPort: number;
	    rconEnabled: boolean;
	    remoteRconPort: number;
	    restEnabled: boolean;
	    remoteRestPort: number;
	    autoStart: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FrpConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.serverAddress = source["serverAddress"];
	        this.serverPort = source["serverPort"];
	        this.encryptedToken = source["encryptedToken"];
	        this.proxyName = source["proxyName"];
	        this.remoteGamePort = source["remoteGamePort"];
	        this.queryEnabled = source["queryEnabled"];
	        this.remoteQueryPort = source["remoteQueryPort"];
	        this.rconEnabled = source["rconEnabled"];
	        this.remoteRconPort = source["remoteRconPort"];
	        this.restEnabled = source["restEnabled"];
	        this.remoteRestPort = source["remoteRestPort"];
	        this.autoStart = source["autoStart"];
	    }
	}
	export class DiscordWebhookConfig {
	    serverId: string;
	    enabled: boolean;
	    encryptedUrl: string;
	    events: string[];
	
	    static createFrom(source: any = {}) {
	        return new DiscordWebhookConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.enabled = source["enabled"];
	        this.encryptedUrl = source["encryptedUrl"];
	        this.events = source["events"];
	    }
	}
	export class PlayerHistoryEntry {
	    serverId: string;
	    userId: string;
	    playerId: string;
	    steamId: string;
	    name: string;
	    aliases: string[];
	    firstSeen: number;
	    lastSeen: number;
	    visits: number;
	    lastIp: string;
	    online: boolean;
	    whitelisted: boolean;
	    note: string;
	    banned: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PlayerHistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.userId = source["userId"];
	        this.playerId = source["playerId"];
	        this.steamId = source["steamId"];
	        this.name = source["name"];
	        this.aliases = source["aliases"];
	        this.firstSeen = source["firstSeen"];
	        this.lastSeen = source["lastSeen"];
	        this.visits = source["visits"];
	        this.lastIp = source["lastIp"];
	        this.online = source["online"];
	        this.whitelisted = source["whitelisted"];
	        this.note = source["note"];
	        this.banned = source["banned"];
	    }
	}
	export class MaintenanceTask {
	    id: string;
	    serverId: string;
	    name: string;
	    type: string;
	    enabled: boolean;
	    schedule: string;
	    intervalMinutes: number;
	    dailyTime: string;
	    lastRun: number;
	    nextRun: number;
	    lastStatus: string;
	    lastMessage: string;
	
	    static createFrom(source: any = {}) {
	        return new MaintenanceTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.serverId = source["serverId"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.enabled = source["enabled"];
	        this.schedule = source["schedule"];
	        this.intervalMinutes = source["intervalMinutes"];
	        this.dailyTime = source["dailyTime"];
	        this.lastRun = source["lastRun"];
	        this.nextRun = source["nextRun"];
	        this.lastStatus = source["lastStatus"];
	        this.lastMessage = source["lastMessage"];
	    }
	}
	export class ServerInstance {
	    id: string;
	    name: string;
	    rootPath: string;
	    executable: string;
	    steamCmdPath: string;
	    publicIp: string;
	    publicPort: number;
	    queryPort: number;
	    rconPort: number;
	    restPort: number;
	    adminPassword: string;
	    serverPassword: string;
	    encryptedAdminPassword?: string;
	    encryptedServerPassword?: string;
	    community: boolean;
	    performanceMode: boolean;
	    legacyPerformanceFlags: boolean;
	    workerThreads: number;
	    processPriority: string;
	    cpuAffinityMode: string;
	    iconId: string;
	    autoRestartHours: number;
	    startOnBoot: boolean;
	    crashRestart: boolean;
	    guardianEnabled: boolean;
	    guardianFailureThreshold: number;
	    guardianCheckSeconds: number;
	    guardianMaxRestarts: number;
	    whitelistEnforced: boolean;
	    backupRetentionMode: string;
	    backupRetentionCount: number;
	    backupRetentionDays: number;
	    updateOnlyWhenEmpty: boolean;
	    updateWarnMinutes: number;
	
	    static createFrom(source: any = {}) {
	        return new ServerInstance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.rootPath = source["rootPath"];
	        this.executable = source["executable"];
	        this.steamCmdPath = source["steamCmdPath"];
	        this.publicIp = source["publicIp"];
	        this.publicPort = source["publicPort"];
	        this.queryPort = source["queryPort"];
	        this.rconPort = source["rconPort"];
	        this.restPort = source["restPort"];
	        this.adminPassword = source["adminPassword"];
	        this.serverPassword = source["serverPassword"];
	        this.encryptedAdminPassword = source["encryptedAdminPassword"];
	        this.encryptedServerPassword = source["encryptedServerPassword"];
	        this.community = source["community"];
	        this.performanceMode = source["performanceMode"];
	        this.legacyPerformanceFlags = source["legacyPerformanceFlags"];
	        this.workerThreads = source["workerThreads"];
	        this.processPriority = source["processPriority"];
	        this.cpuAffinityMode = source["cpuAffinityMode"];
	        this.iconId = source["iconId"];
	        this.autoRestartHours = source["autoRestartHours"];
	        this.startOnBoot = source["startOnBoot"];
	        this.crashRestart = source["crashRestart"];
	        this.guardianEnabled = source["guardianEnabled"];
	        this.guardianFailureThreshold = source["guardianFailureThreshold"];
	        this.guardianCheckSeconds = source["guardianCheckSeconds"];
	        this.guardianMaxRestarts = source["guardianMaxRestarts"];
	        this.whitelistEnforced = source["whitelistEnforced"];
	        this.backupRetentionMode = source["backupRetentionMode"];
	        this.backupRetentionCount = source["backupRetentionCount"];
	        this.backupRetentionDays = source["backupRetentionDays"];
	        this.updateOnlyWhenEmpty = source["updateOnlyWhenEmpty"];
	        this.updateWarnMinutes = source["updateWarnMinutes"];
	    }
	}
	export class AppConfig {
	    instances: ServerInstance[];
	    maintenanceTasks: MaintenanceTask[];
	    playerHistory: PlayerHistoryEntry[];
	    activeEvents: ActiveGameEvent[];
	    discordWebhooks: DiscordWebhookConfig[];
	    frpConfigs: FrpConfig[];
	    selectedId: string;
	    language: string;
	    startupWarnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instances = this.convertValues(source["instances"], ServerInstance);
	        this.maintenanceTasks = this.convertValues(source["maintenanceTasks"], MaintenanceTask);
	        this.playerHistory = this.convertValues(source["playerHistory"], PlayerHistoryEntry);
	        this.activeEvents = this.convertValues(source["activeEvents"], ActiveGameEvent);
	        this.discordWebhooks = this.convertValues(source["discordWebhooks"], DiscordWebhookConfig);
	        this.frpConfigs = this.convertValues(source["frpConfigs"], FrpConfig);
	        this.selectedId = source["selectedId"];
	        this.language = source["language"];
	        this.startupWarnings = source["startupWarnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BackupEntry {
	    name: string;
	    path: string;
	    createdAt: number;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new BackupEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.createdAt = source["createdAt"];
	        this.size = source["size"];
	    }
	}
	export class CapabilityStatus {
	    id: string;
	    name: string;
	    category: string;
	    available: boolean;
	    state: string;
	    detail: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new CapabilityStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.category = source["category"];
	        this.available = source["available"];
	        this.state = source["state"];
	        this.detail = source["detail"];
	        this.reason = source["reason"];
	    }
	}
	export class DiagnosticResult {
	    name: string;
	    status: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	    }
	}
	
	export class DiscordWebhookSettings {
	    serverId: string;
	    enabled: boolean;
	    configured: boolean;
	    events: string[];
	
	    static createFrom(source: any = {}) {
	        return new DiscordWebhookSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.enabled = source["enabled"];
	        this.configured = source["configured"];
	        this.events = source["events"];
	    }
	}
	export class ExtensionStatus {
	    id: string;
	    name: string;
	    supported: boolean;
	    unsupportedReason: string;
	    installed: boolean;
	    enabled: boolean;
	    version: string;
	    path: string;
	    latestVersion: string;
	    latestAsset: string;
	    latestUpdatedAt: string;
	    updateAvailable: boolean;
	    updateCheckError: string;
	    pending: boolean;
	    pendingVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtensionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.supported = source["supported"];
	        this.unsupportedReason = source["unsupportedReason"];
	        this.installed = source["installed"];
	        this.enabled = source["enabled"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.latestVersion = source["latestVersion"];
	        this.latestAsset = source["latestAsset"];
	        this.latestUpdatedAt = source["latestUpdatedAt"];
	        this.updateAvailable = source["updateAvailable"];
	        this.updateCheckError = source["updateCheckError"];
	        this.pending = source["pending"];
	        this.pendingVersion = source["pendingVersion"];
	    }
	}
	export class ExtensionUpdateResult {
	    extensionId: string;
	    version: string;
	    pending: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtensionUpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.extensionId = source["extensionId"];
	        this.version = source["version"];
	        this.pending = source["pending"];
	        this.message = source["message"];
	    }
	}
	
	export class FrpSettings {
	    serverId: string;
	    serverAddress: string;
	    serverPort: number;
	    tokenConfigured: boolean;
	    proxyName: string;
	    remoteGamePort: number;
	    queryEnabled: boolean;
	    remoteQueryPort: number;
	    rconEnabled: boolean;
	    remoteRconPort: number;
	    restEnabled: boolean;
	    remoteRestPort: number;
	    autoStart: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FrpSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.serverAddress = source["serverAddress"];
	        this.serverPort = source["serverPort"];
	        this.tokenConfigured = source["tokenConfigured"];
	        this.proxyName = source["proxyName"];
	        this.remoteGamePort = source["remoteGamePort"];
	        this.queryEnabled = source["queryEnabled"];
	        this.remoteQueryPort = source["remoteQueryPort"];
	        this.rconEnabled = source["rconEnabled"];
	        this.remoteRconPort = source["remoteRconPort"];
	        this.restEnabled = source["restEnabled"];
	        this.remoteRestPort = source["remoteRestPort"];
	        this.autoStart = source["autoStart"];
	    }
	}
	export class FrpStatus {
	    installed: boolean;
	    version: string;
	    path: string;
	    running: boolean;
	    pid: number;
	    settings: FrpSettings;
	
	    static createFrom(source: any = {}) {
	        return new FrpStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.running = source["running"];
	        this.pid = source["pid"];
	        this.settings = this.convertValues(source["settings"], FrpSettings);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GamePreset {
	    id: string;
	    name: string;
	    description: string;
	    values: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new GamePreset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.values = source["values"];
	    }
	}
	export class HostResources {
	    cpuPercent: number;
	    memoryPercent: number;
	    memoryUsedMb: number;
	    memoryTotalMb: number;
	
	    static createFrom(source: any = {}) {
	        return new HostResources(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cpuPercent = source["cpuPercent"];
	        this.memoryPercent = source["memoryPercent"];
	        this.memoryUsedMb = source["memoryUsedMb"];
	        this.memoryTotalMb = source["memoryTotalMb"];
	    }
	}
	export class LauncherUpdateInfo {
	    currentVersion: string;
	    latestVersion: string;
	    title: string;
	    notes: string;
	    publishedAt: string;
	    assetName: string;
	    assetSize: number;
	    updateAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LauncherUpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.title = source["title"];
	        this.notes = source["notes"];
	        this.publishedAt = source["publishedAt"];
	        this.assetName = source["assetName"];
	        this.assetSize = source["assetSize"];
	        this.updateAvailable = source["updateAvailable"];
	    }
	}
	
	export class ModEntry {
	    name: string;
	    kind: string;
	    origin: string;
	    description: string;
	    system: boolean;
	    path: string;
	    enabled: boolean;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new ModEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.origin = source["origin"];
	        this.description = source["description"];
	        this.system = source["system"];
	        this.path = source["path"];
	        this.enabled = source["enabled"];
	        this.size = source["size"];
	    }
	}
	export class OfficialWorkshopMod {
	    name: string;
	    packageName: string;
	    version: string;
	    dependencies: string[];
	    serverCompatible: boolean;
	    enabled: boolean;
	    deployed: boolean;
	    path: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new OfficialWorkshopMod(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.packageName = source["packageName"];
	        this.version = source["version"];
	        this.dependencies = source["dependencies"];
	        this.serverCompatible = source["serverCompatible"];
	        this.enabled = source["enabled"];
	        this.deployed = source["deployed"];
	        this.path = source["path"];
	        this.size = source["size"];
	    }
	}
	
	
	
	export class PerformanceAdvice {
	    level: string;
	    title: string;
	    detail: string;
	    setting: string;
	
	    static createFrom(source: any = {}) {
	        return new PerformanceAdvice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.setting = source["setting"];
	    }
	}
	export class Player {
	    name: string;
	    accountName: string;
	    playerId: string;
	    userId: string;
	    ip: string;
	    ping: number;
	    locationX: number;
	    locationY: number;
	    level: number;
	    buildingCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Player(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.accountName = source["accountName"];
	        this.playerId = source["playerId"];
	        this.userId = source["userId"];
	        this.ip = source["ip"];
	        this.ping = source["ping"];
	        this.locationX = source["locationX"];
	        this.locationY = source["locationY"];
	        this.level = source["level"];
	        this.buildingCount = source["buildingCount"];
	    }
	}
	
	export class PluginCompatibilityIssue {
	    severity: string;
	    component: string;
	    title: string;
	    detail: string;
	    action: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginCompatibilityIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.severity = source["severity"];
	        this.component = source["component"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.action = source["action"];
	    }
	}
	export class PluginCompatibilityReport {
	    serverId: string;
	    checkedAt: number;
	    gameBuildId: string;
	    baselineBuildId: string;
	    palDefenderVersion: string;
	    ue4ssVersion: string;
	    compatible: boolean;
	    safeModeRecommended: boolean;
	    lastCrashSummary: string;
	    issues: PluginCompatibilityIssue[];
	
	    static createFrom(source: any = {}) {
	        return new PluginCompatibilityReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.checkedAt = source["checkedAt"];
	        this.gameBuildId = source["gameBuildId"];
	        this.baselineBuildId = source["baselineBuildId"];
	        this.palDefenderVersion = source["palDefenderVersion"];
	        this.ue4ssVersion = source["ue4ssVersion"];
	        this.compatible = source["compatible"];
	        this.safeModeRecommended = source["safeModeRecommended"];
	        this.lastCrashSummary = source["lastCrashSummary"];
	        this.issues = this.convertValues(source["issues"], PluginCompatibilityIssue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuntimeStatus {
	    running: boolean;
	    pid: number;
	    state: string;
	    stateMessage: string;
	    checkedAt: number;
	    version: string;
	    players: number;
	    maxPlayers: number;
	    fps: number;
	    frameTime: number;
	    uptime: number;
	    baseCampNum: number;
	    worldDays: number;
	    cpu: number;
	    memoryMb: number;
	    restAvailable: boolean;
	    rconAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.pid = source["pid"];
	        this.state = source["state"];
	        this.stateMessage = source["stateMessage"];
	        this.checkedAt = source["checkedAt"];
	        this.version = source["version"];
	        this.players = source["players"];
	        this.maxPlayers = source["maxPlayers"];
	        this.fps = source["fps"];
	        this.frameTime = source["frameTime"];
	        this.uptime = source["uptime"];
	        this.baseCampNum = source["baseCampNum"];
	        this.worldDays = source["worldDays"];
	        this.cpu = source["cpu"];
	        this.memoryMb = source["memoryMb"];
	        this.restAvailable = source["restAvailable"];
	        this.rconAvailable = source["rconAvailable"];
	    }
	}
	export class SafeModeStatus {
	    active: boolean;
	    activatedAt: number;
	    palDefenderWasEnabled: boolean;
	    ue4ssWasEnabled: boolean;
	    palDefenderCurrentlyOff: boolean;
	    ue4ssCurrentlyOff: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SafeModeStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.activatedAt = source["activatedAt"];
	        this.palDefenderWasEnabled = source["palDefenderWasEnabled"];
	        this.ue4ssWasEnabled = source["ue4ssWasEnabled"];
	        this.palDefenderCurrentlyOff = source["palDefenderCurrentlyOff"];
	        this.ue4ssCurrentlyOff = source["ue4ssCurrentlyOff"];
	    }
	}
	export class SaveInspectionResult {
	    serverId: string;
	    levelPath: string;
	    parsedAt: number;
	    players: any[];
	    guilds: any[];
	
	    static createFrom(source: any = {}) {
	        return new SaveInspectionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.levelPath = source["levelPath"];
	        this.parsedAt = source["parsedAt"];
	        this.players = source["players"];
	        this.guilds = source["guilds"];
	    }
	}
	export class SaveInspectorStatus {
	    installed: boolean;
	    version: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveInspectorStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.path = source["path"];
	    }
	}
	export class ServerCapabilityReport {
	    serverId: string;
	    platform: string;
	    checkedAt: number;
	    running: boolean;
	    serverVersion: string;
	    palDefenderVersion: string;
	    ue4ssVersion: string;
	    capabilities: CapabilityStatus[];
	
	    static createFrom(source: any = {}) {
	        return new ServerCapabilityReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.platform = source["platform"];
	        this.checkedAt = source["checkedAt"];
	        this.running = source["running"];
	        this.serverVersion = source["serverVersion"];
	        this.palDefenderVersion = source["palDefenderVersion"];
	        this.ue4ssVersion = source["ue4ssVersion"];
	        this.capabilities = this.convertValues(source["capabilities"], CapabilityStatus);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ServerImportResult {
	    instance: ServerInstance;
	    format: string;
	    detected: string;
	    detectedName: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instance = this.convertValues(source["instance"], ServerInstance);
	        this.format = source["format"];
	        this.detected = source["detected"];
	        this.detectedName = source["detectedName"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ServerInfo {
	    version: string;
	    servername: string;
	    description: string;
	    worldguid: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.servername = source["servername"];
	        this.description = source["description"];
	        this.worldguid = source["worldguid"];
	    }
	}
	
	export class ServerMetrics {
	    serverfps: number;
	    currentplayernum: number;
	    serverframetime: number;
	    maxplayernum: number;
	    uptime: number;
	    basecampnum: number;
	    days: number;
	
	    static createFrom(source: any = {}) {
	        return new ServerMetrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverfps = source["serverfps"];
	        this.currentplayernum = source["currentplayernum"];
	        this.serverframetime = source["serverframetime"];
	        this.maxplayernum = source["maxplayernum"];
	        this.uptime = source["uptime"];
	        this.basecampnum = source["basecampnum"];
	        this.days = source["days"];
	    }
	}
	export class ServerModCatalogEntry {
	    id: string;
	    name: string;
	    version: string;
	    updatedAt: string;
	    description: string;
	    nexusUrl: string;
	    dependency: string;
	    warning: string;
	    folderName: string;
	    installed: boolean;
	    enabled: boolean;
	    installedPath: string;
	    installedVersion: string;
	    installedUpdatedAt: string;
	    latestVersion: string;
	    latestUpdatedAt: string;
	    updateAvailable: boolean;
	    updateCheckError: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerModCatalogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.updatedAt = source["updatedAt"];
	        this.description = source["description"];
	        this.nexusUrl = source["nexusUrl"];
	        this.dependency = source["dependency"];
	        this.warning = source["warning"];
	        this.folderName = source["folderName"];
	        this.installed = source["installed"];
	        this.enabled = source["enabled"];
	        this.installedPath = source["installedPath"];
	        this.installedVersion = source["installedVersion"];
	        this.installedUpdatedAt = source["installedUpdatedAt"];
	        this.latestVersion = source["latestVersion"];
	        this.latestUpdatedAt = source["latestUpdatedAt"];
	        this.updateAvailable = source["updateAvailable"];
	        this.updateCheckError = source["updateCheckError"];
	    }
	}
	export class ServerSettingEntry {
	    key: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerSettingEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	    }
	}
	export class ServerSettings {
	    values: Record<string, string>;
	    entries: ServerSettingEntry[];
	
	    static createFrom(source: any = {}) {
	        return new ServerSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.values = source["values"];
	        this.entries = this.convertValues(source["entries"], ServerSettingEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ServerUpdateStatus {
	    installed: boolean;
	    updateAvailable: boolean;
	    localBuildId: string;
	    remoteBuildId: string;
	    branch: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerUpdateStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.updateAvailable = source["updateAvailable"];
	        this.localBuildId = source["localBuildId"];
	        this.remoteBuildId = source["remoteBuildId"];
	        this.branch = source["branch"];
	    }
	}
	export class SetupEnvironment {
	    platform: string;
	    cpuCores: number;
	    memoryTotalMb: number;
	    diskFreeBytes: number;
	    pathValid: boolean;
	    pathMessage: string;
	    cpuRecommended: boolean;
	    memoryMinimum: boolean;
	    memoryRecommended: boolean;
	    diskMinimum: boolean;
	    canInstall: boolean;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new SetupEnvironment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.cpuCores = source["cpuCores"];
	        this.memoryTotalMb = source["memoryTotalMb"];
	        this.diskFreeBytes = source["diskFreeBytes"];
	        this.pathValid = source["pathValid"];
	        this.pathMessage = source["pathMessage"];
	        this.cpuRecommended = source["cpuRecommended"];
	        this.memoryMinimum = source["memoryMinimum"];
	        this.memoryRecommended = source["memoryRecommended"];
	        this.diskMinimum = source["diskMinimum"];
	        this.canInstall = source["canInstall"];
	        this.warnings = source["warnings"];
	    }
	}
	export class WorldActor {
	    Type: string;
	    InstanceID: string;
	    UnitType: string;
	    NickName: string;
	    TrainerInstanceID: string;
	    TrainerNickName: string;
	    TrainerClass: string;
	    userid: string;
	    ip: string;
	    level: number;
	    HP: number;
	    MaxHP: number;
	    GuildID: string;
	    GuildName: string;
	    Class: string;
	    Action: string;
	    AI_Action: string;
	    LocationX: number;
	    LocationY: number;
	    LocationZ: number;
	    RotationX: number;
	    RotationY: number;
	    RotationZ: number;
	    Stage: string;
	    IsActive: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorldActor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Type = source["Type"];
	        this.InstanceID = source["InstanceID"];
	        this.UnitType = source["UnitType"];
	        this.NickName = source["NickName"];
	        this.TrainerInstanceID = source["TrainerInstanceID"];
	        this.TrainerNickName = source["TrainerNickName"];
	        this.TrainerClass = source["TrainerClass"];
	        this.userid = source["userid"];
	        this.ip = source["ip"];
	        this.level = source["level"];
	        this.HP = source["HP"];
	        this.MaxHP = source["MaxHP"];
	        this.GuildID = source["GuildID"];
	        this.GuildName = source["GuildName"];
	        this.Class = source["Class"];
	        this.Action = source["Action"];
	        this.AI_Action = source["AI_Action"];
	        this.LocationX = source["LocationX"];
	        this.LocationY = source["LocationY"];
	        this.LocationZ = source["LocationZ"];
	        this.RotationX = source["RotationX"];
	        this.RotationY = source["RotationY"];
	        this.RotationZ = source["RotationZ"];
	        this.Stage = source["Stage"];
	        this.IsActive = source["IsActive"];
	    }
	}
	export class WorldSnapshot {
	    Time: string;
	    FPS: number;
	    AverageFPS: number;
	    ActorData: WorldActor[];
	    available: boolean;
	    unavailableReason: string;
	
	    static createFrom(source: any = {}) {
	        return new WorldSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Time = source["Time"];
	        this.FPS = source["FPS"];
	        this.AverageFPS = source["AverageFPS"];
	        this.ActorData = this.convertValues(source["ActorData"], WorldActor);
	        this.available = source["available"];
	        this.unavailableReason = source["unavailableReason"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}


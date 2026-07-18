package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const agentSessionCookie = "pal_agent_session"

type webRPCRequest struct {
	Args []json.RawMessage `json:"args"`
}

type webRPCResponse struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

var webRPCMethods = map[string]bool{
	"Announce": true, "ApplyGamePreset": true, "ApplyLauncherUpdate": true, "ApplyOfficialPvPPreset": true,
	"BanHistoricalPlayer": true, "CheckExtensionUpdates": true, "CheckLauncherUpdate": true, "CheckServerModUpdates": true,
	"ChooseDirectory": true, "ChooseFiles": true, "ClearSteamCMDCache": true, "CreateBackup": true,
	"CreateDiagnosticBundle": true,
	"DeleteInstance":         true, "DeleteMaintenanceTask": true, "DeleteMod": true, "DeleteOfficialWorkshopMod": true,
	"DuplicateInstance": true, "ExportClientMods": true, "ForceStopServer": true, "GetActiveGameEvent": true,
	"GetConfig": true, "GetConsoleLog": true, "GetDiscordWebhookSettings": true, "GetFrpLog": true,
	"GetFrpSettings": true, "GetFrpStatus": true, "GetHostResources": true, "GetLauncherVersion": true,
	"GetAgentPreflight": true, "GetSetupEnvironment": true,
	"GetOfficialWorkshopRoot": true, "GetPerformanceAdvice": true, "GetPlayers": true, "GetPluginCompatibility": true,
	"GetSafeModeStatus": true, "GetSaveInspectorStatus": true, "GetServerCapabilities": true, "GetServerInfo": true,
	"GetServerMetrics": true, "GetServerPaths": true, "GetServerSettings": true, "GetServerSize": true,
	"GetServerUpdateStatus": true, "GetStatus": true, "GetWorldSettingsValues": true, "GetWorldSnapshot": true,
	"ImportExistingServer": true, "ImportUploadedServer": true, "ImportMods": true, "ImportOfficialWorkshopMod": true, "InspectSave": true,
	"InstallFrp": true, "InstallOrUpdateServer": true, "InstallSaveInspector": true, "InstallServerModArchive": true,
	"ListAgentAudit": true, "ListBackups": true, "ListBans": true, "ListExtensions": true, "ListGameEvents": true,
	"ListGamePresets": true, "ListMaintenanceTasks": true, "ListMods": true, "ListOfficialBackups": true,
	"ListOfficialWorkshopMods": true, "ListPlayerHistory": true, "ListServerModCatalog": true, "OpenNexusModPage": true,
	"OpenOfficialBackup": true, "OpenPath": true, "OpenServerPath": true, "PerformManagedUpdate": true,
	"PlayerAction": true, "PruneBackups": true, "QuickSetup": true, "ReadWorldSettings": true, "RestoreBackup": true,
	"RestorePluginsAfterSafeMode": true, "RunDiagnostics": true, "RunMaintenanceTask": true, "SaveDiscordWebhook": true,
	"SaveFrpSettings": true, "SaveInstance": true, "SaveMaintenanceTask": true, "SaveOfficialWorkshopRoot": true,
	"SaveWorld": true, "SaveWorldSettingsValues": true, "SelectInstance": true, "SendRCON": true,
	"SetOfficialWorkshopModEnabled": true, "SetPlayerNote": true, "SetPlayerWhitelist": true, "ShutdownServer": true,
	"StartFrp": true, "StartGameEvent": true, "StartServer": true, "StartServerSafeMode": true,
	"StopFrp": true, "StopGameEvent": true, "StopServer": true, "TestDiscordWebhook": true,
	"ToggleExtension": true, "ToggleMod": true, "UnbanPlayer": true, "UninstallServerMod": true,
	"UpdateAllExtensions": true, "UpdateExtension": true, "WriteWorldSettings": true,
}

var linuxWebPathRPCMethods = map[string]bool{
	"ChooseDirectory": true, "ChooseFiles": true, "CreateDiagnosticBundle": true,
	"ExportClientMods": true, "ImportExistingServer": true, "ImportMods": true,
	"ImportOfficialWorkshopMod": true, "InstallServerModArchive": true,
	"OpenNexusModPage": true, "OpenOfficialBackup": true, "OpenPath": true, "OpenServerPath": true,
	"GetFrpLog": true, "GetFrpSettings": true, "GetFrpStatus": true, "InstallFrp": true,
	"SaveFrpSettings": true, "StartFrp": true, "StopFrp": true,
	"DeleteMod": true, "DeleteOfficialWorkshopMod": true, "SetOfficialWorkshopModEnabled": true,
	"ToggleExtension": true, "ToggleMod": true, "UninstallServerMod": true, "UpdateAllExtensions": true, "UpdateExtension": true,
}

func validateLinuxWebRPCArguments(method string, args []json.RawMessage) error {
	switch method {
	case "QuickSetup":
		if len(args) > 1 {
			var installRoot string
			if json.Unmarshal(args[1], &installRoot) == nil && strings.TrimSpace(installRoot) != "" {
				return errors.New("Linux 服务器安装目录由 Agent 自动管理，不能从浏览器指定")
			}
		}
	case "SaveInstance":
		if len(args) > 0 {
			var instance ServerInstance
			if json.Unmarshal(args[0], &instance) == nil && strings.TrimSpace(instance.ID) == "" {
				return errors.New("请使用一键安装或从本地电脑迁移来创建 Linux 服务器")
			}
		}
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func invokeWebRPC(app *App, name string, rawArgs []json.RawMessage) (result any, err error) {
	if !webRPCMethods[name] {
		return nil, errors.New("method is not available through the web API")
	}
	method := reflect.ValueOf(app).MethodByName(name)
	if !method.IsValid() {
		return nil, errors.New("method was not found")
	}
	methodType := method.Type()
	if methodType.NumIn() != len(rawArgs) {
		return nil, fmt.Errorf("method expects %d arguments, received %d", methodType.NumIn(), len(rawArgs))
	}
	arguments := make([]reflect.Value, methodType.NumIn())
	for index := range arguments {
		argument := reflect.New(methodType.In(index))
		if err := json.Unmarshal(rawArgs[index], argument.Interface()); err != nil {
			return nil, fmt.Errorf("decode argument %d: %w", index+1, err)
		}
		arguments[index] = argument.Elem()
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("method panicked: %v", recovered)
		}
	}()
	outputs := method.Call(arguments)
	if len(outputs) > 0 {
		last := outputs[len(outputs)-1]
		errorType := reflect.TypeOf((*error)(nil)).Elem()
		if last.Type().Implements(errorType) {
			outputs = outputs[:len(outputs)-1]
			if !last.IsNil() {
				return nil, last.Interface().(error)
			}
		}
	}
	switch len(outputs) {
	case 0:
		return nil, nil
	case 1:
		return outputs[0].Interface(), nil
	default:
		values := make([]any, len(outputs))
		for index := range outputs {
			values[index] = outputs[index].Interface()
		}
		return values, nil
	}
}

func serveBackupZIP(app *App, writer http.ResponseWriter, request *http.Request, auditMethod, serverID, filename, source string) {
	if err := validateBackupZIPSource(source); err != nil {
		_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: auditMethod, ServerID: serverID, RemoteIP: requestClientIP(request), Successful: false, Error: auditError(err)})
		writeJSON(writer, http.StatusUnprocessableEntity, webRPCResponse{Error: "备份内容无法安全压缩：" + err.Error()})
		return
	}
	_ = http.NewResponseController(writer).SetWriteDeadline(time.Time{})
	writer.Header().Set("Content-Type", "application/zip")
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	err := writeBackupZIPContents(writer, source)
	_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: auditMethod, ServerID: serverID, RemoteIP: requestClientIP(request), Successful: err == nil, Error: auditError(err)})
}

func normalizeAgentPlatform(platform string) (string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	switch platform {
	case "windows", "linux":
		return platform, nil
	default:
		return "", fmt.Errorf("unsupported agent platform %q", platform)
	}
}

func newAgentHTTPHandler(app *App, auth *agentAuth) (http.Handler, error) {
	return newAgentHTTPHandlerForPlatform(app, auth, runtime.GOOS)
}

func newAgentHTTPHandlerForPlatform(app *App, auth *agentAuth, platform string) (http.Handler, error) {
	platform, err := normalizeAgentPlatform(platform)
	if err != nil {
		return nil, err
	}
	app.setReportedPlatform(platform)
	if err := cleanupAbandonedServerImports(); err != nil {
		return nil, fmt.Errorf("prepare browser server imports: %w", err)
	}
	staticRoot, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	jobs := newPersistentWebJobManager(app)
	mux.HandleFunc("GET /api/v1/health", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "version": LauncherVersion, "platform": platform, "authenticated": auth.validSession(request), "setupRequired": auth.setupRequired()})
	})
	mux.HandleFunc("POST /api/v1/setup", func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, 4096)
		clientIP := requestClientIP(request)
		if !auth.setupRequired() {
			writeJSON(writer, http.StatusConflict, webRPCResponse{Error: "管理密码已创建，请直接登录"})
			return
		}
		var setup agentSetupRequest
		if err := json.NewDecoder(request.Body).Decode(&setup); err != nil {
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: "请输入有效的管理密码"})
			return
		}
		if err := auth.setupPassword(setup.Password); err != nil {
			_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "CreateAdminPassword", RemoteIP: clientIP, Successful: false, Error: auditError(err)})
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		session, err := auth.createSession()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, webRPCResponse{Error: err.Error()})
			return
		}
		setAgentSessionCookie(writer, request, session)
		auth.recordLoginSuccess(clientIP)
		_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "CreateAdminPassword", RemoteIP: clientIP, Successful: true})
		writeJSON(writer, http.StatusCreated, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/session", func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, 4096)
		clientIP := requestClientIP(request)
		if auth.setupRequired() {
			writeJSON(writer, http.StatusConflict, webRPCResponse{Error: "请先创建管理密码"})
			return
		}
		if retry := auth.loginRetryAfter(clientIP, time.Now()); retry > 0 {
			writer.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Round(time.Second)/time.Second))))
			writeJSON(writer, http.StatusTooManyRequests, webRPCResponse{Error: "登录失败次数过多，请稍后再试"})
			return
		}
		var login agentLoginRequest
		decodeErr := json.NewDecoder(request.Body).Decode(&login)
		if decodeErr != nil || !auth.passwordMatches(login.Password) {
			if retry := auth.recordLoginFailure(clientIP, time.Now()); retry > 0 {
				writer.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Round(time.Second)/time.Second))))
				writeJSON(writer, http.StatusTooManyRequests, webRPCResponse{Error: "登录失败次数过多，请稍后再试"})
			} else {
				writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "管理密码错误"})
			}
			return
		}
		auth.recordLoginSuccess(clientIP)
		session, err := auth.createSession()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, webRPCResponse{Error: err.Error()})
			return
		}
		setAgentSessionCookie(writer, request, session)
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /api/v1/session", func(writer http.ResponseWriter, request *http.Request) {
		auth.deleteSession(request)
		secureCookie := agentRequestSecure(request)
		http.SetCookie(writer, &http.Cookie{Name: agentSessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/rpc/{method}", func(writer http.ResponseWriter, request *http.Request) {
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, 8<<20)
		var call webRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		method := request.PathValue("method")
		if platform == "linux" && linuxWebPathRPCMethods[method] {
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: "Linux 网页 Agent 不接受服务器主机路径；请使用页面提供的上传、下载或托管操作"})
			return
		}
		if platform == "linux" {
			if err := validateLinuxWebRPCArguments(method, call.Args); err != nil {
				writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
				return
			}
		}
		result, err := invokeWebRPC(app, method, call.Args)
		if webRPCMutatingMethods[method] {
			_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: method, ServerID: auditServerID(call.Args), RemoteIP: requestClientIP(request), Successful: err == nil, Error: auditError(err)})
		}
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		if platform == "linux" {
			result = sanitizeAgentWebResult(method, result)
		}
		writeJSON(writer, http.StatusOK, webRPCResponse{Result: result})
	})
	mux.HandleFunc("POST /api/v1/jobs/{method}", func(writer http.ResponseWriter, request *http.Request) {
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, 8<<20)
		var call webRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		method := request.PathValue("method")
		if platform == "linux" {
			if err := validateLinuxWebRPCArguments(method, call.Args); err != nil {
				writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
				return
			}
		}
		job, err := jobs.start(app, method, call.Args, requestClientIP(request))
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		writeJSON(writer, http.StatusAccepted, job)
	})
	mux.HandleFunc("GET /api/v1/jobs/{id}", func(writer http.ResponseWriter, request *http.Request) {
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		job, found := jobs.status(request.PathValue("id"))
		if !found {
			writeJSON(writer, http.StatusNotFound, webRPCResponse{Error: "后台任务不存在或已经过期"})
			return
		}
		if platform == "linux" && job.Result != nil {
			job.Result = sanitizeAgentWebResult(job.Method, job.Result)
		}
		writeJSON(writer, http.StatusOK, job)
	})
	mux.HandleFunc("GET /api/v1/jobs", func(writer http.ResponseWriter, request *http.Request) {
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
		writeJSON(writer, http.StatusOK, jobs.list(strings.TrimSpace(request.URL.Query().Get("server")), limit))
	})
	mux.HandleFunc("GET /api/v1/events", func(writer http.ResponseWriter, request *http.Request) {
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		flusher, ok := writer.(http.Flusher)
		if !ok {
			http.Error(writer, "streaming is unavailable", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Cache-Control", "no-cache")
		writer.Header().Set("X-Accel-Buffering", "no")
		// SSE is intentionally long-lived. Remove the server-wide response
		// deadline for this request and send heartbeats so reverse proxies do not
		// treat an otherwise quiet management session as idle.
		_ = http.NewResponseController(writer).SetWriteDeadline(time.Time{})
		id, events := app.events.subscribe()
		defer app.events.unsubscribe(id)
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		_, _ = fmt.Fprint(writer, ": connected\n\n")
		flusher.Flush()
		for {
			select {
			case event := <-events:
				data, _ := json.Marshal(event)
				_, _ = fmt.Fprintf(writer, "data: %s\n\n", data)
				flusher.Flush()
			case <-heartbeat.C:
				_, _ = fmt.Fprint(writer, ": heartbeat\n\n")
				flusher.Flush()
			case <-request.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("GET /api/v1/download/backup/{server}/{name}", func(writer http.ResponseWriter, request *http.Request) {
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		serverID, name := request.PathValue("server"), request.PathValue("name")
		if _, err := app.store.Find(serverID); err != nil {
			writeJSON(writer, http.StatusNotFound, webRPCResponse{Error: err.Error()})
			return
		}
		source, err := backupDownloadSource(serverID, name)
		if err != nil {
			writeJSON(writer, http.StatusNotFound, webRPCResponse{Error: err.Error()})
			return
		}
		serveBackupZIP(app, writer, request, "DownloadBackup", serverID, "palworld-backup-"+name+".zip", source)
	})
	mux.HandleFunc("GET /api/v1/download/official-backup/{server}/{name}", func(writer http.ResponseWriter, request *http.Request) {
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		serverID, name := request.PathValue("server"), request.PathValue("name")
		source, err := app.officialBackupDownloadSource(serverID, name)
		if err != nil {
			writeJSON(writer, http.StatusNotFound, webRPCResponse{Error: err.Error()})
			return
		}
		serveBackupZIP(app, writer, request, "DownloadOfficialBackup", serverID, "palworld-official-backup-"+name+".zip", source)
	})
	mux.HandleFunc("GET /api/v1/download/diagnostic/{server}", func(writer http.ResponseWriter, request *http.Request) {
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		serverID := request.PathValue("server")
		if _, err := app.store.Find(serverID); err != nil {
			writeJSON(writer, http.StatusNotFound, webRPCResponse{Error: err.Error()})
			return
		}
		files, err := diagnosticBundleFiles(app, serverID)
		if err != nil {
			_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "DownloadDiagnosticBundle", ServerID: serverID, RemoteIP: requestClientIP(request), Successful: false, Error: auditError(err)})
			writeJSON(writer, http.StatusInternalServerError, webRPCResponse{Error: "生成诊断包失败：" + err.Error()})
			return
		}
		_ = http.NewResponseController(writer).SetWriteDeadline(time.Time{})
		writer.Header().Set("Content-Type", "application/zip")
		writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "palserver-diagnostic-" + serverID + ".zip"}))
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		err = writeDiagnosticZIP(writer, files)
		_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "DownloadDiagnosticBundle", ServerID: serverID, RemoteIP: requestClientIP(request), Successful: err == nil, Error: auditError(err)})
	})
	mux.HandleFunc("POST /api/v1/upload/mods/{server}/{kind}", func(writer http.ResponseWriter, request *http.Request) {
		serverID := request.PathValue("server")
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, webUploadMaxBody)
		paths, cleanup, err := saveWebMultipartFiles(request, "files", webUploadMaxFileCount)
		if err == nil {
			defer cleanup()
			err = app.ImportMods(serverID, request.PathValue("kind"), paths)
		}
		_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "UploadMods", ServerID: safeAuditServerID(serverID), RemoteIP: requestClientIP(request), Successful: err == nil, Error: auditError(err)})
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/upload/server-mod/{server}/{catalog}", func(writer http.ResponseWriter, request *http.Request) {
		serverID := request.PathValue("server")
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, webUploadMaxBody)
		paths, cleanup, err := saveWebMultipartFiles(request, "files", 1)
		if err == nil {
			defer cleanup()
			err = app.InstallServerModArchive(serverID, request.PathValue("catalog"), paths[0])
		}
		_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "UploadServerMod", ServerID: safeAuditServerID(serverID), RemoteIP: requestClientIP(request), Successful: err == nil, Error: auditError(err)})
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/upload/server-import", func(writer http.ResponseWriter, request *http.Request) {
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, webServerImportMaxBody)
		uploadID, err := saveWebServerImport(request)
		if err != nil {
			_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "UploadServerImport", RemoteIP: requestClientIP(request), Successful: false, Error: auditError(err)})
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		name := ""
		if request.MultipartForm != nil {
			values := request.MultipartForm.Value["name"]
			if len(values) > 0 {
				name = strings.TrimSpace(values[0])
			}
		}
		args, err := marshalServerImportJobArgs(uploadID, name)
		if err != nil {
			if root, pathErr := serverImportDirectory(uploadID); pathErr == nil {
				_ = os.RemoveAll(root)
			}
			writeJSON(writer, http.StatusInternalServerError, webRPCResponse{Error: err.Error()})
			return
		}
		job, err := jobs.start(app, "ImportUploadedServer", args, requestClientIP(request))
		if err != nil {
			if root, pathErr := serverImportDirectory(uploadID); pathErr == nil {
				_ = os.RemoveAll(root)
			}
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "UploadServerImport", RemoteIP: requestClientIP(request), Successful: true})
		writeJSON(writer, http.StatusAccepted, job)
	})
	mux.HandleFunc("POST /api/v1/download/client-mods/{server}", func(writer http.ResponseWriter, request *http.Request) {
		serverID := request.PathValue("server")
		if !auth.validSession(request) {
			writeJSON(writer, http.StatusUnauthorized, webRPCResponse{Error: "需要登录 Agent 管理后台"})
			return
		}
		source, err := app.ExportClientMods(serverID)
		if err == nil {
			err = validateBackupZIPSource(source)
		}
		if err != nil {
			_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "DownloadClientMods", ServerID: safeAuditServerID(serverID), RemoteIP: requestClientIP(request), Successful: false, Error: auditError(err)})
			writeJSON(writer, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
			return
		}
		_ = http.NewResponseController(writer).SetWriteDeadline(time.Time{})
		writer.Header().Set("Content-Type", "application/zip")
		writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "palserver-client-mods-" + serverID + ".zip"}))
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		err = writeBackupZIPContents(writer, source)
		_ = app.appendAgentAudit(AgentAuditEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Method: "DownloadClientMods", ServerID: safeAuditServerID(serverID), RemoteIP: requestClientIP(request), Successful: err == nil, Error: auditError(err)})
	})
	static := http.FileServer(http.FS(staticRoot))
	mux.HandleFunc("GET /", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Pragma", "no-cache")
		path := strings.TrimPrefix(request.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(staticRoot, path); err == nil {
				static.ServeHTTP(writer, request)
				return
			}
		}
		request.URL.Path = "/"
		static.ServeHTTP(writer, request)
	})
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'")
		if !sameOriginAgentRequest(request) {
			writeJSON(writer, http.StatusForbidden, webRPCResponse{Error: "请求来源与 Agent 地址不一致"})
			return
		}
		mux.ServeHTTP(writer, request)
	}), nil
}

# PalDefender 与 UE4SS 自动更新实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 正确检查并安全更新 PalDefender 最新正式版和 UE4SS experimental-latest，在服务器运行时暂存、下次启动前应用并可回滚。

**架构：** 新建独立的扩展更新模块，负责版本源、下载暂存、迁移和回滚；现有 `files.go` 继续提供本地插件管理，`server.go` 在创建 PalServer 进程前应用 pending。前端先展示本地状态，再请求远程更新状态，运行中的服务器允许下载但不覆盖 DLL。

**技术栈：** Go 1.26、Wails 2、React 19、TypeScript、GitHub Releases API、Go `httptest`。

---

## 文件结构

- 创建 `extension_updates.go`：远程版本源、资产选择、暂存清单、应用、迁移、备份和回滚。
- 创建 `extension_updates_test.go`：扩展更新全部单元与 HTTP 测试。
- 修改 `models.go`：扩展插件状态与更新结果字段。
- 修改 `files.go`：新旧 UE4SS 路径识别、状态读取、更新入口委托。
- 修改 `server.go`：启动进程前应用 pending。
- 修改 `setup.go`：新建服务器使用同一 PalDefender 正式版安装逻辑。
- 修改 `frontend/src/App.tsx`：远程检查、更新提示、运行中暂存和批量更新。
- 修改 `core_test.go`：保留现有安装状态与前端行为回归测试。

### 任务 1：版本源和远程更新状态

**文件：**
- 创建：`extension_updates.go`
- 创建：`extension_updates_test.go`
- 修改：`models.go`

- [ ] **步骤 1：编写失败的版本源测试**

```go
func TestExtensionReleaseSourcesUsePalDefenderStableAndUE4SSExperimental(t *testing.T) {
	pd, _ := extensionReleaseSourceFor("paldefender")
	ue, _ := extensionReleaseSourceFor("ue4ss")
	if !strings.HasSuffix(pd.Endpoint, "/Ultimeit/PalDefender/releases/latest") {
		t.Fatalf("PalDefender endpoint = %q", pd.Endpoint)
	}
	if !strings.HasSuffix(ue.Endpoint, "/UE4SS-RE/RE-UE4SS/releases/tags/experimental-latest") {
		t.Fatalf("UE4SS endpoint = %q", ue.Endpoint)
	}
}

func TestSelectUE4SSExperimentalAssetRejectsZDEV(t *testing.T) {
	release := githubRelease{Assets: []githubReleaseAsset{
		{Name: "zDEV-UE4SS_v3.0.1-1011.zip"},
		{Name: "UE4SS_v3.0.1-1011-gb50986bd.zip", UpdatedAt: "2026-07-13T00:29:54Z"},
	}}
	asset, version, err := selectExtensionAsset("ue4ss", release)
	if err != nil || asset.Name != "UE4SS_v3.0.1-1011-gb50986bd.zip" || version != "v3.0.1-1011-gb50986bd" {
		t.Fatalf("asset=%#v version=%q err=%v", asset, version, err)
	}
}
```

- [ ] **步骤 2：运行测试确认失败**

运行：`go test ./... -run 'TestExtensionReleaseSources|TestSelectUE4SSExperimentalAsset' -count=1`

预期：FAIL，提示 `extensionReleaseSourceFor` 或 `selectExtensionAsset` 未定义。

- [ ] **步骤 3：实现最小版本源与资产选择**

在 `extension_updates.go` 定义：

```go
type extensionReleaseSource struct {
	ID       string
	Endpoint string
}

type extensionReleaseInfo struct {
	ExtensionID string
	Version     string
	Asset       githubReleaseAsset
	PublishedAt string
}
```

PalDefender 精确选择 `PalDefender.zip`；UE4SS 选择非 zDEV 的 `UE4SS_*.zip` 并从文件名提取版本。

- [ ] **步骤 4：扩展状态模型并实现远程检查**

在 `ExtensionStatus` 增加：

```go
LatestVersion   string `json:"latestVersion"`
LatestAsset     string `json:"latestAsset"`
LatestUpdatedAt string `json:"latestUpdatedAt"`
UpdateAvailable bool   `json:"updateAvailable"`
UpdateCheckError string `json:"updateCheckError"`
Pending         bool   `json:"pending"`
PendingVersion  string `json:"pendingVersion"`
```

新增 `CheckExtensionUpdates(id string) ([]ExtensionStatus, error)`，使用 30 秒 GitHub 客户端并保留单个扩展的检查错误。

- [ ] **步骤 5：验证任务 1**

运行：`go test ./... -run 'TestExtensionRelease|TestSelectUE4SS|TestCheckExtensionUpdates' -count=1`

预期：PASS。

### 任务 2：下载暂存与清单

**文件：**
- 修改：`extension_updates.go`
- 修改：`extension_updates_test.go`

- [ ] **步骤 1：编写失败的暂存测试**

```go
func TestStageExtensionUpdateDoesNotTouchRunningServerFiles(t *testing.T) {
	root := t.TempDir()
	instance := ServerInstance{ID: "server-1", RootPath: root}
	old := filepath.Join(win64Path(instance), "PalDefender.dll")
	_ = os.MkdirAll(filepath.Dir(old), 0o755)
	_ = os.WriteFile(old, []byte("old"), 0o600)

	manifest, err := stageExtensionPayload(instance, extensionReleaseInfo{
		ExtensionID: "paldefender", Version: "v1.8.3",
	}, fixtureDirectory(t, map[string]string{"PalDefender.dll": "new", "d3d9.dll": "loader"}))
	if err != nil || manifest.Version != "v1.8.3" {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	data, _ := os.ReadFile(old)
	if string(data) != "old" {
		t.Fatalf("running server file changed to %q", data)
	}
}
```

- [ ] **步骤 2：运行测试确认失败**

运行：`go test ./... -run TestStageExtensionUpdateDoesNotTouchRunningServerFiles -count=1`

预期：FAIL，提示暂存函数或清单类型未定义。

- [ ] **步骤 3：实现暂存目录、清单与结构校验**

定义 `extensionUpdateManifest`，将 payload 放入 `Win64/.palserver-launcher/staged/<extension>/pending/payload`，清单原子写入 `manifest.json`。校验：

```go
func validateStagedExtension(extensionID, payload string) error {
	switch extensionID {
	case "paldefender":
		return requireFiles(payload, "PalDefender.dll", "d3d9.dll")
	case "ue4ss":
		return requireFiles(payload, "dwmapi.dll", filepath.Join("ue4ss", "UE4SS.dll"))
	default:
		return errors.New("unknown extension")
	}
}
```

下载失败、解压失败或校验失败时删除 `.incoming-*`，成功后通过同卷重命名替换旧 pending。

- [ ] **步骤 4：实现更新入口**

将 `UpdateExtension` 改为返回 `ExtensionUpdateResult`：

```go
type ExtensionUpdateResult struct {
	ExtensionID string `json:"extensionId"`
	Version     string `json:"version"`
	Pending     bool   `json:"pending"`
	Message     string `json:"message"`
}
```

运行中只暂存；停止时暂存后立即应用。新增 `UpdateAllExtensions(id)` 依次处理已安装扩展并返回结果列表。暂存与备份都在 Win64 下，保证和目标 DLL 同卷。

- [ ] **步骤 5：验证任务 2**

运行：`go test ./... -run 'TestStageExtension|TestValidateStaged|TestUpdateAllExtensions' -count=1`

预期：PASS。

### 任务 3：备份、迁移、应用和回滚

**文件：**
- 修改：`extension_updates.go`
- 修改：`extension_updates_test.go`
- 修改：`files.go`

- [ ] **步骤 1：编写失败的 PalDefender 配置迁移测试**

```go
func TestMigratePalDefenderConfigRemovesObsoleteCrashOption(t *testing.T) {
	input := []byte(`{"version":"1.8.1","blockTowerBossCapture":true,"logChat":false}`)
	updated, err := migratePalDefenderConfig(input)
	if err != nil || bytes.Contains(updated, []byte("blockTowerBossCapture")) || !bytes.Contains(updated, []byte(`"logChat": false`)) {
		t.Fatalf("updated=%s err=%v", updated, err)
	}
}
```

- [ ] **步骤 2：编写失败的 UE4SS 布局迁移测试**

创建旧 `Win64/Mods/CustomMod`、旧 `mods.txt` 和旧设置，应用 fixture experimental payload 后断言：

- `Win64/ue4ss/UE4SS.dll` 存在；
- `Win64/ue4ss/Mods/CustomMod` 被保留；
- 原启用状态被合并；
- `bUseUObjectArrayCache=false`、`GuiConsoleEnabled=0`；
- 旧根目录 `UE4SS.dll` 和旧 `Mods` 已移除。

- [ ] **步骤 3：运行迁移测试确认失败**

运行：`go test ./... -run 'TestMigratePalDefender|TestApplyUE4SSPendingUpdate' -count=1`

预期：FAIL，迁移与应用函数未定义。

- [ ] **步骤 4：实现扩展备份与应用**

实现：

```go
func applyPendingExtensionUpdate(instance ServerInstance, extensionID string) error
func applyPalDefenderPayload(instance ServerInstance, payload string) error
func applyUE4SSPayload(instance ServerInstance, payload string) error
func restoreExtensionBackup(instance ServerInstance, extensionID, backup string) error
```

应用顺序为：读取 pending → 创建完整备份 → 复制到 `.incoming` → 迁移设置/Mods → 交换目标 → 验证 → 写版本元数据 → 删除 pending。失败时回滚并保留备份。

- [ ] **步骤 5：编写并验证回滚测试**

通过缺少 `UE4SS.dll` 或注入失败回调制造应用失败，断言旧 DLL、旧设置和旧 Mods 内容恢复。

运行：`go test ./... -run 'TestApplyPendingExtensionRollsBack|TestExtensionBackupRetention' -count=1`

预期：PASS，并且每个扩展只保留最近三份备份。

### 任务 4：启动流程和新服务器安装集成

**文件：**
- 修改：`server.go`
- 修改：`setup.go`
- 修改：`extension_updates_test.go`

- [ ] **步骤 1：编写失败的启动前应用测试**

将“启动前准备”提取为可测试函数：

```go
func prepareServerBeforeLaunch(instance ServerInstance) error {
	if err := applyPendingExtensionUpdates(instance); err != nil {
		return fmt.Errorf("apply pending extension updates: %w", err)
	}
	return nil
}
```

测试 pending 应用失败时该函数返回错误且未进入进程创建函数。

- [ ] **步骤 2：运行测试确认失败**

运行：`go test ./... -run TestServerStartAppliesPendingExtensionsBeforeProcessLaunch -count=1`

预期：FAIL，准备函数或 pending 集成不存在。

- [ ] **步骤 3：接入 `StartServer`**

在确认服务器未运行、端口可用和程序存在后，DirectX/Engine 配置及 `exec.Command` 之前调用 pending 应用；失败时保持服务器停止。

- [ ] **步骤 4：统一新建服务器 PalDefender 安装**

`setup.go` 使用 PalDefender release source 和相同资产校验，不再直接调用通用 `downloadLatestRelease`。新建服务器安装失败继续保持非致命提示。

- [ ] **步骤 5：验证任务 4**

运行：`go test ./... -run 'TestServerStartAppliesPending|TestManagedSetupUsesPalDefenderStable' -count=1`

预期：PASS。

### 任务 5：插件页面更新体验

**文件：**
- 修改：`frontend/src/App.tsx`
- 修改：`frontend/src/App.css`
- 修改：`core_test.go`

- [ ] **步骤 1：添加失败的前端源码行为测试**

在 `core_test.go` 检查插件页面调用 `CheckExtensionUpdates`，运行中更新按钮仍可用，并展示 pending/更新状态；测试在实现前应失败。

- [ ] **步骤 2：实现异步检查与状态展示**

插件页加载流程：

```tsx
const refresh = useCallback(async () => {
  setItems(await API.ListExtensions(id));
  try { setItems(await API.CheckExtensionUpdates(id)); }
  catch { /* 保留本地状态 */ }
}, [id]);
```

卡片展示本地版本、远端版本、检查错误、发现更新和 pending。更新按钮在服务器运行时显示“下载更新”，服务器停止时显示“安装/更新”。

- [ ] **步骤 3：增加批量更新按钮**

新增“更新全部”按钮调用 `UpdateAllExtensions`；完成后使用返回的 `message` 告知“已更新”或“已下载，重启后生效”。切换启用状态仍要求服务器停止。

- [ ] **步骤 4：生成 Wails 绑定并验证前端**

运行：

```text
wails generate module
cd frontend
npm run build
```

预期：TypeScript 和 Vite 构建均成功。

### 任务 6：完整验证、编译和当前服务器暂存

**文件：**
- 验证全部修改文件
- 生成：`build/bin/palserver-launcher.exe`

- [ ] **步骤 1：运行完整测试**

运行：`go test ./... -count=1`

预期：全部通过。

- [ ] **步骤 2：运行生产构建**

运行：`wails build`

预期：生成 `D:\palserver-GUI-main\build\bin\palserver-launcher.exe`。

- [ ] **步骤 3：检查现有服务器不被停止**

确认当前 PalServer PID 和启动时间未改变，且 UDP 8211、TCP 8212/25575 仍属于原进程。

- [ ] **步骤 4：为当前服务器下载两个 pending 更新**

使用新实现的批量更新入口为 `srv-e62e7d3b3ae52ba1` 暂存 PalDefender `v1.8.3` 和 UE4SS `v3.0.1-1011-gb50986bd`，然后检查两个 `manifest.json` 与 payload 校验通过；不得覆盖当前正在加载的 DLL。

- [ ] **步骤 5：最终状态核对**

确认插件状态显示两个 pending 版本，当前磁盘加载版本仍为 PalDefender `v1.8.1`、UE4SS `v3.0.1`，并明确“下次从新版启动器启动服务器前自动应用”。

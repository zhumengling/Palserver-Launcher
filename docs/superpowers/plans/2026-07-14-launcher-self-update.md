# 启动器自升级实现计划

> **面向 AI 代理的工作者：** 在当前会话内按 TDD 顺序逐项实现并验证。

**目标：** 实现检查 GitHub Release、展示更新说明、用户确认下载、校验、自动替换和重启的 Windows 启动器自升级。

**架构：** 新增独立 `launcher_update.go` 管理 Release、下载和 updater 调度，`launcher_updater_windows.go` 负责 Windows 文件替换、提权与回滚。React 根组件管理全局更新弹窗和进度，当前版本完全由 Go 后端提供。

**技术栈：** Go 1.25、Wails 2、React 19、TypeScript、GitHub REST API、Windows ShellExecute。

---

### 任务 1：版本与 Release 选择

**文件：**
- 创建：`launcher_update.go`
- 修改：`models.go`
- 测试：`launcher_update_test.go`

- [ ] 编写版本标准化、比较、正式 Release 过滤和 Windows amd64 EXE 选择的失败测试。
- [ ] 运行 `go test -count=1 -run 'TestLauncherVersion|TestSelectLauncher' .`，确认因函数缺失失败。
- [ ] 实现 `normalizeLauncherVersion`、`compareLauncherVersions`、`selectLauncherReleaseAsset` 和公开状态模型。
- [ ] 重跑目标测试并确认通过。

### 任务 2：校验和替换回滚

**文件：**
- 创建：`launcher_updater_windows.go`
- 创建：`launcher_updater_other.go`
- 测试：`launcher_update_test.go`

- [ ] 编写 SHA256 文件校验、Windows 参数引用、替换成功和失败回滚测试。
- [ ] 运行目标测试并确认因替换函数缺失失败。
- [ ] 实现 `verifyLauncherSHA256File`、`quoteWindowsArg`、`replaceLauncherExecutable`。
- [ ] 重跑目标测试并确认通过。

### 任务 3：GitHub 查询、下载和 updater 调度

**文件：**
- 修改：`launcher_update.go`
- 修改：`launcher_updater_windows.go`
- 修改：`main.go`
- 修改：`app.go`
- 测试：`launcher_update_test.go`

- [ ] 编写 HTTP 测试服务器测试 Release 解析、下载进度和摘要拒绝路径。
- [ ] 运行目标测试并确认失败。
- [ ] 实现 `CheckLauncherUpdate`、`ApplyLauncherUpdate`、下载进度事件、updater 副本启动及 `main` 的 updater 模式入口。
- [ ] 重跑 Go 测试并确认通过。

### 任务 4：更新弹窗与手动检查入口

**文件：**
- 修改：`frontend/src/App.tsx`
- 修改：`frontend/src/App.css`
- 生成：`frontend/wailsjs/go/main/App.d.ts`
- 生成：`frontend/wailsjs/go/main/App.js`
- 生成：`frontend/wailsjs/go/models.ts`

- [ ] 在根组件加入启动静默检查、手动检查状态、更新进度事件和确认操作。
- [ ] 将左侧版本号改为可点击的检查入口，并在有更新时显示提示点。
- [ ] 新增纯文本更新说明弹窗、下载进度、取消和重试状态。
- [ ] 运行 `wails generate module` 更新绑定，再运行前端构建。

### 任务 5：完整验证

**文件：**
- 检查：所有本次变更

- [ ] 设置 D 盘临时目录和缓存，运行 `go test -count=1 ./...`。
- [ ] 运行 `npm run build --prefix frontend`。
- [ ] 正常关闭正在运行的旧启动器后运行 `wails build`。
- [ ] 检查 `git diff --check` 和 `git status --short`，确认没有临时更新文件进入源码。

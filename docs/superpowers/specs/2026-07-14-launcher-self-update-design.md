# 启动器自升级设计

## 目标

为 Windows 版 Palserver Launcher 增加完整的自升级流程：从 GitHub 正式 Release 检查新版本，展示版本与更新说明，在用户确认后下载并校验新版 EXE，自动替换当前程序并重启。

## 发布源与版本规则

- 发布源固定为 `zhumengling/Palserver-Launcher` 的 GitHub Releases API。
- 当前版本由 Go 常量 `LauncherVersion` 提供，值为 `0.1.0`；前端只从后端读取，不再维护独立显示版本。
- 版本号接受 `v0.1`、`0.1.0`、`v0.2.0` 等形式，缺失的数字段补零后按整数比较。
- 忽略 draft 和 prerelease，只接受最新正式 Release。
- Windows 资产必须是名称以 `palserver-launcher-` 开头、以 `-windows-amd64.exe` 结尾的 EXE。

## 检查与展示

- 应用启动后静默检查一次；网络失败不打扰用户。
- 左侧底部显示当前版本和手动检查按钮。
- 发现新版本时打开更新弹窗，展示当前版本、最新版本、发布时间、资产大小和 Release 更新说明。
- 手动检查会显示“已是最新版”或明确错误；自动检查只在发现新版时提示。
- Release 更新说明按纯文本展示，避免将远端 Markdown/HTML 注入应用。

## 下载与校验

- 用户点击“下载并重启”后，后端重新查询 Release，避免信任前端传回的 URL 或摘要。
- 新版下载到 `%LOCALAPPDATA%\palserver-launcher\updates\<version>\`，通过 Wails 事件实时发送下载百分比和状态文字。
- 优先使用 GitHub Release 资产的 `digest` 字段，要求为 SHA256。缺少合法摘要时终止升级，不安装未校验文件。
- 下载先写 `.download` 临时文件，校验通过后再改名为稳定文件。

## 替换、提权与回滚

- 主 EXE 将自身复制到更新目录作为一次性 updater，并以 `--apply-launcher-update` 模式启动该副本。
- updater 等待原进程退出，先把旧 EXE 改名为 `.old`，再把新版移动到原位置，最后重启原路径。
- 新 EXE 启动成功后由 updater 删除旧文件；替换或重启失败则恢复旧 EXE并尝试启动旧版本。
- updater 普通启动失败且目标目录不可写时，通过 Windows `runas` 请求 UAC 提权后执行相同流程。
- 参数通过独立 argv 传递；提权场景使用 Windows 命令行引用规则，支持空格和引号路径。

## 失败处理

- Release 查询失败、无匹配资产、摘要缺失、下载中断或校验失败均保留当前版本。
- 替换失败时回滚旧 EXE，并在更新目录写入 `update-error.log` 供排查。
- 用户取消弹窗不会产生任何文件或进程变更。

## 测试

- 单元测试覆盖版本标准化与比较、正式 Release 判定、Windows EXE 资产选择、SHA256 成功/失败。
- 文件级测试覆盖替换成功和替换失败回滚。
- 参数测试覆盖带空格、引号的 Windows 路径。
- 完成后运行 Go 全量测试、前端 TypeScript/Vite 构建和 Wails Windows 构建。

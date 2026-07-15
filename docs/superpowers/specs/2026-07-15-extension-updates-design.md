# PalDefender 与 UE4SS 自动更新设计

## 目标

让启动器正确发现、下载并安全应用 PalDefender 与适用于 Palworld 的 UE4SS 最新版本。服务器运行时只下载到暂存区，不覆盖已加载的 DLL；服务器下次启动前自动完成备份、迁移、验证和必要的回滚。

## 当前问题

- PalDefender 只在创建服务器或用户手动点击更新时查询 GitHub，插件页没有远程版本检查，因此新版本发布后不会提示。
- UE4SS 更新逻辑固定调用 GitHub `releases/latest`。该接口排除预发布版本，只返回 2024 年的 stable `v3.0.1`，无法看到为当前 Palworld 持续构建的 `experimental-latest`。
- `experimental-latest` 标签长期不变，不能用标签判断是否更新；必须记录具体资产名称和更新时间。
- 旧 UE4SS 使用 Win64 根目录平铺结构，新 experimental 包使用 `Win64/ue4ss` 子目录，需要迁移设置和 Mods。
- 现有更新直接解压到服务器目录，没有完整备份、原子替换和失败回滚。

## 版本源

### PalDefender

- API：`https://api.github.com/repos/Ultimeit/PalDefender/releases/latest`
- 资产：精确选择 `PalDefender.zip`，大小写不敏感。
- 展示版本：Release tag，例如 `v1.8.3`。
- 更新判断：本地版本与远端 tag 不同，或本地安装元数据缺失。

### UE4SS

- API：`https://api.github.com/repos/UE4SS-RE/RE-UE4SS/releases/tags/experimental-latest`
- 资产：选择 `UE4SS_*.zip`，排除 `zDEV-*`、`zCustomGameConfigs.zip` 和 `zMapGenBP.zip`。
- 展示版本：从资产名提取，例如 `UE4SS_v3.0.1-1011-gb50986bd.zip` 显示为 `v3.0.1-1011-gb50986bd`。
- 更新判断：优先比较资产名称和 `updated_at`；标签仅作为通道标识。

## 数据模型

扩展本地状态包含：

- 已安装版本、是否安装、是否启用；
- 最新版本、最新资产名称和发布时间；
- 是否存在更新、检查错误；
- 是否已有暂存更新、暂存版本；
- 当前实际路径，兼容 UE4SS 新旧布局。

暂存目录位于本机应用数据目录：

```text
%LOCALAPPDATA%/palserver-launcher/extensions/<server-id>/<extension-id>/pending/
```

其中保存解压后的 payload 和 `manifest.json`。清单包含扩展 ID、版本、资产名、资产更新时间、下载时间和目标布局版本。

备份目录位于：

```text
%LOCALAPPDATA%/palserver-launcher/extension-backups/<server-id>/<extension-id>/<timestamp>/
```

## 更新流程

1. 插件页面先读取本地状态，再并行请求两个远端版本源。
2. 用户点击更新后，下载资产到与暂存目录相同的磁盘并安全解压。
3. 校验暂存内容：PalDefender 必须含 `PalDefender.dll` 与 `d3d9.dll`；UE4SS 必须含根目录代理 DLL 和 `ue4ss/UE4SS.dll`。
4. 若服务器正在运行，保留暂存内容并显示“等待服务器重启后应用”。
5. 若服务器已停止，立即调用相同的应用流程。
6. `StartServer` 在启动游戏进程之前应用所有 pending 更新；失败则阻止启动并返回明确错误。
7. 应用前备份所有将被管理或迁移的文件。应用后重新验证；验证失败时删除不完整的新文件并恢复备份。

## PalDefender 迁移

- 更新二进制和加载器，不删除 `PalDefender` 数据目录。
- 保留 `Config.json`、`RESTAPI/RESTConfig.json`、Banlist、Whitelist、Pals 模板和导入规则。
- 对 JSON 配置只删除已由 v1.8.3 废弃且已知会触发崩溃风险的 `blockTowerBossCapture`，其他用户配置保持原值。
- 保持更新前的启用/停用状态。

## UE4SS 迁移

- 使用 experimental 包的新布局：根目录只保留代理 DLL，核心、设置和内置 Mods 位于 `Win64/ue4ss`。
- 将旧 `Win64/Mods` 中的自定义模组和启用列表合并到新 `Win64/ue4ss/Mods`。
- 以新包设置文件为基线，只迁移仍存在的用户键；强制采用 headless 安全值：`ConsoleEnabled=0`、`GuiConsoleEnabled=0`、`GuiConsoleVisible=0`、`bUseUObjectArrayCache=false`。
- 保持 UE4SS 更新前的启用/停用状态。
- 成功后移除已备份的旧根目录 `UE4SS.dll`、旧设置文件和旧 Mods 目录，避免新旧运行时混用。

## 界面行为

- 插件卡片展示本地版本和远端最新版本。
- 状态区分“已是最新”“发现更新”“已下载，等待重启”“检查失败”。
- 服务器运行时允许“下载更新”，但不允许切换启用状态。
- 更新完成后刷新本地与远端状态；错误信息保留 GitHub 状态码、下载、校验或回滚阶段。

## 错误处理

- GitHub 不可用不会改变本地插件状态。
- 下载或解压失败时删除不完整暂存目录。
- 资产结构不符合预期时拒绝暂存。
- 应用失败时恢复备份；若回滚也失败，错误同时包含原始失败与回滚失败，并保留备份目录供人工恢复。
- 不在正在运行的 PalServer 目录中直接覆盖 DLL。

## 测试

- Release 端点和资产选择：PalDefender latest、UE4SS experimental、排除 zDEV。
- UE4SS 资产版本从文件名提取，以及同一 tag 下资产更新时间变化仍能发现更新。
- 服务器运行时只产生 pending，不修改目标 DLL。
- PalDefender 配置保留并移除废弃键。
- UE4SS 新旧目录迁移、Mods 合并和 headless 安全设置。
- 暂存校验失败、应用失败和回滚恢复。
- `StartServer` 在创建进程前应用 pending，应用失败时不启动。
- 前端 TypeScript 构建验证更新状态字段和按钮行为。

## 默认决策

- 当前运行服务器不被自动停止；更新在下次由启动器启动服务器前生效。
- PalDefender 使用最新正式版；UE4SS 使用官方 experimental-latest 非 zDEV 包。
- 不自动安装未安装的 UE4SS；只检查和更新已安装扩展，用户手动安装仍通过插件页完成。
- 仅保留最近三份每扩展备份，避免长期占用磁盘。

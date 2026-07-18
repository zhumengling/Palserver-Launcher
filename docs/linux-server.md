# Linux 后台服务与网页控制台

Palworld 官方 1.0 文档确认支持 Linux 64 位（Ubuntu、AlmaLinux 等），推荐至少 4 核、16 GB 内存和 SSD；大型服务器建议 32 GB 以上。默认游戏端口是 UDP 8211。

官方文档同时提供 Linux SteamCMD 和官方 Docker 镜像两种 Linux 部署方式。本项目当前默认采用原生 SteamCMD + `PalServer.sh`，这样可以直接管理进程、CPU 亲和性、存档和日志，不依赖 Docker。官方 Docker 镜像可用于已有容器平台的高级部署，但 Docker Desktop 因存储 I/O 和存档损坏风险不建议用于正式服。

官方 Linux SteamCMD 安装命令：

```bash
steamcmd +login anonymous +app_update 2394010 validate +quit
```

官方启动入口为 `PalServer.sh`。本项目的 Linux Agent 会自动准备 SteamCMD、执行相同的 App ID 安装流程、生成 `LinuxServer/PalWorldSettings.ini`，并通过网页提供实例、启停、更新、备份、官方 REST、玩家、活动、FRP、性能和维护管理。

## 安装

新 Linux 主机可以直接使用一键安装脚本。它会从 GitHub Releases 下载最新 Agent、校验 SHA-256、安装系统依赖并注册 systemd 服务：

```bash
curl -fsSL https://raw.githubusercontent.com/zhumengling/Palserver-Launcher/main/deploy/linux/install-online.sh | sudo bash
```

脚本默认安装最新 Release。需要固定版本时可以这样执行：

```bash
curl -fsSL https://raw.githubusercontent.com/zhumengling/Palserver-Launcher/main/deploy/linux/install-online.sh | sudo env PALSERVER_VERSION=0.1.5 bash
```

如果仓库使用私有镜像或内部 Release，可设置 `PALSERVER_REPOSITORY`、`PALSERVER_VERSION`、`PALSERVER_RELEASE_URL` 和 `PALSERVER_CHECKSUM_URL` 覆盖下载地址。安装脚本只接受带 SHA-256 校验文件的 Release，校验失败不会替换现有 Agent。

下载并解压 `palserver-agent-linux-amd64.tar.gz`，进入解压目录后执行：

```bash
chmod +x install.sh
sudo ./install.sh
```

源码构建时先生成 `build/bin/pal-agent-linux-amd64`，再运行 `go run ./cmd/package-linux`。该跨平台打包器会固定归档文件顺序、时间戳、UID/GID 和执行权限，并同时生成 `palserver-agent-linux-amd64.tar.gz.sha256`；因此在 Windows 开发机上生成的安装包也可以直接用于 Linux，不依赖 Windows 文件权限。

安装包内的 `pal-agent`、`install.sh`、`uninstall.sh` 和
`palserver-agent.service` 必须保持在同一目录。安装脚本会把 Agent 安装到
`/var/lib/palserver-launcher/bin/pal-agent`，不依赖当前目录继续运行。

重复执行 `sudo ./install.sh` 可以升级现有 Agent。安装器会先把新二进制写入独立临时文件并执行 `--version` 验证，再备份旧二进制和 systemd 单元、原子替换、重启服务，并请求 `/api/v1/health` 完成启动检查。新版本启动失败时会自动恢复旧二进制和旧服务文件，不会覆盖服务器、存档、管理密码、配置或 `secrets.key`。

替换正式文件前，安装器还会以`palserver`服务用户运行`pal-agent --self-test`，实际验证`/var/lib/palserver-launcher`、`/var/lib/palserver`、`/var/lib/palserver-launcher/servers`、认证目录和`/proc`访问权限。自检失败时升级会在替换旧版本前终止。手工部署也可以使用相同命令，并读取输出的JSON检查项。

安装完成后，网页“能力中心”会再次显示同一份Agent部署自检结果，管理员无需登录SSH即可检查服务用户、目录权限、允许的服务器根目录和文件句柄状态。Windows上的Web Preview只显示“Linux平台模拟”警告，不会伪造真实Linux权限检查。

服务默认只监听 `127.0.0.1:8210`。本地访问推荐SSH隧道：

```bash
ssh -L 8210:127.0.0.1:8210 user@server
```

然后打开 `http://127.0.0.1:8210`。第一次访问时在网页中创建管理密码，之后使用该密码登录。密码只以加盐 Argon2id 摘要保存于 `/var/lib/palserver-launcher/admin-auth.json`，不会写入明文。

开发或部署前可以执行 `bash deploy/linux/smoke-test.sh <pal-agent路径>`，它会在临时目录启动Agent，验证健康检查、首次创建密码、认证RPC和正常退出，不会安装systemd服务或修改服务器实例。

源码测试还包含离线 Linux 一键安装回归：测试使用本地假 SteamCMD 执行与正式流程相同的 `+runscript` 和 App ID 2394010 安装脚本，并要求安装结束后同时存在可执行的 `PalServer.sh` 与 `PalServer-Linux-Shipping`。因此 SteamCMD 即使异常返回成功但没有真正生成服务器文件，Agent 也不会再把实例保存成“安装成功”。

如果暂时没有Linux主机，可以在Windows开发机运行：

```powershell
npm run build --prefix frontend
go run -tags webpreview .
```

然后访问`http://127.0.0.1:18210`。该模式强制只监听本机，并使用临时隔离数据目录，默认模拟 Linux Agent 的平台标识，适合检查登录、服务器列表、设置页面、Linux 功能限制和安装前环境诊断。可以传入`--platform windows`切换到 Windows 网页视图；平台模拟只改变前端能力展示，文件和进程操作仍使用开发机的真实操作系统。

## 公网访问

不要直接暴露Palworld REST或RCON。网页控制台公网访问应使用以下任一方式：

- WireGuard或Tailscale；
- Caddy/Nginx HTTPS反向代理；
- Agent自身的`--tls-cert`与`--tls-key`参数。

非回环地址上的明文HTTP默认会被拒绝。只有明确传入`--allow-http`才会允许。

网页后台会按客户端地址限制连续失败登录，并对跨站写请求进行来源校验。第一次访问必须先创建管理密码，初始化入口在成功后立即关闭。启动、停止、更新、玩家操作、配置保存等写操作记录在数据目录的`audit/web-agent.jsonl`，日志不会保存密码、认证摘要或RPC参数，达到5 MB后自动轮转。

## 官方限制

Palworld官方文档目前注明：服务端模组只支持Windows专用服务器。因此Linux界面会保留模组页面和状态说明，但会禁用UE4SS、PalDefender及官方服务端Workshop部署按钮，避免误装Windows DLL导致Linux服务端无法启动。官方后续开放Linux服务端模组后，可以通过平台能力层直接启用。

## 网页功能对齐

Linux Agent 与 Windows 桌面端复用同一套 React 页面和 Go 业务方法。网页端已覆盖实例创建与导入、启停与更新、控制台、官方 REST、玩家管理、世界设置、自动化、活动、启动器备份、官方自动备份、只读存档浏览、FRP、诊断、性能和 Agent 自更新。启动器备份及官方 `backup/world` 记录可以在认证后直接流式下载为 ZIP，服务器真实路径不会暴露在下载地址中，下载结果会写入脱敏审计日志。网页运行在支持 Windows 服务端模组的平台时，还可以上传普通模组和 Nexus ZIP，并直接下载生成的客户端模组包；Linux 仍按 Palworld 官方限制禁用服务端模组写操作。

Linux 网页端按“Agent 在服务器上运行、管理员从另一台电脑使用浏览器”的模型实现。新建服务器时只输入名称；迁移旧服务器时从浏览器选择本地 ZIP、tar.gz 或服务器文件夹，不再输入 Linux 路径。文件夹选择会先在浏览器端筛选 `Pal/Saved`、`Saved/SaveGames` 和世界配置，Agent 再独立校验并智能识别目录层级。迁移时始终通过 SteamCMD 安装一套全新的官方 Linux 服务端，只复制 SaveGames、PalWorldSettings.ini 和密码；Windows EXE、UE4SS、PalDefender DLL、日志与缓存不会进入 Linux 实例。同名实例自动创建独立子目录，失败时回滚新实例和未完成文件。

网页 RPC 响应不返回服务器根目录、执行文件、SteamCMD、插件、FRP 或存档解析器的绝对路径。实例编辑页面不显示高级路径，维护工具也改为 Agent 托管说明；备份恢复使用备份名称作为不透明标识。Linux 文件布局、权限和可执行文件只由 Agent 内核管理，浏览器用户不需要知道服务器主机的文件系统结构。

进程监控、官方 API 页面和玩家页面共享同一套 REST 采集缓存，并会合并同一时刻的并发请求。服务器信息缓存 60 秒、性能指标缓存 3 秒、玩家列表缓存 2 秒、运行时设置缓存 30 秒、GameData 世界快照缓存 15 秒；玩家操作和配置保存后会主动清除相关缓存。这可以减少 Palworld 控制台中重复的 REST accessed 日志，也避免多个浏览器页面同时刷新时给服务端增加不必要负载。

“网络诊断”页面可以生成一键问题排查包。诊断包只收集服务器状态、平台能力、插件兼容报告、脱敏后的世界配置，以及服务器、SteamCMD、FRP 日志各自最后 1 MB；不会包含存档、管理员密码、服务器密码、玩家 IP、公网 IP 或用户主目录路径。网页下载同样需要已认证会话并记录审计。

启动器创建的新存档备份会在复制文件的同时计算 SHA-256，并在备份根目录生成 `.palserver-backup.json`。恢复前会校验全部文件的路径、大小和摘要，拒绝被修改、缺失、重复、符号链接或清单外新增的文件；复制到恢复暂存目录后还会再次校验，再通过目录原子交换替换正式存档。旧版本创建且没有清单的备份仍可恢复，以保持向后兼容。

服务器安装、SteamCMD 更新、存档备份与恢复、存档解析、插件更新和活动设置等耗时操作通过 Agent 后台任务执行。浏览器只使用短连接启动任务并查询状态，因此反向代理超时、临时网络抖动或浏览器请求断开不会终止 Agent 中已经开始的操作；任务完成或失败后再向界面返回结果并写入审计日志。

网页侧边栏会显示后台任务中心。刷新或重新打开网页后，可以重新读取仍在运行、刚完成或失败的任务。任务摘要会写入 Agent 数据目录中的 `web-jobs.json` 并保留 24 小时；较大的任务返回结果只在内存中保留 10 分钟，不会写入磁盘。Agent 重启时仍处于运行状态的旧任务会明确标记为“已中断”，避免界面一直显示为执行中。

同一台服务器不会同时执行两个耗时任务，例如备份尚未完成时不能开始恢复或更新；不同服务器的任务仍可并行运行。SteamCMD 缓存清理、公共解析组件安装等共享任务会与全部服务器任务互斥，避免多页面或多管理员重复点击后同时修改同一目录。

备份、恢复、存档解析、直接安装更新、安全更新、服务器复制、删除和维护计划还会进入 Agent 内核的实例操作锁。该锁不只限制网页请求，也会阻止 Guardian、计划任务和其他后台流程在存档复制、目录删除或 SteamCMD 写入期间同时操作同一服务器。状态栏会分别显示“备份中”“恢复中”“解析中”“复制中”“删除中”或“更新中”，操作失败后锁会自动释放。

以下差异属于运行环境限制，而不是缺失的服务器管理能力：

- 浏览器不提供“打开目录”或复制服务器绝对路径；需要的数据通过备份、诊断包等专用下载接口取得；
- Linux 不执行 DirectX 检查；
- UE4SS、PalDefender、Pak、Lua、LogicMods 和官方服务端 Workshop 目前按官方限制保持只读或禁用，因此网页不提供这些 Windows 文件的上传安装入口。

前端调用的全部 Wails 绑定均由 Web RPC 白名单覆盖，并通过自动测试检查；平台不支持的能力由能力中心明确标记，不使用伪造结果代替。

## 数据目录

- Agent配置与管理密码摘要：`/var/lib/palserver-launcher`
- Linux 自动服务器目录：`/var/lib/palserver-launcher/servers/<实例名>`
- SteamCMD 用户 HOME 与兼容文件：`/var/lib/palserver`
- systemd服务：`palserver-agent.service`

Agent 的 `config.json` 使用临时文件原子替换，每次保存前会把上一份有效配置保留为 `config.json.bak`。如果主配置损坏或在写入过程中丢失，Agent 会自动恢复备份并在网页顶部显示警告；无法解析的原文件会改名为 `config.json.corrupt-<时间>` 保留，不会直接删除。主配置和备份同时损坏时会使用空白配置启动，同时保留两份损坏文件供手工恢复，避免 Agent 因 JSON 错误完全无法启动。

服务器管理员密码和入服密码使用 `/var/lib/palserver-launcher/secrets.key` 进行 AES-GCM 加密，`config.json` 与 `config.json.bak` 不保存明文密码。旧版本留下的明文配置会在首次启动时自动迁移。迁移 Agent 数据时必须同时备份 `secrets.key`；如果只复制配置文件而丢失密钥，网页会提示重新输入对应密码。Windows 桌面版使用当前 Windows 用户的 DPAPI，不需要单独的密钥文件。

systemd 安装版只允许创建、导入和管理 `/var/lib/palserver-launcher/servers` 下的服务器。Linux 新建服务器不再要求用户输入路径，启动器会按服务器名称自动创建子文件夹。这与服务文件的 `ProtectSystem=strict` 和 `ReadWritePaths` 保持一致，可以避免网页管理员误选系统目录后执行安装或删除。手动运行 Agent 时默认不限制服务器根目录；如需相同约束，可设置以冒号分隔的 `PALSERVER_ALLOWED_SERVER_ROOTS`。

路径检查不仅比较文本前缀，还会解析现有目录、符号链接以及目标目录尚未创建时最近的真实祖先。位于 `/var/lib/palserver-launcher/servers` 内但通过符号链接跳转到其他目录的实例会被拒绝；服务器启动脚本也必须解析到该实例根目录内部，不能在网页中把 `Executable` 指向 `/bin/sh` 或其他系统程序。

停止、重启或升级 Agent 本身不会强制结束已经运行的 Palworld 服务器进程。systemd 使用 `KillMode=process` 只管理 Agent 主进程，新 Agent 启动后会重新识别并接管原服务器状态；正常关服仍应在网页控制台中单独执行。

Agent自己启动的PalServer会进入独立进程组，关服时可以安全结束整个服务器进程组。对于手工启动、其他systemd服务启动或导入后识别的PalServer，只有确认其进程组ID等于服务器PID时才发送进程组信号；否则只终止目标服务器进程，避免误伤同一终端或服务组中的其他程序。

每个服务器可以在“自动化 → 更新与访问策略”中单独启用“Agent 启动后自动开服”。主机重启后，systemd 先启动 Agent，Agent 再按服务器列表顺序启动已启用的实例，并在多实例之间间隔 2 秒，避免同时初始化造成瞬时 CPU、内存和磁盘压力。Agent 自身重启但 PalServer 仍在运行时会识别现有进程，不会重复启动；自动启动失败会写入启动警告并通过网页状态事件和 Discord 通知报告。

维护计划在真正开始执行前会同步占用服务器操作锁并把状态写为“执行中”，因此连续点击“立即运行”不会创建重复任务。Agent 如果在任务完成前重启，启动时会把遗留的 `running` 状态恢复为“已中断”，保留原有下次计划时间，并在网页顶部提示管理员检查服务器状态；任务不会永久卡在执行中。

## 官方参考

- https://docs.palworldgame.com/getting-started/requirements
- https://docs.palworldgame.com/getting-started/deploy-dedicated-server
- https://docs.palworldgame.com/settings-and-operation/arguments
- https://docs.palworldgame.com/settings-and-operation/mod
- https://docs.palworldgame.com/category/rest-api
- https://github.com/pocketpairjp/palworld-dedicated-server-docker

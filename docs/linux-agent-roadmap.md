# Linux 后台服务与网页控制台路线

## 当前已完成

- `pal-agent` Linux amd64无CGO构建；
- `/proc`进程、CPU、内存和TCP监听端口采集；
- Linux SteamCMD自动下载和App ID 2394010安装；
- `PalServer.sh`启动、进程组停止、崩溃监控和CPU亲和性；
- 与Wails方法对应的密码认证HTTP RPC及SSE事件；
- 同一套React界面自动切换桌面桥接或网页API；
- systemd安装脚本、Linux Release包和Agent自更新；
- Linux FRP客户端下载与运行；
- Linux x86_64只读存档解析组件；
- 官方限制识别：Linux禁用当前仅支持Windows的服务端模组。
- Ubuntu 24.04真实主机完成Agent启动、首次创建密码、HTTP RPC、SSE、systemd托管和正常退出验证；
- Linux集成测试通过`PalServer.sh`启动、识别`PalServer-Linux-Shipping`子进程并完成优雅关服。
- Linux CI 使用本地假 SteamCMD 完整执行一键创建流程，验证 App ID 2394010 脚本、`PalServer.sh`/Shipping 文件、`LinuxServer/PalWorldSettings.ini`、实例持久化和 Linux 不安装 Windows 插件；无需访问 Steam 网络即可回归安装逻辑。
- 一键安装前检查CPU、总内存、安装磁盘和路径；低于约8 GB物理内存或12 GB剩余磁盘时在下载前阻止安装；
- 提供`webpreview`本地构建标签，在暂时没有Linux主机时也能使用隔离数据目录测试完整网页控制台；预览默认模拟Linux平台能力，也可用`--platform windows`切换Windows网页视图。
- 网页登录具有失败限速和临时锁定；写操作校验同源请求并写入不含参数与凭据的轮转审计日志；

Windows 桌面端继续作为本地一体化管理器，长期版本将把可复用 Go 服务拆分为 `pal-agent`，React 界面同时支持 Wails 本地桥接和 HTTPS 远程 API。

## 目标结构

- `pal-agent`：Linux/Windows 后台服务，负责实例、SteamCMD、进程、备份、插件、官方 REST 与指标采集。
- `pal-web`：复用当前 React 页面，通过版本化 API 管理一个或多个 Agent。
- `pal-desktop`：当前 Wails 应用，默认连接本机 Agent，保留单文件桌面体验。
- `processRuntime`：平台进程接口；Windows 使用 Toolhelp/Process API，Linux 后续使用 `/proc`、cgroup v2 和 systemd。

## 安全边界

- 首次访问创建管理密码，密码使用加盐 Argon2id 摘要保存；远程访问仍应使用 HTTPS、WireGuard 或 Tailscale。
- HTTPS、访问审计、管理员/只读角色和 API 速率限制。
- Agent 默认只监听本机；远程访问推荐 WireGuard/Tailscale，不直接暴露 RCON 或 Palworld REST。

## 后续阶段

1. 在满足官方16/32 GB内存建议的Ubuntu和AlmaLinux主机完成真实Palworld长期运行、升级回滚和大存档压力测试。
2. 增加管理员/只读角色和可选TOTP；登录限速与操作审计已经完成。
3. 支持多节点、集中告警、异地备份复制和滚动更新。
4. 如果Palworld官方开放Linux服务端模组，再启用Workshop和原生插件部署能力。

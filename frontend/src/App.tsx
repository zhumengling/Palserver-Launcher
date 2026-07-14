import { FormEvent, lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Activity, Archive, Ban, BellRing, Box, CalendarClock, CheckCircle2, ChevronRight, CircleOff, Clock3,
  ClipboardList, Copy, Cpu, DatabaseBackup, Download, FileCode2, FolderOpen,
  Gauge, Globe2, HardDrive, History, LayoutDashboard, Map, MemoryStick, Package,
  Play, PlugZap, Plus, RefreshCw, Save, Search, Send, Server, Settings,
  Network, Shield, Square, Terminal, Trash2, Upload, UserCog, Users, X, Zap,
  Wrench,
} from 'lucide-react';
import * as API from '../wailsjs/go/main/App';
import { main } from '../wailsjs/go/models';
import { EventsOn } from '../wailsjs/runtime/runtime';
import './App.css';
import ToolsView from './ToolsView';
import WorldSettingsView from './WorldSettingsView';
import AutomationView from './AutomationView';
import PlayersHistoryView from './PlayersHistoryView';
import EventsView from './EventsView';
import SaveInspectorView from './SaveInspectorView';
import BackupsView from './BackupsView';
import GroupedModsView from './ModsView';
import FrpView from './FrpView';

const GameCatalog = lazy(() => import('./GameCatalog'));

type View = 'overview' | 'performance' | 'console' | 'players' | 'history' | 'automation' | 'events' | 'settings' | 'backups' | 'plugins' | 'mods' | 'map' | 'tools' | 'save-inspector' | 'frp' | 'diagnostics';

const emptyStatus = new main.RuntimeStatus({ running: false, pid: 0, players: 0, maxPlayers: 0, fps: 0, frameTime: 0, uptime: 0, cpu: 0, memoryMb: 0, restAvailable: false, rconAvailable: false, version: '' });
const globalScope = '__global__';
type Notice = { type: 'ok' | 'error'; text: string };
const defaultInstance = () => new main.ServerInstance({ id: '', name: '我的帕鲁服务器', rootPath: '', executable: '', steamCmdPath: '', publicIp: '', publicPort: 8211, queryPort: 27015, rconPort: 25575, restPort: 8212, adminPassword: '', serverPassword: '', community: true, performanceMode: true, iconId: 'SheepBall', autoRestartHours: 0, crashRestart: false, guardianEnabled: false, guardianFailureThreshold: 3, guardianCheckSeconds: 60, guardianMaxRestarts: 3, whitelistEnforced: false, backupRetentionMode: 'tiered', backupRetentionCount: 30, backupRetentionDays: 30, updateOnlyWhenEmpty: true, updateWarnMinutes: 5 });

const nav = [
  ['overview', '概览', LayoutDashboard], ['performance', '性能监控', Cpu], ['console', '控制台', Terminal], ['players', '在线玩家', Users],
  ['history', '玩家档案', History], ['automation', '自动化', Clock3], ['events', '活动与通知', BellRing],
  ['settings', '服务器设置', Settings], ['backups', '存档备份', DatabaseBackup], ['plugins', '插件', PlugZap],
  ['mods', '模组', Package], ['map', '在线地图', Map], ['tools', '维护工具', Wrench], ['save-inspector', '存档浏览', CalendarClock], ['frp', 'FRP 客户端', Network], ['diagnostics', '网络诊断', Activity],
] as const;

function App() {
  const [config, setConfig] = useState<main.AppConfig>(new main.AppConfig({ instances: [], selectedId: '', language: 'zh-CN' }));
  const [view, setView] = useState<View>('overview');
  const [statuses, setStatuses] = useState<Record<string, main.RuntimeStatus>>({});
  const [busyByServer, setBusyByServer] = useState<Record<string, string>>({});
  const [noticeByServer, setNoticeByServer] = useState<Record<string, Notice | null>>({});
  const [editor, setEditor] = useState<main.ServerInstance | null>(null);
  const [setupOpen, setSetupOpen] = useState(false);
  const [setupBusy, setSetupBusy] = useState(false);
  const [setupProgress, setSetupProgress] = useState({ message: '准备开始', percent: 0 });
  const statusRefreshSequence = useRef(0);
  const instancesRef = useRef<main.ServerInstance[]>([]);

  const selected = useMemo(() => config.instances?.find((item) => item.id === config.selectedId), [config.instances, config.selectedId]);
  const selectedScope = selected?.id || globalScope;
  const status = selected ? statuses[selected.id] || emptyStatus : emptyStatus;
  const busy = busyByServer[selectedScope] || busyByServer[globalScope] || '';
  const notice = noticeByServer[selectedScope] || noticeByServer[globalScope] || null;

  const reloadConfig = useCallback(async () => {
    const next = await API.GetConfig();
    setConfig(next);
    return next;
  }, []);
  const refreshStatuses = useCallback(async (instances?: main.ServerInstance[]) => {
    const sequence = ++statusRefreshSequence.current;
    const targets = instances ?? instancesRef.current;
    const pairs = await Promise.all(targets.map(async (instance) => {
      try { return [instance.id, await API.GetStatus(instance.id)] as const; }
      catch { return [instance.id, emptyStatus] as const; }
    }));
    if (sequence === statusRefreshSequence.current) setStatuses(Object.fromEntries(pairs));
  }, []);

  useEffect(() => { reloadConfig(); }, [reloadConfig]);
  useEffect(() => {
    instancesRef.current = config.instances || [];
    void refreshStatuses(config.instances || []);
  }, [config.instances, refreshStatuses]);
  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;
    const poll = async () => {
      try { await refreshStatuses(); }
      finally { if (!cancelled) timer = window.setTimeout(poll, 3000); }
    };
    void poll();
    return () => { cancelled = true; if (timer !== undefined) window.clearTimeout(timer); };
  }, [refreshStatuses]);
  useEffect(() => EventsOn('setup:progress', (payload: { message: string; percent: number }) => setSetupProgress(payload)), []);

  async function run(label: string, action: () => Promise<unknown>, success = '操作完成') {
    const scope = selected?.id || globalScope;
    setBusyByServer((current) => ({ ...current, [scope]: label }));
    setNoticeByServer((current) => ({ ...current, [scope]: null }));
    try {
      await action();
      setNoticeByServer((current) => ({ ...current, [scope]: { type: 'ok', text: success } }));
      const next = await reloadConfig();
      await refreshStatuses(next.instances || []);
    } catch (error) {
      setNoticeByServer((current) => ({ ...current, [scope]: { type: 'error', text: String(error) } }));
    } finally {
      setBusyByServer((current) => { const next = { ...current }; delete next[scope]; return next; });
    }
  }

  async function selectServer(id: string) { await API.SelectInstance(id); await reloadConfig(); setView('overview'); }

  async function quickSetup(name: string, installRoot: string) {
    setSetupProgress({ message: '正在准备安装', percent: 0 });
    setSetupBusy(true);
    setNoticeByServer((current) => ({ ...current, [globalScope]: null }));
    try {
      const instance = await API.QuickSetup(name, installRoot);
      await API.SelectInstance(instance.id);
      const next = await reloadConfig(); setSetupOpen(false); setView('overview');
      setNoticeByServer((current) => ({ ...current, [instance.id]: { type: 'ok', text: '服务器已经自动安装并配置完成' } }));
      await refreshStatuses(next.instances || []);
    } catch (error) {
      setNoticeByServer((current) => ({ ...current, [globalScope]: { type: 'error', text: String(error) } }));
    } finally { setSetupBusy(false); }
  }

  async function importExisting() {
    const root = await API.ChooseDirectory();
    if (!root) return;
    setBusyByServer((current) => ({ ...current, [globalScope]: 'import-existing' }));
    setNoticeByServer((current) => ({ ...current, [globalScope]: null }));
    try {
      const instance = await API.ImportExistingServer(root);
      await API.SelectInstance(instance.id);
      const next = await reloadConfig();
      setView('overview');
      setNoticeByServer((current) => ({ ...current, [instance.id]: { type: 'ok', text: '已有服务器已导入' } }));
      await refreshStatuses(next.instances || []);
    } catch (error) {
      setNoticeByServer((current) => ({ ...current, [globalScope]: { type: 'error', text: String(error) } }));
    } finally {
      setBusyByServer((current) => { const next = { ...current }; delete next[globalScope]; return next; });
    }
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand"><div className="brand-mark"><Server size={18}/></div><div><strong>Palserver</strong><span>Control Center</span></div></div>
        <div className="server-list-label"><span>服务器</span><button title="一键安装新服务器" onClick={() => setSetupOpen(true)}><Plus size={16}/></button></div>
        <div className="server-list">
          {config.instances?.map((item) => <button className={`server-item ${item.id === config.selectedId ? 'active' : ''}`} key={item.id} onClick={() => selectServer(item.id)}>
            <img className="server-icon" src={`/server-icons/${item.iconId || 'SheepBall'}.png`} alt=""/><span className={`status-dot ${statuses[item.id]?.running ? 'online' : ''}`}/><span className="server-name">{item.name}</span><ChevronRight size={14}/>
          </button>)}
          {!config.instances?.length && <button className="empty-server" onClick={() => setSetupOpen(true)}><Plus size={18}/>一键安装服务器</button>}
        </div>
        <nav>{nav.map(([id, label, Icon]) => <button key={id} className={view === id ? 'active' : ''} disabled={!selected} onClick={() => setView(id)}><Icon size={17}/><span>{label}</span></button>)}</nav>
        <div className="sidebar-footer"><span>Wails + Go</span><span className="version">v0.1</span></div>
      </aside>

      <main>
        <header className="topbar">
          <div><p className="eyebrow">{nav.find(([id]) => id === view)?.[1] || '概览'}</p><h1>{selected?.name || 'Palserver Launcher'}</h1></div>
          {selected && <div className="command-cluster">
            <div className={`server-state ${status.running ? 'online' : ''}`}><span/>{status.running ? `运行中 · PID ${status.pid}` : '已停止'}</div>
            <button className="icon-button" title="刷新" onClick={() => refreshStatuses()}><RefreshCw size={17}/></button>
            {status.running ? <button className="danger" onClick={() => run('stop', () => API.StopServer(selected.id), '已发送关服指令')}><Square size={15}/>停止</button>
              : <button className="primary" onClick={() => run('start', () => API.StartServer(selected.id), '服务器已启动')}><Play size={15}/>启动</button>}
          </div>}
        </header>

        {notice && <div className={`notice ${notice.type}`}><span>{notice.type === 'ok' ? <CheckCircle2 size={16}/> : <CircleOff size={16}/>}</span>{notice.text}<button onClick={() => setNoticeByServer((current) => ({ ...current, [selectedScope]: null, [globalScope]: null }))}><X size={14}/></button></div>}
        <section className="workspace">
          {!selected ? <Welcome onCreate={() => setSetupOpen(true)} onImport={importExisting}/> : <>
            {view === 'overview' && <Overview key={selected.id} instance={selected} status={status} busy={busy} onEdit={() => setEditor(new main.ServerInstance(selected))} onRun={run} onDeleted={async () => { await reloadConfig(); setView('overview'); }}/>}
            {view === 'performance' && <PerformanceView key={selected.id} status={status}/>}
            {view === 'console' && <ConsoleView key={selected.id} id={selected.id} run={run}/>}
            {view === 'players' && <PlayersView key={selected.id} id={selected.id} run={run}/>}
            {view === 'history' && <PlayersHistoryView key={selected.id} id={selected.id} run={run}/>}
            {view === 'automation' && <AutomationView key={selected.id} instance={selected} run={run} onChanged={async () => { await reloadConfig(); }}/>}
            {view === 'events' && <EventsView key={selected.id} id={selected.id} run={run}/>}
            {view === 'settings' && <WorldSettingsView key={selected.id} id={selected.id} running={status.running} run={run}/>}
            {view === 'backups' && <BackupsView key={selected.id} instance={selected} running={status.running} run={run} onChanged={async () => { await reloadConfig(); }}/>}
            {view === 'plugins' && <PluginsView key={selected.id} id={selected.id} running={status.running} run={run}/>}
            {view === 'mods' && <GroupedModsView key={selected.id} id={selected.id} running={status.running} run={run}/>}
            {view === 'map' && <MapView key={selected.id} id={selected.id}/>}
            {view === 'tools' && <ToolsView key={selected.id} instance={selected} running={status.running} run={run} onChanged={async () => { await reloadConfig(); }}/>}
            {view === 'save-inspector' && <SaveInspectorView key={selected.id} id={selected.id} run={run}/>}
            {view === 'frp' && <FrpView key={selected.id} instance={selected} run={run}/>}
            {view === 'diagnostics' && <DiagnosticsView key={selected.id} id={selected.id}/>}
          </>}
        </section>
      </main>
      {editor && <InstanceDialog value={editor} onClose={() => setEditor(null)} onSaved={async () => { setEditor(null); const next = await reloadConfig(); await refreshStatuses(next.instances || []); }}/>}
      {setupOpen && <QuickSetupDialog installing={setupBusy} progress={setupProgress} onClose={() => !setupBusy && setSetupOpen(false)} onInstall={quickSetup}/>}
      {busy && <div className="busy-layer"><RefreshCw className="spin" size={22}/><span>正在执行...</span></div>}
    </div>
  );
}

function Welcome({ onCreate, onImport }: { onCreate: () => void; onImport: () => void }) {
  return <div className="welcome"><div className="welcome-icon"><Server size={30}/></div><h2>准备你的帕鲁服务器</h2><p>启动器会自动下载 SteamCMD、安装服务器并生成管理配置。</p><div className="welcome-actions"><button className="primary" onClick={onCreate}><Download size={16}/>一键安装新服务器</button><button className="ghost" onClick={onImport}><FolderOpen size={16}/>导入已有服务器</button></div><small>大多数用户只需选择一键安装，无需准备任何程序或目录。</small></div>;
}

function Overview({ instance, status, busy, onEdit, onRun, onDeleted }: { instance: main.ServerInstance; status: main.RuntimeStatus; busy: string; onEdit: () => void; onRun: Function; onDeleted: () => Promise<void> }) {
  const [serverSize, setServerSize] = useState(0);
  useEffect(() => { API.GetServerSize(instance.id).then(setServerSize).catch(() => setServerSize(0)); }, [instance.id]);
  const cards = [[status.players, '在线玩家', Users], [status.fps.toFixed(0), '服务器 FPS', Gauge], [`${status.memoryMb.toFixed(0)} MB`, '进程内存', MemoryStick], [formatBytes(serverSize), '目录占用', HardDrive], [status.version || '-', '游戏版本', Box]] as const;
  return <div className="stack">
    <div className="metrics-grid">{cards.map(([value, label, Icon]) => <div className="metric" key={label}><Icon size={18}/><div><strong>{value}</strong><span>{label}</span></div></div>)}</div>
    <div className="two-columns">
      <section className="panel"><div className="panel-heading"><div><h2>服务器控制</h2><p>生命周期与程序更新</p></div><button className="ghost" onClick={onEdit}><Settings size={15}/>编辑实例</button></div>
        <div className="action-list"><ActionRow icon={Download} title="安全更新服务器" detail="检测版本并执行备份、提醒、停服和重启" action="更新" onClick={() => onRun('install', () => API.PerformManagedUpdate(instance.id, false), '服务器更新完成')}/>
        <ActionRow icon={Archive} title="打开服务器目录" detail={instance.rootPath} action="打开" onClick={() => API.OpenPath(instance.rootPath)}/>
        <ActionRow icon={Trash2} title="强制结束进程" detail="仅在正常停止无效时使用" action="强停" danger disabled={!status.running || !!busy} onClick={() => confirm('确定强制结束服务器进程？') && onRun('force', () => API.ForceStopServer(instance.id), '服务器进程已结束')}/>
        <ActionRow icon={Trash2} title="移除服务器" detail="移除启动器记录，或连同服务器文件一起删除" action="移除" danger disabled={status.running || !!busy} onClick={() => { if (!confirm('确定移除这个服务器？')) return; const files = confirm('是否同时删除服务器文件和存档？\n点击“确定”删除文件，点击“取消”仅移除记录。'); onRun('delete-server', async () => { await API.DeleteInstance(instance.id, files); await onDeleted(); }, files ? '服务器和文件已删除' : '服务器已从启动器移除'); }}/></div>
      </section>
      <section className="panel"><div className="panel-heading"><div><h2>连接信息</h2><p>客户端与管理接口</p></div></div>
        <dl className="details"><Detail label="游戏地址" value={`${instance.publicIp || '本机公网 IP'}:${instance.publicPort}/UDP`}/><Detail label="RCON" value={`127.0.0.1:${instance.rconPort}`} ok={status.rconAvailable}/><Detail label="REST API" value={`127.0.0.1:${instance.restPort}`} ok={status.restAvailable}/><Detail label="查询端口" value={String(instance.queryPort)}/></dl>
      </section>
    </div>
  </div>;
}

function PerformanceView({ status }: { status: main.RuntimeStatus }) {
  const [host, setHost] = useState<main.HostResources>(new main.HostResources({ cpuPercent: 0, memoryPercent: 0, memoryUsedMb: 0, memoryTotalMb: 0 }));
  useEffect(() => { const load = () => API.GetHostResources().then(setHost).catch(() => {}); load(); const timer = setInterval(load, 3000); return () => clearInterval(timer); }, []);
  const metrics = [
    ['整机 CPU', host.cpuPercent, `${host.cpuPercent.toFixed(0)}%`, Cpu],
    ['整机内存', host.memoryPercent, `${(host.memoryUsedMb / 1024).toFixed(1)} / ${(host.memoryTotalMb / 1024).toFixed(1)} GB`, MemoryStick],
    ['服务器内存', host.memoryTotalMb ? status.memoryMb / host.memoryTotalMb * 100 : 0, `${status.memoryMb.toFixed(0)} MB`, Server],
    ['服务器帧率', Math.min(100, status.fps / 1.2), `${status.fps.toFixed(0)} FPS`, Gauge],
  ] as const;
  return <div className="performance-grid">{metrics.map(([label, percent, value, Icon]) => <section className="panel performance-card" key={label}><div className="performance-title"><span><Icon size={18}/>{label}</span><strong>{value}</strong></div><div className="performance-bar"><span style={{ width: `${Math.max(0, Math.min(100, percent))}%` }}/></div><small>{status.running ? '每 3 秒刷新' : label.startsWith('服务器') ? '服务器未运行' : '整机实时资源'}</small></section>)}</div>;
}

function ActionRow({ icon: Icon, title, detail, action, onClick, danger, disabled }: any) { return <div className="action-row"><div className="action-icon"><Icon size={17}/></div><div><strong>{title}</strong><span>{detail}</span></div><button className={danger ? 'text-danger' : 'ghost'} disabled={disabled} onClick={onClick}>{action}</button></div>; }
function Detail({ label, value, ok }: { label: string; value: string; ok?: boolean }) { return <div><dt>{label}</dt><dd>{ok !== undefined && <span className={`mini-dot ${ok ? 'online' : ''}`}/>}<code>{value}</code><button title="复制" onClick={() => navigator.clipboard.writeText(value)}><Copy size={13}/></button></dd></div>; }

function ConsoleView({ id, run }: { id: string; run: Function }) {
  const [log, setLog] = useState(''); const [command, setCommand] = useState('Info');
  const refresh = useCallback(async () => setLog(await API.GetConsoleLog(id, 500)), [id]);
  useEffect(() => { refresh(); const timer = setInterval(refresh, 2500); return () => clearInterval(timer); }, [refresh]);
  async function submit(e: FormEvent) { e.preventDefault(); const cmd = command.trim(); if (!cmd) return; await run('rcon', async () => { const response = await API.SendRCON(id, cmd); setLog((v) => `${v}\n> ${cmd}\n${response}`); }, '命令已执行'); }
  return <section className="panel console-panel"><div className="panel-heading"><div><h2>实时控制台</h2><p>服务器日志与 RCON 命令</p></div><button className="ghost" onClick={refresh}><RefreshCw size={15}/>刷新</button></div><pre className="console-output">{log || '等待服务器日志...'}</pre><form className="command-input" onSubmit={submit}><Terminal size={16}/><input value={command} onChange={(e) => setCommand(e.target.value)} placeholder="输入 RCON 命令"/><button className="primary"><Send size={15}/>发送</button></form></section>;
}

function PlayersView({ id, run }: { id: string; run: Function }) {
  const [players, setPlayers] = useState<main.Player[]>([]); const [selected, setSelected] = useState<main.Player | null>(null); const [query, setQuery] = useState('');
  const refresh = useCallback(async () => { try { setPlayers(await API.GetPlayers(id)); } catch { setPlayers([]); } }, [id]);
  useEffect(() => { refresh(); const timer = setInterval(refresh, 3000); return () => clearInterval(timer); }, [refresh]);
  const filtered = players.filter((p) => `${p.name}${p.userId}${p.ip}`.toLowerCase().includes(query.toLowerCase()));
  return <section className="panel"><div className="panel-heading"><div><h2>在线玩家</h2><p>{players.length} 名玩家已连接</p></div><div className="toolbar"><label className="search"><Search size={15}/><input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索玩家"/></label><button className="ghost" onClick={refresh}><RefreshCw size={15}/></button></div></div>
    <div className="table-wrap"><table><thead><tr><th>玩家</th><th>等级</th><th>延迟</th><th>地址</th><th>坐标</th><th/></tr></thead><tbody>{filtered.map((player) => <tr key={player.userId}><td><strong>{player.name}</strong><small>{player.userId}</small></td><td>Lv {player.level}</td><td>{player.ping.toFixed(0)} ms</td><td><code>{player.ip}</code></td><td>{player.locationX.toFixed(0)}, {player.locationY.toFixed(0)}</td><td><button className="ghost" onClick={() => setSelected(player)}><UserCog size={15}/>管理</button></td></tr>)}</tbody></table>{!filtered.length && <Empty icon={Users} text="当前没有在线玩家"/>}</div>
    {selected && <PlayerDialog player={selected} onClose={() => setSelected(null)} onAction={(request) => run('player-action', () => API.PlayerAction(id, request), '玩家操作已执行')}/>} </section>;
}

function PlayerDialog({ player, onClose, onAction }: { player: main.Player; onClose: () => void; onAction: (r: main.ActionRequest) => void }) {
  const [kind, setKind] = useState('item'); const [value, setValue] = useState('Wood'); const [extra, setExtra] = useState(''); const [amount, setAmount] = useState(1); const [catalogTarget, setCatalogTarget] = useState<'value' | 'extra' | null>(null);
  const send = (action: string, val = value, count = amount) => onAction(new main.ActionRequest({ action, userId: player.userId, value: val, amount: count, extra }));
  const changeKind = (next: string) => {
    setKind(next); setAmount(1); setExtra('');
    if (next === 'item') setValue('Wood');
    else if (next === 'pal') setValue('SheepBall');
    else if (next === 'egg') { setValue('PalEgg_Normal_01'); setExtra('SheepBall'); }
    else if (next === 'learntech') setValue('all');
    else setValue('');
  };
  const valueUsesCatalog = ['item', 'pal', 'egg'].includes(kind);
  const valueEnabled = valueUsesCatalog || kind === 'learntech';
  const valueLabel = kind === 'egg' ? '蛋类型' : kind === 'learntech' ? '科技 ID（all = 全部）' : kind === 'pal' ? '帕鲁 ID / 名称' : '道具 ID / 名称';
  const amountLabel = kind === 'egg' ? '帕鲁等级' : '数量 / 点数';
  return <><div className="modal-backdrop"><div className="modal wide"><div className="modal-header"><div><h2>{player.name}</h2><p>{player.userId}</p></div><button onClick={onClose}><X size={18}/></button></div>
    <div className="quick-actions"><button onClick={() => send('setadmin')}><Shield size={16}/>设为管理员</button><button onClick={() => send('kick')}><Zap size={16}/>踢出</button><button className="danger-soft" onClick={() => send('ban')}><Ban size={16}/>封禁</button><button className="danger-soft" onClick={() => send('ipban')}><Globe2 size={16}/>封禁 IP</button></div>
    <div className={`form-grid reward-grid ${kind === 'egg' ? 'has-extra' : ''}`}><label><span>给予类型</span><select value={kind} onChange={(e) => changeKind(e.target.value)}><option value="item">道具</option><option value="pal">帕鲁</option><option value="egg">帕鲁蛋</option><option value="exp">经验</option><option value="stats">属性点</option><option value="relic">捕获力</option><option value="tech">科技点</option><option value="bosstech">古代科技点</option><option value="learntech">解锁科技</option></select></label><label><span>{valueLabel}</span><div className="input-action"><input disabled={!valueEnabled} value={value} placeholder={valueEnabled ? '输入内部 ID' : '此类型无需填写 ID'} onChange={(e) => setValue(e.target.value)}/>{valueUsesCatalog && <button type="button" title="打开完整目录" onClick={() => setCatalogTarget('value')}><Search size={15}/></button>}</div></label>{kind === 'egg' && <label><span>蛋内帕鲁</span><div className="input-action"><input value={extra} onChange={(e) => setExtra(e.target.value)}/><button type="button" title="选择帕鲁" onClick={() => setCatalogTarget('extra')}><Search size={15}/></button></div></label>}<label><span>{amountLabel}</span><input type="number" min="1" max={kind === 'egg' ? 100 : undefined} value={amount} disabled={kind === 'learntech'} onChange={(e) => setAmount(Number(e.target.value))}/></label></div>
    <div className="reward-hint">道具与帕鲁目录已包含 1.0 新内容；属性点、帕鲁蛋和科技解锁由 PalDefender RCON 执行。</div>
    <div className="modal-actions"><button className="ghost" onClick={onClose}>关闭</button><button className="primary" onClick={() => send(kind)}><Package size={15}/>执行给予</button></div></div></div>
    {catalogTarget && <Suspense fallback={<div className="busy-layer"><RefreshCw className="spin" size={20}/><span>正在加载游戏数据...</span></div>}><GameCatalog kind={catalogTarget === 'extra' || kind === 'pal' ? 'pal' : 'item'} filterPrefix={kind === 'egg' && catalogTarget === 'value' ? 'PalEgg_' : ''} title={kind === 'egg' && catalogTarget === 'value' ? '选择帕鲁蛋类型' : undefined} selected={catalogTarget === 'extra' ? extra : value} onClose={() => setCatalogTarget(null)} onSelect={catalogTarget === 'extra' ? setExtra : setValue}/></Suspense>}</>;
}

function SettingsView({ id, running, run }: { id: string; running: boolean; run: Function }) { const [content, setContent] = useState(''); useEffect(() => { API.ReadWorldSettings(id).then(setContent); }, [id]); return <section className="panel"><div className="panel-heading"><div><h2>PalWorldSettings.ini</h2><p>结构化设置将在后续版本继续扩展，当前可完整编辑官方配置</p></div><button className="primary" disabled={running} onClick={() => run('save-settings', () => API.WriteWorldSettings(id, content), '设置已保存')}><Save size={15}/>保存</button></div>{running && <div className="inline-warning">停止服务器后才能保存设置。</div>}<textarea className="code-editor" spellCheck={false} value={content} onChange={(e) => setContent(e.target.value)}/></section>; }

function PluginsView({ id, running, run }: { id: string; running: boolean; run: Function }) { const [items, setItems] = useState<main.ExtensionStatus[]>([]); const refresh = useCallback(() => API.ListExtensions(id).then(setItems), [id]); useEffect(() => { refresh(); }, [refresh]); return <div className="plugin-grid">{items.map((item) => <section className="panel plugin" key={item.id}><div className="plugin-icon">{item.id === 'paldefender' ? <Shield size={24}/> : <FileCode2 size={24}/>}</div><div><h2>{item.name}</h2><p>{item.installed ? `版本 ${item.version || '未知'}` : '尚未安装'}</p></div><span className={`badge ${item.enabled ? 'success' : ''}`}>{item.enabled ? '已启用' : item.installed ? '已停用' : '未安装'}</span><div className="plugin-actions">{item.id === 'paldefender' && item.installed && <button className="ghost" onClick={() => API.OpenServerPath(id, 'paldefender')}><FolderOpen size={14}/>配置</button>}<button className="ghost" disabled={running || !item.installed} onClick={() => run('toggle-plugin', async () => { await API.ToggleExtension(id, item.id, !item.enabled); await refresh(); }, '插件状态已更新')}>{item.enabled ? '停用' : '启用'}</button><button className="primary" disabled={running} onClick={() => run('update-plugin', async () => { await API.UpdateExtension(id, item.id); await refresh(); }, `${item.name} 已更新`)}><Download size={15}/>安装/更新</button></div></section>)}</div>; }

function ModsView({ id, run }: { id: string; run: Function }) { const [items, setItems] = useState<main.ModEntry[]>([]); const [kind, setKind] = useState('pak'); const refresh = useCallback(() => API.ListMods(id).then(setItems), [id]); useEffect(() => { refresh(); }, [refresh]); async function importMods() { const files = await API.ChooseFiles('选择模组文件'); if (files.length) await run('import-mod', async () => { await API.ImportMods(id, kind, files); await refresh(); }, '模组已导入'); }
  return <section className="panel"><div className="panel-heading"><div><h2>模组管理</h2><p>Lua、Pak、LogicMods 与 DLL 文件</p></div><div className="toolbar"><select value={kind} onChange={(e) => setKind(e.target.value)}><option value="pak">Pak</option><option value="paklogic">Pak LogicMods</option><option value="lua">Lua</option><option value="dll">DLL</option></select><button className="ghost" onClick={() => run('export-client-mods', () => API.ExportClientMods(id), '客户端模组包已生成')}><Package size={15}/>导出客户端包</button><button className="primary" onClick={importMods}><Upload size={15}/>导入</button></div></div><div className="table-wrap"><table><thead><tr><th>名称</th><th>类型</th><th>大小</th><th>状态</th><th/></tr></thead><tbody>{items.map((item) => <tr key={item.path}><td><strong>{item.name}</strong><small>{item.path}</small></td><td><span className="badge">{item.kind.toUpperCase()}</span></td><td>{formatBytes(item.size)}</td><td><span className={`badge ${item.enabled ? 'success' : ''}`}>{item.enabled ? '启用' : '停用'}</span></td><td className="row-actions"><button className="ghost" onClick={() => run('toggle-mod', async () => { await API.ToggleMod(id, item.path, !item.enabled); await refresh(); }, '模组状态已更新')}>{item.enabled ? '停用' : '启用'}</button><button className="icon-button danger-icon" onClick={() => confirm('删除这个模组？') && run('delete-mod', async () => { await API.DeleteMod(id, item.path); await refresh(); }, '模组已删除')}><Trash2 size={15}/></button></td></tr>)}</tbody></table>{!items.length && <Empty icon={Package} text="还没有导入模组"/>}</div></section>; }

function MapView({ id }: { id: string }) { const [players, setPlayers] = useState<main.Player[]>([]); useEffect(() => { const load = () => API.GetPlayers(id).then(setPlayers).catch(() => setPlayers([])); load(); const timer = setInterval(load, 3000); return () => clearInterval(timer); }, [id]); return <section className="panel map-panel"><div className="panel-heading"><div><h2>在线地图</h2><p>Palworld 世界地图底图 · REST API 坐标每 3 秒刷新</p></div><span className="badge success">{players.length} 在线</span></div><div className="map-canvas"><div className="map-stage"><img className="world-map-image" src="/map/palworld-world-map.webp" alt="Palworld 世界地图" draggable={false}/><div className="map-grid"/>{players.map((p, index) => { const x = ((p.locationY - 157664.56) / 462.96 + 500) / 10; const y = ((p.locationX + 123467.16) / 462.96 + 500) / 10; return <div className="player-pin" title={`${p.name} (${p.locationX.toFixed(0)}, ${p.locationY.toFixed(0)})`} key={p.userId} style={{ left: `${Math.max(3, Math.min(97, x))}%`, top: `${Math.max(3, Math.min(97, y))}%` }}><span>{index + 1}</span><label><strong>{p.name}</strong><small>{p.locationX.toFixed(0)}, {p.locationY.toFixed(0)}</small></label></div>; })}{!players.length && <div className="map-empty"><Map size={28}/><span>没有可显示的在线玩家</span></div>}</div></div></section>; }

function DiagnosticsView({ id }: { id: string }) { const [items, setItems] = useState<main.DiagnosticResult[]>([]); const [loading, setLoading] = useState(false); async function run() { setLoading(true); try { setItems(await API.RunDiagnostics(id)); } finally { setLoading(false); } } useEffect(() => { run(); }, [id]); return <section className="panel"><div className="panel-heading"><div><h2>网络与环境诊断</h2><p>检查程序、REST、RCON、公网端口和 FRP 转发提示</p></div><button className="primary" onClick={run}><RefreshCw className={loading ? 'spin' : ''} size={15}/>重新检测</button></div><div className="diagnostic-list">{items.map((item) => <div className="diagnostic" key={item.name}><span className={`diagnostic-icon ${item.status}`}>{item.status === 'ok' ? <CheckCircle2 size={17}/> : item.status === 'warn' ? <Zap size={17}/> : <CircleOff size={17}/>}</span><div><strong>{item.name}</strong><span>{item.detail}</span></div><span className={`badge ${item.status === 'ok' ? 'success' : ''}`}>{item.status.toUpperCase()}</span></div>)}</div></section>; }

function QuickSetupDialog({ installing, progress, onClose, onInstall }: { installing: boolean; progress: { message: string; percent: number }; onClose: () => void; onInstall: (name: string, installRoot: string) => void }) {
  const [name, setName] = useState('我的帕鲁服务器');
  const [installRoot, setInstallRoot] = useState('');
  async function chooseInstallRoot() { const path = await API.ChooseDirectory(); if (path) setInstallRoot(path); }
  function submit(e: FormEvent) { e.preventDefault(); if (!installRoot) return; onInstall(name.trim() || '我的帕鲁服务器', installRoot); }
  return <div className="modal-backdrop"><form className="modal setup-modal" onSubmit={submit}><div className="modal-header"><div><h2>一键安装新服务器</h2><p>SteamCMD、服务器程序和管理配置均由启动器自动准备</p></div>{!installing && <button type="button" onClick={onClose}><X size={18}/></button>}</div>
    {!installing ? <><div className="setup-body"><div className="setup-illustration"><Download size={25}/></div><label><span>服务器名称</span><input autoFocus value={name} onChange={(e) => setName(e.target.value)} /></label><label className="setup-location"><span>服务器安装目录</span><div className="input-action"><input readOnly value={installRoot} placeholder="请选择不含中文的空目录"/><button type="button" title="选择安装目录" onClick={chooseInstallRoot}><FolderOpen size={15}/></button></div></label><div className="setup-checks"><span><CheckCircle2 size={15}/>自动检查 DirectX 运行库</span><span><CheckCircle2 size={15}/>自动下载 SteamCMD</span><span><CheckCircle2 size={15}/>自动安装帕鲁服务器</span><span><CheckCircle2 size={15}/>自动安装 PalDefender</span></div><p className="setup-note">SteamCMD 在 Windows 上不支持中文安装路径。建议新建并选择例如 D:\PalworldServers\Server1 的英文目录。缺少 DirectX 运行库时会请求管理员权限自动修复。</p></div><div className="modal-actions"><button type="button" className="ghost" onClick={onClose}>取消</button><button className="primary" disabled={!installRoot}><Download size={15}/>开始自动安装</button></div></>
      : <div className="setup-progress"><RefreshCw className="spin" size={28}/><h3>{progress.message}</h3><div className="progress-track"><span style={{ width: `${Math.max(2, progress.percent)}%` }}/></div><strong>{progress.percent}%</strong><p>请保持网络连接，安装过程不需要其他操作。</p></div>}
  </form></div>;
}

function InstanceDialog({ value, onClose, onSaved }: { value: main.ServerInstance; onClose: () => void; onSaved: () => void }) { const [form, setForm] = useState(new main.ServerInstance(value)); const [showAdmin, setShowAdmin] = useState(false); const [showServer, setShowServer] = useState(false); const field = (key: keyof main.ServerInstance, val: any) => setForm(new main.ServerInstance({ ...form, [key]: val })); async function choose(key: 'rootPath' | 'steamCmdPath') { const path = await API.ChooseDirectory(); if (path) field(key, path); } async function save(e: FormEvent) { e.preventDefault(); await API.SaveInstance(new main.ServerInstance({ ...form, publicPort: Number(form.publicPort), queryPort: Number(form.queryPort), rconPort: Number(form.rconPort), restPort: Number(form.restPort) })); await onSaved(); }
  return <div className="modal-backdrop"><form className="modal wide" onSubmit={save}><div className="modal-header"><div><h2>编辑服务器</h2><p>服务器名称、地址、端口和管理密码</p></div><button type="button" onClick={onClose}><X size={18}/></button></div><div className="form-grid two"><Field label="服务器名称" value={form.name} onChange={(v: string) => field('name', v)}/><Field label="公网 IP（可留空）" value={form.publicIp} onChange={(v: string) => field('publicIp', v)}/><Field label="游戏端口 / UDP" type="number" value={form.publicPort} onChange={(v: string) => field('publicPort', Number(v))}/><Field label="查询端口" type="number" value={form.queryPort} onChange={(v: string) => field('queryPort', Number(v))}/><Field label="RCON 端口" type="number" value={form.rconPort} onChange={(v: string) => field('rconPort', v)}/><Field label="REST API 端口" type="number" value={form.restPort} onChange={(v: string) => field('restPort', Number(v))}/><PasswordField label="服务器密码" value={form.serverPassword} visible={showServer} onToggle={() => setShowServer(!showServer)} onChange={(v: string) => field('serverPassword', v)}/><PasswordField label="管理员密码" value={form.adminPassword} visible={showAdmin} onToggle={() => setShowAdmin(!showAdmin)} onChange={(v: string) => field('adminPassword', v)}/></div><details className="advanced-paths"><summary>高级路径（一般不需要修改）</summary><div className="form-grid two"><label><span>服务器目录</span><div className="input-action"><input value={form.rootPath} onChange={(e) => field('rootPath', e.target.value)}/><button type="button" onClick={() => choose('rootPath')}><FolderOpen size={15}/></button></div></label><Field label="服务器程序" value={form.executable} onChange={(v: string) => field('executable', v)}/><label><span>SteamCMD 路径</span><div className="input-action"><input value={form.steamCmdPath} onChange={(e) => field('steamCmdPath', e.target.value)}/><button type="button" onClick={() => choose('steamCmdPath')}><FolderOpen size={15}/></button></div></label></div></details><div className="toggle-row"><label><input type="checkbox" checked={form.community} onChange={(e) => field('community', e.target.checked)}/><span>公开到社区服务器列表</span></label><label><input type="checkbox" checked={form.performanceMode} onChange={(e) => field('performanceMode', e.target.checked)}/><span>启用性能启动参数</span></label></div><div className="modal-actions"><button type="button" className="ghost" onClick={onClose}>取消</button><button className="primary"><Save size={15}/>保存服务器</button></div></form></div>; }

function Field({ label, value, onChange, type = 'text', placeholder }: any) { return <label><span>{label}</span><input type={type} value={value ?? ''} placeholder={placeholder} onChange={(e) => onChange(e.target.value)}/></label>; }
function PasswordField({ label, value, visible, onToggle, onChange }: { label: string; value: string; visible: boolean; onToggle: () => void; onChange: (value: string) => void }) { return <label><span>{label}</span><div className="input-action"><input type={visible ? 'text' : 'password'} value={value ?? ''} onChange={(e) => onChange(e.target.value)}/><button type="button" title={visible ? '隐藏密码' : '显示密码'} onClick={onToggle}>{visible ? '隐藏' : '显示'}</button></div></label>; }
function Empty({ icon: Icon, text }: { icon: any; text: string }) { return <div className="empty"><Icon size={24}/><span>{text}</span></div>; }
function formatBytes(value: number) { if (!value) return '0 B'; const units = ['B','KB','MB','GB']; const i = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** i).toFixed(i ? 1 : 0)} ${units[i]}`; }

export default App;

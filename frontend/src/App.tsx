import { ChangeEvent, FormEvent, lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Activity, Archive, Ban, BellRing, Box, CalendarClock, CheckCircle2, ChevronRight, CircleOff, Clock3,
  ClipboardList, Copy, Cpu, DatabaseBackup, Download, FileCode2, FolderOpen,
  Gauge, Globe2, HardDrive, History, LayoutDashboard, Map, MemoryStick, Package,
  Menu, Play, PlugZap, Plus, RefreshCw, Save, Search, Send, Server, Settings,
  Network, Shield, ShieldCheck, Square, Terminal, Trash2, TriangleAlert, Upload, UserCog, Users, X, Zap,
  Wrench,
} from 'lucide-react';
import API, { isLinuxPlatform, isWebMode, listAgentJobs, uploadAndImportServer, type AgentBackgroundJob } from './platformApi';
import { main } from '../wailsjs/go/models';
import { EventsOn } from './platformEvents';
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
import MapView from './MapView';
import ServerPerformanceSettings from './ServerPerformanceSettings';
import OfficialApiView from './OfficialApiView';
import CapabilitiesView from './CapabilitiesView';

const GameCatalog = lazy(() => import('./GameCatalog'));

type View = 'overview' | 'capabilities' | 'official-api' | 'performance' | 'console' | 'players' | 'history' | 'automation' | 'events' | 'settings' | 'backups' | 'plugins' | 'mods' | 'map' | 'tools' | 'save-inspector' | 'frp' | 'diagnostics';

const emptyStatus = new main.RuntimeStatus({ running: false, pid: 0, players: 0, maxPlayers: 0, fps: 0, frameTime: 0, uptime: 0, cpu: 0, memoryMb: 0, restAvailable: false, rconAvailable: false, version: '' });
const globalScope = '__global__';
const runtimeStateLabels: Record<string, string> = { starting: '启动中', running: '运行中', degraded: '降级运行', stopping: '停止中', updating: '更新中', backing_up: '备份中', restoring: '恢复中', inspecting: '解析中', duplicating: '复制中', deleting: '删除中', restarting: '重启中', stopped: '已停止' };
const transitionalRuntimeStates = new Set(['starting', 'stopping', 'updating', 'backing_up', 'restoring', 'inspecting', 'duplicating', 'deleting', 'restarting']);
type Notice = { type: 'ok' | 'error'; text: string };
type SharedRun = <T>(label: string, action: () => Promise<T>, success?: string | ((result: T) => string)) => Promise<void>;
const errorText = (error: unknown) => error instanceof Error ? error.message : String(error);
const defaultInstance = () => new main.ServerInstance({ id: '', name: '我的帕鲁服务器', rootPath: '', executable: '', steamCmdPath: '', publicIp: '', publicPort: 8211, queryPort: 27015, rconPort: 25575, restPort: 8212, adminPassword: '', serverPassword: '', community: true, performanceMode: true, legacyPerformanceFlags: false, workerThreads: 0, processPriority: 'above_normal', cpuAffinityMode: 'auto', iconId: 'SheepBall', autoRestartHours: 0, startOnBoot: false, crashRestart: false, guardianEnabled: false, guardianFailureThreshold: 3, guardianCheckSeconds: 60, guardianMaxRestarts: 3, whitelistEnforced: false, backupRetentionMode: 'tiered', backupRetentionCount: 30, backupRetentionDays: 30, updateOnlyWhenEmpty: true, updateWarnMinutes: 5 });

const nav = [
  ['overview', '概览', LayoutDashboard], ['capabilities', '能力中心', ShieldCheck], ['official-api', '官方 API', ClipboardList], ['performance', '性能监控', Cpu], ['console', '控制台', Terminal], ['players', '在线玩家', Users],
  ['history', '玩家档案', History], ['automation', '自动化', Clock3], ['events', '活动与通知', BellRing],
  ['settings', '服务器设置', Settings], ['backups', '存档备份', DatabaseBackup], ['plugins', '插件', PlugZap],
  ['mods', '模组', Package], ['map', '在线地图', Map], ['tools', '维护工具', Wrench], ['save-inspector', '存档浏览', CalendarClock], ['frp', 'FRP 客户端', Network], ['diagnostics', '网络诊断', Activity],
] as const;

function App() {
  const linuxPlatform = isLinuxPlatform();
  const [config, setConfig] = useState<main.AppConfig>(new main.AppConfig({ instances: [], selectedId: '', language: 'zh-CN' }));
  const [view, setView] = useState<View>('overview');
  const [statuses, setStatuses] = useState<Record<string, main.RuntimeStatus>>({});
  const [statusErrors, setStatusErrors] = useState<Record<string, string>>({});
  const [busyByServer, setBusyByServer] = useState<Record<string, string>>({});
  const [noticeByServer, setNoticeByServer] = useState<Record<string, Notice | null>>({});
  const [editor, setEditor] = useState<main.ServerInstance | null>(null);
  const [setupOpen, setSetupOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [createMenuOpen, setCreateMenuOpen] = useState(false);
  const [setupBusy, setSetupBusy] = useState(false);
  const [setupProgress, setSetupProgress] = useState({ message: '准备开始', percent: 0 });
  const [launcherVersion, setLauncherVersion] = useState('v0.1.6');
  const [launcherUpdate, setLauncherUpdate] = useState<main.LauncherUpdateInfo | null>(null);
  const [launcherUpdateOpen, setLauncherUpdateOpen] = useState(false);
  const [launcherUpdateBusy, setLauncherUpdateBusy] = useState(false);
  const [launcherUpdateProgress, setLauncherUpdateProgress] = useState({ message: '准备下载', percent: 0, downloaded: 0, total: 0 });
  const [launcherUpdateError, setLauncherUpdateError] = useState('');
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const statusRefreshSequence = useRef(0);
  const instancesRef = useRef<main.ServerInstance[]>([]);

  const selected = useMemo(() => config.instances?.find((item) => item.id === config.selectedId), [config.instances, config.selectedId]);
  const selectedScope = selected?.id || globalScope;
  const status = selected ? statuses[selected.id] || emptyStatus : emptyStatus;
  const statusError = selected ? statusErrors[selected.id] || '' : '';
  const hasStatus = Boolean(selected && Object.prototype.hasOwnProperty.call(statuses, selected.id));
  const statusKnown = hasStatus && !statusError;
  const runtimeState = status.state || (status.running ? status.restAvailable ? 'running' : 'degraded' : 'stopped');
  const runtimeStateLabel = runtimeStateLabels[runtimeState] || runtimeState;
  const runtimeTransitioning = transitionalRuntimeStates.has(runtimeState);
  const busy = busyByServer[selectedScope] || busyByServer[globalScope] || '';
  const notice = noticeByServer[selectedScope] || noticeByServer[globalScope] || null;
  const startupWarnings = (config as main.AppConfig & { startupWarnings?: string[] }).startupWarnings || [];
  const linuxHiddenViews = new Set<View>(['plugins', 'mods', 'frp']);
  const visibleNav = nav.filter(([id]) => !linuxPlatform || !linuxHiddenViews.has(id));

  const reloadConfig = useCallback(async () => {
    const next = await API.GetConfig();
    setConfig(next);
    return next;
  }, []);
  const refreshStatuses = useCallback(async (instances?: main.ServerInstance[]) => {
    const sequence = ++statusRefreshSequence.current;
    const targets = instances ?? instancesRef.current;
    const results = await Promise.all(targets.map(async (instance) => {
      try { return { id: instance.id, status: await API.GetStatus(instance.id), error: '' }; }
      catch (error) { return { id: instance.id, status: null, error: errorText(error) }; }
    }));
    if (sequence !== statusRefreshSequence.current) return;
    setStatuses((current) => {
      const next: Record<string, main.RuntimeStatus> = {};
      for (const result of results) {
        if (result.status) next[result.id] = result.status;
        else if (current[result.id]) next[result.id] = current[result.id];
      }
      return next;
    });
    setStatusErrors(Object.fromEntries(results.filter((result) => result.error).map((result) => [result.id, result.error])));
  }, []);

  useEffect(() => { reloadConfig(); }, [reloadConfig]);
  useEffect(() => {
    API.GetLauncherVersion().then(setLauncherVersion).catch(() => undefined);
    // Check once on startup; network failures stay silent, while a real update is surfaced.
    API.CheckLauncherUpdate().then((info) => {
      setLauncherUpdate(info);
      if (info.updateAvailable) setLauncherUpdateOpen(true);
    }).catch(() => undefined);
    const off = EventsOn('launcher:update-progress', (payload: { message: string; percent: number; downloaded: number; total: number }) => {
      setLauncherUpdateProgress(payload);
    });
    return off;
  }, []);
  useEffect(() => {
    instancesRef.current = config.instances || [];
    void refreshStatuses(config.instances || []);
  }, [config.instances, refreshStatuses]);
  useEffect(() => {
    const offStatus = EventsOn('server:status', (id: string, payload: main.RuntimeStatus) => {
      setStatuses((current) => ({ ...current, [id]: new main.RuntimeStatus(payload) }));
      setStatusErrors((current) => { if (!current[id]) return current; const next = { ...current }; delete next[id]; return next; });
    });
    const offError = EventsOn('server:status-error', (id: string, message: string) => setStatusErrors((current) => ({ ...current, [id]: message })));
    return () => { offStatus(); offError(); };
  }, []);
  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;
    const poll = async () => {
      try { await refreshStatuses(); }
      finally { if (!cancelled) timer = window.setTimeout(poll, 15000); }
    };
    void poll();
    return () => { cancelled = true; if (timer !== undefined) window.clearTimeout(timer); };
  }, [refreshStatuses]);
  useEffect(() => EventsOn('setup:progress', (payload: { message: string; percent: number }) => setSetupProgress(payload)), []);
  useEffect(() => {
    if (!mobileNavOpen) return;
    const closeOnEscape = (event: KeyboardEvent) => { if (event.key === 'Escape') setMobileNavOpen(false); };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [mobileNavOpen]);
  useEffect(() => {
    if (linuxPlatform && linuxHiddenViews.has(view)) setView('overview');
  }, [linuxPlatform, view]);

  async function run<T>(label: string, action: () => Promise<T>, success: string | ((result: T) => string) = '操作完成') {
    const scope = selected?.id || globalScope;
    setBusyByServer((current) => ({ ...current, [scope]: label }));
    setNoticeByServer((current) => ({ ...current, [scope]: null }));
    try {
      const result = await action();
      const successText = typeof success === 'function' ? success(result) : success;
      setNoticeByServer((current) => ({ ...current, [scope]: { type: 'ok', text: successText } }));
      const next = await reloadConfig();
      await refreshStatuses(next.instances || []);
    } catch (error) {
      setNoticeByServer((current) => ({ ...current, [scope]: { type: 'error', text: errorText(error) } }));
    } finally {
      setBusyByServer((current) => { const next = { ...current }; delete next[scope]; return next; });
    }
  }

  async function selectServer(id: string) {
    setMobileNavOpen(false);
    await API.SelectInstance(id);
    await reloadConfig();
    setView('overview');
  }

  function selectView(next: View) {
    setView(next);
    setMobileNavOpen(false);
  }

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
      setNoticeByServer((current) => ({ ...current, [globalScope]: { type: 'error', text: errorText(error) } }));
    } finally { setSetupBusy(false); }
  }

  async function importExisting() {
    if (isWebMode) {
      setImportOpen(true);
      return;
    }
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
      setNoticeByServer((current) => ({ ...current, [globalScope]: { type: 'error', text: errorText(error) } }));
    } finally {
      setBusyByServer((current) => { const next = { ...current }; delete next[globalScope]; return next; });
    }
  }

  async function importLocalServer(name: string, files: File[]) {
    setBusyByServer((current) => ({ ...current, [globalScope]: 'import-server-upload' }));
    setNoticeByServer((current) => ({ ...current, [globalScope]: null }));
    try {
      const result = await uploadAndImportServer(name, files);
      const instance = result?.instance;
      if (!instance?.id) throw new Error('Agent 没有返回新服务器实例');
      await API.SelectInstance(instance.id);
      const next = await reloadConfig();
      setImportOpen(false);
      setView('overview');
      const detected = result.detected ? `，已识别并迁移${result.detected}` : '';
      setNoticeByServer((current) => ({ ...current, [instance.id]: { type: 'ok', text: `全新的 Linux 服务器已安装${detected}` } }));
      await refreshStatuses(next.instances || []);
    } catch (error) {
      setNoticeByServer((current) => ({ ...current, [globalScope]: { type: 'error', text: errorText(error) } }));
      throw error;
    } finally {
      setBusyByServer((current) => { const next = { ...current }; delete next[globalScope]; return next; });
    }
  }

  async function checkLauncherUpdate() {
    setLauncherUpdateError('');
    try {
      const info = await API.CheckLauncherUpdate();
      setLauncherUpdate(info);
      setLauncherUpdateOpen(true);
    } catch (error) {
      setLauncherUpdateError(errorText(error));
      setLauncherUpdateOpen(true);
    }
  }

  async function applyLauncherUpdate() {
    setLauncherUpdateBusy(true);
    setLauncherUpdateError('');
    setLauncherUpdateProgress({ message: '准备下载', percent: 0, downloaded: 0, total: launcherUpdate?.assetSize || 0 });
    try {
      await API.ApplyLauncherUpdate();
    } catch (error) {
      setLauncherUpdateError(errorText(error));
      setLauncherUpdateBusy(false);
    }
  }

  return (
    <div className="app-shell">
      <aside id="primary-navigation" className={`sidebar ${mobileNavOpen ? 'open' : ''}`} aria-label="服务器与功能导航">
        <div className="brand"><div className="brand-mark"><Server size={18}/></div><div><strong>Palserver</strong><span>Control Center</span></div></div>
        <div className="server-list-label"><span>服务器</span><button title="新增服务器" onClick={() => { setMobileNavOpen(false); setCreateMenuOpen(true); }}><Plus size={16}/></button></div>
        <div className="server-list">
          {config.instances?.map((item) => <button className={`server-item ${item.id === config.selectedId ? 'active' : ''}`} key={item.id} onClick={() => selectServer(item.id)}>
            <img className="server-icon" src={`/server-icons/${item.iconId || 'SheepBall'}.png`} alt=""/><span title={statusErrors[item.id] || undefined} className={`status-dot ${statusErrors[item.id] ? 'error' : statuses[item.id]?.running ? 'online' : ''}`}/><span className="server-name">{item.name}</span><ChevronRight size={14}/>
          </button>)}
          {!config.instances?.length && <button className="empty-server" onClick={() => { setMobileNavOpen(false); setCreateMenuOpen(true); }}><Plus size={18}/>新增服务器</button>}
        </div>
        <nav>{visibleNav.map(([id, label, Icon]) => <button key={id} className={view === id ? 'active' : ''} disabled={!selected} onClick={() => selectView(id)}><Icon size={17}/><span>{label}</span></button>)}</nav>
        {isWebMode && <BackgroundJobs/>}
        <div className="sidebar-footer"><button className="version" title={isWebMode ? `检查 ${linuxPlatform ? 'Linux ' : ''}Agent 更新` : '检查启动器更新'} onClick={checkLauncherUpdate}>{launcherVersion}{launcherUpdate?.updateAvailable && <i/>}</button></div>
      </aside>

      {mobileNavOpen && <button className="sidebar-backdrop" type="button" aria-label="关闭导航菜单" onClick={() => setMobileNavOpen(false)}/>}

      <main>
        <header className="topbar">
          <div className="topbar-title"><button className="icon-button mobile-nav-toggle" type="button" aria-label="打开导航菜单" aria-controls="primary-navigation" aria-expanded={mobileNavOpen} onClick={() => setMobileNavOpen((open) => !open)}><Menu size={18}/></button><div><p className="eyebrow">{nav.find(([id]) => id === view)?.[1] || '概览'}</p><h1>{selected?.name || 'Palserver Launcher'}</h1></div></div>
          {selected && <div className="command-cluster">
            <div className={`server-state ${statusError || runtimeState === 'degraded' ? 'error' : status.running ? 'online' : ''}`} title={statusError || status.stateMessage || undefined}><span/>{statusError ? hasStatus ? `状态异常 · 上次 ${runtimeStateLabel}` : '状态未知' : !hasStatus ? '状态检测中' : `${runtimeStateLabel}${status.pid ? ` · PID ${status.pid}` : ''}`}</div>
            <button className="icon-button" title={statusError ? `重新采集：${statusError}` : '刷新'} onClick={() => refreshStatuses()}><RefreshCw size={17}/></button>
            {runtimeTransitioning ? <button className="ghost" disabled title={status.stateMessage || runtimeStateLabel}><RefreshCw className="spin" size={15}/>{runtimeStateLabel}</button> : status.running ? <button className="danger" disabled={!statusKnown} title={!statusKnown ? '请先恢复状态采集后再停止服务器' : ''} onClick={() => run('stop', () => API.StopServer(selected.id), '已发送关服指令')}><Square size={15}/>停止</button>
              : <button className="primary" disabled={!statusKnown} title={!statusKnown ? '请先恢复状态采集后再启动服务器' : ''} onClick={() => run('start', () => API.StartServer(selected.id), '服务器已启动')}><Play size={15}/>启动</button>}
          </div>}
        </header>

        {notice && <div className={`notice ${notice.type}`}><span>{notice.type === 'ok' ? <CheckCircle2 size={16}/> : <CircleOff size={16}/>}</span>{notice.text}<button onClick={() => setNoticeByServer((current) => ({ ...current, [selectedScope]: null, [globalScope]: null }))}><X size={14}/></button></div>}
        {startupWarnings.map((warning, index) => <div className="notice error status-notice" key={`${warning}-${index}`}><TriangleAlert size={16}/><span>{warning}</span></div>)}
        {selected && statusError && <div className="notice error status-notice"><TriangleAlert size={16}/><span>服务器状态采集失败，已保留上一次有效状态：{statusError}</span></div>}
        <section className="workspace">
          {!selected ? <Welcome onCreate={() => setSetupOpen(true)} onImport={importExisting}/> : <>
            {view === 'overview' && <Overview key={selected.id} instance={selected} status={status} busy={busy} onEdit={() => setEditor(new main.ServerInstance(selected))} onRun={run} onDeleted={async () => { await reloadConfig(); setView('overview'); }}/>}
            {view === 'capabilities' && <CapabilitiesView key={selected.id} id={selected.id}/>}
            {view === 'official-api' && <OfficialApiView key={selected.id} id={selected.id} running={status.running} restAvailable={status.restAvailable} run={run}/>}
            {view === 'performance' && <PerformanceView key={selected.id} id={selected.id} status={status}/>}
            {view === 'console' && <ConsoleView key={selected.id} id={selected.id} run={run}/>}
            {view === 'players' && <PlayersView key={selected.id} id={selected.id} run={run}/>}
            {view === 'history' && <PlayersHistoryView key={selected.id} id={selected.id} run={run}/>}
            {view === 'automation' && <AutomationView key={selected.id} instance={selected} run={run} onChanged={async () => { await reloadConfig(); }}/>}
            {view === 'events' && <EventsView key={selected.id} id={selected.id} run={run}/>}
            {view === 'settings' && <WorldSettingsView key={selected.id} id={selected.id} running={status.running} run={run}/>}
            {view === 'backups' && <BackupsView key={selected.id} instance={selected} running={status.running} run={run} onChanged={async () => { await reloadConfig(); }}/>}
            {view === 'plugins' && <PluginsView key={selected.id} id={selected.id} running={status.running} busy={Boolean(busy)} run={run}/>}
            {view === 'mods' && <GroupedModsView key={selected.id} id={selected.id} running={status.running} run={run}/>}
            {view === 'map' && <MapView key={selected.id} id={selected.id}/>}
            {view === 'tools' && <ToolsView key={selected.id} instance={selected} running={status.running} run={run} onChanged={async () => { await reloadConfig(); }}/>}
            {view === 'save-inspector' && <SaveInspectorView key={selected.id} id={selected.id} run={run}/>}
            {view === 'frp' && <FrpView key={selected.id} instance={selected} run={run}/>}
            {view === 'diagnostics' && <DiagnosticsView key={selected.id} id={selected.id} run={run}/>}
          </>}
        </section>
      </main>
      {editor && <InstanceDialog value={editor} onClose={() => setEditor(null)} onSaved={async () => { setEditor(null); const next = await reloadConfig(); await refreshStatuses(next.instances || []); }}/>} {/* edit server */}
      {createMenuOpen && <CreateServerMenu onClose={() => setCreateMenuOpen(false)} onCreate={() => { setCreateMenuOpen(false); setSetupOpen(true); }} onImport={() => { setCreateMenuOpen(false); void importExisting(); }}/>} {/* create menu */}
      {setupOpen && <QuickSetupDialog installing={setupBusy} progress={setupProgress} onClose={() => !setupBusy && setSetupOpen(false)} onInstall={quickSetup}/>}
      {importOpen && <ImportServerDialog onClose={() => !busy && setImportOpen(false)} onImport={importLocalServer}/>}
      {launcherUpdateOpen && <LauncherUpdateDialog info={launcherUpdate} busy={launcherUpdateBusy} progress={launcherUpdateProgress} error={launcherUpdateError} onClose={() => !launcherUpdateBusy && setLauncherUpdateOpen(false)} onCheck={checkLauncherUpdate} onApply={applyLauncherUpdate}/>}
      {busy && <div className="busy-layer"><RefreshCw className="spin" size={22}/><span>正在执行...</span></div>}
    </div>
  );
}

function LauncherUpdateDialog({ info, busy, progress, error, onClose, onCheck, onApply }: { info: main.LauncherUpdateInfo | null; busy: boolean; progress: { message: string; percent: number; downloaded: number; total: number }; error: string; onClose: () => void; onCheck: () => void; onApply: () => void }) {
  return <div className="modal-backdrop"><section className="modal launcher-update-modal">
    <div className="modal-header"><div><h2>启动器更新</h2><p>检查 GitHub Releases 中的稳定版本</p></div><button onClick={onClose} disabled={busy}><X size={16}/></button></div>
    {error ? <div className="launcher-update-error"><CircleOff size={17}/><span>{error}</span></div> : info ? <div className="launcher-update-body">
      <div className="launcher-update-summary"><Download size={23}/><div><strong>{info.updateAvailable ? `${info.title || '发现新版本'} · ${info.latestVersion}` : '已是最新版本'}</strong><span>{info.updateAvailable ? `当前 ${info.currentVersion} · ${info.assetName} · ${formatBytes(info.assetSize)}` : `当前版本 ${info.currentVersion}`}</span>{info.updateAvailable && info.publishedAt && <span>发布时间：{new Date(info.publishedAt).toLocaleString()}</span>}</div></div>
      {info.notes && <pre className="launcher-update-notes">{info.notes}</pre>}
      {busy && <div className="launcher-update-progress"><div className="progress-track"><span style={{ width: `${Math.max(0, Math.min(100, progress.percent))}%` }}/></div><p>{progress.message} · {progress.percent}%{progress.total > 0 ? ` · ${formatBytes(progress.downloaded)} / ${formatBytes(progress.total)}` : ''}</p></div>}
    </div> : <div className="launcher-update-empty">正在检查更新…</div>}
    <div className="modal-actions"><button className="ghost" onClick={onCheck} disabled={busy}><RefreshCw size={14}/>重新检查</button>{info?.updateAvailable && <button className="primary" onClick={onApply} disabled={busy}>{busy ? '正在更新…' : '下载并重启'}<Download size={14}/></button>}</div>
  </section></div>;
}

function CreateServerMenu({ onClose, onCreate, onImport }: { onClose: () => void; onCreate: () => void; onImport: () => void }) {
  return <div className="modal-backdrop"><section className="modal create-server-menu"><div className="modal-header"><div><h2>新增服务器</h2><p>选择创建方式；Linux 服务器目录由 Agent 自动管理</p></div><button type="button" onClick={onClose}><X size={18}/></button></div><div className="create-server-options"><button className="create-server-option primary" onClick={onCreate}><Download size={22}/><span><strong>一键安装全新服务器</strong><small>自动准备 SteamCMD、官方服务端和配置</small></span></button><button className="create-server-option ghost" onClick={onImport}><Upload size={22}/><span><strong>从当前电脑迁移服务器</strong><small>通过浏览器选择本地文件夹或压缩包，自动识别存档</small></span></button></div></section></div>;
}

function Welcome({ onCreate, onImport }: { onCreate: () => void; onImport: () => void }) {
  const remoteLinux = isWebMode && isLinuxPlatform();
  return <div className="welcome"><div className="welcome-icon"><Server size={30}/></div><h2>准备你的帕鲁服务器</h2><p>Agent 会在 Linux 主机上自动下载 SteamCMD、安装服务器并生成管理配置。</p><div className="welcome-actions"><button className="primary" onClick={onCreate}><Download size={16}/>一键安装新服务器</button><button className="ghost" onClick={onImport}><Upload size={16}/>{remoteLinux ? '从本地电脑迁移' : '导入已有服务器'}</button></div><small>{remoteLinux ? '浏览器只负责上传存档；Linux 路径、程序和权限均由 Agent 自动管理。' : '大多数用户只需选择一键安装，无需准备任何程序或目录。'}</small></div>;
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
        {isWebMode ? <ActionRow icon={Archive} title="服务器文件由程序自动管理" detail="服务器、存档和配置会自动保存在 Linux Agent 的专用目录中，无需填写路径" action="自动管理" disabled/> : <ActionRow icon={Archive} title="打开服务器目录" detail={instance.rootPath} action="打开" onClick={() => onRun('open-server-path', () => API.OpenPath(instance.rootPath), '服务器目录已打开')}/>} {/* storage */}
        <ActionRow icon={Trash2} title="强制结束进程" detail="仅在正常停止无效时使用" action="强停" danger disabled={!status.running || !!busy} onClick={() => confirm('确定强制结束服务器进程？') && onRun('force', () => API.ForceStopServer(instance.id), '服务器进程已结束')}/>
        <ActionRow icon={Trash2} title="移除服务器" detail="移除启动器记录，或连同服务器文件一起删除" action="移除" danger disabled={status.running || !!busy} onClick={() => { if (!confirm('确定移除这个服务器？')) return; const files = confirm('是否同时删除服务器文件和存档？\n点击“确定”删除文件，点击“取消”仅移除记录。'); onRun('delete-server', async () => { await API.DeleteInstance(instance.id, files); await onDeleted(); }, files ? '服务器和文件已删除' : '服务器已从启动器移除'); }}/></div>
      </section>
      <section className="panel"><div className="panel-heading"><div><h2>连接信息</h2><p>客户端与管理接口</p></div></div>
        <dl className="details"><Detail label="游戏地址" value={`${instance.publicIp || '本机公网 IP'}:${instance.publicPort}/UDP`}/><Detail label="RCON" value={`127.0.0.1:${instance.rconPort}`} ok={status.rconAvailable}/><Detail label="REST API" value={`127.0.0.1:${instance.restPort}`} ok={status.restAvailable}/><Detail label="查询端口" value={String(instance.queryPort)}/></dl>
      </section>
    </div>
  </div>;
}

function PerformanceView({ id, status }: { id: string; status: main.RuntimeStatus }) {
  const [host, setHost] = useState<main.HostResources>(new main.HostResources({ cpuPercent: 0, memoryPercent: 0, memoryUsedMb: 0, memoryTotalMb: 0 }));
  const [advice, setAdvice] = useState<main.PerformanceAdvice[]>([]);
  useEffect(() => { const load = () => API.GetHostResources().then(setHost).catch(() => {}); load(); const timer = setInterval(load, 3000); return () => clearInterval(timer); }, []);
  useEffect(() => { const load = () => API.GetPerformanceAdvice(id).then(setAdvice).catch(() => setAdvice([])); load(); const timer = setInterval(load, 6000); return () => clearInterval(timer); }, [id]);
  const metrics = [
    ['整机 CPU', host.cpuPercent, `${host.cpuPercent.toFixed(0)}%`, Cpu],
    ['整机内存', host.memoryPercent, `${(host.memoryUsedMb / 1024).toFixed(1)} / ${(host.memoryTotalMb / 1024).toFixed(1)} GB`, MemoryStick],
    ['服务器 CPU', Math.min(100, status.cpu), `${status.cpu.toFixed(0)}%`, Cpu],
    ['服务器内存', host.memoryTotalMb ? status.memoryMb / host.memoryTotalMb * 100 : 0, `${status.memoryMb.toFixed(0)} MB`, Server],
    ['服务器帧率', Math.min(100, status.fps / 1.2), `${status.fps.toFixed(0)} FPS`, Gauge],
  ] as const;
  return <div className="stack"><div className="performance-grid">{metrics.map(([label, percent, value, Icon]) => <section className="panel performance-card" key={label}><div className="performance-title"><span><Icon size={18}/>{label}</span><strong>{value}</strong></div><div className="performance-bar"><span style={{ width: `${Math.max(0, Math.min(100, percent))}%` }}/></div><small>{!status.running && label.startsWith('服务器') ? '服务器未运行' : label === '服务器 CPU' ? '100% 约等于占满一个逻辑核心' : status.running ? '每 3 秒刷新' : '整机实时资源'}</small></section>)}</div><section className="panel"><div className="panel-heading"><div><h2>性能顾问</h2><p>根据官方指标、据点数量和世界配置给出负载建议</p></div><span className="badge">据点 {status.baseCampNum} · 世界第 {status.worldDays} 天</span></div><div className="compact-list">{advice.map((item) => <div key={`${item.title}-${item.setting}`}><span><strong>{item.title}</strong><small>{item.detail}{item.setting ? ` · ${item.setting}` : ''}</small></span><span className={`badge ${item.level === 'warn' ? 'danger-badge' : ''}`}>{item.level === 'warn' ? '注意' : '建议'}</span></div>)}{!advice.length && <span className="compact-empty">当前没有需要处理的性能建议</span>}</div></section></div>;
}

const backgroundJobLabels: Record<string, string> = {
  QuickSetup: '安装服务器', InstallOrUpdateServer: '安装/更新服务器', PerformManagedUpdate: '安全更新',
  CreateBackup: '创建备份', RestoreBackup: '恢复备份', PruneBackups: '清理备份', DuplicateInstance: '复制服务器',
  DeleteInstance: '删除服务器', InspectSave: '解析存档', InstallFrp: '安装 FRP', InstallSaveInspector: '安装解析器',
  UpdateAllExtensions: '更新全部插件', UpdateExtension: '更新插件', ApplyGamePreset: '应用预设',
  ApplyOfficialPvPPreset: '应用 PvP 预设', StartGameEvent: '启动活动', StopGameEvent: '停止活动', SaveWorld: '保存世界',
};

function BackgroundJobs() {
  const [jobs, setJobs] = useState<AgentBackgroundJob[]>([]);
  const load = useCallback(() => listAgentJobs('', 30).then(setJobs).catch(() => undefined), []);
  useEffect(() => {
    void load();
    const timer = window.setInterval(load, 3000);
    const off = EventsOn('web:job', () => void load());
    return () => { window.clearInterval(timer); off(); };
  }, [load]);
  const visible = jobs.filter((job) => job.state === 'running' || job.state === 'error' || Date.now() - job.finishedAt < 10 * 60 * 1000).slice(0, 4);
  if (!visible.length) return null;
  return <section className="background-jobs"><strong>后台任务</strong>{visible.map((job) => <div className={job.state} key={job.id} title={job.error || backgroundJobLabels[job.method] || job.method}>{job.state === 'running' ? <RefreshCw className="spin" size={12}/> : job.state === 'error' ? <CircleOff size={12}/> : <CheckCircle2 size={12}/>}<span>{backgroundJobLabels[job.method] || job.method}<small>{job.state === 'running' ? '正在执行' : job.state === 'error' ? job.error || '执行失败' : '已完成'}</small></span></div>)}</section>;
}

function ActionRow({ icon: Icon, title, detail, action, onClick, danger, disabled }: any) { return <div className="action-row"><div className="action-icon"><Icon size={17}/></div><div><strong>{title}</strong><span>{detail}</span></div><button className={danger ? 'text-danger' : 'ghost'} disabled={disabled} onClick={onClick}>{action}</button></div>; }
function Detail({ label, value, ok }: { label: string; value: string; ok?: boolean }) { return <div><dt>{label}</dt><dd>{ok !== undefined && <span className={`mini-dot ${ok ? 'online' : ''}`}/>}<code>{value}</code><button title="复制" onClick={() => navigator.clipboard.writeText(value)}><Copy size={13}/></button></dd></div>; }

function ConsoleView({ id, run }: { id: string; run: Function }) {
  const [log, setLog] = useState(''); const [command, setCommand] = useState('Info');
  const [capabilities, setCapabilities] = useState<main.ServerCapabilityReport | null>(null);
  const rconCapability = capabilities?.capabilities?.find((item) => item.id === 'rcon');
  const rconReady = Boolean(rconCapability?.available);
  const refresh = useCallback(async () => setLog(await API.GetConsoleLog(id, 500)), [id]);
  const refreshCapabilities = useCallback(() => API.GetServerCapabilities(id).then(setCapabilities).catch(() => setCapabilities(null)), [id]);
  useEffect(() => { refresh(); void refreshCapabilities(); const timer = setInterval(refresh, 2500); const capabilityTimer = setInterval(refreshCapabilities, 10000); return () => { clearInterval(timer); clearInterval(capabilityTimer); }; }, [refresh, refreshCapabilities]);
  async function submit(e: FormEvent) { e.preventDefault(); const cmd = command.trim(); if (!cmd) return; await run('rcon', async () => { const response = await API.SendRCON(id, cmd); setLog((v) => `${v}\n> ${cmd}\n${response}`); }, '命令已执行'); }
  return <section className="panel console-panel"><div className="panel-heading"><div><h2>实时控制台</h2><p>服务器日志与 RCON 命令</p></div><button className="ghost" onClick={() => { void refresh(); void refreshCapabilities(); }}><RefreshCw size={15}/>刷新</button></div>{!rconReady && <div className="inline-warning">RCON 命令不可用：{capabilities ? rconCapability?.reason || '未识别 RCON 能力' : '正在检测服务器功能可用性'}</div>}<pre className="console-output">{log || '等待服务器日志...'}</pre><form className="command-input" onSubmit={submit}><Terminal size={16}/><input disabled={!rconReady} value={command} onChange={(e) => setCommand(e.target.value)} placeholder={rconReady ? '输入 RCON 命令' : 'RCON 当前不可用'}/><button className="primary" disabled={!rconReady} title={!rconReady ? rconCapability?.reason || '正在检测 RCON 能力' : ''}><Send size={15}/>发送</button></form></section>;
}

function PlayersView({ id, run }: { id: string; run: Function }) {
  const [players, setPlayers] = useState<main.Player[]>([]); const [selected, setSelected] = useState<main.Player | null>(null); const [query, setQuery] = useState('');
  const [capabilities, setCapabilities] = useState<main.ServerCapabilityReport | null>(null);
  const [loadError, setLoadError] = useState('');
  const capability = (capabilityId: string) => capabilities?.capabilities?.find((item) => item.id === capabilityId);
  const playerListCapability = capability('player-list');
  const refreshCapabilities = useCallback(() => API.GetServerCapabilities(id).then(setCapabilities).catch(() => setCapabilities(null)), [id]);
  const refresh = useCallback(async () => {
    if (playerListCapability && !playerListCapability.available) { setPlayers([]); setLoadError(''); return; }
    try { setPlayers(await API.GetPlayers(id)); setLoadError(''); }
    catch (error) { setLoadError(errorText(error)); }
  }, [id, playerListCapability?.available]);
  useEffect(() => { refresh(); refreshCapabilities(); const timer = setInterval(refresh, 3000); const capabilityTimer = setInterval(refreshCapabilities, 10000); return () => { clearInterval(timer); clearInterval(capabilityTimer); }; }, [refresh, refreshCapabilities]);
  const filtered = players.filter((p) => `${p.name}${p.accountName}${p.playerId}${p.userId}${p.ip}`.toLowerCase().includes(query.toLowerCase()));
  return <section className="panel"><div className="panel-heading"><div><h2>在线玩家</h2><p>{players.length} 名玩家已连接</p></div><div className="toolbar"><label className="search"><Search size={15}/><input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索玩家"/></label><button className="ghost" onClick={() => { void refresh(); void refreshCapabilities(); }}><RefreshCw size={15}/></button></div></div>
    {playerListCapability && !playerListCapability.available && <div className="inline-warning">在线玩家不可用：{playerListCapability.reason}</div>}
    {loadError && <div className="inline-warning">玩家列表刷新失败，当前保留上一次成功结果：{loadError}</div>}
    <div className="table-wrap"><table><thead><tr><th>玩家 / 平台账户</th><th>等级</th><th>建筑</th><th>延迟</th><th>地址</th><th>坐标</th><th/></tr></thead><tbody>{filtered.map((player) => <tr key={player.userId}><td><strong>{player.name}</strong><small>{player.accountName || '平台账户未知'}</small><small>{player.userId}</small></td><td>Lv {player.level}</td><td>{player.buildingCount}</td><td>{player.ping.toFixed(0)} ms</td><td><code>{player.ip}</code></td><td>{player.locationX.toFixed(0)}, {player.locationY.toFixed(0)}</td><td><button className="ghost" onClick={() => setSelected(player)}><UserCog size={15}/>管理</button></td></tr>)}</tbody></table>{!filtered.length && <Empty icon={Users} text={playerListCapability && !playerListCapability.available ? '玩家列表当前不可用' : loadError ? '玩家列表暂时无法刷新' : '当前没有在线玩家'}/>}</div>
    {selected && <PlayerDialog player={selected} capabilities={capabilities} onClose={() => setSelected(null)} onAction={(request) => run('player-action', () => API.PlayerAction(id, request), '玩家操作已执行')}/>} </section>;
}

function PlayerDialog({ player, capabilities, onClose, onAction }: { player: main.Player; capabilities: main.ServerCapabilityReport | null; onClose: () => void; onAction: (r: main.ActionRequest) => void }) {
  const [kind, setKind] = useState('item');
  const [value, setValue] = useState('Wood');
  const [extra, setExtra] = useState('');
  const [amount, setAmount] = useState(1);
  const [catalogTarget, setCatalogTarget] = useState<'value' | 'extra' | 'activeSkills' | 'passives' | null>(null);
  const [palLevel, setPalLevel] = useState(1);
  const [palCustom, setPalCustom] = useState(false);
  const [palGender, setPalGender] = useState('Random');
  const [palNickname, setPalNickname] = useState('');
  const [palShiny, setPalShiny] = useState(false);
  const [partnerSkillLevel, setPartnerSkillLevel] = useState(1);
  const [activeSkills, setActiveSkills] = useState<string[]>([]);
  const [passives, setPassives] = useState<string[]>([]);
  const [abilityNames, setAbilityNames] = useState<Record<string, string>>({});
  const [ivs, setIVs] = useState({ health: 0, attackMelee: 0, attackShot: 0, defense: 0 });
  const [palSouls, setPalSouls] = useState({ health: 0, attack: 0, defense: 0, craftSpeed: 0 });
  const capability = (id: string) => capabilities?.capabilities?.find((item) => item.id === id);
  const capabilityBlocked = (item?: main.CapabilityStatus) => !capabilities || !item?.available;
  const capabilityReason = (item?: main.CapabilityStatus) => !capabilities ? '正在检测服务器功能可用性' : item?.reason || '';
  const moderationCapability = capability('player-moderation');
  const rconAdminCapability = capability('rcon-admin');
  const rewardCapability = capability('player-rewards');
  const customPalCapability = capability('custom-pal');
  const selectedCapability = palCustom && kind === 'pal' ? customPalCapability : rewardCapability;
  const send = (action: string, val = value, count = amount) => onAction(new main.ActionRequest({
    action, userId: player.userId, value: val, amount: count, extra,
    pal: action === 'pal' ? {
      custom: palCustom, level: palLevel, gender: palGender, nickname: palNickname, shiny: palShiny,
      partnerSkillLevel, activeSkills, passives, ivs, palSouls,
    } : undefined,
  }));
  const changeKind = (next: string) => {
    setKind(next); setAmount(1); setExtra(''); setCatalogTarget(null);
    if (next === 'item') setValue('Wood');
    else if (next === 'pal') { setValue('SheepBall'); setPalLevel(1); setPalCustom(false); setActiveSkills([]); setPassives([]); setAbilityNames({}); }
    else if (next === 'egg') { setValue('PalEgg_Normal_01'); setExtra('SheepBall'); }
    else if (next === 'learntech') setValue('all');
    else setValue('');
  };
  const valueUsesCatalog = ['item', 'pal', 'egg'].includes(kind);
  const valueEnabled = valueUsesCatalog || kind === 'learntech';
  const valueLabel = kind === 'egg' ? '蛋类型' : kind === 'learntech' ? '科技 ID（all = 全部）' : kind === 'pal' ? '帕鲁 ID / 名称' : '道具 ID / 名称';
  const amountLabel = kind === 'egg' ? '帕鲁等级' : kind === 'pal' ? '给予数量' : '数量 / 点数';
  const toggleLimited = (values: string[], setter: (next: string[]) => void, id: string, maximum: number) => {
    if (values.includes(id)) setter(values.filter((value) => value !== id));
    else if (values.length < maximum) setter([...values, id]);
  };
  const setIV = (key: keyof typeof ivs, next: number) => setIVs((current) => ({ ...current, [key]: next }));
  const setSoul = (key: keyof typeof palSouls, next: number) => setPalSouls((current) => ({ ...current, [key]: next }));
  const catalogKind = catalogTarget === 'activeSkills' ? 'skill' : catalogTarget === 'passives' ? 'passive' : catalogTarget === 'extra' || kind === 'pal' ? 'pal' : 'item';
  const catalogSelectedMany = catalogTarget === 'activeSkills' ? activeSkills : catalogTarget === 'passives' ? passives : [];
  const catalogMaximum = catalogTarget === 'activeSkills' ? 3 : catalogTarget === 'passives' ? 4 : undefined;
  const chooseCatalogEntry = (entry: { id: string; nameZh?: string; name?: string }) => {
    const id = entry.id;
    if (catalogTarget === 'activeSkills') {
      toggleLimited(activeSkills, setActiveSkills, id, 3);
      setAbilityNames((current) => ({ ...current, [id]: entry.nameZh || entry.name || id }));
    } else if (catalogTarget === 'passives') {
      toggleLimited(passives, setPassives, id, 4);
      setAbilityNames((current) => ({ ...current, [id]: entry.nameZh || entry.name || id }));
    } else if (catalogTarget === 'extra') setExtra(id);
    else { setValue(id); if (kind === 'pal') { setActiveSkills([]); setAbilityNames({}); } }
  };
  return <><div className="modal-backdrop"><div className="modal wide"><div className="modal-header"><div><h2>{player.name}</h2><p>{player.userId}</p></div><button onClick={onClose}><X size={18}/></button></div>
    <div className="quick-actions"><button disabled={capabilityBlocked(rconAdminCapability)} title={capabilityReason(rconAdminCapability)} onClick={() => send('setadmin')}><Shield size={16}/>设为管理员</button><button disabled={capabilityBlocked(moderationCapability)} title={capabilityReason(moderationCapability)} onClick={() => send('kick')}><Zap size={16}/>踢出</button><button disabled={capabilityBlocked(moderationCapability)} title={capabilityReason(moderationCapability)} className="danger-soft" onClick={() => send('ban')}><Ban size={16}/>封禁</button><button disabled={capabilityBlocked(rconAdminCapability)} title={capabilityReason(rconAdminCapability)} className="danger-soft" onClick={() => send('ipban')}><Globe2 size={16}/>封禁 IP</button></div>
    <div className={`form-grid reward-grid ${kind === 'egg' ? 'has-extra' : ''} ${kind === 'pal' ? 'has-pal-level' : ''}`}><label><span>给予类型</span><select value={kind} onChange={(e) => changeKind(e.target.value)}><option value="item">道具</option><option value="pal">帕鲁</option><option value="egg">帕鲁蛋</option><option value="exp">经验</option><option value="stats">属性点</option><option value="relic">捕获力</option><option value="tech">科技点</option><option value="bosstech">古代科技点</option><option value="learntech">解锁科技</option></select></label><label><span>{valueLabel}</span><div className="input-action"><input disabled={!valueEnabled} value={value} placeholder={valueEnabled ? '输入内部 ID' : '此类型无需填写 ID'} onChange={(e) => setValue(e.target.value)}/>{valueUsesCatalog && <button type="button" title="打开完整目录" onClick={() => setCatalogTarget('value')}><Search size={15}/></button>}</div></label>{kind === 'egg' && <label><span>蛋内帕鲁</span><div className="input-action"><input value={extra} onChange={(e) => setExtra(e.target.value)}/><button type="button" title="选择帕鲁" onClick={() => setCatalogTarget('extra')}><Search size={15}/></button></div></label>}<label><span>{amountLabel}</span><input type="number" min="1" max={kind === 'egg' ? 100 : kind === 'pal' ? 20 : undefined} value={amount} disabled={kind === 'learntech'} onChange={(e) => setAmount(Number(e.target.value))}/></label>{kind === 'pal' && <label><span>帕鲁等级</span><input type="number" min="1" max="255" value={palLevel} onChange={(e) => setPalLevel(Number(e.target.value))}/></label>}</div>
    {kind === 'pal' && <div className="pal-custom-section">
      <label className="pal-custom-toggle"><input type="checkbox" disabled={capabilityBlocked(customPalCapability)} checked={palCustom} onChange={(event) => setPalCustom(event.target.checked)}/><span><strong>高级自定义帕鲁</strong><small>{capabilityBlocked(customPalCapability) ? capabilityReason(customPalCapability) : '启用后使用 PalDefender PalTemplate，可指定性别、技能、被动、个体值和魂强化。'}</small></span></label>
      {palCustom && <><div className="pal-custom-grid"><label><span>昵称（可选）</span><input maxLength={64} value={palNickname} onChange={(event) => setPalNickname(event.target.value)}/></label><label><span>性别</span><select value={palGender} onChange={(event) => setPalGender(event.target.value)}><option value="Random">随机</option><option value="Male">雄性</option><option value="Female">雌性</option><option value="None">无性别</option></select></label><label><span>伙伴技能等级</span><input type="number" min="1" max="255" value={partnerSkillLevel} onChange={(event) => setPartnerSkillLevel(Number(event.target.value))}/></label><label className="pal-check"><input type="checkbox" checked={palShiny} onChange={(event) => setPalShiny(event.target.checked)}/><span>闪光帕鲁</span></label></div>
      <div className="pal-ability-grid"><div><div className="pal-ability-heading"><span><strong>主动技能</strong><small>最多 3 个，目录会优先显示该帕鲁可学习的技能。</small></span><button className="ghost" type="button" onClick={() => setCatalogTarget('activeSkills')}><Search size={14}/>选择技能</button></div><div className="pal-chips">{activeSkills.map((id) => <button type="button" key={id} title={id} onClick={() => toggleLimited(activeSkills, setActiveSkills, id, 3)}>{abilityNames[id] || id}<X size={12}/></button>)}{!activeSkills.length && <span>未指定，使用插件默认技能</span>}</div></div><div><div className="pal-ability-heading"><span><strong>被动词条</strong><small>最多 4 个，仅列出当前已实装词条。</small></span><button className="ghost" type="button" onClick={() => setCatalogTarget('passives')}><Search size={14}/>选择词条</button></div><div className="pal-chips">{passives.map((id) => <button type="button" key={id} title={id} onClick={() => toggleLimited(passives, setPassives, id, 4)}>{abilityNames[id] || id}<X size={12}/></button>)}{!passives.length && <span>未指定被动词条</span>}</div></div></div>
      <div className="pal-stats"><div><h3>个体值 IV（0–255）</h3><div>{([['health', '生命'], ['attackMelee', '近战攻击'], ['attackShot', '远程攻击'], ['defense', '防御']] as const).map(([key, label]) => <label key={key}><span>{label}</span><input type="number" min="0" max="255" value={ivs[key]} onChange={(event) => setIV(key, Number(event.target.value))}/></label>)}</div></div><div><h3>帕鲁魂强化（0–255）</h3><div>{([['health', '生命'], ['attack', '攻击'], ['defense', '防御'], ['craftSpeed', '制作速度']] as const).map(([key, label]) => <label key={key}><span>{label}</span><input type="number" min="0" max="255" value={palSouls[key]} onChange={(event) => setSoul(key, Number(event.target.value))}/></label>)}</div></div></div></>}
    </div>}
    <div className={`reward-hint ${capabilityBlocked(selectedCapability) ? 'capability-blocked' : ''}`}>{capabilityBlocked(selectedCapability) ? `当前不可执行：${capabilityReason(selectedCapability)}` : '道具和帕鲁均使用中文目录并保留内部 ID 搜索；普通帕鲁可直接指定等级，高级自定义由 PalDefender PalTemplate 执行，并受服务器导入规则限制。'}</div>
    <div className="modal-actions"><button className="ghost" onClick={onClose}>关闭</button><button className="primary" disabled={capabilityBlocked(selectedCapability)} title={capabilityReason(selectedCapability)} onClick={() => send(kind)}><Package size={15}/>执行给予</button></div></div></div>
    {catalogTarget && <Suspense fallback={<div className="busy-layer"><RefreshCw className="spin" size={20}/><span>正在加载游戏数据...</span></div>}><GameCatalog kind={catalogKind} filterPrefix={kind === 'egg' && catalogTarget === 'value' ? 'PalEgg_' : ''} title={kind === 'egg' && catalogTarget === 'value' ? '选择帕鲁蛋类型' : undefined} selected={catalogTarget === 'extra' ? extra : value} selectedMany={catalogSelectedMany} multiSelect={catalogTarget === 'activeSkills' || catalogTarget === 'passives'} maxSelected={catalogMaximum} recommendedPalId={value} onClose={() => setCatalogTarget(null)} onSelect={chooseCatalogEntry}/></Suspense>}</>;
}

function PluginsView({ id, running, busy, run }: { id: string; running: boolean; busy: boolean; run: SharedRun }) {
  const [items, setItems] = useState<main.ExtensionStatus[]>([]);
  const [compatibility, setCompatibility] = useState<main.PluginCompatibilityReport | null>(null);
  const [safeMode, setSafeMode] = useState<main.SafeModeStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const [checkError, setCheckError] = useState('');
  const refreshGeneration = useRef(0);
  const refresh = useCallback(async () => {
    const generation = ++refreshGeneration.current;
    const commitRefresh = (action: () => void) => { if (generation === refreshGeneration.current) action(); };
    commitRefresh(() => { setChecking(true); setCheckError(''); });
    try {
      const [local, compatibilityReport, safeModeStatus] = await Promise.all([API.ListExtensions(id), API.GetPluginCompatibility(id), API.GetSafeModeStatus(id)]);
      commitRefresh(() => { setItems(local); setCompatibility(compatibilityReport); setSafeMode(safeModeStatus); });
      try {
        const checked = await API.CheckExtensionUpdates(id);
        commitRefresh(() => setItems(checked));
      } catch (error) {
        commitRefresh(() => setCheckError(`在线检查失败，本地插件状态已保留：${errorText(error)}`));
      }
    } catch (error) {
      commitRefresh(() => { setItems([]); setCheckError(`读取插件状态失败：${errorText(error)}`); });
    } finally {
      commitRefresh(() => setChecking(false));
    }
  }, [id]);
  useEffect(() => { void refresh(); return () => { refreshGeneration.current++; }; }, [refresh]);
  const hasUpdateCandidates = items.some((item) => item.supported && item.installed && item.updateAvailable && !item.pending);
  return <div className="plugin-page">
    <section className="panel plugin-page-header" aria-busy={checking || busy}>
      <div><h2>插件更新</h2><p>检查远程版本；服务器运行时可先下载，并在重启时应用。</p>{checkError && <div className="plugin-page-error" role="status">{checkError}</div>}</div>
      <div className="plugin-page-actions">
        <button type="button" className="ghost" title="重新检查插件更新" disabled={busy || checking} onClick={() => void refresh()}><RefreshCw className={checking ? 'spin' : ''} size={15}/>{checking ? '检查中' : '重新检查'}</button>
        <button type="button" className="primary" title="更新所有发现新版本的已安装插件" disabled={busy || checking || !hasUpdateCandidates} onClick={() => run('update-all-plugins', async () => { const result = await API.UpdateAllExtensions(id); await refresh(); return result; }, (result: main.ExtensionUpdateResult[]) => result.some((entry) => entry.pending) ? '插件更新已下载，重启时应用' : '插件更新已应用')}><Download size={15}/>更新全部</button>
      </div>
    </section>
    <section className={`panel plugin-compatibility ${compatibility?.compatible ? 'compatible' : 'attention'}`}>
      <div className="plugin-compatibility-summary">{compatibility?.compatible ? <CheckCircle2 size={20}/> : <TriangleAlert size={20}/>}<span><strong>{compatibility?.compatible ? '插件组合未发现阻断问题' : '插件组合需要处理'}</strong><small>游戏 Build {compatibility?.gameBuildId || '未知'} · 稳定基线 {compatibility?.baselineBuildId || '尚未建立'}</small></span>{safeMode?.active && <span className="badge danger-badge">安全模式已启用</span>}</div>
      <div className="plugin-compatibility-actions"><button className="ghost" disabled={busy || running || !safeMode?.active} onClick={() => run('restore-safe-mode', async () => { await API.RestorePluginsAfterSafeMode(id); await refresh(); }, '原插件启用状态已恢复')}><RefreshCw size={14}/>恢复插件</button><button className="danger" disabled={busy || running || !items.some((item) => item.supported && item.installed)} onClick={() => confirm('安全模式会临时停用 UE4SS 和 PalDefender，然后启动服务器。继续吗？') && run('safe-mode-start', async () => { await API.StartServerSafeMode(id); await refresh(); }, '服务器已使用插件安全模式启动')}><ShieldCheck size={14}/>安全模式启动</button></div>
      {!!compatibility?.issues?.length && <div className="plugin-compatibility-issues">{compatibility.issues.map((issue, index) => <div className={issue.severity} key={`${issue.component}-${issue.title}-${index}`}><TriangleAlert size={14}/><span><strong>{issue.title}</strong><small>{issue.component} · {issue.detail}</small><em>{issue.action}</em></span></div>)}</div>}
    </section>
    <div className="plugin-grid">{items.map((item) => <section className="panel plugin" key={item.id}>
      <div className="plugin-icon">{item.id === 'paldefender' ? <Shield size={24}/> : <FileCode2 size={24}/>}</div>
      <div><h2>{item.name}</h2><p>{!item.supported ? '当前系统不支持' : item.installed ? '已安装' : '尚未安装'}</p></div>
      <span className={`badge ${item.enabled ? 'success' : ''}`}>{!item.supported ? '不可用' : item.enabled ? '已启用' : item.installed ? '已停用' : '未安装'}</span>
      <dl className="plugin-versions"><div><dt>当前版本</dt><dd>{item.installed ? item.version || '未知' : '未安装'}</dd></div><div><dt>最新版本</dt><dd>{item.latestVersion || (checking ? '检查中…' : item.updateCheckError ? '检查失败' : '未知')}</dd></div><div><dt>更新时间</dt><dd>{item.latestUpdatedAt ? <time dateTime={item.latestUpdatedAt}>{new Date(item.latestUpdatedAt).toLocaleString('zh-CN')}</time> : '未知'}</dd></div></dl>
      {item.pending ? <div className="plugin-update-status pending" aria-live="polite"><Clock3 size={15}/><span>版本 {item.pendingVersion || '未知'} 已下载，等待重启/下次启动应用</span></div> : item.updateAvailable ? <div className="plugin-update-status available" aria-live="polite"><Download size={15}/><span>发现新版本 {item.latestVersion || ''}</span></div> : !item.installed ? <div className="plugin-update-status"><Package size={15}/><span>可安装最新版本</span></div> : !item.updateCheckError ? <div className="plugin-update-status current"><CheckCircle2 size={15}/><span>已是最新</span></div> : null}
      {item.updateCheckError && <div className="plugin-update-error" role="status">{item.supported ? `检查更新失败：${item.updateCheckError}` : item.unsupportedReason}</div>}
      <div className="plugin-actions">{item.id === 'paldefender' && item.supported && item.installed && <button type="button" className="ghost" disabled={busy} onClick={() => run('open-paldefender-config', () => API.OpenServerPath(id, 'paldefender'), 'PalDefender 配置已打开')}><FolderOpen size={14}/>配置</button>}<button type="button" className="ghost" disabled={busy || running || !item.supported || !item.installed} onClick={() => run('toggle-plugin', async () => { await API.ToggleExtension(id, item.id, !item.enabled); await refresh(); }, '插件状态已更新')}>{item.enabled ? '停用' : '启用'}</button><button type="button" className="primary" disabled={busy || checking || item.pending || !item.supported} title={!item.supported ? item.unsupportedReason : item.pending ? '该更新已下载，等待应用' : running ? '服务器运行中，将下载更新并在重启时应用' : '安装或更新插件'} onClick={() => run('update-plugin', async () => { const result = await API.UpdateExtension(id, item.id); await refresh(); return result; }, (result: main.ExtensionUpdateResult) => result.message)}><Download size={15}/>{running ? '下载更新' : '安装/更新'}</button></div>
    </section>)}</div>
  </div>;
}

function DiagnosticsView({ id, run: runAction }: { id: string; run: SharedRun }) { const [items, setItems] = useState<main.DiagnosticResult[]>([]); const [loading, setLoading] = useState(false); async function refresh() { setLoading(true); try { setItems(await API.RunDiagnostics(id)); } finally { setLoading(false); } } function downloadBundle() { if (isWebMode) { const link = document.createElement('a'); link.href = `/api/v1/download/diagnostic/${encodeURIComponent(id)}`; link.download = ''; document.body.appendChild(link); link.click(); link.remove(); return; } void runAction('diagnostic-bundle', async () => { const path = await API.CreateDiagnosticBundle(id); await API.OpenPath(path); }, '诊断包已经生成'); } useEffect(() => { void refresh(); }, [id]); return <section className="panel"><div className="panel-heading"><div><h2>网络与环境诊断</h2><p>检查程序、REST、RCON、公网端口和 FRP 转发提示</p></div><div className="toolbar"><button className="ghost" onClick={downloadBundle}><Download size={15}/>下载诊断包</button><button className="primary" onClick={refresh}><RefreshCw className={loading ? 'spin' : ''} size={15}/>重新检测</button></div></div><div className="diagnostic-list">{items.map((item) => <div className="diagnostic" key={item.name}><span className={`diagnostic-icon ${item.status}`}>{item.status === 'ok' ? <CheckCircle2 size={17}/> : item.status === 'warn' ? <Zap size={17}/> : <CircleOff size={17}/>}</span><div><strong>{item.name}</strong><span>{item.detail}</span></div><span className={`badge ${item.status === 'ok' ? 'success' : ''}`}>{item.status.toUpperCase()}</span></div>)}</div></section>; }

function ImportServerDialog({ onClose, onImport }: { onClose: () => void; onImport: (name: string, files: File[]) => Promise<void> }) {
  const [name, setName] = useState('');
  const [files, setFiles] = useState<File[]>([]);
  const [source, setSource] = useState('');
  const [error, setError] = useState('');
  const [importing, setImporting] = useState(false);
  const folderInput = useRef<HTMLInputElement>(null);
  useEffect(() => { folderInput.current?.setAttribute('webkitdirectory', ''); }, []);
  const totalSize = files.reduce((sum, file) => sum + file.size, 0);
  const portableFile = (file: File) => {
    const relative = ((file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name).replace(/\\/g, '/').toLowerCase();
    return /(^|\/)(pal\/)?saved\/(savegames|config)(\/|$)/.test(relative) || /(^|\/)savegames(\/|$)/.test(relative) || /(^|\/)(level\.sav|palworldsettings\.ini)$/.test(relative);
  };
  function selectArchive(event: ChangeEvent<HTMLInputElement>) {
    const selected = Array.from(event.target.files || []);
    setFiles(selected); setSource(selected[0]?.name || ''); setError('');
  }
  function selectFolder(event: ChangeEvent<HTMLInputElement>) {
    const selected = Array.from(event.target.files || []);
    const relativePath = (file: File) => ((file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name).replace(/\\/g, '/');
    const rawWorldDirs = selected.filter((file) => /(^|\/)level\.sav$/i.test(relativePath(file))).map((file) => {
      const value = relativePath(file); return value.slice(0, value.lastIndexOf('/'));
    });
    const portable = selected.filter((file) => portableFile(file) || rawWorldDirs.some((dir) => relativePath(file).startsWith(`${dir}/`)));
    if (!portable.length) {
      setFiles([]); setSource(''); setError('没有在所选文件夹中发现 Pal/Saved、Saved/SaveGames 或 PalWorldSettings.ini');
      return;
    }
    setFiles(portable);
    const firstPath = (portable[0] as File & { webkitRelativePath?: string }).webkitRelativePath || portable[0].name;
    setSource(firstPath.split('/')[0] || '本地服务器文件夹'); setError('');
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!files.length || importing) return;
    setImporting(true); setError('');
    try { await onImport(name.trim(), files); }
    catch (nextError) { setError(errorText(nextError)); }
    finally { setImporting(false); }
  }
  return <div className="modal-backdrop"><form className="modal import-server-modal" onSubmit={submit}>
    <div className="modal-header"><div><h2>从本地电脑迁移服务器</h2><p>浏览器上传存档，Agent 自动识别并安装全新的 Linux 服务端</p></div><button type="button" disabled={importing} onClick={onClose}><X size={18}/></button></div>
    <div className="import-server-body"><label><span>新服务器名称（可留空自动识别）</span><input value={name} onChange={(event) => setName(event.target.value)} placeholder="优先读取 PalWorldSettings.ini 中的 ServerName"/></label>
      <div className="import-source-grid"><label className="import-source-card"><Archive size={23}/><strong>选择备份压缩包</strong><span>支持 ZIP、tar.gz、tgz</span><input type="file" accept=".zip,.tar.gz,.tgz,application/zip,application/gzip" onChange={selectArchive}/></label><label className="import-source-card"><FolderOpen size={23}/><strong>选择服务器文件夹</strong><span>自动筛选存档和配置，不上传 Windows 程序</span><input ref={folderInput} type="file" multiple onChange={selectFolder}/></label></div>
      {files.length > 0 && <div className="import-selection"><CheckCircle2 size={17}/><span><strong>{source}</strong><small>将上传 {files.length} 个有效文件，共 {formatBytes(totalSize)}</small></span></div>}
      {error && <div className="inline-warning"><TriangleAlert size={15}/>{error}</div>}
      <div className="import-flow"><ShieldCheck size={18}/><span><strong>只迁移可跨平台的数据</strong><small>Agent 会重新下载官方 Linux 服务端，只导入 SaveGames、世界设置和密码；Windows EXE、UE4SS、PalDefender DLL、日志和缓存不会复制。</small></span></div>
    </div>
    <div className="modal-actions"><button type="button" className="ghost" disabled={importing} onClick={onClose}>取消</button><button className="primary" disabled={!files.length || importing}>{importing ? <RefreshCw className="spin" size={15}/> : <Upload size={15}/>}{importing ? '正在上传、识别并安装' : '开始智能迁移'}</button></div>
  </form></div>;
}

function QuickSetupDialog({ installing, progress, onClose, onInstall }: { installing: boolean; progress: { message: string; percent: number }; onClose: () => void; onInstall: (name: string, installRoot: string) => void }) {
  const linuxPlatform = isLinuxPlatform();
  const [name, setName] = useState('我的帕鲁服务器');
  const [installRoot, setInstallRoot] = useState('');
  const [environment, setEnvironment] = useState<main.SetupEnvironment | null>(null);
  const [environmentError, setEnvironmentError] = useState('');
  const [checkingEnvironment, setCheckingEnvironment] = useState(false);
  useEffect(() => {
    if (linuxPlatform) {
      let cancelled = false;
      setCheckingEnvironment(true); setEnvironmentError('');
      API.GetSetupEnvironment('').then((result) => { if (!cancelled) setEnvironment(result); }).catch((error) => { if (!cancelled) { setEnvironment(null); setEnvironmentError(errorText(error)); } }).finally(() => { if (!cancelled) setCheckingEnvironment(false); });
      return () => { cancelled = true; };
    }
    if (!installRoot.trim()) { setEnvironment(null); setEnvironmentError(''); setCheckingEnvironment(false); return; }
    let cancelled = false;
    setCheckingEnvironment(true); setEnvironmentError('');
    const timer = window.setTimeout(async () => {
      try { const result = await API.GetSetupEnvironment(installRoot); if (!cancelled) setEnvironment(result); }
      catch (error) { if (!cancelled) { setEnvironment(null); setEnvironmentError(errorText(error)); } }
      finally { if (!cancelled) setCheckingEnvironment(false); }
    }, 300);
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [installRoot, linuxPlatform]);
  async function chooseInstallRoot() { if (isWebMode) return; const path = await API.ChooseDirectory(); if (path) setInstallRoot(path); }
  function submit(e: FormEvent) { e.preventDefault(); if ((!linuxPlatform && !installRoot) || !environment?.canInstall) return; onInstall(name.trim() || '我的帕鲁服务器', linuxPlatform ? '' : installRoot); }
  if (linuxPlatform) return <LinuxQuickSetupDialog installing={installing} progress={progress} onClose={onClose} onInstall={onInstall} name={name} setName={setName} environment={environment} environmentError={environmentError} checkingEnvironment={checkingEnvironment}/>;
  return <div className="modal-backdrop"><form className="modal setup-modal" onSubmit={submit}><div className="modal-header"><div><h2>一键安装新服务器</h2><p>SteamCMD、服务器程序和管理配置均由启动器自动准备</p></div>{!installing && <button type="button" onClick={onClose}><X size={18}/></button>}</div>
    {!installing ? <><div className="setup-body"><div className="setup-illustration"><Download size={25}/></div><label><span>服务器名称</span><input autoFocus value={name} onChange={(e) => setName(e.target.value)} /></label><label className="setup-location"><span>服务器安装目录</span><div className="input-action"><input readOnly={!isWebMode} value={installRoot} onChange={(event) => setInstallRoot(event.target.value)} placeholder={isWebMode ? linuxPlatform ? '/srv/palworld/server1' : 'D:\\PalworldServers\\Server1' : '请选择不含中文的空目录'}/>{!isWebMode && <button type="button" title="选择安装目录" onClick={chooseInstallRoot}><FolderOpen size={15}/></button>}</div></label>{checkingEnvironment && <div className="setup-environment-loading"><RefreshCw className="spin" size={14}/>正在检查 CPU、内存和安装磁盘</div>}{environment && <div className="setup-environment"><SetupResource icon={Cpu} label="CPU" value={`${environment.cpuCores} 核`} ok={environment.cpuRecommended}/><SetupResource icon={MemoryStick} label="内存" value={`${(environment.memoryTotalMb / 1024).toFixed(1)} GB`} ok={environment.memoryRecommended} minimum={environment.memoryMinimum}/><SetupResource icon={HardDrive} label="可用磁盘" value={formatBytes(environment.diskFreeBytes)} ok={environment.diskMinimum}/><SetupResource icon={FolderOpen} label="安装路径" value={environment.pathValid ? '有效' : '不可用'} ok={environment.pathValid}/></div>}{environmentError && <div className="inline-warning">环境检查失败：{environmentError}</div>}{environment?.warnings?.length ? <div className={`setup-environment-warning ${environment.canInstall ? '' : 'blocking'}`}><TriangleAlert size={15}/><span>{environment.warnings.join('；')}</span></div> : null}<div className="setup-checks"><span><CheckCircle2 size={15}/>{linuxPlatform ? '自动检查 Linux 环境' : '自动检查 DirectX 运行库'}</span><span><CheckCircle2 size={15}/>自动下载 SteamCMD</span><span><CheckCircle2 size={15}/>自动安装帕鲁服务器</span><span><CheckCircle2 size={15}/>{linuxPlatform ? '自动生成 LinuxServer 配置' : '自动安装 PalDefender'}</span></div><p className="setup-note">{linuxPlatform ? '目录必须是 Linux 绝对路径，并且运行 pal-agent 的用户需要有读写权限。' : 'SteamCMD 在 Windows 上不支持中文安装路径。建议使用例如 D:\\PalworldServers\\Server1 的英文目录。缺少 DirectX 运行库时会请求管理员权限自动修复。'}</p></div><div className="modal-actions"><button type="button" className="ghost" onClick={onClose}>取消</button><button className="primary" disabled={!installRoot || checkingEnvironment || !environment?.canInstall}><Download size={15}/>开始自动安装</button></div></>
      : <div className="setup-progress"><RefreshCw className="spin" size={28}/><h3>{progress.message}</h3><div className="progress-track"><span style={{ width: `${Math.max(2, progress.percent)}%` }}/></div><strong>{progress.percent}%</strong><p>请保持网络连接，安装过程不需要其他操作。</p></div>}
  </form></div>;
}

function LinuxQuickSetupDialog({ installing, progress, onClose, onInstall, name, setName, environment, environmentError, checkingEnvironment }: { installing: boolean; progress: { message: string; percent: number }; onClose: () => void; onInstall: (name: string, installRoot: string) => void; name: string; setName: (value: string) => void; environment: main.SetupEnvironment | null; environmentError: string; checkingEnvironment: boolean }) {
  function submit(event: FormEvent) {
    event.preventDefault();
    if (environment?.canInstall) onInstall(name.trim() || '我的帕鲁服务器', '');
  }
  return <div className="modal-backdrop"><form className="modal setup-modal" onSubmit={submit}><div className="modal-header"><div><h2>一键安装 Linux 服务器</h2><p>SteamCMD、PalServer.sh 和管理配置会自动安装</p></div>{!installing && <button type="button" onClick={onClose}><X size={18}/></button>}</div>
    {!installing ? <><div className="setup-body"><div className="setup-illustration"><Download size={25}/></div><label><span>服务器名称</span><input autoFocus value={name} onChange={(event) => setName(event.target.value)}/></label><div className="setup-fixed-location"><span>服务器存放位置</span><strong>{environment?.pathMessage || 'Agent 程序数据目录/servers'}</strong><small>启动器会为每个服务器自动创建独立文件夹，不需要输入 Linux 路径。</small></div>{checkingEnvironment && <div className="setup-environment-loading"><RefreshCw className="spin" size={14}/>正在检查 Linux 环境和存储空间</div>}{environment && <div className="setup-environment"><SetupResource icon={Cpu} label="CPU" value={`${environment.cpuCores} 核`} ok={environment.cpuRecommended}/><SetupResource icon={MemoryStick} label="内存" value={`${(environment.memoryTotalMb / 1024).toFixed(1)} GB`} ok={environment.memoryRecommended} minimum={environment.memoryMinimum}/><SetupResource icon={HardDrive} label="可用磁盘" value={formatBytes(environment.diskFreeBytes)} ok={environment.diskMinimum} /><SetupResource icon={FolderOpen} label="服务器根目录" value={environment.pathValid ? '有效' : '不可用'} ok={environment.pathValid}/></div>}{environmentError && <div className="inline-warning">环境检查失败：{environmentError}</div>}{environment?.warnings?.length ? <div className={`setup-environment-warning ${environment.canInstall ? '' : 'blocking'}`}><TriangleAlert size={15}/><span>{environment.warnings.join('；')}</span></div> : null}<div className="setup-checks"><span><CheckCircle2 size={15}/>自动检查 Linux 环境</span><span><CheckCircle2 size={15}/>自动下载 SteamCMD</span><span><CheckCircle2 size={15}/>自动安装 Palworld 服务器</span><span><CheckCircle2 size={15}/>自动生成 LinuxServer 配置</span></div><p className="setup-note">服务器会自动放在 Agent 程序数据目录的 servers 文件夹中，每个服务器使用单独的子文件夹。</p></div><div className="modal-actions"><button type="button" className="ghost" onClick={onClose}>取消</button><button className="primary" disabled={checkingEnvironment || !environment?.canInstall}><Download size={15}/>开始自动安装</button></div></>
      : <div className="setup-progress"><RefreshCw className="spin" size={28}/><h3>{progress.message}</h3><div className="progress-track"><span style={{ width: `${Math.max(2, progress.percent)}%` }}/></div><strong>{progress.percent}%</strong><p>请保持网络连接，安装过程不需要其他操作。</p></div>}
  </form></div>;
}

function SetupResource({ icon: Icon, label, value, ok, minimum = ok }: { icon: any; label: string; value: string; ok: boolean; minimum?: boolean }) { return <div className={!minimum ? 'error' : ok ? 'ok' : 'warn'}><Icon size={16}/><span><small>{label}</small><strong>{value}</strong></span>{ok ? <CheckCircle2 size={14}/> : <TriangleAlert size={14}/>}</div>; }

function InstanceDialog({ value, onClose, onSaved }: { value: main.ServerInstance; onClose: () => void; onSaved: () => void }) { const linuxPlatform = isLinuxPlatform(); const [form, setForm] = useState(new main.ServerInstance(value)); const [showAdmin, setShowAdmin] = useState(false); const [showServer, setShowServer] = useState(false); const field = (key: keyof main.ServerInstance, val: any) => setForm(new main.ServerInstance({ ...form, [key]: val })); async function choose(key: 'rootPath' | 'steamCmdPath') { if (isWebMode) return; const path = await API.ChooseDirectory(); if (path) field(key, path); } async function save(e: FormEvent) { e.preventDefault(); await API.SaveInstance(new main.ServerInstance({ ...form, publicPort: Number(form.publicPort), queryPort: Number(form.queryPort), rconPort: Number(form.rconPort), restPort: Number(form.restPort) })); await onSaved(); }
  return <div className="modal-backdrop"><form className="modal wide" onSubmit={save}><div className="modal-header"><div><h2>编辑服务器</h2><p>服务器名称、地址、端口和管理密码</p></div><button type="button" onClick={onClose}><X size={18}/></button></div><div className="form-grid two"><Field label="服务器名称" value={form.name} onChange={(v: string) => field('name', v)}/><Field label="公网 IP（可留空）" value={form.publicIp} onChange={(v: string) => field('publicIp', v)}/><Field label="游戏端口 / UDP" type="number" value={form.publicPort} onChange={(v: string) => field('publicPort', Number(v))}/><Field label="查询端口" type="number" value={form.queryPort} onChange={(v: string) => field('queryPort', Number(v))}/><Field label="RCON 端口" type="number" value={form.rconPort} onChange={(v: string) => field('rconPort', v)}/><Field label="REST API 端口" type="number" value={form.restPort} onChange={(v: string) => field('restPort', Number(v))}/><PasswordField label="服务器密码" value={form.serverPassword} visible={showServer} onToggle={() => setShowServer(!showServer)} onChange={(v: string) => field('serverPassword', v)}/><PasswordField label="管理员密码" value={form.adminPassword} visible={showAdmin} onToggle={() => setShowAdmin(!showAdmin)} onChange={(v: string) => field('adminPassword', v)}/></div><ServerPerformanceSettings value={form} onChange={field}/><details className="advanced-paths"><summary>高级路径（一般不需要修改）</summary><div className="form-grid two"><label><span>服务器目录</span><div className="input-action"><input value={form.rootPath} onChange={(e) => field('rootPath', e.target.value)}/><button type="button" disabled={isWebMode} title={isWebMode ? `网页模式请直接输入${linuxPlatform ? ' Linux' : ''}路径` : '选择目录'} onClick={() => choose('rootPath')}><FolderOpen size={15}/></button></div></label><Field label="服务器程序" value={form.executable} onChange={(v: string) => field('executable', v)}/><label><span>SteamCMD 路径</span><div className="input-action"><input value={form.steamCmdPath} onChange={(e) => field('steamCmdPath', e.target.value)}/><button type="button" disabled={isWebMode} title={isWebMode ? `网页模式请直接输入${linuxPlatform ? ' Linux' : ''}路径` : '选择目录'} onClick={() => choose('steamCmdPath')}><FolderOpen size={15}/></button></div></label></div></details><div className="toggle-row"><label><input type="checkbox" checked={form.community} onChange={(e) => field('community', e.target.checked)}/><span>公开到社区服务器列表</span></label></div><div className="modal-actions"><button type="button" className="ghost" onClick={onClose}>取消</button><button className="primary"><Save size={15}/>保存服务器</button></div></form></div>; }

function Field({ label, value, onChange, type = 'text', placeholder }: any) { return <label><span>{label}</span><input type={type} value={value ?? ''} placeholder={placeholder} onChange={(e) => onChange(e.target.value)}/></label>; }
function PasswordField({ label, value, visible, onToggle, onChange }: { label: string; value: string; visible: boolean; onToggle: () => void; onChange: (value: string) => void }) { return <label><span>{label}</span><div className="input-action"><input type={visible ? 'text' : 'password'} value={value ?? ''} onChange={(e) => onChange(e.target.value)}/><button type="button" title={visible ? '隐藏密码' : '显示密码'} onClick={onToggle}>{visible ? '隐藏' : '显示'}</button></div></label>; }
function Empty({ icon: Icon, text }: { icon: any; text: string }) { return <div className="empty"><Icon size={24}/><span>{text}</span></div>; }
function formatBytes(value: number) { if (!value) return '0 B'; const units = ['B','KB','MB','GB']; const i = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** i).toFixed(i ? 1 : 0)} ${units[i]}`; }

export default App;

import { FormEvent, useEffect, useState } from 'react';
import { Archive, Ban, Copy, Eraser, ExternalLink, FolderOpen, Package, Radio, Save, Shield, Trash2 } from 'lucide-react';
import API, { downloadAgentArchive, isLinuxPlatform, isWebMode } from './platformApi';
import { main } from '../wailsjs/go/models';
import iconIds from './data/server-icons.json';

type Run = (label: string, action: () => Promise<unknown>, success?: string) => Promise<void>;

export default function ToolsView({ instance, running, run, onChanged }: { instance: main.ServerInstance; running: boolean; run: Run; onChanged: () => Promise<void> }) {
  const linuxPlatform = isLinuxPlatform();
  const [message, setMessage] = useState('');
  const [bans, setBans] = useState<string[]>([]);
  const [backups, setBackups] = useState<main.BackupEntry[]>([]);
  const [paths, setPaths] = useState<Record<string,string>>({});
  const [autoRestartHours, setAutoRestartHours] = useState(instance.autoRestartHours || 0);
  const [crashRestart, setCrashRestart] = useState(!!instance.crashRestart);
  const [iconId, setIconId] = useState(instance.iconId || 'SheepBall');
  const load = async () => {
    const [banList, officialBackups, serverPaths] = await Promise.all([API.ListBans(instance.id), API.ListOfficialBackups(instance.id), isWebMode && linuxPlatform ? Promise.resolve({}) : API.GetServerPaths(instance.id)]);
    setBans(banList); setBackups(officialBackups); setPaths(serverPaths);
  };
  useEffect(() => { setAutoRestartHours(instance.autoRestartHours || 0); setCrashRestart(!!instance.crashRestart); setIconId(instance.iconId || 'SheepBall'); load(); }, [instance.id]);
  async function announce(event: FormEvent) { event.preventDefault(); const text = message.trim(); if (!text) return; await run('announce', () => API.Announce(instance.id, text), '公告已发送'); setMessage(''); }
  const savePolicy = () => run('save-policy', async () => { await API.SaveInstance(new main.ServerInstance({ ...instance, autoRestartHours, crashRestart, iconId })); await onChanged(); }, '运行策略已保存');
  const duplicate = () => run('duplicate-server', async () => { const copy = await API.DuplicateInstance(instance.id); await API.SelectInstance(copy.id); await onChanged(); }, '服务器及存档已完整复制');
  const remove = (files: boolean) => run('delete-server', async () => { await API.DeleteInstance(instance.id, files); await onChanged(); }, files ? '服务器和文件已删除' : '服务器已从启动器移除');
  const openOrCopyPath = (key: string) => isWebMode ? navigator.clipboard.writeText(paths[key] || '') : API.OpenServerPath(instance.id, key);
  const downloadOfficialBackup = (name: string) => {
    const link = document.createElement('a');
    link.href = `/api/v1/download/official-backup/${encodeURIComponent(instance.id)}/${encodeURIComponent(name)}`;
    link.download = '';
    document.body.appendChild(link); link.click(); link.remove();
  };
  const exportClientMods = async () => {
    if (isWebMode) {
      await downloadAgentArchive(`/api/v1/download/client-mods/${encodeURIComponent(instance.id)}`, `palserver-client-mods-${instance.id}.zip`);
      return;
    }
    const path = await API.ExportClientMods(instance.id);
    await API.OpenPath(path);
  };
  return <div className="tools-grid">
    <section className="panel"><div className="panel-heading"><div><h2>广播与控制</h2><p>通过官方 REST API 向在线玩家发送公告</p></div><Radio size={18}/></div><form className="tool-form" onSubmit={announce}><input value={message} onChange={(event) => setMessage(event.target.value)} placeholder="输入服务器公告"/><button className="primary">发送公告</button></form></section>
    <section className="panel"><div className="panel-heading"><div><h2>运行策略</h2><p>迁移自旧版的定时重启和崩溃恢复</p></div><button className="primary" disabled={running} onClick={savePolicy}><Save size={14}/>保存</button></div><div className="tool-settings"><label><span>自动重启间隔</span><select disabled={running} value={autoRestartHours} onChange={(event) => setAutoRestartHours(Number(event.target.value))}><option value={0}>关闭</option><option value={6}>每 6 小时</option><option value={12}>每 12 小时</option><option value={24}>每 24 小时</option></select></label><label className="check-setting"><span><strong>崩溃自动重启</strong><small>服务器异常退出后等待 5 秒重新启动</small></span><input disabled={running} type="checkbox" checked={crashRestart} onChange={(event) => setCrashRestart(event.target.checked)}/></label><label><span>服务器图标</span><div className="icon-picker"><img src={`/server-icons/${iconId}.png`} alt=""/><select value={iconId} onChange={(event) => setIconId(event.target.value)}>{iconIds.map((id) => <option key={id}>{id}</option>)}</select></div></label></div></section>
    {isWebMode && linuxPlatform ? <section className="panel"><div className="panel-heading"><div><h2>Agent 托管存储</h2><p>网页用户无需了解或输入 Linux 文件路径</p></div><Shield size={18}/></div><div className="inline-warning">服务器程序、存档、配置、日志和权限均由 Agent 在独立目录中自动管理。需要迁移数据时请使用“从本地电脑迁移”，需要取回数据时直接下载备份或诊断包。</div></section> : <section className="panel"><div className="panel-heading"><div><h2>常用目录</h2><p>{isWebMode ? '点击复制服务器上的绝对路径' : '旧版右键菜单中的全部目录入口'}</p></div><FolderOpen size={18}/></div><div className="directory-grid">{[['server','服务器目录'],['saved','Saved 目录'],['world','当前世界存档'],['config','配置目录'],['logs','PalDefender 日志'],['paldefender','PalDefender 配置']].map(([key,label]) => <button className="ghost" key={key} disabled={!paths[key] || (linuxPlatform && ['logs','paldefender'].includes(key))} onClick={() => run(`open-${key}`, () => openOrCopyPath(key), isWebMode ? '路径已复制' : key === 'paldefender' ? 'PalDefender 配置已打开' : '目录已打开')}><FolderOpen size={14}/>{label}{isWebMode ? <Copy size={12}/> : <ExternalLink size={12}/>}</button>)}</div></section>}
    <section className="panel"><div className="panel-heading"><div><h2>封禁列表</h2><p>来自 Pal/Saved/SaveGames/banlist.txt</p></div><Ban size={18}/></div><div className="compact-list">{bans.map((user) => <div key={user}><code>{user}</code><button className="ghost" onClick={() => run('unban', async () => { await API.UnbanPlayer(instance.id, user); await load(); }, '已解除封禁')}>解封</button></div>)}{!bans.length && <span className="compact-empty">没有封禁记录</span>}</div></section>
    <section className="panel"><div className="panel-heading"><div><h2>官方自动备份</h2><p>显示游戏自身创建的 backup/world 记录</p></div><Archive size={18}/></div><div className="compact-list">{backups.slice(0,20).map((backup) => <div key={backup.path}><span><strong>{backup.name}</strong><small>{new Date(backup.createdAt).toLocaleString()}</small></span><button className="ghost" onClick={() => isWebMode ? downloadOfficialBackup(backup.name) : API.OpenOfficialBackup(instance.id, backup.path)}>{isWebMode ? '下载 ZIP' : '打开'}</button></div>)}{!backups.length && <span className="compact-empty">还没有官方备份</span>}</div></section>
    <section className="panel"><div className="panel-heading"><div><h2>迁移与清理</h2><p>服务器复制、客户端模组包和 SteamCMD 缓存</p></div><Package size={18}/></div><div className="maintenance-actions"><button className="ghost" disabled={running} onClick={duplicate}><Copy size={14}/>完整复制服务器</button><button className="ghost" disabled={linuxPlatform} onClick={() => run('export-client-mods', exportClientMods, isWebMode ? '客户端模组包已下载' : '客户端模组目录已打开')}><Package size={14}/>导出客户端模组</button><button className="ghost" onClick={() => confirm('清理 SteamCMD 下载缓存？服务器文件不会被删除。') && run('clear-cache', () => API.ClearSteamCMDCache(), 'SteamCMD 缓存已清理')}><Eraser size={14}/>清理 SteamCMD 缓存</button><button className="ghost" disabled={linuxPlatform || !paths.paldefender} onClick={() => isWebMode ? run('copy-paldefender-config', () => openOrCopyPath('paldefender'), 'PalDefender 配置路径已复制') : run('open-paldefender-config', () => API.OpenServerPath(instance.id, 'paldefender'), 'PalDefender 配置已打开')}><Shield size={14}/>{isWebMode ? '复制 PalDefender 配置路径' : '打开 PalDefender 配置'}</button><button className="text-danger" disabled={running} onClick={() => confirm('仅从启动器移除，保留服务器文件？') && remove(false)}><Trash2 size={14}/>移除记录</button><button className="danger" disabled={running} onClick={() => confirm('永久删除服务器程序和存档？此操作不可恢复。') && remove(true)}><Trash2 size={14}/>删除服务器和文件</button></div></section>
  </div>;
}

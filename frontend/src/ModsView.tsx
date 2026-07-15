import { useCallback, useEffect, useMemo, useState } from 'react';
import { AlertTriangle, Box, CheckCircle2, Code2, ExternalLink, FileArchive, FileCode2, FolderOpen, Package, RefreshCw, Save, ShieldCheck, Trash2, Upload } from 'lucide-react';
import * as API from '../wailsjs/go/main/App';
import { main } from '../wailsjs/go/models';

const sections = [
  { id: 'ue4ss-system', title: 'UE4SS 内置组件', detail: '随 UE4SS 安装的运行库和调试组件，不允许删除', icon: ShieldCheck },
  { id: 'ue4ss-lua', title: 'UE4SS Lua 模组', detail: '用户安装并由 UE4SS 加载的 Lua 模组', icon: Code2 },
  { id: 'logicmods', title: 'LogicMods', detail: '位于 Paks/LogicMods 的 Blueprint 或逻辑模组', icon: FileCode2 },
  { id: 'pak', title: 'Pak 模组', detail: '位于游戏 Paks 目录的内容模组', icon: Package },
  { id: 'dll', title: 'DLL 扩展', detail: '位于服务器 Win64 目录的原生扩展', icon: Box },
] as const;

export default function ModsView({ id, running, run }: { id: string; running: boolean; run: Function }) {
  const [items, setItems] = useState<main.ModEntry[]>([]);
  const [catalog, setCatalog] = useState<main.ServerModCatalogEntry[]>([]);
  const [officialWorkshop, setOfficialWorkshop] = useState<main.OfficialWorkshopMod[]>([]);
  const [workshopRoot, setWorkshopRoot] = useState('');
  const [kind, setKind] = useState('pak');
  const [checkingUpdates, setCheckingUpdates] = useState(false);
  const refresh = useCallback(async () => {
    const [mods, serverCatalog, official, root] = await Promise.all([API.ListMods(id), API.ListServerModCatalog(id), API.ListOfficialWorkshopMods(id), API.GetOfficialWorkshopRoot(id)]);
    setItems(mods); setCatalog(serverCatalog); setOfficialWorkshop(official); setWorkshopRoot(root);
  }, [id]);
  useEffect(() => { refresh(); }, [refresh]);
  const grouped = useMemo(() => Object.fromEntries(sections.map((section) => [section.id, items.filter((item) => item.origin === section.id)])), [items]);
  const sortedCatalog = useMemo(() => [...catalog].sort((left, right) => right.updatedAt.localeCompare(left.updatedAt)), [catalog]);
  async function importMods() {
    const files = await API.ChooseFiles('选择模组文件');
    if (files.length) await run('import-mod', async () => { await API.ImportMods(id, kind, files); await refresh(); }, '模组已导入');
  }
  async function importOfficialWorkshop() {
    const folder = await API.ChooseDirectory();
    if (!folder) return;
    await run('official-workshop-import', async () => { await API.ImportOfficialWorkshopMod(id, folder); await refresh(); }, '官方 Workshop 模组已导入，重启服务器后会自动部署');
  }
  async function installCatalogMod(entry: main.ServerModCatalogEntry) {
    const files = await API.ChooseFiles(`选择从 Nexus 下载的 ${entry.name} ZIP`);
    if (!files.length) return;
    await run(`catalog-mod-${entry.id}`, async () => {
      await API.InstallServerModArchive(id, entry.id, files[0]);
      await refresh();
    }, entry.installed ? `${entry.name} 已更新` : `${entry.name} 已安装`);
  }
  async function checkCatalogUpdates() {
    setCheckingUpdates(true);
    try {
      await run('check-server-mod-updates', async () => setCatalog(await API.CheckServerModUpdates(id)), 'UE4SS 插件更新检查完成');
    } finally {
      setCheckingUpdates(false);
    }
  }
  return <div className="stack">
    <section className="panel">
      <div className="panel-heading"><div><h2>官方 Workshop 模组</h2><p>读取 Info.json 与 PackageName；按 PalModSettings.ini 的 ActiveModList 启用，重启后由游戏自动部署。</p></div><button className="primary" disabled={running} onClick={importOfficialWorkshop}><FolderOpen size={14}/>导入/更新文件夹</button></div>
      {running && <div className="inline-warning">官方 Workshop 模组会在服务器重启时部署，运行中不能修改。</div>}
      <div className="inline-form"><label><span>外部 Workshop 根目录（切换后需重新启用其中的模组；可留空，使用服务器 Mods/Workshop）</span><input value={workshopRoot} disabled={running} onChange={(event) => setWorkshopRoot(event.target.value)} placeholder="例如 C:\\Steam\\steamapps\\workshop\\content\\1623730"/></label><button className="ghost" disabled={running} onClick={() => run('save-workshop-root', async () => { await API.SaveOfficialWorkshopRoot(id, workshopRoot); await refresh(); }, 'Workshop 根目录已保存，请重新选择要启用的模组')}><Save size={14}/>保存</button></div>
      <div className="table-wrap"><table><thead><tr><th>模组</th><th>依赖</th><th>服务端兼容</th><th>状态</th><th/></tr></thead><tbody>{officialWorkshop.map((item) => <tr key={item.path}><td><strong>{item.packageName}</strong><small>{item.version ? `v${item.version}` : item.name}</small></td><td>{item.dependencies.length ? item.dependencies.join('、') : '无'}</td><td><span className={`badge ${item.serverCompatible ? 'success' : 'danger-badge'}`}>{item.serverCompatible ? '支持服务端' : '不支持服务端'}</span></td><td><span className={`badge ${item.enabled ? 'success' : ''}`}>{item.enabled ? item.deployed ? '已部署' : '待重启部署' : '已停用'}</span></td><td className="row-actions"><button className="ghost" disabled={running || !item.serverCompatible} onClick={() => run('official-workshop-toggle', async () => { await API.SetOfficialWorkshopModEnabled(id, item.packageName, !item.enabled); await refresh(); }, item.enabled ? '官方模组已停用' : '官方模组已启用')}>{item.enabled ? '停用' : '启用'}</button><button className="icon-button danger-icon" disabled={running} title="删除" onClick={() => confirm('删除这个官方 Workshop 模组？') && run('official-workshop-delete', async () => { await API.DeleteOfficialWorkshopMod(id, item.path); await refresh(); }, '官方 Workshop 模组已删除')}><Trash2 size={15}/></button></td></tr>)}</tbody></table>{!officialWorkshop.length && <div className="empty"><Package size={24}/><span>还没有官方 Workshop 模组</span></div>}</div>
    </section>
    <section className="panel server-mod-catalog">
      <div className="panel-heading"><div><h2>UE4SS 服务器插件（可选安装）</h2><p>放在普通模组管理上方，按 Nexus 最后更新时间从新到旧排列</p></div><div className="toolbar"><span className="badge success"><ShieldCheck size={13}/>已核验 {sortedCatalog.length} 项</span><button className="ghost" disabled={checkingUpdates} onClick={checkCatalogUpdates}><RefreshCw className={checkingUpdates ? 'spin' : ''} size={14}/>{checkingUpdates ? '检查中' : '检查更新'}</button></div></div>
      <div className="nexus-workflow"><FileArchive size={18}/><div><strong>Nexus 安装流程</strong><span>先打开作者页面下载 ZIP，再由启动器识别目录、备份旧版本并安装。Nexus 登录和作者文件分发规则不会被绕过。</span></div></div>
      {running && <div className="inline-warning">服务器运行期间不能安装、更新或卸载插件，请先停止服务器。</div>}
      <div className="server-mod-list">
        <div className="server-mod-list-head"><span>排序</span><span>插件</span><span>主要功能</span><span>兼容与依赖</span><span>操作</span></div>
        {sortedCatalog.map((entry, index) => <article className={`server-mod-row ${entry.installed ? 'installed' : ''}`} key={entry.id}>
          <span className="server-mod-rank">{String(index + 1).padStart(2, '0')}</span>
          <div className="server-mod-name"><strong>{entry.name}</strong><small>{entry.installedVersion ? `已装 v${entry.installedVersion}` : `目录版本 v${entry.version}`} · <code>{entry.folderName}</code></small><div className="server-mod-badges"><span className={`badge ${entry.installed && entry.enabled ? 'success' : ''}`}>{entry.installed ? entry.enabled ? <><CheckCircle2 size={12}/>已安装</> : '已安装 · 已停用' : '可选安装'}</span>{entry.updateAvailable && <span className="badge danger-badge">发现更新{entry.latestVersion ? ` v${entry.latestVersion}` : ''}</span>}{entry.installed && entry.latestVersion && !entry.updateAvailable && <span className="badge success">已是最新</span>}</div></div>
          <p>{entry.description}</p>
          <div className="server-mod-meta"><strong>{entry.latestUpdatedAt ? `Nexus v${entry.latestVersion || '?'} · ${entry.latestUpdatedAt}` : `核验版本 v${entry.version} · ${entry.updatedAt}`}</strong><span>{entry.dependency}</span>{entry.updateCheckError ? <small className="check-error"><AlertTriangle size={12}/>检查失败：{entry.updateCheckError}</small> : entry.warning && <small><AlertTriangle size={12}/>{entry.warning}</small>}</div>
          <div className="server-mod-actions"><button className="ghost" onClick={() => run(`open-nexus-${entry.id}`, () => API.OpenNexusModPage(entry.id), '已打开 Nexus 页面')}><ExternalLink size={14}/>Nexus</button><button className="primary" disabled={running} onClick={() => installCatalogMod(entry)}><Upload size={14}/>{entry.updateAvailable ? '选择更新 ZIP' : entry.installed ? '更新 ZIP' : '安装 ZIP'}</button>{entry.installed && <button className="icon-button danger-icon" title="卸载" disabled={running} onClick={() => confirm(`确定卸载 ${entry.name}？`) && run(`uninstall-catalog-${entry.id}`, async () => { await API.UninstallServerMod(id, entry.id); await refresh(); }, `${entry.name} 已卸载`)}><Trash2 size={14}/></button>}</div>
        </article>)}
      </div>
    </section>
    <section className="panel"><div className="panel-heading"><div><h2>导入模组</h2><p>选择正确类型后，启动器会放入对应目录</p></div><div className="toolbar"><select value={kind} onChange={(event) => setKind(event.target.value)}><option value="pak">Pak 模组</option><option value="paklogic">LogicMods</option><option value="lua">UE4SS Lua 模组</option><option value="dll">DLL 扩展</option></select><button className="ghost" onClick={() => run('export-client-mods', () => API.ExportClientMods(id), '客户端模组包已生成')}><Package size={15}/>导出客户端包</button><button className="primary" onClick={importMods}><Upload size={15}/>导入</button></div></div></section>
    {sections.map(({ id: origin, title, detail, icon: Icon }) => <section className="panel" key={origin}><div className="panel-heading"><div><h2>{title}</h2><p>{detail}</p></div><span className="badge"><Icon size={13}/>{grouped[origin]?.length || 0}</span></div><div className="table-wrap"><table><thead><tr><th>名称</th><th>说明</th><th>大小</th><th>状态</th><th/></tr></thead><tbody>{(grouped[origin] || []).map((item) => <tr key={item.path}><td><strong>{item.name}</strong><small>{item.path}</small></td><td>{item.description}</td><td>{formatBytes(item.size)}</td><td><span className={`badge ${item.enabled ? 'success' : ''}`}>{item.enabled ? '启用' : '停用'}</span></td><td className="row-actions"><button className="ghost" onClick={() => run('toggle-mod', async () => { await API.ToggleMod(id, item.path, !item.enabled); await refresh(); }, '模组状态已更新')}>{item.enabled ? '停用' : '启用'}</button>{!item.system && <button className="icon-button danger-icon" title="删除" onClick={() => confirm('删除这个模组？') && run('delete-mod', async () => { await API.DeleteMod(id, item.path); await refresh(); }, '模组已删除')}><Trash2 size={15}/></button>}</td></tr>)}</tbody></table>{!grouped[origin]?.length && <div className="empty"><Icon size={24}/><span>没有此类模组</span></div>}</div></section>)}
  </div>;
}

function formatBytes(value: number) { if (!value) return '0 B'; const units = ['B','KB','MB','GB']; const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`; }

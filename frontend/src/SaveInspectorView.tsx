import { useEffect, useState } from 'react';
import { Database, Download, RefreshCw, Search } from 'lucide-react';
import API, { isLinuxPlatform } from './platformApi';
import { main } from '../wailsjs/go/models';

export default function SaveInspectorView({ id, run }: { id: string; run: Function }) {
  const linuxPlatform = isLinuxPlatform();
  const [status, setStatus] = useState(new main.SaveInspectorStatus({ installed: false, version: '', path: '' })); const [result, setResult] = useState<main.SaveInspectionResult | null>(null);
  const refresh = () => API.GetSaveInspectorStatus().then(setStatus);
  useEffect(() => { refresh(); setResult(null); }, [id]);
  return <div className="stack"><section className="panel"><div className="panel-heading"><div><h2>只读存档浏览</h2><p>解析前自动备份，不写入 Level.sav</p></div><span className={`badge ${status.installed ? 'success' : ''}`}>{status.installed ? `解析器 ${status.version}` : '未安装解析器'}</span></div><div className="inspector-actions"><Database size={22}/><div><strong>按需解析玩家与公会数据</strong><span>首次使用会下载约 {linuxPlatform ? '162 MB 的 Linux' : '148 MB 的 Windows'} 解析组件，大型存档可能需要数分钟。</span></div>{!status.installed && <button className="ghost" onClick={() => run('install-inspector', async () => { await API.InstallSaveInspector(); await refresh(); }, '存档解析器已安装')}><Download size={14}/>安装组件</button>}<button className="primary" onClick={() => run('inspect-save', async () => setResult(await API.InspectSave(id)), '存档解析完成')}><Search size={14}/>开始解析</button></div></section>
    {result && <><section className="panel"><div className="panel-heading"><div><h2>玩家数据</h2><p>{result.players?.length || 0} 条记录 · {new Date(result.parsedAt).toLocaleString()}</p></div><RefreshCw size={16}/></div><RawTable records={result.players || []}/></section><section className="panel"><div className="panel-heading"><div><h2>公会数据</h2><p>{result.guilds?.length || 0} 条记录</p></div></div><RawTable records={result.guilds || []}/></section></>}
  </div>;
}

function RawTable({ records }: { records: Record<string, unknown>[] }) { const keys = Array.from(new Set(records.flatMap((item) => Object.keys(item)))).slice(0, 6); return <div className="table-wrap"><table><thead><tr>{keys.map((key) => <th key={key}>{key}</th>)}</tr></thead><tbody>{records.map((record, index) => <tr key={index}>{keys.map((key) => <td key={key}>{typeof record[key] === 'object' ? JSON.stringify(record[key]) : String(record[key] ?? '')}</td>)}</tr>)}</tbody></table>{!records.length && <div className="empty"><Database size={22}/><span>没有解析到记录</span></div>}</div>; }

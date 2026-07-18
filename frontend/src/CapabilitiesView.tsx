import { useCallback, useEffect, useMemo, useState } from 'react';
import { CheckCircle2, CircleOff, RefreshCw, ShieldCheck, TriangleAlert } from 'lucide-react';
import API, { isWebMode } from './platformApi';
import { main } from '../wailsjs/go/models';

const categoryOrder = ['内核', '管理通道', '插件', '玩家管理', '玩家奖励', '官方运维', '恢复'];

export default function CapabilitiesView({ id }: { id: string }) {
  const [report, setReport] = useState<main.ServerCapabilityReport | null>(null);
  const [audit, setAudit] = useState<main.AgentAuditEntry[]>([]);
  const [preflight, setPreflight] = useState<main.AgentPreflightReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const refresh = useCallback(async () => {
    setLoading(true); setError('');
    try {
      const [nextReport, nextAudit, nextPreflight] = await Promise.all([API.GetServerCapabilities(id), API.ListAgentAudit(30).catch(() => []), isWebMode ? API.GetAgentPreflight().catch(() => null) : Promise.resolve(null)]);
      setReport(nextReport); setAudit(nextAudit); setPreflight(nextPreflight);
    }
    catch (nextError) { setError(String(nextError)); }
    finally { setLoading(false); }
  }, [id]);
  useEffect(() => { void refresh(); const timer = window.setInterval(() => void refresh(), 10000); return () => window.clearInterval(timer); }, [refresh]);
  const grouped = useMemo(() => {
    const result: Record<string, main.CapabilityStatus[]> = {};
    for (const item of report?.capabilities || []) (result[item.category] ||= []).push(item);
    return result;
  }, [report]);
  return <div className="stack capability-page">
    <section className="panel capability-header"><div><h2>服务器能力中心</h2><p>{report?.platform ? `${report.platform.toUpperCase()} · ` : ''}按当前进程、官方 API、RCON 和插件状态判断每项功能是否真正可用。</p></div><button className="ghost" disabled={loading} onClick={() => void refresh()}><RefreshCw className={loading ? 'spin' : ''} size={15}/>{loading ? '检测中' : '重新检测'}</button></section>
    {error && <div className="inline-warning">能力检测失败：{error}</div>}
    {report && <section className="capability-summary panel"><div><ShieldCheck size={22}/><span><strong>{report.running ? '服务器运行中' : '服务器未运行'}</strong><small>服务端 {report.serverVersion || '版本未知'} · PalDefender {report.palDefenderVersion || '未知'} · UE4SS {report.ue4ssVersion || '未知'}</small></span></div><time>{new Date(report.checkedAt).toLocaleString()}</time></section>}
    {categoryOrder.filter((category) => grouped[category]?.length).map((category) => <section className="panel" key={category}><div className="panel-heading"><div><h2>{category}</h2><p>不可用功能会给出缺少的组件或通道</p></div></div><div className="capability-grid">{grouped[category].map((item) => <article className={`capability-card ${item.state}`} key={item.id}>{item.state === 'ready' ? <CheckCircle2 size={18}/> : item.state === 'warn' ? <TriangleAlert size={18}/> : <CircleOff size={18}/>}<span><strong>{item.name}</strong><small>{item.detail || item.reason}</small>{item.reason && item.detail && <em>{item.reason}</em>}</span><b>{item.state === 'ready' ? '可用' : item.state === 'warn' ? '需确认' : '不可用'}</b></article>)}</div></section>)}
    {preflight && <section className="panel"><div className="panel-heading"><div><h2>Agent 部署自检</h2><p>{preflight.simulatedPlatform ? `当前在 ${preflight.hostPlatform.toUpperCase()} 模拟 ${preflight.platform.toUpperCase()} 网页` : `${preflight.platform.toUpperCase()} ${preflight.architecture} · ${preflight.user || '当前服务用户'}`}</p></div><span className={`badge ${preflight.ok && !preflight.simulatedPlatform ? 'success' : preflight.ok ? '' : 'danger-badge'}`}>{preflight.simulatedPlatform ? '预览模拟' : preflight.ok ? '自检通过' : '发现问题'}</span></div><div className="diagnostic-list">{preflight.checks?.map((item, index) => <div className="diagnostic" key={`${item.name}-${index}`}><span className={`diagnostic-icon ${item.status === 'ok' ? 'ok' : item.status === 'warning' ? 'warn' : 'error'}`}>{item.status === 'ok' ? <CheckCircle2 size={17}/> : item.status === 'warning' ? <TriangleAlert size={17}/> : <CircleOff size={17}/>}</span><div><strong>{item.name}</strong><span>{item.detail}</span></div><span className={`badge ${item.status === 'ok' ? 'success' : ''}`}>{item.status === 'ok' ? 'OK' : item.status === 'warning' ? 'WARN' : 'ERROR'}</span></div>)}</div></section>}
    {(isWebMode || audit.length > 0) && <section className="panel"><div className="panel-heading"><div><h2>网页操作审计</h2><p>只记录管理方法、服务器、来源和结果，不记录密码、令牌或RPC参数</p></div><span className="badge">最近 {audit.length} 条</span></div><div className="table-wrap"><table><thead><tr><th>时间</th><th>操作</th><th>服务器</th><th>来源</th><th>结果</th></tr></thead><tbody>{audit.map((entry, index) => <tr key={`${entry.time}-${entry.method}-${index}`}><td>{new Date(entry.time).toLocaleString()}</td><td><code>{entry.method}</code></td><td><code>{entry.serverId || '-'}</code></td><td><code>{entry.remoteIp || '-'}</code></td><td><span className={`badge ${entry.successful ? 'success' : 'danger-badge'}`}>{entry.successful ? '成功' : entry.error || '失败'}</span></td></tr>)}</tbody></table>{!audit.length && <div className="empty"><ShieldCheck size={22}/><span>还没有网页管理操作记录</span></div>}</div></section>}
  </div>;
}

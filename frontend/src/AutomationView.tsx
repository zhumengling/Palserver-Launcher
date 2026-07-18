import { FormEvent, useCallback, useEffect, useState } from 'react';
import { Clock3, Download, Play, Plus, RefreshCw, Save, ShieldCheck, Trash2 } from 'lucide-react';
import API from './platformApi';
import { main } from '../wailsjs/go/models';

const maintenanceStatusLabels: Record<string, string> = { running: '执行中', ok: '已完成', error: '失败', skipped: '已跳过', interrupted: '已中断' };

export default function AutomationView({ instance, run, onChanged }: { instance: main.ServerInstance; run: Function; onChanged: () => Promise<void> }) {
  const [tasks, setTasks] = useState<main.MaintenanceTask[]>([]);
  const [update, setUpdate] = useState<main.ServerUpdateStatus | null>(null);
  const [policy, setPolicy] = useState(new main.ServerInstance(instance));
  const [form, setForm] = useState(new main.MaintenanceTask({ id: '', serverId: instance.id, name: '每日重启', type: 'restart', enabled: true, schedule: 'daily', intervalMinutes: 60, dailyTime: '04:00', lastRun: 0, nextRun: 0, lastStatus: '', lastMessage: '' }));
  const refresh = useCallback(async () => {
    setTasks((await API.ListMaintenanceTasks(instance.id)).filter((task) => task.type !== 'backup'));
    setUpdate(await API.GetServerUpdateStatus(instance.id).catch(() => null));
  }, [instance.id]);
  useEffect(() => { setPolicy(new main.ServerInstance(instance)); refresh(); }, [instance, refresh]);
  const field = (key: keyof main.ServerInstance, value: unknown) => setPolicy(new main.ServerInstance({ ...policy, [key]: value }));
  async function saveTask(event: FormEvent) { event.preventDefault(); await run('save-task', async () => { await API.SaveMaintenanceTask(form); setForm(new main.MaintenanceTask({ ...form, id: '' })); await refresh(); }, '维护任务已保存'); }
  return <div className="stack">
    <div className="two-columns">
      <section className="panel"><div className="panel-heading"><div><h2>安全更新</h2><p>Build ID 检测、备份、提醒、停服和重启</p></div><button className="ghost" onClick={refresh}><RefreshCw size={15}/></button></div>
        <div className="update-summary"><Download size={20}/><div><strong>{update?.updateAvailable ? '发现服务器更新' : update ? '当前已是最新版本' : '暂时无法查询版本'}</strong><span>本地 {update?.localBuildId || '-'} · 远端 {update?.remoteBuildId || '-'}</span></div><button className="primary" onClick={() => run('managed-update', () => API.PerformManagedUpdate(instance.id, false), '服务器更新完成')}>安全更新</button></div>
      </section>
      <section className="panel"><div className="panel-heading"><div><h2>Guardian</h2><p>检测进程、REST 和 RCON 无响应</p></div><ShieldCheck size={18}/></div>
        <div className="settings-fields compact-settings"><Toggle label="启用 Guardian" checked={policy.guardianEnabled} onChange={(v) => field('guardianEnabled', v)}/><NumberSetting label="失败阈值" value={policy.guardianFailureThreshold} onChange={(v) => field('guardianFailureThreshold', v)}/><NumberSetting label="检测间隔（秒）" value={policy.guardianCheckSeconds} onChange={(v) => field('guardianCheckSeconds', v)}/><NumberSetting label="每小时最多重启" value={policy.guardianMaxRestarts} onChange={(v) => field('guardianMaxRestarts', v)}/></div>
      </section>
    </div>
    <section className="panel"><div className="panel-heading"><div><h2>维护计划</h2><p>计划会持久保存，启动器重启后继续执行</p></div><Clock3 size={18}/></div>
      <form className="inline-form task-form" onSubmit={saveTask}><input value={form.name} onChange={(e) => setForm(new main.MaintenanceTask({ ...form, name: e.target.value }))}/><select value={form.type} onChange={(e) => setForm(new main.MaintenanceTask({ ...form, type: e.target.value }))}><option value="restart">重启</option><option value="update">更新</option></select><select value={form.schedule} onChange={(e) => setForm(new main.MaintenanceTask({ ...form, schedule: e.target.value }))}><option value="daily">每天</option><option value="interval">间隔</option></select>{form.schedule === 'daily' ? <input type="time" value={form.dailyTime} onChange={(e) => setForm(new main.MaintenanceTask({ ...form, dailyTime: e.target.value }))}/> : <input type="number" min="5" value={form.intervalMinutes} onChange={(e) => setForm(new main.MaintenanceTask({ ...form, intervalMinutes: Number(e.target.value) }))}/>}<button className="primary"><Plus size={14}/>保存任务</button></form>
      <div className="compact-list task-list">{tasks.map((task) => <div key={task.id}><div><strong>{task.name}</strong><small>{task.type.toUpperCase()} · {task.schedule === 'daily' ? task.dailyTime : `${task.intervalMinutes} 分钟`} · {maintenanceStatusLabels[task.lastStatus] || task.lastStatus || '等待执行'}{task.lastMessage ? ` · ${task.lastMessage}` : ''}</small></div><div className="row-actions"><button className="ghost" title="立即运行" onClick={() => run('run-task', async () => { await API.RunMaintenanceTask(task.id); await refresh(); }, '维护任务已开始')}><Play size={14}/></button><button className="icon-button danger-icon" title="删除" onClick={() => run('delete-task', async () => { await API.DeleteMaintenanceTask(task.id); await refresh(); }, '维护任务已删除')}><Trash2 size={14}/></button></div></div>)}</div>
    </section>
    <section className="panel"><div className="panel-heading"><div><h2>更新与访问策略</h2><p>备份保留策略已移至“存档备份”页面</p></div><button className="primary" onClick={() => run('save-policy', async () => { await API.SaveInstance(policy); await onChanged(); }, '自动化策略已保存')}><Save size={14}/>保存策略</button></div>
      <div className="settings-fields compact-settings"><Toggle label="Agent 启动后自动开服" checked={policy.startOnBoot} onChange={(v) => field('startOnBoot', v)}/><Toggle label="仅无人时自动更新" checked={policy.updateOnlyWhenEmpty} onChange={(v) => field('updateOnlyWhenEmpty', v)}/><NumberSetting label="更新提醒分钟" value={policy.updateWarnMinutes} onChange={(v) => field('updateWarnMinutes', v)}/><Toggle label="强制白名单" checked={policy.whitelistEnforced} onChange={(v) => field('whitelistEnforced', v)}/><Toggle label="进程崩溃后重启" checked={policy.crashRestart} onChange={(v) => field('crashRestart', v)}/></div>
    </section>
  </div>;
}

function Toggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) { return <label><span><strong>{label}</strong></span><input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)}/></label>; }
function NumberSetting({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) { return <label><span><strong>{label}</strong></span><input type="number" min="1" value={value || 1} onChange={(e) => onChange(Number(e.target.value))}/></label>; }

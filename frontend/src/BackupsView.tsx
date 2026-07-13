import { FormEvent, useCallback, useEffect, useState } from 'react';
import { Archive, Clock3, Play, Plus, RefreshCw, Save, Trash2 } from 'lucide-react';
import * as API from '../wailsjs/go/main/App';
import { main } from '../wailsjs/go/models';

export default function BackupsView({ instance, running, run, onChanged }: { instance: main.ServerInstance; running: boolean; run: Function; onChanged: () => Promise<void> }) {
  const [items, setItems] = useState<main.BackupEntry[]>([]);
  const [tasks, setTasks] = useState<main.MaintenanceTask[]>([]);
  const [policy, setPolicy] = useState(new main.ServerInstance(instance));
  const [form, setForm] = useState(new main.MaintenanceTask({ id: '', serverId: instance.id, name: '每日备份', type: 'backup', enabled: true, schedule: 'daily', intervalMinutes: 60, dailyTime: '04:00', lastRun: 0, nextRun: 0, lastStatus: '', lastMessage: '' }));
  const refresh = useCallback(async () => {
    const [backups, maintenance] = await Promise.all([API.ListBackups(instance.id), API.ListMaintenanceTasks(instance.id)]);
    setItems(backups);
    setTasks(maintenance.filter((task) => task.type === 'backup'));
  }, [instance.id]);
  useEffect(() => {
    setPolicy(new main.ServerInstance(instance));
    setForm((current) => new main.MaintenanceTask({ ...current, id: '', serverId: instance.id, type: 'backup' }));
    refresh();
  }, [instance, refresh]);
  const field = (key: keyof main.ServerInstance, value: unknown) => setPolicy(new main.ServerInstance({ ...policy, [key]: value }));
  async function saveTask(event: FormEvent) {
    event.preventDefault();
    await run('save-backup-task', async () => {
      await API.SaveMaintenanceTask(new main.MaintenanceTask({ ...form, serverId: instance.id, type: 'backup' }));
      setForm(new main.MaintenanceTask({ ...form, id: '', serverId: instance.id, type: 'backup' }));
      await refresh();
    }, '自动备份计划已保存');
  }
  return <div className="stack">
    <section className="panel"><div className="panel-heading"><div><h2>存档备份</h2><p>手动创建、查看和恢复启动器备份</p></div><button className="primary" onClick={() => run('backup', async () => { await API.CreateBackup(instance.id); await refresh(); }, '备份创建完成')}><Plus size={15}/>立即备份</button></div><div className="list">{items.map((item) => <div className="list-row" key={item.path}><Archive size={18}/><div><strong>{item.name}</strong><span>{formatBytes(item.size)} · {new Date(item.createdAt).toLocaleString()}</span></div><button className="ghost" disabled={running} onClick={() => confirm('恢复会覆盖当前存档，继续吗？') && run('restore', () => API.RestoreBackup(instance.id, item.path), '备份恢复完成')}><RefreshCw size={14}/>恢复</button></div>)}{!items.length && <div className="empty"><Archive size={24}/><span>还没有备份</span></div>}</div></section>
    <section className="panel"><div className="panel-heading"><div><h2>自动备份计划</h2><p>原维护计划中的备份任务已统一迁移到这里管理</p></div><Clock3 size={18}/></div><form className="inline-form task-form" onSubmit={saveTask}><input value={form.name} onChange={(e) => setForm(new main.MaintenanceTask({ ...form, name: e.target.value }))}/><select value={form.schedule} onChange={(e) => setForm(new main.MaintenanceTask({ ...form, schedule: e.target.value }))}><option value="daily">每天</option><option value="interval">间隔</option></select>{form.schedule === 'daily' ? <input type="time" value={form.dailyTime} onChange={(e) => setForm(new main.MaintenanceTask({ ...form, dailyTime: e.target.value }))}/> : <input type="number" min="5" value={form.intervalMinutes} onChange={(e) => setForm(new main.MaintenanceTask({ ...form, intervalMinutes: Number(e.target.value) }))}/>}<button className="primary"><Plus size={14}/>保存计划</button></form><div className="compact-list task-list">{tasks.map((task) => <div key={task.id}><div><strong>{task.name}</strong><small>{task.schedule === 'daily' ? `每天 ${task.dailyTime}` : `每 ${task.intervalMinutes} 分钟`} · {task.lastStatus || '等待执行'}</small></div><div className="row-actions"><button className="ghost" title="立即备份" onClick={() => run('run-backup-task', async () => { await API.RunMaintenanceTask(task.id); await refresh(); }, '备份任务已开始')}><Play size={14}/></button><button className="icon-button danger-icon" title="删除" onClick={() => run('delete-backup-task', async () => { await API.DeleteMaintenanceTask(task.id); await refresh(); }, '备份计划已删除')}><Trash2 size={14}/></button></div></div>)}{!tasks.length && <span className="compact-empty">还没有自动备份计划</span>}</div></section>
    <section className="panel"><div className="panel-heading"><div><h2>备份保留策略</h2><p>统一控制自动备份和手动备份的清理规则</p></div><button className="primary" onClick={() => run('save-backup-policy', async () => { await API.SaveInstance(policy); await onChanged(); }, '备份策略已保存')}><Save size={14}/>保存策略</button></div><div className="settings-fields compact-settings"><label><span><strong>保留方式</strong></span><select value={policy.backupRetentionMode} onChange={(e) => field('backupRetentionMode', e.target.value)}><option value="tiered">分层保留</option><option value="count">按数量</option><option value="days">按天数</option></select></label><NumberSetting label="最多保留数量" value={policy.backupRetentionCount} onChange={(value) => field('backupRetentionCount', value)}/><NumberSetting label="最多保留天数" value={policy.backupRetentionDays} onChange={(value) => field('backupRetentionDays', value)}/></div></section>
  </div>;
}

function NumberSetting({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) { return <label><span><strong>{label}</strong></span><input type="number" min="1" value={value || 1} onChange={(event) => onChange(Number(event.target.value))}/></label>; }
function formatBytes(value: number) { if (!value) return '0 B'; const units = ['B','KB','MB','GB']; const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`; }

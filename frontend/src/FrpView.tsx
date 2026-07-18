import { useCallback, useEffect, useState } from 'react';
import { Download, Network, Play, RefreshCw, Save, ShieldAlert, Square, Terminal } from 'lucide-react';
import API from './platformApi';
import { main } from '../wailsjs/go/models';

export default function FrpView({ instance, run }: { instance: main.ServerInstance; run: Function }) {
  const [status, setStatus] = useState(new main.FrpStatus({ installed: false, version: '', path: '', running: false, pid: 0, settings: {} }));
  const [settings, setSettings] = useState(new main.FrpSettings({ serverId: instance.id, serverAddress: '', serverPort: 7000, tokenConfigured: false, proxyName: '', remoteGamePort: instance.publicPort, queryEnabled: false, remoteQueryPort: instance.queryPort, rconEnabled: false, remoteRconPort: instance.rconPort, restEnabled: false, remoteRestPort: instance.restPort, autoStart: false }));
  const [token, setToken] = useState('');
  const [log, setLog] = useState('');
  const field = (key: keyof main.FrpSettings, value: any) => setSettings(new main.FrpSettings({ ...settings, [key]: value }));

  const refreshRuntime = useCallback(async () => {
    const [nextStatus, nextLog] = await Promise.all([API.GetFrpStatus(instance.id), API.GetFrpLog(instance.id, 120)]);
    setStatus(nextStatus); setLog(nextLog);
  }, [instance.id]);

  useEffect(() => {
    API.GetFrpSettings(instance.id).then(setSettings);
    refreshRuntime();
    const timer = window.setInterval(refreshRuntime, 3000);
    return () => window.clearInterval(timer);
  }, [instance.id, refreshRuntime]);

  async function save() {
    await API.SaveFrpSettings(new main.FrpSettings({ ...settings, serverId: instance.id }), token);
    setToken('');
    setSettings(await API.GetFrpSettings(instance.id));
    await refreshRuntime();
  }

  async function start() {
    await save();
    await API.StartFrp(instance.id);
    await refreshRuntime();
  }

  return <div className="stack">
    <section className="panel">
      <div className="panel-heading"><div><h2>FRP 客户端</h2><p>启动器自动下载并隐藏运行 frpc，游戏连接使用 UDP 转发</p></div><div className="toolbar"><span className={`badge ${status.running ? 'success' : ''}`}>{status.running ? `运行中 · PID ${status.pid}` : status.installed ? '已停止' : '未安装'}</span><button className="ghost" onClick={refreshRuntime}><RefreshCw size={14}/>刷新</button></div></div>
      <div className="frp-status-card"><Network size={22}/><div><strong>{status.installed ? `frpc ${status.version || '已安装'}` : '尚未安装 FRP 客户端'}</strong><span>{status.path || '将安装到启动器运行目录'}</span></div><button className="ghost" disabled={status.running} onClick={() => run('install-frp', async () => { await API.InstallFrp(); await refreshRuntime(); }, 'FRP 客户端已安装/更新')}><Download size={14}/>安装/更新</button></div>
    </section>

    <div className="frp-grid">
      <section className="panel">
        <div className="panel-heading"><div><h2>服务端连接</h2><p>填写公网 FRPS 地址、端口与认证 Token</p></div><Save size={17}/></div>
        <div className="frp-form">
          <label><span>FRPS 地址</span><input value={settings.serverAddress} onChange={(e) => field('serverAddress', e.target.value)} placeholder="frps.example.com"/></label>
          <label><span>FRPS 端口</span><input type="number" min="1" max="65535" value={settings.serverPort} onChange={(e) => field('serverPort', Number(e.target.value))}/></label>
          <label><span>认证 Token</span><input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder={settings.tokenConfigured ? '已加密保存，留空保持不变' : '请输入 FRPS token'}/></label>
          <label><span>代理名称</span><input value={settings.proxyName} onChange={(e) => field('proxyName', e.target.value)} placeholder="pal-main"/></label>
          <label className="frp-check"><input type="checkbox" checked={settings.autoStart} onChange={(e) => field('autoStart', e.target.checked)}/><span><strong>启动器打开时自动连接</strong><small>关闭启动器时会停止其管理的 FRP 客户端</small></span></label>
        </div>
      </section>

      <section className="panel">
        <div className="panel-heading"><div><h2>端口映射</h2><p>游戏和查询端口使用 UDP，管理端口使用 TCP</p></div><Network size={17}/></div>
        <div className="frp-tunnel-list">
          <div className="frp-tunnel-row fixed"><div><strong>游戏端口</strong><span>UDP · 本机 {instance.publicPort}</span></div><label>公网端口<input type="number" min="1" max="65535" value={settings.remoteGamePort} onChange={(e) => field('remoteGamePort', Number(e.target.value))}/></label></div>
          <div className="frp-tunnel-row"><label className="frp-check"><input type="checkbox" checked={settings.queryEnabled} onChange={(e) => field('queryEnabled', e.target.checked)}/><span><strong>查询端口</strong><small>UDP · 本机 {instance.queryPort}</small></span></label><label>公网端口<input disabled={!settings.queryEnabled} type="number" min="1" max="65535" value={settings.remoteQueryPort} onChange={(e) => field('remoteQueryPort', Number(e.target.value))}/></label></div>
          <div className="frp-tunnel-row"><label className="frp-check"><input type="checkbox" checked={settings.rconEnabled} onChange={(e) => field('rconEnabled', e.target.checked)}/><span><strong>RCON 管理端口</strong><small>TCP · 本机 {instance.rconPort}</small></span></label><label>公网端口<input disabled={!settings.rconEnabled} type="number" min="1" max="65535" value={settings.remoteRconPort} onChange={(e) => field('remoteRconPort', Number(e.target.value))}/></label></div>
          <div className="frp-tunnel-row"><label className="frp-check"><input type="checkbox" checked={settings.restEnabled} onChange={(e) => field('restEnabled', e.target.checked)}/><span><strong>REST API 管理端口</strong><small>TCP · 本机 {instance.restPort}</small></span></label><label>公网端口<input disabled={!settings.restEnabled} type="number" min="1" max="65535" value={settings.remoteRestPort} onChange={(e) => field('remoteRestPort', Number(e.target.value))}/></label></div>
        </div>
        {(settings.rconEnabled || settings.restEnabled) && <div className="frp-security-warning"><ShieldAlert size={16}/><span>管理端口暴露到公网存在风险。请使用高强度管理员密码，并在 FRPS 防火墙限制访问来源。</span></div>}
      </section>
    </div>

    <section className="panel">
      <div className="panel-heading"><div><h2>连接控制</h2><p>frpc 以隐藏窗口运行，日志统一显示在这里</p></div><div className="toolbar"><button className="ghost" disabled={status.running} onClick={() => run('save-frp', save, 'FRP 配置已保存')}><Save size={14}/>保存配置</button>{status.running ? <button className="danger" onClick={() => run('stop-frp', async () => { await API.StopFrp(instance.id); await refreshRuntime(); }, 'FRP 客户端已停止')}><Square size={14}/>停止</button> : <button className="primary" disabled={!status.installed} onClick={() => run('start-frp', start, 'FRP 客户端已启动')}><Play size={14}/>保存并启动</button>}</div></div>
      <div className="frp-log-heading"><Terminal size={14}/><span>frpc.log</span></div>
      <pre className="frp-log">{log || '暂无日志。安装并启动 FRP 客户端后会显示连接状态。'}</pre>
    </section>
  </div>;
}

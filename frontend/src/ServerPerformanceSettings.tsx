import { Cpu } from 'lucide-react';

import { main } from '../wailsjs/go/models';

type Props = {
  value: main.ServerInstance;
  onChange: (key: keyof main.ServerInstance, value: unknown) => void;
};

export default function ServerPerformanceSettings({ value, onChange }: Props) {
  const legacyFlags = Boolean(value.legacyPerformanceFlags);
  return (
    <section className="server-performance-settings">
      <div className="server-performance-heading">
        <Cpu size={17}/>
        <div><strong>CPU 与性能</strong><small>Palworld 1.0 默认策略</small></div>
      </div>
      <div className="server-performance-grid">
        <label className="check-setting">
          <span><strong>120 FPS / Tick 优化</strong><small>保留当前 Engine.ini 默认性能配置</small></span>
          <input type="checkbox" checked={Boolean(value.performanceMode)} onChange={(event) => onChange('performanceMode', event.target.checked)}/>
        </label>
        <label className="check-setting">
          <span><strong>旧版多线程参数</strong><small>1.0 默认关闭，仅用于兼容性测试</small></span>
          <input type="checkbox" checked={legacyFlags} onChange={(event) => onChange('legacyPerformanceFlags', event.target.checked)}/>
        </label>
        <label>
          <span><strong>Worker Threads</strong><small>需配合旧版多线程参数，0 表示自动</small></span>
          <input type="number" min="0" max="256" disabled={!legacyFlags} value={value.workerThreads || 0} onChange={(event) => onChange('workerThreads', Number(event.target.value))}/>
        </label>
        <label>
          <span><strong>进程优先级</strong><small>Above Normal 兼顾性能与系统响应</small></span>
          <select value={value.processPriority || 'above_normal'} onChange={(event) => onChange('processPriority', event.target.value)}>
            <option value="normal">Normal</option>
            <option value="above_normal">Above Normal</option>
            <option value="high">High</option>
          </select>
        </label>
        <label>
          <span><strong>CPU 核心分配</strong><small>多开时按物理核心自动隔离</small></span>
          <select value={value.cpuAffinityMode || 'auto'} onChange={(event) => onChange('cpuAffinityMode', event.target.value)}>
            <option value="auto">自动隔离</option>
            <option value="all">不限制核心</option>
          </select>
        </label>
      </div>
    </section>
  );
}

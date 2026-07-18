import * as DesktopAPI from '../wailsjs/go/main/App';

export const isWebMode = !(window as any).go?.main?.App;
let agentPlatform = isWebMode ? '' : 'windows';

const longRunningWebMethods = new Set([
  'ApplyGamePreset', 'ApplyOfficialPvPPreset', 'ClearSteamCMDCache', 'CreateBackup', 'DeleteInstance',
  'DuplicateInstance', 'ExportClientMods', 'InspectSave', 'InstallFrp', 'InstallOrUpdateServer',
  'ImportUploadedServer', 'InstallSaveInspector', 'PerformManagedUpdate', 'PruneBackups', 'QuickSetup', 'RestoreBackup',
  'SaveWorld', 'StartGameEvent', 'StopGameEvent', 'UpdateAllExtensions', 'UpdateExtension',
]);

export function getAgentPlatform(): string {
  return agentPlatform;
}

export function isLinuxPlatform(): boolean {
  return getAgentPlatform() === 'linux';
}

type DesktopAPIType = typeof DesktopAPI;

export type AgentBackgroundJob = {
  id: string; method: string; serverId: string; state: 'running' | 'completed' | 'error';
  createdAt: number; startedAt: number; finishedAt: number; error?: string;
};

function agentAuthError(payload: any, status: number) {
  if (status === 401) {
    window.dispatchEvent(new CustomEvent('pal-agent-auth-required'));
    return new Error(payload?.error || '需要登录 Agent 管理后台');
  }
  return new Error(payload?.error || `HTTP ${status}`);
}

async function agentResponseError(response: Response): Promise<Error> {
  const payload = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
  return agentAuthError(payload, response.status);
}

export async function uploadAgentFiles(endpoint: string, files: File[]): Promise<void> {
  const form = new FormData();
  files.forEach((file) => form.append('files', file, file.name));
  const response = await fetch(endpoint, { method: 'POST', credentials: 'same-origin', body: form });
  if (!response.ok) throw await agentResponseError(response);
}

export async function downloadAgentArchive(endpoint: string, fallbackName: string): Promise<void> {
  const response = await fetch(endpoint, { method: 'POST', credentials: 'same-origin' });
  if (!response.ok) throw await agentResponseError(response);
  const blob = await response.blob();
  const disposition = response.headers.get('Content-Disposition') || '';
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  const quoted = disposition.match(/filename="([^"]+)"/i)?.[1];
  const filename = encoded ? decodeURIComponent(encoded) : quoted || fallbackName;
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

const wait = (milliseconds: number) => new Promise((resolve) => window.setTimeout(resolve, milliseconds));

async function webBackgroundJob(method: string, args: unknown[]) {
  const started = await fetch(`/api/v1/jobs/${encodeURIComponent(method)}`, {
    method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ args }),
  });
  const initial = await started.json().catch(() => ({ error: `HTTP ${started.status}` }));
  if (!started.ok) throw agentAuthError(initial, started.status);
  let failures = 0;
  for (;;) {
    await wait(750);
    let response: Response;
    try {
      response = await fetch(`/api/v1/jobs/${encodeURIComponent(initial.id)}`, { credentials: 'same-origin', cache: 'no-store' });
      failures = 0;
    } catch (error) {
      failures++;
      if (failures < 30) continue;
      throw new Error(`与 Agent 的连接持续中断，后台任务可能仍在运行：${error}`);
    }
    const job = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
    if (!response.ok) throw agentAuthError(job, response.status);
    if (job.state === 'completed') return job.result;
    if (job.state === 'error') throw new Error(job.error || '后台任务执行失败');
  }
}

async function waitForWebJob(initial: any) {
  let failures = 0;
  for (;;) {
    await wait(750);
    let response: Response;
    try {
      response = await fetch(`/api/v1/jobs/${encodeURIComponent(initial.id)}`, { credentials: 'same-origin', cache: 'no-store' });
      failures = 0;
    } catch (error) {
      failures++;
      if (failures < 30) continue;
      throw new Error(`与 Agent 的连接持续中断，任务可能仍在运行：${error}`);
    }
    const job = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
    if (!response.ok) throw agentAuthError(job, response.status);
    if (job.state === 'completed') return job.result;
    if (job.state === 'error') throw new Error(job.error || '后台任务执行失败');
  }
}

export async function uploadAndImportServer(name: string, files: File[]): Promise<any> {
  if (!isWebMode) throw new Error('服务器导入上传仅适用于 Linux 网页 Agent');
  if (!files.length) throw new Error('请选择服务器 ZIP 文件或服务器文件夹');
  const form = new FormData();
  form.append('name', name);
  files.forEach((file) => {
    const relative = (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name;
    form.append('files', file, file.name);
    form.append('paths', relative);
  });
  const response = await fetch('/api/v1/upload/server-import', { method: 'POST', credentials: 'same-origin', body: form });
  const initial = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
  if (!response.ok) throw agentAuthError(initial, response.status);
  return waitForWebJob(initial);
}

async function webRPC(method: string, args: unknown[]) {
  if (longRunningWebMethods.has(method)) return webBackgroundJob(method, args);
  const response = await fetch(`/api/v1/rpc/${encodeURIComponent(method)}`, {
    method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ args }),
  });
  const payload = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
  if (response.status === 401) throw agentAuthError(payload, response.status);
  if (!response.ok || payload.error) throw new Error(payload.error || `HTTP ${response.status}`);
  return payload.result;
}

const WebAPI = new Proxy({} as DesktopAPIType, {
  get(_target, property) {
    if (typeof property !== 'string') return undefined;
    return (...args: unknown[]) => webRPC(property, args);
  },
});

const API: DesktopAPIType = isWebMode ? WebAPI : DesktopAPI;

export async function getAgentHealth(): Promise<{ ok: boolean; version: string; platform: string; authenticated: boolean; setupRequired: boolean }> {
  const response = await fetch('/api/v1/health', { credentials: 'same-origin', cache: 'no-store' });
  if (!response.ok) throw new Error(`Agent 健康检查失败：HTTP ${response.status}`);
  const payload = await response.json().catch(() => null);
  if (!payload || payload.ok !== true) throw new Error('没有收到有效的 Agent 健康响应，请检查网页反向代理是否将 /api 请求转发到 Agent');
  agentPlatform = payload.platform || agentPlatform;
  document.documentElement.dataset.agentPlatform = agentPlatform;
  return payload;
}

export async function createAgentPassword(password: string): Promise<void> {
  const response = await fetch('/api/v1/setup', {
    method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password }),
  });
  const payload = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
  if (!response.ok) throw new Error(payload.error || '创建管理密码失败');
}

export async function loginAgent(password: string): Promise<void> {
  const response = await fetch('/api/v1/session', {
    method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password }),
  });
  const payload = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
  if (!response.ok) throw new Error(payload.error || '登录失败');
}

export async function logoutAgent(): Promise<void> {
  await fetch('/api/v1/session', { method: 'DELETE', credentials: 'same-origin' });
}

export async function listAgentJobs(serverId = '', limit = 100): Promise<AgentBackgroundJob[]> {
  if (!isWebMode) return [];
  const query = new URLSearchParams({ limit: String(limit) });
  if (serverId) query.set('server', serverId);
  const response = await fetch(`/api/v1/jobs?${query}`, { credentials: 'same-origin', cache: 'no-store' });
  const payload = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
  if (!response.ok) throw agentAuthError(payload, response.status);
  return Array.isArray(payload) ? payload : [];
}

export default API;

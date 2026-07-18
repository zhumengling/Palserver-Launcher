import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8');
const api = readFileSync(new URL('../src/platformApi.ts', import.meta.url), 'utf8');

test('Linux server migration selects data on the browser computer instead of typing an Agent path', () => {
  assert.match(app, /从本地电脑迁移服务器/);
  assert.match(app, /webkitdirectory/);
  assert.match(app, /只迁移可跨平台的数据/);
  assert.match(api, /\/api\/v1\/upload\/server-import/);
  assert.doesNotMatch(app, /navigator\.clipboard\.writeText\(instance\.rootPath\)/);
  assert.doesNotMatch(app, /请输入 Linux 帕鲁服务器完整路径/);
  assert.match(app, /新增服务器/);
  assert.match(app, /从当前电脑迁移服务器/);
  assert.match(app, /\['plugins', 'mods', 'frp'\]/);
  assert.match(app, /linuxHiddenViews\.has\(view\)/);
  assert.match(app, /服务器文件由程序自动管理/);
  assert.match(readFileSync(new URL('../src/ModsView.tsx', import.meta.url), 'utf8'), /LinuxReadOnlyMods/);
  assert.match(readFileSync(new URL('../src/ModsView.tsx', import.meta.url), 'utf8'), /已移除安装、更新、启用、停用和卸载入口/);
});

test('remote Linux import reinstalls the runtime and ignores Windows components', () => {
  assert.match(app, /重新下载官方 Linux 服务端/);
  assert.match(app, /Windows EXE、UE4SS、PalDefender DLL/);
  assert.match(app, /服务器文件由程序自动管理/);
});

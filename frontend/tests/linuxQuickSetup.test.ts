import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const source = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8');
const start = source.indexOf('function LinuxQuickSetupDialog');
const end = source.indexOf('function SetupResource', start);
const dialog = source.slice(start, end);

test('Linux quick setup uses the Agent managed server directory without a path field', () => {
  assert.ok(start >= 0 && end > start, 'Linux quick setup component was not found');
  assert.match(dialog, /服务器存放位置/);
  assert.match(dialog, /onInstall\([^,]+, ''\)/);
  assert.doesNotMatch(dialog, /setup-location|value=\{installRoot\}|\/srv\/palworld/);
  assert.equal((dialog.match(/<input\b/g) || []).length, 1, 'Linux setup should only ask for the server name');
});

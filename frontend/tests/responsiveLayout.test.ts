import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const appSource = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8');
const cssSource = readFileSync(new URL('../src/App.css', import.meta.url), 'utf8');

test('mobile navigation exposes an accessible drawer toggle and backdrop', () => {
  assert.match(appSource, /aria-controls="primary-navigation"/);
  assert.match(appSource, /aria-expanded=\{mobileNavOpen\}/);
  assert.match(appSource, /className=\{`sidebar \$\{mobileNavOpen \? 'open' : ''\}`\}/);
  assert.match(appSource, /className="sidebar-backdrop"/);
  assert.match(appSource, /event\.key === 'Escape'/);
});

test('phone breakpoint gives the workspace the full viewport width', () => {
  const compact = cssSource.replace(/\s+/g, '');
  assert.match(compact, /@media\(max-width:620px\).*?\.app-shell\{grid-template-columns:minmax\(0,1fr\)\}/);
  assert.match(compact, /\.sidebar\{position:fixed;.*?transform:translateX\(-105%\)/);
  assert.match(compact, /\.sidebar\.open\{visibility:visible;transform:translateX\(0\)\}/);
  assert.match(compact, /\.sidebar-backdrop\{display:block;position:fixed;inset:000min\(82vw,280px\)/);
});

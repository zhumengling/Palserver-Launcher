import assert from 'node:assert/strict';
import test from 'node:test';

import {
  CATALOG_PAGE_SIZE,
  catalogCategories,
  catalogEntryMeta,
  catalogImageSource,
  filterCatalogEntries,
  nextCatalogLimit,
  type CatalogEntry,
} from '../src/catalogUtils.ts';

const entries: CatalogEntry[] = [
  { id: 'Wood_WorldTree', name: 'Mythical Wood', nameZh: '神秘木材', category: 'Material', source: 'official' },
  { id: 'BeamLauncher', name: 'Beam Launcher', nameZh: '光束炮发射器', category: 'Weapon', source: 'official' },
  { id: 'PalEgg_Dragon_05', name: 'Huge Dragon Egg', category: 'Material', source: 'official' },
  { id: 'WorldTreeDragon', name: 'World Tree Dragon', nameZh: '枯星龙', source: 'official', elements: ['Dragon'] },
  { id: 'Boss_Anubis', name: 'Anubis（首领）', source: 'official', elements: ['Earth'], variant: 'boss' },
  { id: 'AssaultRifle_NPC_GrassBoss', name: 'NPC Assault Rifle', category: 'Weapon', source: 'developer' },
  { id: 'Debug_Handgun_Stun', name: 'Debug Handgun', category: 'Legacy', source: 'legacy' },
];

const abilityEntries: CatalogEntry[] = [
  { id: 'AirCanon', name: 'Air Cannon', nameZh: '空气弹', category: 'Normal', power: 40, cooldown: 2, source: 'official' },
  { id: 'Legend', name: 'Legend', nameZh: '传说', category: '其他效果', rank: 4, source: 'official' },
];

test('目录搜索覆盖名称、内部 ID、分类和来源', () => {
  assert.deepEqual(filterCatalogEntries(entries, { query: 'worldtree' }).map((entry) => entry.id), ['Wood_WorldTree', 'WorldTreeDragon']);
  assert.deepEqual(filterCatalogEntries(entries, { query: '神秘木材' }).map((entry) => entry.id), ['Wood_WorldTree']);
  assert.deepEqual(filterCatalogEntries(entries, { query: '枯星龙' }).map((entry) => entry.id), ['WorldTreeDragon']);
  assert.deepEqual(filterCatalogEntries(entries, { query: 'weapon' }).map((entry) => entry.id), ['BeamLauncher']);
  assert.deepEqual(filterCatalogEntries(entries, { query: '材料' }).map((entry) => entry.id), ['Wood_WorldTree', 'PalEgg_Dragon_05']);
  assert.deepEqual(filterCatalogEntries(entries, { query: '龙' }).map((entry) => entry.id), ['WorldTreeDragon']);
  assert.deepEqual(filterCatalogEntries(entries, { query: '首领' }).map((entry) => entry.id), ['Boss_Anubis']);
  assert.deepEqual(filterCatalogEntries(entries, { query: '开发/隐藏', includeUnavailable: true }).map((entry) => entry.id), ['AssaultRifle_NPC_GrassBoss']);
  assert.deepEqual(filterCatalogEntries(entries, { query: '旧版兼容', includeUnavailable: true }).map((entry) => entry.id), ['Debug_Handgun_Stun']);
  assert.deepEqual(filterCatalogEntries(entries, { query: 'legacy', includeUnavailable: true }).map((entry) => entry.id), ['Debug_Handgun_Stun']);
});

test('默认仅显示正式道具，打开高级开关后包含开发和旧版条目', () => {
  assert.deepEqual(filterCatalogEntries(entries).map((entry) => entry.id), ['Wood_WorldTree', 'BeamLauncher', 'PalEgg_Dragon_05', 'WorldTreeDragon', 'Boss_Anubis']);
  assert.equal(filterCatalogEntries(entries, { includeUnavailable: true }).length, 7);
});

test('目录可组合蛋前缀与道具分类筛选', () => {
  assert.deepEqual(filterCatalogEntries(entries, { category: 'Material', filterPrefix: 'PalEgg_' }).map((entry) => entry.id), ['PalEgg_Dragon_05']);
  assert.deepEqual(catalogCategories(entries), ['Legacy', 'Material', 'Weapon']);
});

test('目录分页可以逐步展示到完整结果而不是固定截断 300 项', () => {
  assert.equal(CATALOG_PAGE_SIZE, 200);
  assert.equal(nextCatalogLimit(200, 650), 400);
  assert.equal(nextCatalogLimit(600, 650), 650);
});

test('目录条目元信息区分当前官方内容和旧版兼容内容', () => {
  assert.equal(catalogEntryMeta(entries[0], 'item'), '材料 · Wood_WorldTree');
  assert.equal(catalogEntryMeta(entries[5], 'item'), '开发/隐藏 · AssaultRifle_NPC_GrassBoss');
  assert.equal(catalogEntryMeta(entries[6], 'item'), '旧版兼容 · Debug_Handgun_Stun');
  assert.equal(catalogEntryMeta({ id: 'WorldTreeDragon', paldexNumber: 204, elements: ['Normal'] }, 'pal'), '#204 · 普通 · WorldTreeDragon');
  assert.equal(catalogEntryMeta({ id: 'Boss_Anubis', paldexNumber: 100, elements: ['Earth'], variant: 'boss' }, 'pal'), '#100 · 首领 · 地 · Boss_Anubis');
  assert.equal(catalogEntryMeta(abilityEntries[0], 'skill'), '普通 · 威力 40 · 冷却 2 秒 · AirCanon');
  assert.equal(catalogEntryMeta(abilityEntries[1], 'passive'), '其他效果 · 等级 4 · Legend');
  assert.equal(catalogEntryMeta({ id: 'MudShot', nameZh: '泥浆投掷', category: 'Earth', categoryZh: 'Earth', power: 40 }, 'skill'), '地 · 威力 40 · MudShot');
  assert.equal(catalogEntryMeta({ id: 'NeutralSkill', nameZh: '无属性技能', category: 'None' }, 'skill'), '无属性 · NeutralSkill');
});

test('上游图标加载失败后回退到本地图标占位', () => {
  const entry = { id: 'GasMask', image: 'https://example.invalid/missing.webp' };
  assert.equal(catalogImageSource(entry, false), entry.image);
  assert.equal(catalogImageSource(entry, true), undefined);
});

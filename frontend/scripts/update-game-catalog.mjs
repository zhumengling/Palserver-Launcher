import { createHash } from 'node:crypto';
import { rename, rm, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const atlasBase = 'https://awy64.github.io/palworld-atlas-data/v1';
const atlasRepository = 'https://github.com/Awy64/palworld-atlas-data';
const atlasBuildPath = 'builds/24181105';
const completeCatalogCommit = '4ff53eae0ffeb41c849ef3021d7c5f1f22ece732';
const completeCatalogRepository = 'https://github.com/deafdudecomputers/PalWorldSaveTools';
const completeCatalogBase = `https://raw.githubusercontent.com/deafdudecomputers/PalWorldSaveTools/${completeCatalogCommit}/resources/game_data`;
const fullItemCatalogUrl = `${completeCatalogBase}/items.json`;
const fullCharacterCatalogUrl = `${completeCatalogBase}/characters.json`;
const fullSkillCatalogUrl = `${completeCatalogBase}/skills.json`;
const chineseCatalogCommit = 'f51a6a3b2f2b45907d314ca3da8e8530acc6d7e2';
const chineseCatalogRepository = 'https://github.com/zhudikangta/paltoolbox';
const chineseCatalogBase = `https://raw.githubusercontent.com/zhudikangta/paltoolbox/${chineseCatalogCommit}/游戏内容/幻兽帕鲁1.0/数据包`;
const chineseItemCatalogUrl = encodeURI(`${chineseCatalogBase}/物品.json`);
const chinesePalCatalogUrl = encodeURI(`${chineseCatalogBase}/帕鲁.json`);
const chineseSkillCatalogUrl = encodeURI(`${chineseCatalogBase}/技能.json`);
const dataDirectory = new URL('../src/data/', import.meta.url);
const outputFile = new URL('game-catalog.json', dataDirectory);

const expectedSnapshot = Object.freeze({
  steamBuildId: '24181105',
  atlasItemChecksum: '7a48993c911a09a1030a92b9dbe454e3cbcc287b51fc9e3c94fe82d941a6c5a8',
  atlasPalChecksum: '57fb4bf837061c1160d5f72755152245fe793e1b0073328714efd63c65ba5b47',
  completeItemChecksum: '70f242bffca0a64e66e0e91fa8ea1c0709b383c2dbcd4847afb2ff24a5a9ae39',
  completeCharacterChecksum: '6ea22f750780ec89fb0ceeef8304335318c0cedffafd47c4a32e2fd85c6e0d39',
  completeSkillChecksum: 'b9172f389bf56a307194d25b70aca23f8610ef81de32bb44bda827f65b83add1',
  chineseItemChecksum: '4c16bdc5d727739796fd4a04192ae77c172737ea5ad78b37710924561eb63b57',
  chinesePalChecksum: 'd336740885c9d4378784564532b775e35a356843078bee072e234d227223acfc',
  chineseSkillChecksum: '49f180c123df6d5d9a5a5c831aaec7d667f57633840da3a927110dbcb5da6911',
  officialItemCount: 1891,
  completeItemCount: 2466,
  developerItemCount: 575,
  legacyItemCount: 1,
  completeCharacterCount: 1123,
  chinesePalCount: 753,
  palCount: 289,
  bossPalCount: 289,
  completeSkillCount: 375,
  completePassiveCount: 1905,
  skillCount: 375,
  passiveCount: 488,
});

const categoryLabels = Object.freeze({
  Accessory: '饰品', Ammo: '弹药', Armor: '防具', Blueprint: '图纸', CaptureItemModifier: '捕获模块',
  Consume: '消耗品', Essential: '重要道具', Food: '食物', Glider: '滑翔装备', Legacy: '旧版兼容道具',
  Material: '材料', PalSummon: '帕鲁装备', SpecialWeapon: '特殊武器', Weapon: '武器', Other: '道具',
});

const skillFallbackNames = Object.freeze({
  Throw: '投掷', Scratch: '抓挠', WorkAttack: '工作攻击', Human_Rolling: '翻滚',
  Funnel_RaijinDaughter: '雷鸣童女浮游炮', Funnel_DreamDemon: '梦魇浮游炮', Funnel_RaijinDaughter_Water: '水灵童女浮游炮',
  BlueThunderHorse_PartnerSkill: '青雷马伙伴技能', PoseidonOrca_PartnerSkill: '海神鲸伙伴技能',
  PoseidonOrca_PartnerSkill_SpearBullet: '海神鲸长枪弹', GrassGolem_PartnerSkill: '草木魔像伙伴技能',
  GrassGolem_Dark_PartnerSkill: '暗黑草木魔像伙伴技能', Unique_LegendDeer_RadiantPurge: '默世鹿·辉光净化',
  Unique_KingWhale_TidalBore: '奥沧鲸·潮汐钻击', Unique_KingWhale_SuperTidalBore: '奥沧鲸·超级潮汐钻击',
  Unique_ThunderDragonMan_GYM_Act: '雷龙人首领技', Unique_BlueSkyDragon_GYM_Act: '青天龙首领技',
  Unique_LilyQueen_GYM_Act: '百合女王首领技', Unique_MoonQueen_GYM_Act: '月之女王首领技',
  Unique_LilyQueen_LilyHealing_Boss: '百合女王·治愈百合', Unique_MoonQueen_GYM_Hard_Act: '月之女王·强化首领技',
  Unique_WorldTreeDragon_PaldiumShot: '枯星龙·帕鲁矿射击', Unique_WorldTreeDragon_PaldiumCannon: '枯星龙·帕鲁矿炮',
  Unique_WorldTreeDragon_Supernova: '枯星龙·超新星', Unique_WorldTreeDragon_HaloBeam: '枯星龙·光环射线',
  Unique_WorldTreeDragon_PaldiumRain: '枯星龙·帕鲁矿之雨', Unique_WorldTreeDragon_LaserGliding: '枯星龙·激光滑翔',
  Unique_WorldTreeDragon_HaloCutter: '枯星龙·光环切割', Unique_WorldTreeDragon_PaldiumExplosion: '枯星龙·帕鲁矿爆破',
  Unique_WorldTreeDragon_BigBang: '枯星龙·宇宙大爆炸',
});

// Upstream 1.0 Chinese data currently leaves this variant suffix in English.
// Keep launcher-facing names fully localized while preserving the exact Pal ID.
const palNameOverrides = Object.freeze({
  PlantSlime_Flower: '花叶泥泥',
});

// Unverified compatibility token retained from the launcher's older catalog.
// It is hidden by default and is not counted as current Palworld 1.0 content.
const legacyItems = Object.freeze([
  {
    id: 'PV_AssaultRifle_Default1', name: 'PV_ITEMS', nameZh: '突击步枪（旧版兼容）', category: 'Legacy', subcategory: '',
    rarity: 0, maxStack: 0, sortId: 0, legalInGame: false, source: 'legacy',
    image: 'https://cdn.paldb.cc/image/Others/InventoryItemIcon/Texture/T_itemicon_Weapon_AssaultRifle_Default1.webp',
  },
]);

async function fetchChecked(url) {
  const response = await fetch(url, { headers: { 'User-Agent': 'Palserver-Launcher catalog updater' } });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  return response;
}

async function fetchJson(url) {
  return (await fetchChecked(url)).json();
}

async function fetchSnapshot(url, expectedChecksum, label) {
  const bytes = Buffer.from(await (await fetchChecked(url)).arrayBuffer());
  const checksum = sha256(bytes);
  assertEqual(checksum, expectedChecksum, `${label} downloaded SHA-256`);
  try {
    return { data: JSON.parse(bytes.toString('utf8')), checksum };
  } catch (error) {
    throw new Error(`${label} is not valid JSON: ${error instanceof Error ? error.message : error}`);
  }
}

function sha256(value) { return createHash('sha256').update(value).digest('hex'); }

function assertEqual(actual, expected, label) {
  if (actual !== expected) throw new Error(`${label} is ${JSON.stringify(actual)}, expected ${JSON.stringify(expected)}`);
}

function assertUnique(records, label) {
  const seen = new Set();
  for (const record of records) {
    if (!record.id || seen.has(record.id)) throw new Error(`${label} contains an empty or duplicate ID: ${record.id}`);
    seen.add(record.id);
  }
}

function optionalProperty(target, name, value) {
  if (value !== undefined && value !== null && value !== '') target[name] = value;
}

function enumSuffix(value) { return String(value || '').split('::').pop() || ''; }
function compareOrdinal(left, right) { return left < right ? -1 : left > right ? 1 : 0; }
function digestIDs(ids) { return sha256([...ids].sort(compareOrdinal).join('\n')); }

function validChineseName(value) {
  const name = String(value || '').trim();
  return Boolean(name && !/^zh[- ]?hans? text$/i.test(name) && /\p{Script=Han}/u.test(name));
}

function normalizeItemCategory(record) {
  const value = record.type_a_display || enumSuffix(record.type_a);
  const categories = { Ammo: 'Ammo', Blueprint: 'Blueprint', Consumable: 'Consume', 'Pal Weapon': 'PalSummon', 'Sphere Modifier': 'CaptureItemModifier', SpecialWeapon: 'SpecialWeapon' };
  return categories[value] || value || 'Other';
}

function itemImage(record) {
  const name = String(record.icon || '').split('/').pop();
  return name ? `https://cdn.paldb.cc/image/Others/InventoryItemIcon/Texture/${name}` : '';
}

function palImage(record) {
  const name = String(record?.icon || '').split('/').pop();
  return name ? `https://cdn.paldb.cc/image/Pal/Texture/PalIcon/Normal/${name}` : '';
}

function verifiedBossRecord(record, characterByID) {
  const candidates = [characterByID.get(`BOSS_${record.id}`), characterByID.get(`Boss_${record.id}`)].filter(Boolean);
  if (candidates.length !== 1) throw new Error(`pal ${record.id} has ${candidates.length} matching boss tokens in the pinned character snapshot`);
  return candidates[0];
}

function flattenImplementedPassives(chineseSkillData) {
  const passives = [];
  for (const [category, records] of Object.entries(chineseSkillData.passive?.['已实装'] || {})) {
    for (const record of records || []) passives.push({ ...record, categoryZh: category });
  }
  return passives;
}

function skillFallbackName(id) {
  if (skillFallbackNames[id]) return skillFallbackNames[id];
  const phantasmal = id.match(/_(PhantasmalDeathray|PhantasmalEye|PhantasmalBolt|PhantasmalSphere)$/);
  if (phantasmal) return ({ PhantasmalDeathray: '幻灵死亡射线', PhantasmalEye: '幻灵之眼', PhantasmalBolt: '幻灵雷矢', PhantasmalSphere: '幻灵之球' })[phantasmal[1]];
  const barrier = id.match(/Unique_LegendDeer_BarrierRelease_(Normal|Grass|Water)$/);
  if (barrier) return `默世鹿·结界释放（${{ Normal: '普通', Grass: '草', Water: '水' }[barrier[1]]}）`;
  return `特殊技能（${id}）`;
}

function passiveFallbackName(id) {
  let match = id.match(/^Logging_up(\d)(?:_Otomo_only)?$/);
  if (match) return `伐木效率提升 ${match[1]}${id.endsWith('_Otomo_only') ? '（伙伴限定）' : ''}`;
  match = id.match(/^Mining_up(\d)(?:_Otomo_only)?$/);
  if (match) return `采矿效率提升 ${match[1]}${id.endsWith('_Otomo_only') ? '（伙伴限定）' : ''}`;
  match = id.match(/^Mute_(\d)$/); if (match) return `沉默效果 ${match[1]}`;
  match = id.match(/^StealExpert_(\d)$/); if (match) return `偷窃专家 ${match[1]}`;
  match = id.match(/^Support_(up|down)(\d)$/); if (match) return `支援${match[1] === 'up' ? '提升' : '降低'} ${match[2]}`;
  match = id.match(/^(MeleeAttack|ShotAttack)_(up|down)(\d)$/);
  if (match) return `${match[1] === 'MeleeAttack' ? '近战攻击' : '远程攻击'}${match[2] === 'up' ? '提升' : '降低'} ${match[3]}`;
  match = id.match(/^GiveA(Fire|Water|Leaf|Electricity|Ice|Earth|Dark|Dragon)$/);
  if (match) return `赋予${{ Fire: '火', Water: '水', Leaf: '草', Electricity: '雷', Ice: '冰', Earth: '地', Dark: '暗', Dragon: '龙' }[match[1]]}属性`;
  if (id === 'CraftSpeed*3') return '制作速度三倍';
  if (id === 'CraftSpeed*5') return '制作速度五倍';
  if (id === 'CollectItem') return '物品采集';
  if (id === 'CollectItem_CuteFox') return '玉藻狐物品采集';
  const simple = {
    BulletSpeed_L: '弹速降低', BulletSpeed_H: '弹速提升', RecoilIncrease: '后坐力增加', RecoilDecrease: '后坐力降低',
    AccuracyIncrease: '精准度提升', AccuracyDecrease: '精准度降低', AccuracySuperDecrease: '精准度大幅降低',
  };
  if (simple[id]) return simple[id];
  if (id.startsWith('Test')) return `测试被动（${id}）`;
  return `特殊被动（${id}）`;
}

function verifySnapshot(manifest, data) {
  const { officialItems, completeItems, officialPals, completeCharacters, completeSkills, completePassives, chineseItems, chinesePals, chineseActiveSkills, chinesePassives } = data;
  assertEqual(manifest.steamBuildId, expectedSnapshot.steamBuildId, 'server build ID');
  assertEqual(manifest.checksums?.items, expectedSnapshot.atlasItemChecksum, 'manifest Atlas item checksum');
  assertEqual(manifest.checksums?.pals, expectedSnapshot.atlasPalChecksum, 'manifest Atlas pal checksum');
  assertEqual(officialItems.length, expectedSnapshot.officialItemCount, 'official item count');
  assertEqual(completeItems.length, expectedSnapshot.completeItemCount, 'complete item count');
  assertEqual(officialPals.length, expectedSnapshot.palCount, 'pal count');
  assertEqual(completeCharacters.length, expectedSnapshot.completeCharacterCount, 'complete character count');
  assertEqual(completeSkills.length, expectedSnapshot.completeSkillCount, 'complete skill count');
  assertEqual(completePassives.length, expectedSnapshot.completePassiveCount, 'complete passive count');
  assertEqual(chineseItems.length, expectedSnapshot.completeItemCount, 'Chinese item count');
  assertEqual(chinesePals.length, expectedSnapshot.chinesePalCount, 'Chinese pal count');
  assertEqual(chineseActiveSkills.length, expectedSnapshot.skillCount, 'translated enabled skill count');
  assertEqual(chinesePassives.length, expectedSnapshot.passiveCount, 'translated implemented passive count');
  assertEqual(legacyItems.length, expectedSnapshot.legacyItemCount, 'legacy item count');
}

async function writeAtomically(serialized) {
  const temporaryFile = new URL(`game-catalog.json.${process.pid}.${Date.now()}.tmp`, dataDirectory);
  try {
    await writeFile(temporaryFile, serialized, 'utf8');
    await rename(temporaryFile, outputFile);
  } finally {
    await rm(temporaryFile, { force: true });
  }
}

async function main() {
  const buildBase = `${atlasBase}/${atlasBuildPath}`;
  const [manifest, atlasItemsSnapshot, atlasPalsSnapshot, completeItemsSnapshot, completeCharactersSnapshot, completeSkillsSnapshot, chineseItemsSnapshot, chinesePalsSnapshot, chineseSkillsSnapshot] = await Promise.all([
    fetchJson(`${buildBase}/manifest.json`),
    fetchSnapshot(`${buildBase}/items/index.json`, expectedSnapshot.atlasItemChecksum, 'Atlas item catalog'),
    fetchSnapshot(`${buildBase}/pals/index.json`, expectedSnapshot.atlasPalChecksum, 'Atlas pal catalog'),
    fetchSnapshot(fullItemCatalogUrl, expectedSnapshot.completeItemChecksum, 'complete item catalog'),
    fetchSnapshot(fullCharacterCatalogUrl, expectedSnapshot.completeCharacterChecksum, 'complete character catalog'),
    fetchSnapshot(fullSkillCatalogUrl, expectedSnapshot.completeSkillChecksum, 'complete skill catalog'),
    fetchSnapshot(chineseItemCatalogUrl, expectedSnapshot.chineseItemChecksum, 'Chinese item catalog'),
    fetchSnapshot(chinesePalCatalogUrl, expectedSnapshot.chinesePalChecksum, 'Chinese pal catalog'),
    fetchSnapshot(chineseSkillCatalogUrl, expectedSnapshot.chineseSkillChecksum, 'Chinese skill catalog'),
  ]);

  const officialItems = atlasItemsSnapshot.data.records;
  const officialPals = atlasPalsSnapshot.data.records;
  const completeItems = completeItemsSnapshot.data.items;
  const completeCharacters = completeCharactersSnapshot.data.pals;
  const completeSkills = completeSkillsSnapshot.data.skills;
  const completePassives = completeSkillsSnapshot.data.passives;
  const chineseItems = chineseItemsSnapshot.data;
  const chinesePals = chinesePalsSnapshot.data;
  const chineseActiveSkills = chineseSkillsSnapshot.data.active.filter((record) => !record['禁用']);
  const chinesePassives = flattenImplementedPassives(chineseSkillsSnapshot.data);

  assertUnique(officialItems, 'official item catalog');
  assertUnique(completeItems.map((entry) => ({ id: entry.asset })), 'complete item catalog');
  assertUnique(officialPals, 'official pal catalog');
  assertUnique(completeCharacters.map((entry) => ({ id: entry.asset })), 'complete character catalog');
  assertUnique(completeSkills.map((entry) => ({ id: entry.asset })), 'complete skill catalog');
  assertUnique(completePassives.map((entry) => ({ id: entry.asset })), 'complete passive catalog');
  assertUnique(chineseItems, 'Chinese item catalog');
  assertUnique(chinesePals, 'Chinese pal catalog');
  assertUnique(chineseActiveSkills, 'Chinese active skill catalog');
  assertUnique(chinesePassives, 'Chinese passive catalog');
  verifySnapshot(manifest, { officialItems, completeItems, officialPals, completeCharacters, completeSkills, completePassives, chineseItems, chinesePals, chineseActiveSkills, chinesePassives });

  const officialItemIDs = new Set(officialItems.map((entry) => entry.id));
  const completeItemIDs = new Set(completeItems.map((entry) => entry.asset));
  const chineseItemByID = new Map(chineseItems.map((entry) => [entry.id, entry]));
  const chineseNameByEnglish = new Map();
  for (const record of completeItems) {
    const translated = chineseItemByID.get(record.asset)?.['中文名'];
    if (record.name && validChineseName(translated) && !chineseNameByEnglish.has(record.name)) chineseNameByEnglish.set(record.name, translated.trim());
  }
  const missingOfficialItems = [...officialItemIDs].filter((id) => !completeItemIDs.has(id));
  if (missingOfficialItems.length) throw new Error(`complete item snapshot is missing ${missingOfficialItems.length} official IDs`);

  const items = {};
  const sortedItems = [...completeItems].sort((left, right) => {
    const availabilityDifference = Number(!officialItemIDs.has(left.asset)) - Number(!officialItemIDs.has(right.asset));
    if (availabilityDifference) return availabilityDifference;
    const categoryDifference = compareOrdinal(normalizeItemCategory(left), normalizeItemCategory(right));
    if (categoryDifference) return categoryDifference;
    return (left.sort_id ?? 0) - (right.sort_id ?? 0) || compareOrdinal(left.asset, right.asset);
  });

  for (const record of sortedItems) {
    const id = record.asset;
    const legalInGame = officialItemIDs.has(id);
    const category = normalizeItemCategory(record);
    const directTranslation = chineseItemByID.get(id)?.['中文名'];
    const nameZh = validChineseName(directTranslation)
      ? directTranslation.trim()
      : chineseNameByEnglish.get(record.name)
        || `开发/隐藏${categoryLabels[category] || '道具'}（${id}）`;
    const entry = {
      id, name: record.name || id, nameZh, category, subcategory: record.type_b_display || enumSuffix(record.type_b),
      rarity: record.rarity ?? 0, maxStack: record.max_stack ?? 0, sortId: record.sort_id ?? 0,
      legalInGame, source: legalInGame ? 'official' : 'developer',
    };
    optionalProperty(entry, 'image', itemImage(record));
    items[id] = entry;
  }
  for (const entry of legacyItems) items[entry.id] = { ...entry };

  const developerItemCount = completeItems.filter((entry) => !officialItemIDs.has(entry.asset)).length;
  assertEqual(developerItemCount, expectedSnapshot.developerItemCount, 'developer item count');
  assertEqual(Object.keys(items).length, expectedSnapshot.completeItemCount + expectedSnapshot.legacyItemCount, 'combined item count');

  const characterByID = new Map(completeCharacters.map((record) => [record.asset, record]));
  const chinesePalByID = new Map(chinesePals.map((record) => [record.id, record]));
  const pals = {};
  const bossPals = {};
  const sortedPals = [...officialPals].sort((left, right) => (left.paldexNumber ?? 0) - (right.paldexNumber ?? 0) || compareOrdinal(left.id, right.id));
  for (const record of sortedPals) {
    const translatedRecord = chinesePalByID.get(record.id);
    if (!translatedRecord || !validChineseName(translatedRecord['中文名'])) throw new Error(`pal ${record.id} is missing a Chinese name`);
    const bossRecord = verifiedBossRecord(record, characterByID);
    const iconRecord = characterByID.get(record.id) || bossRecord;
    const entry = {
      id: record.id, name: record.name || record.id, nameZh: palNameOverrides[record.id] || translatedRecord['中文名'].trim(),
      paldexNumber: record.paldexNumber ?? 0, elements: record.elements || [], source: 'official',
      learnset: (translatedRecord['技能学习'] || []).map((skill) => skill['技能ID']).filter(Boolean),
    };
    optionalProperty(entry, 'image', palImage(iconRecord));
    pals[record.id] = entry;
    const bossEntry = { ...entry, id: bossRecord.asset, name: `${entry.name} (Boss)`, nameZh: `${entry.nameZh}（首领）`, variant: 'boss' };
    optionalProperty(bossEntry, 'image', palImage(bossRecord));
    bossPals[bossEntry.id] = bossEntry;
  }

  const completeSkillByID = new Map(completeSkills.map((record) => [record.asset, record]));
  const skills = {};
  for (const translatedRecord of [...chineseActiveSkills].sort((left, right) => compareOrdinal(left.id, right.id))) {
    const record = completeSkillByID.get(translatedRecord.id);
    if (!record) throw new Error(`translated skill ${translatedRecord.id} is missing from the pinned skill snapshot`);
    const nameZh = validChineseName(translatedRecord['中文名']) ? translatedRecord['中文名'].trim() : skillFallbackName(record.asset);
    skills[record.asset] = {
      id: record.asset, name: record.name || record.asset, nameZh,
      category: record.element || translatedRecord['属性英文'] || 'Normal', categoryZh: translatedRecord['属性'] || '',
      power: record.display_power ?? record.power ?? 0, cooldown: record.cooldown ?? 0,
      description: record.description || '', descriptionZh: translatedRecord['描述'] || '', source: 'official',
    };
  }

  const completePassiveByID = new Map(completePassives.map((record) => [record.asset, record]));
  const passives = {};
  for (const translatedRecord of [...chinesePassives].sort((left, right) => compareOrdinal(left.id, right.id))) {
    const record = completePassiveByID.get(translatedRecord.id);
    if (!record) throw new Error(`translated passive ${translatedRecord.id} is missing from the pinned passive snapshot`);
    const nameZh = validChineseName(translatedRecord['中文名']) ? translatedRecord['中文名'].trim() : passiveFallbackName(record.asset);
    passives[record.asset] = {
      id: record.asset, name: record.name || record.asset, nameZh, category: translatedRecord.categoryZh || '其他效果',
      rank: record.rank ?? translatedRecord['等级'] ?? 0, description: record.description || '',
      descriptionZh: (translatedRecord['效果描述'] || []).join('；'), source: 'official',
    };
  }

  assertEqual(Object.keys(pals).length, expectedSnapshot.palCount, 'generated pal count');
  assertEqual(Object.keys(bossPals).length, expectedSnapshot.bossPalCount, 'verified boss pal count');
  assertEqual(Object.keys(skills).length, expectedSnapshot.skillCount, 'generated skill count');
  assertEqual(Object.keys(passives).length, expectedSnapshot.passiveCount, 'generated passive count');
  for (const [kind, records] of Object.entries({ item: items, pal: pals, bossPal: bossPals, skill: skills, passive: passives })) {
    const missing = Object.values(records).filter((entry) => !validChineseName(entry.nameZh));
    if (missing.length) throw new Error(`${kind} catalog has ${missing.length} entries without Chinese display names`);
  }

  const metadata = {
    schemaVersion: 4, steamBuildId: manifest.steamBuildId, generatedAt: manifest.generatedAt,
    source: `${buildBase}/manifest.json`, sourceRepository: atlasRepository, sourceLicense: 'MIT',
    itemSource: fullItemCatalogUrl, characterSource: fullCharacterCatalogUrl, skillSource: fullSkillCatalogUrl,
    completeCatalogRepository, completeCatalogCommit, completeCatalogLicense: 'MIT',
    chineseCatalogRepository, chineseCatalogCommit, chineseItemSource: chineseItemCatalogUrl,
    chinesePalSource: chinesePalCatalogUrl, chineseSkillSource: chineseSkillCatalogUrl,
    atlasItemChecksum: atlasItemsSnapshot.checksum, atlasPalChecksum: atlasPalsSnapshot.checksum,
    completeItemChecksum: completeItemsSnapshot.checksum, completeCharacterChecksum: completeCharactersSnapshot.checksum,
    completeSkillChecksum: completeSkillsSnapshot.checksum, chineseItemChecksum: chineseItemsSnapshot.checksum,
    chinesePalChecksum: chinesePalsSnapshot.checksum, chineseSkillChecksum: chineseSkillsSnapshot.checksum,
    officialItemCount: officialItems.length, developerItemCount, legacyItemCount: legacyItems.length,
    itemCount: Object.keys(items).length, palCount: Object.keys(pals).length, bossPalCount: Object.keys(bossPals).length,
    palEntryCount: Object.keys(pals).length + Object.keys(bossPals).length,
    skillCount: Object.keys(skills).length, passiveCount: Object.keys(passives).length,
    itemIdDigest: digestIDs(Object.keys(items)), palIdDigest: digestIDs(Object.keys(pals)),
    bossPalIdDigest: digestIDs(Object.keys(bossPals)), skillIdDigest: digestIDs(Object.keys(skills)),
    passiveIdDigest: digestIDs(Object.keys(passives)),
  };

  const serialized = `${JSON.stringify({ metadata, items, pals, bossPals, skills, passives }, null, 2)}\n`;
  await writeAtomically(serialized);
  console.log(`Updated ${metadata.itemCount} items, ${metadata.palEntryCount} Pal entries, ${metadata.skillCount} skills, and ${metadata.passiveCount} passives.`);
  console.log(`Catalog: ${fileURLToPath(outputFile)}`);
  console.log(`SHA-256: ${sha256(serialized)}`);
}

await main();

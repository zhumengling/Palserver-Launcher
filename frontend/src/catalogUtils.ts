export type CatalogEntry = {
  id: string;
  name?: string;
  nameZh?: string;
  image?: string;
  category?: string;
  categoryZh?: string;
  subcategory?: string;
  description?: string;
  descriptionZh?: string;
  rarity?: number;
  rank?: number;
  power?: number;
  cooldown?: number;
  source?: 'official' | 'developer' | 'legacy';
  variant?: 'boss';
  paldexNumber?: number;
  elements?: string[];
  learnset?: string[];
};

export const CATALOG_PAGE_SIZE = 200;

const categoryLabels: Record<string, string> = {
  Accessory: '饰品',
  Ammo: '弹药',
  Armor: '防具',
  Blueprint: '图纸',
  CaptureItemModifier: '捕获模块',
  Consume: '消耗品',
  Essential: '重要道具',
  Food: '食物',
  Glider: '滑翔装备',
  Legacy: '旧版兼容',
  Material: '材料',
  PalSummon: '帕鲁召唤',
  SpecialWeapon: '特殊武器',
  Weapon: '武器',
};

const elementLabels: Record<string, string> = {
  None: '无属性',
  Normal: '普通',
  Fire: '火',
  Water: '水',
  Leaf: '草',
  Electricity: '雷',
  Earth: '地',
  Ice: '冰',
  Dragon: '龙',
  Dark: '暗',
};

function localizedCategory(entry: CatalogEntry) {
  const translated = entry.categoryZh?.trim();
  if (translated && /\p{Script=Han}/u.test(translated)) return translated;
  return catalogCategoryLabel(translated || entry.category || '');
}

const sourceLabels: Record<NonNullable<CatalogEntry['source']>, string> = {
  official: '正式',
  developer: '开发/隐藏 开发 隐藏',
  legacy: '旧版兼容',
};

export function catalogCategoryLabel(category: string) {
  return categoryLabels[category] || elementLabels[category] || category;
}

export function catalogCategories(entries: CatalogEntry[]) {
  return [...new Set(entries.map((entry) => entry.category).filter((value): value is string => Boolean(value)))]
    .sort((left, right) => left.localeCompare(right));
}

export function filterCatalogEntries(entries: CatalogEntry[], options: {
  query?: string;
  category?: string;
  filterPrefix?: string;
  includeUnavailable?: boolean;
} = {}) {
  const query = options.query?.trim().toLowerCase() || '';
  return entries.filter((entry) => {
    if (options.filterPrefix && !entry.id.startsWith(options.filterPrefix)) return false;
    if (!options.includeUnavailable && entry.source && entry.source !== 'official') return false;
    if (options.category && entry.category !== options.category) return false;
    if (!query) return true;
    const searchText = [
      entry.id,
      entry.name,
      entry.nameZh,
      entry.category,
      entry.category ? catalogCategoryLabel(entry.category) : '',
      entry.categoryZh,
      entry.subcategory,
      entry.description,
      entry.descriptionZh,
      entry.source,
      entry.source ? sourceLabels[entry.source] : '',
      entry.variant,
      entry.variant === 'boss' ? '首领 Boss' : '',
      entry.paldexNumber,
      ...(entry.elements || []),
      ...(entry.elements || []).map((element) => elementLabels[element] || element),
    ].filter(Boolean).join(' ').toLowerCase();
    return searchText.includes(query);
  });
}

export function nextCatalogLimit(current: number, total: number) {
  return Math.min(total, current + CATALOG_PAGE_SIZE);
}

export function catalogImageSource(entry: CatalogEntry, failed: boolean) {
  return failed ? undefined : entry.image;
}

export function catalogEntryMeta(entry: CatalogEntry, kind: 'item' | 'pal' | 'skill' | 'passive') {
  if (kind === 'pal') {
    const values = [
      entry.paldexNumber ? `#${entry.paldexNumber}` : '',
      entry.variant === 'boss' ? '首领' : '',
      ...(entry.elements || []).map((element) => elementLabels[element] || element),
      entry.id,
    ];
    return values.filter(Boolean).join(' · ');
  }
  if (kind === 'skill') {
    return [
      localizedCategory(entry),
      entry.power !== undefined ? `威力 ${entry.power}` : '',
      entry.cooldown !== undefined ? `冷却 ${entry.cooldown} 秒` : '',
      entry.id,
    ].filter(Boolean).join(' · ');
  }
  if (kind === 'passive') {
    return [localizedCategory(entry), entry.rank !== undefined ? `等级 ${entry.rank}` : '', entry.id].filter(Boolean).join(' · ');
  }
  const category = entry.source === 'legacy' ? '旧版兼容' : entry.source === 'developer' ? '开发/隐藏' : catalogCategoryLabel(entry.category || '');
  return [category, entry.id].filter(Boolean).join(' · ');
}

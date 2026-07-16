import { useEffect, useMemo, useState } from 'react';
import { Box, Check, Search, X } from 'lucide-react';
import catalogData from './data/game-catalog.json';
import {
  CATALOG_PAGE_SIZE,
  catalogCategories,
  catalogCategoryLabel,
  catalogEntryMeta,
  catalogImageSource,
  filterCatalogEntries,
  nextCatalogLimit,
  type CatalogEntry,
} from './catalogUtils';

export type CatalogKind = 'item' | 'pal' | 'skill' | 'passive';

const items = Object.values(catalogData.items) as CatalogEntry[];
const pals = [...Object.values(catalogData.pals), ...Object.values(catalogData.bossPals)] as CatalogEntry[];
const skills = Object.values(catalogData.skills) as CatalogEntry[];
const passives = Object.values(catalogData.passives) as CatalogEntry[];
const emptySelection: string[] = [];

function CatalogIcon({ entry }: { entry: CatalogEntry }) {
  const [failed, setFailed] = useState(false);
  const image = catalogImageSource(entry, failed);
  return <span className="catalog-icon">{image ? <img src={image} alt="" loading="lazy" onError={() => setFailed(true)}/> : <Box size={16}/>}</span>;
}

function catalogSource(kind: CatalogKind) {
  if (kind === 'item') return items;
  if (kind === 'pal') return pals;
  if (kind === 'skill') return skills;
  return passives;
}

function catalogTitle(kind: CatalogKind) {
  if (kind === 'item') return '选择道具';
  if (kind === 'pal') return '选择帕鲁';
  if (kind === 'skill') return '选择主动技能';
  return '选择被动词条';
}

function catalogSummary(kind: CatalogKind, entryCount: number) {
  if (kind === 'item') return `正式 ${catalogData.metadata.officialItemCount} · 开发/隐藏 ${catalogData.metadata.developerItemCount} · 旧版 ${catalogData.metadata.legacyItemCount}`;
  if (kind === 'pal') return `标准 ${catalogData.metadata.palCount} · 已验证首领 ${catalogData.metadata.bossPalCount} · 共 ${entryCount} 项`;
  if (kind === 'skill') return `可用主动技能 ${catalogData.metadata.skillCount} 项`;
  return `已实装被动词条 ${catalogData.metadata.passiveCount} 项`;
}

export default function GameCatalog({
  kind,
  selected = '',
  selectedMany = emptySelection,
  multiSelect = false,
  maxSelected,
  recommendedIds = emptySelection,
  recommendedPalId = '',
  onClose,
  onSelect,
  filterPrefix = '',
  title,
}: {
  kind: CatalogKind;
  selected?: string;
  selectedMany?: string[];
  multiSelect?: boolean;
  maxSelected?: number;
  recommendedIds?: string[];
  recommendedPalId?: string;
  onClose: () => void;
  onSelect: (entry: CatalogEntry) => void;
  filterPrefix?: string;
  title?: string;
}) {
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('');
  const [includeUnavailable, setIncludeUnavailable] = useState(false);
  const [visibleLimit, setVisibleLimit] = useState(CATALOG_PAGE_SIZE);
  const source = catalogSource(kind);
  const recommended = useMemo(() => {
    if (recommendedIds.length) return new Set(recommendedIds);
    if (kind === 'skill' && recommendedPalId) {
      const pal = (catalogData.pals as Record<string, CatalogEntry>)[recommendedPalId] || (catalogData.bossPals as Record<string, CatalogEntry>)[recommendedPalId];
      return new Set(pal?.learnset || []);
    }
    return new Set<string>();
  }, [kind, recommendedIds, recommendedPalId]);
  const selectedSet = useMemo(() => new Set(selectedMany), [selectedMany]);
  const entries = useMemo(() => {
    const filtered = filterCatalogEntries(source, { filterPrefix, includeUnavailable: kind !== 'item' || includeUnavailable });
    if (!recommended.size) return filtered;
    return [...filtered].sort((left, right) => Number(recommended.has(right.id)) - Number(recommended.has(left.id)));
  }, [filterPrefix, includeUnavailable, kind, recommended, source]);
  const categories = useMemo(() => kind === 'pal' ? [] : catalogCategories(entries), [entries, kind]);
  const filtered = useMemo(() => filterCatalogEntries(entries, { query, category, includeUnavailable: kind !== 'item' || includeUnavailable }), [category, entries, includeUnavailable, kind, query]);
  const visible = filtered.slice(0, visibleLimit);

  useEffect(() => setVisibleLimit(CATALOG_PAGE_SIZE), [category, filterPrefix, includeUnavailable, kind, query]);
  useEffect(() => setCategory(''), [filterPrefix, includeUnavailable, kind]);
  useEffect(() => setIncludeUnavailable(false), [filterPrefix, kind]);

  const choose = (entry: CatalogEntry) => {
    onSelect(entry);
    if (!multiSelect) onClose();
  };

  return <div className="modal-backdrop catalog-backdrop"><div className="modal catalog-modal">
    <div className="modal-header"><div><h2>{title || catalogTitle(kind)}</h2><p>服务器 Build {catalogData.metadata.steamBuildId} · {catalogSummary(kind, entries.length)}</p></div><button onClick={onClose}><X size={18}/></button></div>
    <div className={`catalog-tools ${kind === 'item' ? 'with-unavailable' : categories.length ? 'with-filter' : 'single'}`}>
      <label className="catalog-search"><Search size={16}/><input autoFocus value={query} onChange={(event) => setQuery(event.target.value)} placeholder="输入中文名、英文名、内部 ID、分类或属性"/></label>
      {categories.length > 0 && <select className="catalog-filter" value={category} onChange={(event) => setCategory(event.target.value)}><option value="">全部分类</option>{categories.map((value) => <option key={value} value={value}>{catalogCategoryLabel(value)}</option>)}</select>}
      {kind === 'item' && <label className="catalog-unavailable"><input type="checkbox" checked={includeUnavailable} onChange={(event) => setIncludeUnavailable(event.target.checked)}/><span>开发/隐藏/旧版</span></label>}
    </div>
    <div className="catalog-summary">显示 {visible.length} / {filtered.length} 项{multiSelect ? ` · 已选择 ${selectedMany.length}${maxSelected ? ` / ${maxSelected}` : ''}` : ''}</div>
    {filtered.length ? <div className="catalog-list">{visible.map((entry) => {
      const isSelected = selected === entry.id || selectedSet.has(entry.id);
      const limitReached = Boolean(multiSelect && maxSelected && selectedMany.length >= maxSelected && !isSelected);
      return <button key={entry.id} disabled={limitReached} className={isSelected ? 'selected' : ''} onClick={() => choose(entry)}>
        <CatalogIcon entry={entry}/>
        <span><strong>{entry.nameZh || entry.name || entry.id}{recommended.has(entry.id) && <em className="catalog-recommended">推荐</em>}</strong><small>{catalogEntryMeta(entry, kind)}</small></span>
        {isSelected && <Check size={16}/>}
      </button>;
    })}{visible.length < filtered.length && <button className="catalog-more" onClick={() => setVisibleLimit((current) => nextCatalogLimit(current, filtered.length))}>显示更多（剩余 {filtered.length - visible.length} 项）</button>}</div> : <div className="empty catalog-empty"><Search size={22}/><span>没有匹配结果</span></div>}
    {multiSelect && <div className="catalog-actions"><span>点击条目可添加或移除</span><button className="primary" onClick={onClose}><Check size={15}/>完成</button></div>}
  </div></div>;
}

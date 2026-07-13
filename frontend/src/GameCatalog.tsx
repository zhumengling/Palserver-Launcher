import { useMemo, useState } from 'react';
import { Box, Check, Search, X } from 'lucide-react';
import itemsData from './data/items.json';
import palsData from './data/pals.json';

type CatalogEntry = { id: string; name?: string; image?: string };

const items = Object.values(itemsData) as CatalogEntry[];
const basePals = Object.values(palsData) as CatalogEntry[];
const pals = basePals.flatMap((pal) => [pal, { ...pal, id: `BOSS_${pal.id}`, name: `${pal.name || pal.id}（首领）` }]);

export default function GameCatalog({ kind, selected, onClose, onSelect, filterPrefix = '', title }: {
  kind: 'item' | 'pal';
  selected: string;
  onClose: () => void;
  onSelect: (id: string) => void;
  filterPrefix?: string;
  title?: string;
}) {
  const [query, setQuery] = useState('');
  const entries = useMemo(() => {
    const source = kind === 'item' ? items : pals;
    return filterPrefix ? source.filter((entry) => entry.id.startsWith(filterPrefix)) : source;
  }, [filterPrefix, kind]);
  const filtered = useMemo(() => {
    const value = query.trim().toLowerCase();
    if (!value) return entries.slice(0, 300);
    return entries.filter((entry) => `${entry.id} ${entry.name || ''}`.toLowerCase().includes(value)).slice(0, 300);
  }, [entries, query]);

  return <div className="modal-backdrop catalog-backdrop"><div className="modal catalog-modal">
    <div className="modal-header"><div><h2>{title || (kind === 'item' ? '选择道具' : '选择帕鲁')}</h2><p>Palworld 1.0 目录共 {entries.length} 项，可按名称或内部 ID 搜索</p></div><button onClick={onClose}><X size={18}/></button></div>
    <label className="catalog-search"><Search size={16}/><input autoFocus value={query} onChange={(event) => setQuery(event.target.value)} placeholder="输入名称或 ID"/></label>
    <div className="catalog-summary">显示 {filtered.length} 项{filtered.length === 300 && entries.length > 300 ? '，继续输入可缩小范围' : ''}</div>
    <div className="catalog-list">{filtered.map((entry) => <button key={entry.id} className={selected === entry.id ? 'selected' : ''} onClick={() => { onSelect(entry.id); onClose(); }}>
      <span className="catalog-icon">{entry.image ? <img src={entry.image} alt="" loading="lazy"/> : <Box size={16}/>}</span>
      <span><strong>{entry.name || entry.id}</strong><small>{entry.id}</small></span>
      {selected === entry.id && <Check size={16}/>}
    </button>)}</div>
    {!filtered.length && <div className="empty"><Search size={22}/><span>没有匹配结果</span></div>}
  </div></div>;
}

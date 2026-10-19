import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useQuery } from '@tanstack/react-query';
import { api, get, type CategoryRow, type TagRow } from '../lib/api';
import {
  invalidateAll,
  useCategories,
  useSettings,
  useTags,
  useQueryClientInvalidate,
} from '../lib/queries';
import { useUI } from '../lib/store';
import { Empty, Icon, Modal } from './ui';

function CategoryRowView({
  c,
  all,
  index,
  count,
}: {
  c: CategoryRow;
  all: CategoryRow[];
  index: number;
  count: number;
}) {
  const qc = useQueryClient();
  const { toast, set, setFilters, resetFilters } = useUI();
  const [name, setName] = useState(c.name);
  const [del, setDel] = useState(false);
  const [moveTo, setMoveTo] = useState('');
  const siblings = all.filter((x) => x.library === c.library);
  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
      invalidateAll(qc);
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    }
  };
  const swap = (dir: -1 | 1) => {
    const ids = siblings.map((s) => s.id);
    const j = index + dir;
    if (j < 0 || j >= ids.length) return;
    [ids[index], ids[j]] = [ids[j]!, ids[index]!];
    void run(() => api('POST', '/categories/reorder', { library: c.library, ids }));
  };
  return (
    <li className="mgr-row">
      <input
        type="color"
        aria-label={`Color for ${c.name}`}
        value={c.color}
        onChange={(e) =>
          void run(() => api('PATCH', `/categories/${c.id}`, { color: e.target.value }))
        }
        style={{ width: 28, height: 28, border: 0, background: 'none', padding: 0 }}
      />
      <input
        className="input"
        aria-label={`Name of ${c.name}`}
        value={name}
        onChange={(e) => setName(e.target.value)}
        onBlur={() =>
          name.trim() &&
          name !== c.name &&
          void run(() => api('PATCH', `/categories/${c.id}`, { name }))
        }
        style={{ maxWidth: 260 }}
      />
      <button
        className="btn ghost sm"
        onClick={() => {
          resetFilters();
          setFilters({ category: [c.id] });
          set({ route: 'library' });
        }}
      >
        {count} titles
      </button>
      <span style={{ marginLeft: 'auto', display: 'flex', gap: 4 }}>
        <button
          className="btn ghost sm icon"
          aria-label="Move up"
          disabled={index === 0}
          onClick={() => swap(-1)}
        >
          ↑
        </button>
        <button
          className="btn ghost sm icon"
          aria-label="Move down"
          disabled={index === siblings.length - 1}
          onClick={() => swap(1)}
        >
          ↓
        </button>
        <button
          className="btn ghost sm icon danger"
          aria-label={`Delete ${c.name}`}
          onClick={() =>
            count ? setDel(true) : void run(() => api('DELETE', `/categories/${c.id}`))
          }
        >
          <Icon name="trash" size={14} />
        </button>
      </span>
      {del && (
        <Modal title={`Delete “${c.name}”`} onClose={() => setDel(false)}>
          <div>This category has {count} titles. Move them to:</div>
          <select className="select" value={moveTo} onChange={(e) => setMoveTo(e.target.value)}>
            <option value="">Choose a category…</option>
            {siblings
              .filter((s) => s.id !== c.id)
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
          </select>
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
            <button className="btn" onClick={() => setDel(false)}>
              Cancel
            </button>
            <button
              className="btn danger"
              disabled={!moveTo}
              onClick={() => {
                setDel(false);
                void run(() => api('DELETE', `/categories/${c.id}?moveTo=${moveTo}`));
              }}
            >
              Move and delete
            </button>
          </div>
        </Modal>
      )}
    </li>
  );
}

export function CategoriesPage() {
  const cats = useCategories().data ?? [];
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  const [library, setLibrary] = useState<'reading' | 'watching'>('reading');
  const [name, setName] = useState('');
  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    try {
      await api('POST', '/categories', { library, name: name.trim() });
      setName('');
      invalidateAll(qc);
    } catch (err) {
      toast({ tone: 'error', message: (err as Error).message });
    }
  };
  return (
    <div className="page-pad" style={{ maxWidth: 820, display: 'grid', gap: 24 }}>
      <h1>Categories</h1>
      <form
        onSubmit={add}
        className="dcard"
        style={{
          display: 'flex',
          flexDirection: 'row',
          gap: 10,
          alignItems: 'center',
          flexWrap: 'wrap',
        }}
      >
        <div className="seg" role="group" aria-label="Library">
          {(['reading', 'watching'] as const).map((l) => (
            <button
              type="button"
              key={l}
              aria-pressed={library === l}
              onClick={() => setLibrary(l)}
            >
              {l === 'reading' ? 'Reading' : 'Watching'}
            </button>
          ))}
        </div>
        <input
          className="input"
          style={{ flex: 1, minWidth: 180 }}
          placeholder="New category name (Harem, Mystery, Sports… get a themed color)"
          aria-label="New category name"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <button className="btn primary" disabled={!name.trim()}>
          <Icon name="plus" size={14} /> Add category
        </button>
      </form>
      {(['reading', 'watching'] as const).map((lib) => {
        const list = cats.filter((c) => c.library === lib);
        return (
          <section key={lib} style={{ display: 'grid', gap: 8 }}>
            <h2 style={{ fontSize: 'var(--text-lg)' }}>
              {lib === 'reading' ? 'Reading' : 'Watching'}
            </h2>
            {list.length === 0 ? (
              <div className="muted">No categories yet.</div>
            ) : (
              <ul className="mgr-list">
                {list.map((c, i) => (
                  <CategoryRowView key={c.id} c={c} all={cats} index={i} count={c.count} />
                ))}
              </ul>
            )}
          </section>
        );
      })}
    </div>
  );
}

interface Suggestion {
  from: string;
  to: string;
  reason: string;
  distance: number;
}

export function TagsPage() {
  const tags = useTags().data ?? [];
  const settings = useSettings().data;
  const sugg = useQuery({
    queryKey: ['tag-suggestions'],
    queryFn: () => get<Suggestion[]>('/tags/suggestions'),
  });
  const qc = useQueryClient();
  const refresh = useQueryClientInvalidate();
  const { toast, setFilters, resetFilters, set } = useUI();
  const [q, setQ] = useState('');
  const [picked, setPicked] = useState<string[]>([]);
  const [mergeInto, setMergeInto] = useState('');
  const hidden = new Set((settings?.hiddenTags ?? []).map((t) => t.toLowerCase()));
  const run = async (fn: () => Promise<unknown>, msg?: string) => {
    try {
      await fn();
      invalidateAll(qc);
      void qc.invalidateQueries({ queryKey: ['tag-suggestions'] });
      refresh(['settings']);
      if (msg) toast({ tone: 'success', message: msg });
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    }
  };
  const toggleHidden = (name: string) => {
    if (!settings) return;
    const next = hidden.has(name.toLowerCase())
      ? settings.hiddenTags.filter((t) => t.toLowerCase() !== name.toLowerCase())
      : [...settings.hiddenTags, name];
    void run(() => api('PUT', '/settings', { ...settings, hiddenTags: next }));
  };
  const merge = (from: string[], to: string) =>
    run(() => api('POST', '/tags/merge', { from, to }), `Merged into “${to}”`);
  const shown = tags.filter((t) => t.name.toLowerCase().includes(q.toLowerCase()));

  return (
    <div className="page-pad" style={{ maxWidth: 980, display: 'grid', gap: 20 }}>
      <h1>Tags</h1>
      {(sugg.data?.length ?? 0) > 0 && (
        <section className="dcard">
          <header>
            <h3>Cleanup suggestions</h3>
            <span className="muted" style={{ fontSize: 'var(--text-sm)' }}>
              Nothing changes until you confirm. Merges teach future imports.
            </span>
          </header>
          <ul className="mgr-list">
            {sugg.data!.map((s) => (
              <li key={`${s.from}>${s.to}`} className="mgr-row">
                <span className="chip tag">{s.from}</span> → <span className="chip">{s.to}</span>
                <span className="muted" style={{ fontSize: 'var(--text-xs)' }}>
                  {s.reason}
                </span>
                <span style={{ marginLeft: 'auto', display: 'flex', gap: 6 }}>
                  <button className="btn sm primary" onClick={() => void merge([s.from], s.to)}>
                    Merge
                  </button>
                  <button
                    className="btn sm ghost"
                    onClick={() => void merge([s.to], s.from)}
                    title="Keep the other spelling"
                  >
                    Swap
                  </button>
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
      <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <input
          className="input"
          style={{ maxWidth: 280 }}
          placeholder="Filter tags…"
          aria-label="Filter tags"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        {picked.length > 1 && (
          <form
            style={{ display: 'flex', gap: 6 }}
            onSubmit={(e) => {
              e.preventDefault();
              if (mergeInto.trim()) {
                void merge(picked, mergeInto.trim());
                setPicked([]);
                setMergeInto('');
              }
            }}
          >
            <input
              className="input"
              placeholder={`Merge ${picked.length} tags into…`}
              aria-label="Merge target"
              list="tag-names"
              value={mergeInto}
              onChange={(e) => setMergeInto(e.target.value)}
            />
            <datalist id="tag-names">
              {picked.map((p) => (
                <option key={p} value={p} />
              ))}
            </datalist>
            <button className="btn primary" disabled={!mergeInto.trim()}>
              Merge
            </button>
          </form>
        )}
        <span className="muted" style={{ marginLeft: 'auto' }}>
          {tags.length} tags
        </span>
      </div>
      {tags.length === 0 ? (
        <Empty title="No tags yet" icon="tag">
          Tags appear as you add titles or import a spreadsheet.
        </Empty>
      ) : (
        <ul className="mgr-list">
          {shown.map((t) => (
            <TagRowView
              key={t.id}
              t={t}
              picked={picked.includes(t.name)}
              onPick={() =>
                setPicked((p) =>
                  p.includes(t.name) ? p.filter((x) => x !== t.name) : [...p, t.name],
                )
              }
              hidden={hidden.has(t.name.toLowerCase())}
              onHide={() => toggleHidden(t.name)}
              onRename={(n) =>
                void run(() => api('PATCH', `/tags/${encodeURIComponent(t.name)}`, { name: n }))
              }
              onColor={(c) =>
                void run(() => api('PATCH', `/tags/${encodeURIComponent(t.name)}`, { color: c }))
              }
              onOpen={() => {
                resetFilters();
                setFilters({ tags: [t.name] });
                set({ route: 'library' });
              }}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function TagRowView({
  t,
  picked,
  onPick,
  hidden,
  onHide,
  onRename,
  onColor,
  onOpen,
}: {
  t: TagRow;
  picked: boolean;
  onPick: () => void;
  hidden: boolean;
  onHide: () => void;
  onRename: (n: string) => void;
  onColor: (c: string) => void;
  onOpen: () => void;
}) {
  const [name, setName] = useState(t.name);
  return (
    <li className="mgr-row">
      <input type="checkbox" aria-label={`Select ${t.name}`} checked={picked} onChange={onPick} />
      <input
        type="color"
        aria-label={`Color for ${t.name}`}
        value={t.color ?? '#6b7280'}
        onChange={(e) => onColor(e.target.value)}
        style={{ width: 24, height: 24, border: 0, background: 'none', padding: 0 }}
      />
      <input
        className="input"
        style={{ maxWidth: 240 }}
        aria-label={`Rename ${t.name}`}
        value={name}
        onChange={(e) => setName(e.target.value)}
        onBlur={() => name.trim() && name !== t.name && onRename(name.trim())}
      />
      <button className="btn ghost sm" onClick={onOpen}>
        {t.count} titles
      </button>
      {t.aliases.length > 0 && (
        <span
          className="muted truncate"
          style={{ fontSize: 'var(--text-xs)', maxWidth: 220 }}
          title={t.aliases.join(', ')}
        >
          aka {t.aliases.join(', ')}
        </span>
      )}
      <label
        className="muted"
        style={{
          marginLeft: 'auto',
          display: 'flex',
          gap: 6,
          alignItems: 'center',
          fontSize: 'var(--text-sm)',
        }}
      >
        <input type="checkbox" checked={hidden} onChange={onHide} /> Hide
      </label>
    </li>
  );
}

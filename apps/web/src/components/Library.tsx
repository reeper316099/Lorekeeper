import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { Entry } from '@lorekeeper/shared';
import { api } from '../lib/api';
import { behindOf, isTyping, timeAgo, useLookup } from '../lib/helpers';
import { invalidateAll, useEntries, useMeta, useProgress, useUpdateEntry } from '../lib/queries';
import { activeFilterCount, useUI, type ColumnKey } from '../lib/store';
import { useQueryClient } from '@tanstack/react-query';
import { ProgressCell, ScoreSelect, StatusSelect, TagChips } from './cells';
import { FilterBar } from './FilterBar';
import { Cover, Empty, Icon, Popover } from './ui';

const COL_DEF: Record<ColumnKey, { label: string; width: string; sort?: string }> = {
  category: { label: 'Category', width: '120px', sort: 'category' },
  status: { label: 'Status', width: '130px', sort: 'status' },
  score: { label: 'Score', width: '130px', sort: 'score' },
  progress: { label: 'Progress', width: '190px' },
  tags: { label: 'Tags', width: 'minmax(110px, 1fr)' },
  leftOff: { label: 'Left off', width: 'minmax(110px, 1fr)' },
  updated: { label: 'Updated', width: '100px', sort: 'updatedAt' },
};

function TableView({
  items,
  hasMore,
  loadMore,
}: {
  items: Entry[];
  hasMore: boolean;
  loadMore: () => void;
}) {
  const { columns, density, selected, toggleSelected, cursor, set, filters, setFilters } = useUI();
  const look = useLookup();
  const ref = useRef<HTMLDivElement>(null);
  const rowH = density === 'compact' ? 44 : 60;
  const virt = useVirtualizer({
    count: items.length,
    getScrollElement: () => ref.current,
    estimateSize: () => rowH,
    overscan: 12,
  });
  const template = `28px minmax(220px, 2fr) ${columns.map((c) => COL_DEF[c].width).join(' ')}`;
  const vItems = virt.getVirtualItems();
  const last = vItems[vItems.length - 1];

  useEffect(() => {
    if (last && last.index >= items.length - 25 && hasMore) loadMore();
  }, [last, items.length, hasMore, loadMore]);

  useEffect(() => {
    const idx = items.findIndex((e) => e.id === cursor);
    if (idx >= 0) virt.scrollToIndex(idx, { align: 'auto' });
  }, [cursor, items, virt]);

  const allSel = items.length > 0 && items.every((e) => selected.includes(e.id));
  const sortBy = (key?: string) => {
    if (!key) return;
    setFilters(
      filters.sort === key
        ? { order: filters.order === 'asc' ? 'desc' : 'asc' }
        : { sort: key, order: 'asc' },
    );
  };

  return (
    <div
      className="scroller"
      ref={ref}
      role="table"
      aria-label="Library"
      aria-rowcount={items.length}
    >
      <div className="thead" style={{ gridTemplateColumns: template }} role="row">
        <input
          type="checkbox"
          aria-label="Select all"
          checked={allSel}
          onChange={() => set({ selected: allSel ? [] : items.map((e) => e.id) })}
        />
        <button onClick={() => sortBy('title')}>
          Title {filters.sort === 'title' && (filters.order === 'asc' ? '↑' : '↓')}
        </button>
        {columns.map((c) => (
          <button
            key={c}
            onClick={() => sortBy(COL_DEF[c].sort)}
            style={{ cursor: COL_DEF[c].sort ? 'pointer' : 'default' }}
          >
            {COL_DEF[c].label}{' '}
            {COL_DEF[c].sort === filters.sort && (filters.order === 'asc' ? '↑' : '↓')}
          </button>
        ))}
      </div>
      <div style={{ height: virt.getTotalSize(), position: 'relative' }}>
        {vItems.map((v) => {
          const e = items[v.index]!;
          const isSel = selected.includes(e.id);
          const color = look.color(e);
          return (
            <div
              key={e.id}
              role="row"
              aria-rowindex={v.index + 1}
              className={`trow ${isSel ? 'sel' : ''} ${cursor === e.id ? 'cursor' : ''}`}
              style={{
                gridTemplateColumns: template,
                height: rowH,
                top: 0,
                transform: `translateY(${v.start}px)`,
              }}
              onClick={() => set({ cursor: e.id })}
            >
              <input
                type="checkbox"
                aria-label={`Select ${e.title}`}
                checked={isSel}
                onChange={() => toggleSelected(e.id)}
              />
              <div className="cell-title" role="cell">
                {density === 'comfortable' && <Cover entry={e} color={color} size={30} />}
                <button
                  className="truncate"
                  onClick={() => set({ drawerId: e.id, cursor: e.id })}
                  title={e.title}
                >
                  {e.title}
                </button>
                {e.favorite && (
                  <span style={{ color: 'var(--warning)' }}>
                    <Icon name="star" size={13} />
                  </span>
                )}
              </div>
              {columns.map((c) => (
                <div key={c} role="cell" className="truncate" style={{ minWidth: 0 }}>
                  {c === 'category' && (
                    <span
                      className="chip"
                      style={{ background: `color-mix(in oklab, ${color} 30%, transparent)` }}
                    >
                      <span className="cat-dot" style={{ background: color }} />
                      {look.catName(e)}
                    </span>
                  )}
                  {c === 'status' && <StatusSelect entry={e} />}
                  {c === 'score' && <ScoreSelect entry={e} />}
                  {c === 'progress' && <ProgressCell entry={e} />}
                  {c === 'tags' && <TagChips tags={e.tags} />}
                  {c === 'leftOff' && (
                    <span className="muted truncate" title={e.notes.extra1 ?? ''}>
                      {e.notes.extra1 ?? ''}
                    </span>
                  )}
                  {c === 'updated' && <span className="muted">{timeAgo(e.updatedAt)}</span>}
                </div>
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function GridView({
  items,
  hasMore,
  loadMore,
}: {
  items: Entry[];
  hasMore: boolean;
  loadMore: () => void;
}) {
  const { selected, toggleSelected, set } = useUI();
  const look = useLookup();
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(1000);
  useLayoutEffect(() => {
    if (!ref.current) return;
    const ro = new ResizeObserver(([r]) => r && setWidth(r.contentRect.width));
    ro.observe(ref.current);
    return () => ro.disconnect();
  }, []);
  const gap = 16;
  const pad = 24;
  const cols = Math.max(2, Math.floor((width - pad * 2 + gap) / (190 + gap)));
  const cardH = 330;
  const rows = Math.ceil(items.length / cols);
  const virt = useVirtualizer({
    count: rows,
    getScrollElement: () => ref.current,
    estimateSize: () => cardH + gap,
    overscan: 3,
  });
  const vItems = virt.getVirtualItems();
  const last = vItems[vItems.length - 1];
  useEffect(() => {
    if (last && last.index >= rows - 3 && hasMore) loadMore();
  }, [last, rows, hasMore, loadMore]);

  return (
    <div className="scroller" ref={ref} style={{ padding: `${pad}px` }}>
      <div style={{ height: virt.getTotalSize(), position: 'relative' }}>
        {vItems.map((v) => (
          <div
            key={v.key}
            style={{
              position: 'absolute',
              top: 0,
              left: 0,
              right: 0,
              height: cardH,
              transform: `translateY(${v.start}px)`,
              display: 'grid',
              gridTemplateColumns: `repeat(${cols}, 1fr)`,
              gap,
            }}
          >
            {items.slice(v.index * cols, v.index * cols + cols).map((e) => {
              const color = look.color(e);
              const behind = behindOf(e);
              const pct =
                e.current.number && e.totalUnits
                  ? Math.min(100, (e.current.number / e.totalUnits) * 100)
                  : null;
              return (
                <article key={e.id} className={`card ${selected.includes(e.id) ? 'sel' : ''}`}>
                  <div
                    className="art"
                    style={{
                      background: `linear-gradient(145deg, ${color}, color-mix(in oklab, ${color} 50%, #000))`,
                    }}
                  >
                    <input
                      className="chk"
                      type="checkbox"
                      aria-label={`Select ${e.title}`}
                      checked={selected.includes(e.id)}
                      onChange={() => toggleSelected(e.id)}
                    />
                    {behind > 0 && <span className="chip behind">{behind} behind</span>}
                    <span style={{ zIndex: 1 }} aria-hidden="true">
                      {e.title
                        .replace(/[^\p{L}\p{N} ]/gu, '')
                        .split(/\s+/)
                        .slice(0, 2)
                        .map((w) => w[0])
                        .join('')
                        .toUpperCase()}
                    </span>
                  </div>
                  <div className="meta">
                    <button className="t" onClick={() => set({ drawerId: e.id })}>
                      {e.title}
                    </button>
                    <div
                      style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}
                    >
                      <StatusSelect entry={e} />
                      {e.score && <span className="chip">★ {e.score}</span>}
                    </div>
                    <ProgressCell entry={e} compact />
                    {pct !== null && (
                      <div className="bar">
                        <i style={{ width: `${pct}%` }} />
                      </div>
                    )}
                    {e.notes.extra1 && (
                      <div
                        className="muted truncate"
                        style={{ fontSize: 'var(--text-xs)' }}
                        title={e.notes.extra1}
                      >
                        ↳ {e.notes.extra1}
                      </div>
                    )}
                  </div>
                </article>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}

function BulkBar() {
  const { selected, set, toast } = useUI();
  const look = useLookup();
  const qc = useQueryClient();
  const [tag, setTag] = useState('');
  if (selected.length === 0) return null;
  const run = async (action: string, value?: string, msg?: string) => {
    try {
      const r = await api<{ affected: number }>('POST', '/entries/bulk', {
        ids: selected,
        action,
        value,
      });
      invalidateAll(qc);
      set({ selected: [] });
      toast({ tone: 'success', message: msg ?? `Updated ${r.affected} titles` });
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    }
  };
  return (
    <div className="bulkbar" role="toolbar" aria-label="Bulk actions">
      <strong>{selected.length} selected</strong>
      <select
        className="select"
        style={{ width: 'auto' }}
        aria-label="Set status"
        value=""
        onChange={(e) => e.target.value && void run('set_status', e.target.value)}
      >
        <option value="">Set status…</option>
        {(['planned', 'in_progress', 'completed', 'on_hold', 'dropped', 'repeating'] as const).map(
          (s) => (
            <option key={s} value={s}>
              {s.replace('_', ' ')}
            </option>
          ),
        )}
      </select>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (tag.trim()) {
            void run('add_tag', tag.trim());
            setTag('');
          }
        }}
        style={{ display: 'flex', gap: 4 }}
      >
        <input
          className="input"
          style={{ width: 130 }}
          placeholder="Add tag…"
          value={tag}
          onChange={(e) => setTag(e.target.value)}
          aria-label="Add tag to selection"
        />
      </form>
      <select
        className="select"
        style={{ width: 'auto' }}
        aria-label="Move to category"
        value=""
        onChange={(e) => e.target.value && void run('move', e.target.value)}
      >
        <option value="">Move to…</option>
        {look.cats.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name} ({c.library})
          </option>
        ))}
      </select>
      <button
        className="btn danger sm"
        onClick={() => {
          if (confirm(`Delete ${selected.length} titles? A backup is saved first.`))
            void run('delete', undefined, `Deleted ${selected.length} titles (backup saved)`);
        }}
      >
        <Icon name="trash" size={14} /> Delete
      </button>
      <button className="btn ghost sm" onClick={() => set({ selected: [] })}>
        Clear
      </button>
    </div>
  );
}

function ColumnChooser() {
  const { columns, set } = useUI();
  return (
    <Popover
      align="right"
      trigger={({ toggle }) => (
        <button className="btn sm" onClick={toggle}>
          <Icon name="rows" size={14} /> Columns
        </button>
      )}
    >
      {() => (
        <>
          {(Object.keys(COL_DEF) as ColumnKey[]).map((c) => (
            <label key={c} className="pop-item">
              <input
                type="checkbox"
                checked={columns.includes(c)}
                onChange={() =>
                  set({
                    columns: columns.includes(c)
                      ? columns.filter((x) => x !== c)
                      : (Object.keys(COL_DEF) as ColumnKey[]).filter(
                          (k) => k === c || columns.includes(k),
                        ),
                  })
                }
              />
              {COL_DEF[c].label}
            </label>
          ))}
        </>
      )}
    </Popover>
  );
}

export function Library() {
  const { filters, view, set, cursor } = useUI();
  const { items, total, isLoading, isFetching, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useEntries(filters);
  const meta = useMeta().data;
  const update = useUpdateEntry();
  const progress = useProgress();
  const loadMore = useMemo(
    () => () => {
      if (!isFetchingNextPage) void fetchNextPage();
    },
    [isFetchingNextPage, fetchNextPage],
  );

  // J/K move, E edit, +/- progress, X select
  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (isTyping(ev.target) || ev.metaKey || ev.ctrlKey || ev.altKey) return;
      const idx = items.findIndex((e) => e.id === cursor);
      const cur = items[idx];
      if (ev.key === 'j' || ev.key === 'ArrowDown') {
        set({ cursor: items[Math.min(items.length - 1, idx + 1)]?.id ?? null });
        ev.preventDefault();
      } else if (ev.key === 'k' || ev.key === 'ArrowUp') {
        set({ cursor: items[Math.max(0, idx - 1)]?.id ?? null });
        ev.preventDefault();
      } else if (ev.key === 'e' && cur) {
        set({ drawerId: cur.id });
        ev.preventDefault();
      } else if ((ev.key === '+' || ev.key === '=') && cur)
        progress.mutate({ id: cur.id, body: { delta: 1 } });
      else if (ev.key === '-' && cur) progress.mutate({ id: cur.id, body: { delta: -1 } });
      else if (ev.key === 'x' && cur) useUI.getState().toggleSelected(cur.id);
      else if (ev.key === 'f' && cur)
        update.mutate({ id: cur.id, patch: { favorite: !cur.favorite } });
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [items, cursor, set, progress, update]);

  const empty = !isLoading && total === 0;
  const hasFilters = activeFilterCount(filters) > 0 || filters.q.trim() !== '';

  return (
    <div className="page lib">
      <div style={{ display: 'flex', alignItems: 'center' }}>
        <div style={{ flex: 1 }}>
          <FilterBar total={total} loading={isLoading || (isFetching && !items.length)} />
        </div>
        <div style={{ display: 'flex', gap: 8, padding: '0 var(--space-5)' }}>
          {view === 'table' && <ColumnChooser />}
          <div className="seg" role="group" aria-label="View">
            <button
              aria-pressed={view === 'table'}
              onClick={() => set({ view: 'table' })}
              aria-label="Table view"
            >
              <Icon name="rows" size={14} />
            </button>
            <button
              aria-pressed={view === 'grid'}
              onClick={() => set({ view: 'grid' })}
              aria-label="Card view"
            >
              <Icon name="grid" size={14} />
            </button>
          </div>
        </div>
      </div>
      {isLoading && (
        <div style={{ padding: 'var(--space-5)', display: 'grid', gap: 10 }}>
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="skeleton" style={{ height: 48 }} />
          ))}
        </div>
      )}
      {empty && (
        <Empty
          title={
            hasFilters
              ? 'No matches'
              : meta?.entries === 0
                ? 'Your library is empty'
                : 'Nothing here'
          }
          icon={hasFilters ? 'search' : 'import'}
        >
          {hasFilters ? (
            <>
              Try loosening the filters or search.
              <br />
              <button
                className="btn"
                style={{ marginTop: 12 }}
                onClick={() => useUI.getState().resetFilters()}
              >
                Clear filters
              </button>
            </>
          ) : (
            <>
              Import your spreadsheets or add a first title.
              <br />
              <span style={{ display: 'inline-flex', gap: 8, marginTop: 12 }}>
                <button className="btn primary" onClick={() => set({ route: 'import' })}>
                  <Icon name="import" size={14} /> Import spreadsheet
                </button>
                <button className="btn" onClick={() => set({ quickAdd: true })}>
                  <Icon name="plus" size={14} /> Add title
                </button>
              </span>
            </>
          )}
        </Empty>
      )}
      {!isLoading &&
        total > 0 &&
        (view === 'table' ? (
          <TableView items={items} hasMore={!!hasNextPage} loadMore={loadMore} />
        ) : (
          <GridView items={items} hasMore={!!hasNextPage} loadMore={loadMore} />
        ))}
      <BulkBar />
    </div>
  );
}

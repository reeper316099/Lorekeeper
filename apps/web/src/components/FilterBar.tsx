import { useState } from 'react';
import { api } from '../lib/api';
import { activeFilterCount, defaultFilters, useUI, type Filters } from '../lib/store';
import { useSettings, useTags, useCategories, useQueryClientInvalidate } from '../lib/queries';
import { GENERIC_STATUS, Icon, MultiSelect, Popover, STATUS_ORDER } from './ui';

const SORTS: [string, string][] = [
  ['title', 'Title'],
  ['updatedAt', 'Recently updated'],
  ['createdAt', 'Date added'],
  ['score', 'Score'],
  ['status', 'Status'],
  ['category', 'Category'],
];

function SavedViews() {
  const { filters, setFilters, toast } = useUI();
  const settings = useSettings().data;
  const refresh = useQueryClientInvalidate();
  const [name, setName] = useState('');
  const views = settings?.savedViews ?? [];

  const save = async (close: () => void) => {
    if (!settings || !name.trim()) return;
    const query = Object.fromEntries(
      Object.entries(filters).map(([k, v]) => [k, JSON.stringify(v)]),
    );
    await api('PUT', '/settings', {
      ...settings,
      savedViews: [...views, { id: crypto.randomUUID(), name: name.trim(), query }],
    });
    refresh(['settings']);
    setName('');
    close();
    toast({ tone: 'success', message: `Saved view “${name.trim()}”` });
  };
  const remove = async (id: string) => {
    if (!settings) return;
    await api('PUT', '/settings', { ...settings, savedViews: views.filter((v) => v.id !== id) });
    refresh(['settings']);
  };
  const apply = (q: Record<string, string>, close: () => void) => {
    const next: Record<string, unknown> = { ...defaultFilters };
    for (const [k, v] of Object.entries(q)) {
      try {
        next[k] = JSON.parse(v);
      } catch {
        /* ignore bad value */
      }
    }
    setFilters(next as Partial<Filters>);
    close();
  };

  return (
    <Popover
      trigger={({ toggle }) => (
        <button className="btn sm" onClick={toggle}>
          <Icon name="star" size={14} /> Views
        </button>
      )}
    >
      {(close) => (
        <div style={{ minWidth: 240 }}>
          {views.length === 0 && (
            <div className="muted" style={{ padding: 8 }}>
              No saved views yet
            </div>
          )}
          {views.map((v) => (
            <div key={v.id} className="pop-item">
              <button
                className="pop-item"
                style={{ padding: 0 }}
                onClick={() => apply(v.query, close)}
              >
                {v.name}
              </button>
              <button
                className="btn ghost sm icon"
                aria-label={`Delete view ${v.name}`}
                onClick={() => void remove(v.id)}
              >
                <Icon name="close" size={12} />
              </button>
            </div>
          ))}
          <form
            style={{
              display: 'flex',
              gap: 6,
              padding: 6,
              borderTop: '1px solid var(--border)',
              marginTop: 6,
            }}
            onSubmit={(e) => {
              e.preventDefault();
              void save(close);
            }}
          >
            <input
              className="input"
              placeholder="Save current filters as…"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <button className="btn sm primary" disabled={!name.trim()}>
              Save
            </button>
          </form>
        </div>
      )}
    </Popover>
  );
}

export function FilterBar({ total, loading }: { total: number; loading: boolean }) {
  const { filters: f, setFilters, resetFilters } = useUI();
  const cats = useCategories().data ?? [];
  const tags = useTags().data ?? [];
  const n = activeFilterCount(f);
  const catOptions = cats
    .filter((c) => f.library === 'all' || c.library === f.library)
    .map((c) => ({
      value: c.id,
      label:
        f.library === 'all' ? `${c.name} · ${c.library === 'reading' ? 'Read' : 'Watch'}` : c.name,
      color: c.color,
      count: c.count,
    }));
  const tagOptions = tags.map((t) => ({ value: t.name, label: t.name, count: t.count }));

  return (
    <div className="filterbar" role="search" aria-label="Library filters">
      <div className="seg" role="group" aria-label="Library">
        {(['all', 'reading', 'watching'] as const).map((l) => (
          <button
            key={l}
            aria-pressed={f.library === l}
            onClick={() => setFilters({ library: l, category: [] })}
          >
            {l === 'all' ? 'All' : l === 'reading' ? 'Reading' : 'Watching'}
          </button>
        ))}
      </div>
      <MultiSelect
        label="Status"
        selected={f.status}
        onChange={(status) => setFilters({ status })}
        options={STATUS_ORDER.map((s) => ({ value: s, label: GENERIC_STATUS[s] }))}
      />
      <MultiSelect
        label="Category"
        selected={f.category}
        onChange={(category) => setFilters({ category })}
        options={catOptions}
      />
      <MultiSelect
        label="Tags"
        selected={f.tags}
        onChange={(t) => setFilters({ tags: t })}
        options={tagOptions}
        searchable
      />
      {f.tags.length > 1 && (
        <div className="seg" role="group" aria-label="Tag match mode">
          <button aria-pressed={f.tagMode === 'all'} onClick={() => setFilters({ tagMode: 'all' })}>
            All
          </button>
          <button aria-pressed={f.tagMode === 'any'} onClick={() => setFilters({ tagMode: 'any' })}>
            Any
          </button>
        </div>
      )}
      <MultiSelect
        label="Exclude"
        selected={f.excludeTags}
        onChange={(t) => setFilters({ excludeTags: t })}
        options={tagOptions}
        searchable
      />
      <Popover
        trigger={({ toggle }) => (
          <button className="btn sm" onClick={toggle}>
            <Icon name="filter" size={14} /> More
          </button>
        )}
      >
        {() => (
          <div style={{ display: 'grid', gap: 10, minWidth: 260, padding: 6 }}>
            <div className="row2">
              <div className="field">
                <label htmlFor="minS">Min score</label>
                <input
                  id="minS"
                  className="input"
                  type="number"
                  min={1}
                  max={10}
                  value={f.minScore}
                  onChange={(e) => setFilters({ minScore: e.target.value })}
                />
              </div>
              <div className="field">
                <label htmlFor="maxS">Max score</label>
                <input
                  id="maxS"
                  className="input"
                  type="number"
                  min={1}
                  max={10}
                  value={f.maxScore}
                  onChange={(e) => setFilters({ maxScore: e.target.value })}
                />
              </div>
            </div>
            <div className="field">
              <label htmlFor="src">Source / platform</label>
              <input
                id="src"
                className="input"
                placeholder="e.g. YouTube"
                value={f.source}
                onChange={(e) => setFilters({ source: e.target.value })}
              />
            </div>
            <div className="field">
              <label htmlFor="mt">Media type</label>
              <select
                id="mt"
                className="select"
                value={f.mediaType[0] ?? ''}
                onChange={(e) => setFilters({ mediaType: e.target.value ? [e.target.value] : [] })}
              >
                <option value="">Any</option>
                {[
                  'manga',
                  'manhwa',
                  'manhua',
                  'anime',
                  'donghua',
                  'tv',
                  'cartoon',
                  'kdrama',
                  'documentary',
                  'unknown',
                ].map((m) => (
                  <option key={m}>{m}</option>
                ))}
              </select>
            </div>
            {(
              [
                ['behind', 'Behind the recap'],
                ['hasNotes', 'Has notes'],
                ['favorite', 'Favorites only'],
              ] as const
            ).map(([k, l]) => (
              <label key={k} className="pop-item">
                <input
                  type="checkbox"
                  checked={f[k]}
                  onChange={(e) => setFilters({ [k]: e.target.checked })}
                />{' '}
                {l}
              </label>
            ))}
          </div>
        )}
      </Popover>
      <SavedViews />
      {n > 0 && (
        <button className="btn ghost sm" onClick={resetFilters}>
          Clear ({n})
        </button>
      )}
      <div className="spacer" />
      <select
        className="select"
        style={{ width: 'auto' }}
        aria-label="Sort by"
        value={f.sort}
        onChange={(e) => setFilters({ sort: e.target.value })}
      >
        {SORTS.map(([v, l]) => (
          <option key={v} value={v}>
            {l}
          </option>
        ))}
      </select>
      <button
        className="btn sm icon"
        aria-label={f.order === 'asc' ? 'Ascending' : 'Descending'}
        onClick={() => setFilters({ order: f.order === 'asc' ? 'desc' : 'asc' })}
      >
        {f.order === 'asc' ? '↑' : '↓'}
      </button>
      <span className="count" aria-live="polite">
        {loading ? 'Loading…' : `${total.toLocaleString()} title${total === 1 ? '' : 's'}`}
      </span>
    </div>
  );
}

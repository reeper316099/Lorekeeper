import { Fragment, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Entry } from '@lorekeeper/shared';
import { api, get } from '../lib/api';
import { useLookup } from '../lib/helpers';
import { useFocusTrap } from '../lib/focus';
import { useUI, type Route } from '../lib/store';
import { Icon } from './ui';

interface SearchResult {
  entries: { entry: Entry; score: number }[];
  tags: { id: string; name: string }[];
  categories: { id: string; name: string; library: string }[];
}
interface Item {
  id: string;
  group: string;
  label: string;
  hint?: string;
  icon: string;
  run: () => void;
}

export function Palette() {
  const { palette, set, setFilters, resetFilters, theme, toast } = useUI();
  const [q, setQ] = useState('');
  const [idx, setIdx] = useState(0);
  const ref = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  useFocusTrap(dialogRef, palette);
  const look = useLookup();
  useEffect(() => {
    if (palette) {
      setQ('');
      setIdx(0);
    }
  }, [palette]);

  const search = useQuery({
    queryKey: ['palette', q],
    enabled: palette && q.trim().length > 0,
    queryFn: () => get<SearchResult>(`/search?q=${encodeURIComponent(q.trim())}&limit=8`),
    placeholderData: (p) => p,
  });

  const items = useMemo<Item[]>(() => {
    const close = () => set({ palette: false });
    const go = (route: Route) => () => {
      set({ route });
      close();
    };
    const actions: Item[] = [
      {
        id: 'a-add',
        group: 'Actions',
        label: 'Add a title',
        hint: 'N',
        icon: 'plus',
        run: () => {
          close();
          set({ quickAdd: true });
        },
      },
      { id: 'a-dash', group: 'Go to', label: 'Dashboard', icon: 'dash', run: go('dashboard') },
      { id: 'a-lib', group: 'Go to', label: 'Library', icon: 'library', run: go('library') },
      { id: 'a-cat', group: 'Go to', label: 'Categories', icon: 'folder', run: go('categories') },
      { id: 'a-tags', group: 'Go to', label: 'Tags', icon: 'tag', run: go('tags') },
      {
        id: 'a-imp',
        group: 'Go to',
        label: 'Import / Export / Backups',
        icon: 'import',
        run: go('import'),
      },
      { id: 'a-set', group: 'Go to', label: 'Settings', icon: 'settings', run: go('settings') },
      {
        id: 'a-theme',
        group: 'Actions',
        label: `Switch to ${theme === 'light' ? 'dark' : 'light'} theme`,
        icon: theme === 'light' ? 'moon' : 'sun',
        run: () => {
          set({ theme: theme === 'light' ? 'dark' : 'light' });
          close();
        },
      },
      {
        id: 'a-behind',
        group: 'Actions',
        label: 'Show titles behind the recap',
        icon: 'filter',
        run: () => {
          resetFilters();
          setFilters({ behind: true });
          set({ route: 'library' });
          close();
        },
      },
      {
        id: 'a-backup',
        group: 'Actions',
        label: 'Back up now',
        icon: 'import',
        run: () => {
          close();
          void api('POST', '/backups').then(() =>
            toast({ tone: 'success', message: 'Backup created' }),
          );
        },
      },
      {
        id: 'a-help',
        group: 'Actions',
        label: 'Keyboard shortcuts',
        hint: '?',
        icon: 'star',
        run: () => {
          close();
          set({ help: true });
        },
      },
    ];
    const lc = q.trim().toLowerCase();
    const filtered = lc ? actions.filter((a) => a.label.toLowerCase().includes(lc)) : actions;
    const found: Item[] = [];
    if (lc && search.data) {
      for (const { entry } of search.data.entries)
        found.push({
          id: `e-${entry.id}`,
          group: 'Titles',
          label: entry.title,
          hint: look.catName(entry),
          icon: 'book',
          run: () => {
            close();
            set({ drawerId: entry.id });
          },
        });
      for (const t of search.data.tags)
        found.push({
          id: `t-${t.id}`,
          group: 'Tags',
          label: `Tag: ${t.name}`,
          icon: 'tag',
          run: () => {
            close();
            resetFilters();
            setFilters({ tags: [t.name] });
            set({ route: 'library' });
          },
        });
      for (const c of search.data.categories)
        found.push({
          id: `c-${c.id}`,
          group: 'Categories',
          label: `Category: ${c.name}`,
          hint: c.library,
          icon: 'folder',
          run: () => {
            close();
            resetFilters();
            setFilters({ category: [c.id] });
            set({ route: 'library' });
          },
        });
    }
    return [...found, ...filtered];
  }, [q, search.data, theme, set, setFilters, resetFilters, look, toast]);

  useEffect(() => {
    setIdx(0);
  }, [q]);
  useEffect(() => {
    if (palette) setTimeout(() => ref.current?.focus(), 0);
  }, [palette]);
  useEffect(() => {
    if (!palette) return;
    const key = (e: KeyboardEvent) => e.key === 'Escape' && set({ palette: false });
    document.addEventListener('keydown', key);
    return () => document.removeEventListener('keydown', key);
  }, [palette, set]);
  if (!palette) return null;

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setIdx((i) => Math.min(items.length - 1, i + 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setIdx((i) => Math.max(0, i - 1));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      items[idx]?.run();
    } else if (e.key === 'Escape') set({ palette: false });
  };
  let lastGroup = '';
  return (
    <>
      <div className="scrim" onClick={() => set({ palette: false })} />
      <div
        ref={dialogRef}
        className="modal palette"
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        tabIndex={-1}
        onKeyDown={onKey}
      >
        <div className="search" style={{ maxWidth: 'none' }}>
          <Icon name="search" size={16} />
          <input
            ref={ref}
            role="combobox"
            aria-expanded="true"
            aria-controls="palette-list"
            aria-label="Search titles and commands"
            placeholder="Search titles, tags, or type a command…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
        <ul id="palette-list" role="listbox" className="palette-list">
          {items.map((it, i) => {
            const head =
              it.group !== lastGroup
                ? ((lastGroup = it.group),
                  (
                    <li key={`g-${it.group}-${i}`} className="palette-group" role="presentation">
                      {it.group}
                    </li>
                  ))
                : null;
            return (
              <Fragment key={it.id}>
                {head}
                <li
                  role="option"
                  aria-selected={i === idx}
                  className={`palette-item ${i === idx ? 'active' : ''}`}
                  onMouseEnter={() => setIdx(i)}
                  onClick={it.run}
                >
                  <Icon name={it.icon} size={15} /> <span className="truncate">{it.label}</span>
                  {it.hint && (
                    <span
                      className="muted"
                      style={{ marginLeft: 'auto', fontSize: 'var(--text-xs)' }}
                    >
                      {it.hint}
                    </span>
                  )}
                </li>
              </Fragment>
            );
          })}
          {items.length === 0 && (
            <li className="muted" style={{ padding: 12 }}>
              No results
            </li>
          )}
        </ul>
      </div>
    </>
  );
}

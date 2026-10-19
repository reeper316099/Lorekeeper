import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';

export type Route =
  'dashboard' | 'library' | 'categories' | 'tags' | 'import' | 'anilist' | 'settings';

export interface Filters {
  q: string;
  library: 'all' | 'reading' | 'watching';
  category: string[];
  status: string[];
  tags: string[];
  tagMode: 'all' | 'any';
  excludeTags: string[];
  mediaType: string[];
  minScore: string;
  maxScore: string;
  source: string;
  behind: boolean;
  hasNotes: boolean;
  favorite: boolean;
  sort: string;
  order: 'asc' | 'desc';
}

export const defaultFilters: Filters = {
  q: '',
  library: 'all',
  category: [],
  status: [],
  tags: [],
  tagMode: 'all',
  excludeTags: [],
  mediaType: [],
  minScore: '',
  maxScore: '',
  source: '',
  behind: false,
  hasNotes: false,
  favorite: false,
  sort: 'title',
  order: 'asc',
};

/** localStorage can be blocked or throw; fall back to memory. */
const safeStorage = createJSONStorage(() => ({
  getItem: (k: string) => {
    try {
      return localStorage.getItem(k);
    } catch {
      return null;
    }
  },
  setItem: (k: string, v: string) => {
    try {
      localStorage.setItem(k, v);
    } catch {
      /* ignore */
    }
  },
  removeItem: (k: string) => {
    try {
      localStorage.removeItem(k);
    } catch {
      /* ignore */
    }
  },
}));

export const ALL_COLUMNS = [
  'category',
  'status',
  'score',
  'progress',
  'tags',
  'leftOff',
  'updated',
] as const;
export type ColumnKey = (typeof ALL_COLUMNS)[number];

export interface Toast {
  id: number;
  message: string;
  tone?: 'info' | 'error' | 'success';
  action?: { label: string; run: () => void };
}

interface UI {
  theme: 'dark' | 'light' | 'system';
  accent: string;
  density: 'comfortable' | 'compact';
  sidebarCollapsed: boolean;
  route: Route;
  view: 'table' | 'grid';
  columns: ColumnKey[];
  filters: Filters;
  selected: string[];
  cursor: string | null;
  drawerId: string | null;
  quickAdd: boolean;
  help: boolean;
  palette: boolean;
  toasts: Toast[];
  set: (patch: Partial<UI>) => void;
  setFilters: (patch: Partial<Filters>) => void;
  resetFilters: () => void;
  toggleSelected: (id: string) => void;
  toast: (t: Omit<Toast, 'id'>) => void;
  dismissToast: (id: number) => void;
}

let toastId = 1;

export const useUI = create<UI>()(
  persist(
    (set) => ({
      theme: 'dark',
      accent: '#f2603c',
      density: 'comfortable',
      sidebarCollapsed: false,
      route: 'library',
      view: 'table',
      columns: ['category', 'status', 'score', 'progress', 'tags', 'leftOff'],
      filters: defaultFilters,
      selected: [],
      cursor: null,
      drawerId: null,
      quickAdd: false,
      help: false,
      palette: false,
      toasts: [],
      set: (patch) => set(patch),
      setFilters: (patch) => set((s) => ({ filters: { ...s.filters, ...patch }, selected: [] })),
      resetFilters: () => set({ filters: defaultFilters, selected: [] }),
      toggleSelected: (id) =>
        set((s) => ({
          selected: s.selected.includes(id)
            ? s.selected.filter((x) => x !== id)
            : [...s.selected, id],
        })),
      toast: (t) => {
        const id = toastId++;
        set((s) => ({ toasts: [...s.toasts, { ...t, id }] }));
        setTimeout(
          () => set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) })),
          t.action ? 7000 : 4000,
        );
      },
      dismissToast: (id) => set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) })),
    }),
    {
      name: 'lorekeeper-ui',
      storage: safeStorage,
      partialize: (s) => ({
        theme: s.theme,
        accent: s.accent,
        density: s.density,
        sidebarCollapsed: s.sidebarCollapsed,
        route: s.route,
        view: s.view,
        columns: s.columns,
        filters: s.filters,
      }),
    },
  ),
);

/** Filters → API query string. Only non-default values are sent. */
export function filtersToParams(f: Filters): URLSearchParams {
  const p = new URLSearchParams();
  if (f.q.trim()) p.set('q', f.q.trim());
  if (f.library !== 'all') p.set('library', f.library);
  if (f.category.length) p.set('category', f.category.join(','));
  if (f.status.length) p.set('status', f.status.join(','));
  if (f.tags.length) p.set('tags', f.tags.join(','));
  if (f.tags.length) p.set('tagMode', f.tagMode);
  if (f.excludeTags.length) p.set('excludeTags', f.excludeTags.join(','));
  if (f.mediaType.length) p.set('mediaType', f.mediaType.join(','));
  if (f.minScore) p.set('minScore', f.minScore);
  if (f.maxScore) p.set('maxScore', f.maxScore);
  if (f.source) p.set('source', f.source);
  if (f.behind) p.set('behind', 'true');
  if (f.hasNotes) p.set('hasNotes', 'true');
  if (f.favorite) p.set('favorite', 'true');
  // With a search term, relevance ordering applies unless the user picked a sort explicitly.
  if (f.sort !== 'title' || !f.q.trim()) p.set('sort', f.sort);
  p.set('order', f.order);
  return p;
}

export function activeFilterCount(f: Filters): number {
  return (
    (f.library !== 'all' ? 1 : 0) +
    f.category.length +
    f.status.length +
    f.tags.length +
    f.excludeTags.length +
    f.mediaType.length +
    (f.minScore ? 1 : 0) +
    (f.maxScore ? 1 : 0) +
    (f.source ? 1 : 0) +
    (f.behind ? 1 : 0) +
    (f.hasNotes ? 1 : 0) +
    (f.favorite ? 1 : 0)
  );
}

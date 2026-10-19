import MiniSearch from 'minisearch';
import { behindBy, type Entry } from '@lorekeeper/shared';
import { z } from 'zod';
import type { Store } from './storage/store.js';

type Doc = { id: string; title: string; alt: string; tags: string; notes: string };

const docOf = (e: Entry): Doc => ({
  id: e.id,
  title: e.title,
  alt: e.altTitles.join(' '),
  tags: e.tags.join(' '),
  notes: [e.notes.general, e.notes.extra1, e.notes.extra2].filter(Boolean).join(' '),
});

export class SearchIndex {
  private ms?: MiniSearch<Doc>;
  private builtAt = -1;
  /** The entry object each document was built from. Entries are replaced, never mutated, so identity tells us what changed. */
  private seen = new Map<string, Entry>();

  constructor(private store: Store) {}

  private ensure(): MiniSearch<Doc> {
    if (this.ms && this.builtAt === this.store.version) return this.ms;
    const all = this.store.all();
    if (!this.ms) {
      this.ms = new MiniSearch<Doc>({
        fields: ['title', 'alt', 'tags', 'notes'],
        searchOptions: {
          boost: { title: 6, alt: 3, tags: 2, notes: 1 },
          prefix: true,
          // Allow roughly one typo for mid-length words and two for long ones.
          fuzzy: (term) => (term.length >= 7 ? 0.3 : term.length > 4 ? 0.2 : false),
          combineWith: 'AND',
        },
      });
      this.ms.addAll(all.map(docOf));
      this.seen = new Map(all.map((e) => [e.id, e]));
    } else {
      // Incremental update: only touch entries whose object changed.
      const live = new Set<string>();
      for (const e of all) {
        live.add(e.id);
        const prev = this.seen.get(e.id);
        if (prev === e) continue;
        if (prev) this.ms.replace(docOf(e));
        else this.ms.add(docOf(e));
        this.seen.set(e.id, e);
      }
      if (this.seen.size !== live.size) {
        for (const id of [...this.seen.keys()]) {
          if (!live.has(id)) {
            this.ms.discard(id);
            this.seen.delete(id);
          }
        }
      }
    }
    this.builtAt = this.store.version;
    return this.ms;
  }

  /** Ranked entry ids with the matched terms (for highlighting). */
  search(q: string, limit = 50): { id: string; score: number; terms: string[] }[] {
    if (!q.trim()) return [];
    return this.ensure()
      .search(q)
      .slice(0, limit)
      .map((r) => ({ id: r.id as string, score: r.score, terms: r.terms }));
  }

  ids(q: string): Set<string> {
    return new Set(
      this.ensure()
        .search(q)
        .map((r) => r.id as string),
    );
  }
}

const arr = <T extends z.ZodTypeAny>(s: T) =>
  z.preprocess(
    (v) => (v === undefined ? undefined : Array.isArray(v) ? v : String(v).split(',')),
    z.array(s).optional(),
  );
const bool = z.preprocess(
  (v) => (v === 'true' || v === '1' || v === true ? true : v === 'false' || v === '0' ? false : v),
  z.boolean().optional(),
);

export const EntryQuerySchema = z.object({
  q: z.string().optional(),
  library: z.enum(['reading', 'watching']).optional(),
  category: arr(z.string()),
  status: arr(z.string()),
  tags: arr(z.string()),
  tagMode: z.enum(['all', 'any']).default('all'),
  excludeTags: arr(z.string()),
  mediaType: arr(z.string()),
  minScore: z.coerce.number().optional(),
  maxScore: z.coerce.number().optional(),
  hasNotes: bool,
  behind: bool,
  favorite: bool,
  source: z.string().optional(),
  sort: z
    .enum(['title', 'score', 'updatedAt', 'createdAt', 'status', 'category', 'relevance'])
    .optional(),
  order: z.enum(['asc', 'desc']).default('asc'),
  page: z.coerce.number().int().min(1).default(1),
  limit: z.coerce.number().int().min(1).max(1000).default(100),
});
export type EntryQuery = z.infer<typeof EntryQuerySchema>;

/** Strip trailing "[]" from query keys so `status[]=a` works. */
export function normalizeQueryKeys(raw: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(raw).map(([k, v]) => [k.replace(/\[\]$/, ''), v]));
}

const collator = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true });
const titleOrder = new WeakMap<Store, { v: number; list: Entry[] }>();

/** All entries ordered by title, cached per store version (sorting 10k titles on every request is slow). */
function sortedByTitle(store: Store): Entry[] {
  const hit = titleOrder.get(store);
  if (hit && hit.v === store.version) return hit.list;
  const list = [...store.all()].sort((a, b) => collator.compare(a.title, b.title));
  titleOrder.set(store, { v: store.version, list });
  return list;
}

export function queryEntries(store: Store, index: SearchIndex, q: EntryQuery) {
  const sort = q.sort ?? (q.q?.trim() ? 'relevance' : 'title');
  const presorted = sort === 'title' && !q.q?.trim();
  let list: Entry[] = presorted ? sortedByTitle(store) : store.all();
  const rank = new Map<string, number>();
  if (q.q?.trim()) {
    const hits = index.search(q.q, 5000);
    hits.forEach((h, i) => rank.set(h.id, i));
    list = list.filter((e) => rank.has(e.id));
  }
  const lc = (xs?: string[]) => xs?.map((x) => x.toLowerCase());
  const tags = lc(q.tags);
  const ex = lc(q.excludeTags);
  const cats = q.category && new Set(q.category);
  const sts = q.status && new Set(q.status);
  const mts = q.mediaType && new Set(q.mediaType);
  const src = q.source?.toLowerCase();
  list = list.filter((e) => {
    if (q.library && e.library !== q.library) return false;
    if (cats && !cats.has(e.categoryId)) return false;
    if (sts && !sts.has(e.status ?? 'none')) return false;
    if (mts && !mts.has(e.mediaType)) return false;
    if (q.minScore !== undefined && (e.score ?? 0) < q.minScore) return false;
    if (q.maxScore !== undefined && (e.score === null || e.score > q.maxScore)) return false;
    if (
      q.hasNotes !== undefined &&
      Boolean(e.notes.general || e.notes.extra1 || e.notes.extra2) !== q.hasNotes
    )
      return false;
    if (q.favorite !== undefined && e.favorite !== q.favorite) return false;
    if (q.behind) {
      const b = behindBy(e.current, e.consumedUpTo);
      if (b === null || b <= 0) return false;
    }
    if (src && !e.sources.some((s) => s.toLowerCase() === src)) return false;
    const et = e.tags.map((t) => t.toLowerCase());
    if (
      tags?.length &&
      !(q.tagMode === 'any' ? tags.some((t) => et.includes(t)) : tags.every((t) => et.includes(t)))
    )
      return false;
    if (ex?.some((t) => et.includes(t))) return false;
    return true;
  });

  const dir = q.order === 'asc' ? 1 : -1;
  const catName = (e: Entry) => store.categories.find((c) => c.id === e.categoryId)?.name ?? '';
  const cmp = (a: Entry, b: Entry): number => {
    switch (sort) {
      case 'score':
        return ((a.score ?? -1) - (b.score ?? -1)) * dir;
      case 'updatedAt':
        return a.updatedAt.localeCompare(b.updatedAt) * dir;
      case 'createdAt':
        return a.createdAt.localeCompare(b.createdAt) * dir;
      case 'status':
        return (a.status ?? '').localeCompare(b.status ?? '') * dir;
      case 'category':
        return catName(a).localeCompare(catName(b)) * dir;
      case 'relevance':
        return rank.size ? (rank.get(a.id)! - rank.get(b.id)!) * dir : 0;
      default:
        return collator.compare(a.title, b.title) * dir;
    }
  };
  if (presorted) {
    if (q.order === 'desc') list = [...list].reverse();
  } else {
    list = [...list].sort(sort === 'relevance' ? (a, b) => rank.get(a.id)! - rank.get(b.id)! : cmp);
  }
  const total = list.length;
  const start = (q.page - 1) * q.limit;
  return { items: list.slice(start, start + q.limit), total, page: q.page, limit: q.limit };
}

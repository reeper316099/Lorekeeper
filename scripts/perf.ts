/**
 * Performance check at scale: builds a throwaway data folder with N synthetic entries and times the hot paths.
 *   npm run perf            (10,000 entries)
 *   npm run perf -- 25000
 * Targets: search < 50 ms on 5k entries, cold open comfortably < 3 s, writes never block reads.
 */
import { mkdtempSync, rmSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { performance } from 'node:perf_hooks';
import {
  overview,
  queryEntries,
  EntryQuerySchema,
  SearchIndex,
  Store,
} from '../packages/server/src/index.ts';
import { EntrySchema } from '../packages/shared/src/index.ts';

const N = Number(process.argv[2] ?? 10_000);
const keep = process.env.PERF_KEEP_DIR; // keep the generated data so the UI can be tried on it
const dir = keep ?? mkdtempSync(path.join(os.tmpdir(), 'lorekeeper-perf-'));
const WORDS = [
  'Silent',
  'Crimson',
  'Hollow',
  'Gilded',
  'Fallen',
  'Wandering',
  'Eternal',
  'Ashen',
  'Velvet',
  'Iron',
  'Lunar',
  'Broken',
  'Hidden',
  'Scarlet',
  'Verdant',
  'Frozen',
];
const NOUNS = [
  'Crown',
  'Garden',
  'Oath',
  'Tower',
  'Contract',
  'Blade',
  'Archive',
  'Throne',
  'Lantern',
  'Covenant',
  'Harbor',
  'Saga',
];
const TAGS = Array.from({ length: 60 }, (_, i) => `Tag ${i}`);
const STATUSES = ['planned', 'in_progress', 'completed', 'on_hold', 'dropped'] as const;

const time = async <T>(label: string, fn: () => T | Promise<T>, runs = 1) => {
  const samples: number[] = [];
  let out!: T;
  for (let i = 0; i < runs; i++) {
    const t = performance.now();
    out = await fn();
    samples.push(performance.now() - t);
  }
  samples.sort((a, b) => a - b);
  const p50 = samples[Math.floor(samples.length / 2)]!;
  const p95 = samples[Math.min(samples.length - 1, Math.floor(samples.length * 0.95))]!;
  console.log(
    `${label.padEnd(44)} ${p50.toFixed(1).padStart(8)} ms p50 ${p95.toFixed(1).padStart(8)} ms p95${runs > 1 ? `  (${runs} runs)` : ''}`,
  );
  return out;
};

const store = new Store(dir);
await store.open();
const cats = [
  await store.createCategory({ library: 'reading', name: 'Big' }),
  await store.createCategory({ library: 'watching', name: 'Other' }),
];
const now = new Date().toISOString();
const entries = Array.from({ length: N }, (_, i) =>
  EntrySchema.parse({
    id: `perf-${i}`,
    library: i % 3 ? 'reading' : 'watching',
    categoryId: cats[i % 3 ? 0 : 1]!.id,
    title: `${WORDS[i % 16]} ${NOUNS[(i >> 4) % 12]} ${i}`,
    status: STATUSES[i % 5],
    score: (i % 10) + 1,
    tags: [TAGS[i % 60]!, TAGS[(i * 7) % 60]!, TAGS[(i * 13) % 60]!],
    current: { raw: String(i % 300), number: i % 300, season: null, kind: 'number' },
    notes: {
      general: `Notes about entry ${i} with a few words`,
      extra1: i % 4 ? null : 'Left off somewhere',
      extra2: null,
    },
    createdAt: now,
    updatedAt: now,
  }),
);
console.log(`Perf run with ${N.toLocaleString()} synthetic entries\n`);
await time('bulk insert (1 write per category)', () => store.insertEntries(entries));
await store.close();

const reopened = new Store(dir);
await time('cold open (read + validate all JSON)', () => reopened.open());
const index = new SearchIndex(reopened);
const q = (o: Record<string, unknown>) => queryEntries(reopened, index, EntryQuerySchema.parse(o));
await time('first search (builds the index)', () => q({ q: 'crimson' }));
await time('search "crimson tower" (warm)', () => q({ q: 'crimson tower', limit: 50 }), 30);
const fz = await time('search fuzzy "cirmson" (warm)', () => q({ q: 'cirmson', limit: 50 }), 30);
console.log('  fuzzy hits:', fz.total);
await time('search prefix "wand" (warm)', () => q({ q: 'wand', limit: 50 }), 30);
await time('list, sort by title, page 1', () => q({ sort: 'title', limit: 100 }), 15);
await time(
  'list, 2 tags (all) + status filter',
  () => q({ tags: 'Tag 3,Tag 21', tagMode: 'any', status: 'in_progress', limit: 100 }),
  15,
);
await time('stats overview', () => overview(reopened), 5);

const target = reopened.all()[N >> 1]!;
await time(
  'PATCH one entry (persist the big file)',
  () => reopened.updateEntry(target.id, { score: 9 }),
  8,
);
await time(
  'search right after a write (incremental index)',
  () => q({ q: 'lantern', limit: 50 }),
  8,
);
let readMs = 0;
const writes = Promise.all(
  Array.from({ length: 20 }, (_, i) => reopened.setProgress(reopened.all()[i]!.id, { delta: 1 })),
);
const t0 = performance.now();
while (reopened.version >= 0) {
  const t = performance.now();
  q({ q: 'iron', limit: 20 });
  readMs = Math.max(readMs, performance.now() - t);
  if (performance.now() - t0 > 150) break;
}
await time('20 concurrent progress writes (coalesced)', () => writes);
console.log(
  `${'slowest search while writes were in flight'.padEnd(44)} ${readMs.toFixed(1).padStart(8)} ms`,
);

await reopened.close();
if (!keep) rmSync(dir, { recursive: true, force: true });

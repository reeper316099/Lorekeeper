/**
 * Generates go/internal/search/testdata/{corpus,rankings}.json from the real Node implementation
 * (MiniSearch through SearchIndex): an invented corpus of entries, a list of queries with the ranked
 * results, and a list of edits followed by the results again. The Go index must rank the same.
 *   npx tsx scripts/gen-go-search-fixtures.ts
 */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { SearchIndex, Store } from '../packages/server/src/index.ts';
import { EntrySchema, type Entry } from '../packages/shared/src/index.ts';

let seed = 20260101;
const rnd = (n: number) => {
  seed = (seed * 1664525 + 1013904223) % 4294967296;
  return seed % n;
};
const pick = <T>(xs: T[]) => xs[rnd(xs.length)]!;

const WORDS = [
  'Crimson',
  'Tower',
  'Hollow',
  'Lantern',
  'Marlow’s',
  'Orchard',
  'Silent',
  'Harbor',
  'Gilded',
  'Archive',
  'Velvet',
  'Covenant',
  'Wandering',
  'Blade',
  'Eternal',
  'Garden',
  'Ashen',
  'Throne',
  'Lunar',
  'Saga',
  'Frozen',
  'Oath',
  'Scarlet',
  'Contract',
  'Verdant',
  'Crown',
  'Café',
  'Nebula',
  'Ünder',
  'Fáerie',
  'Quiet',
  'Clockwork',
  'Meridian',
  'Glass',
  'Paper',
  'Lanterns',
  'Moonlit',
  'Sorcery',
  'Academy',
  'Rank',
  'Board',
  'Sword',
  'Arts',
  'Quest',
  'Dungeon',
  'Reborn',
  'Villainess',
  'Tales',
  'of',
  'the',
  'a',
  '12',
  'Season',
  'Two',
  'Re:Zero',
  'Mr.',
  'Pell',
  'Slow-Burn',
  'Cozy',
  'Town',
  'Brigade',
  'Maple',
];
const TAGS = [
  'Romance',
  'Comedy',
  'Slow Burn',
  'Cozy Town',
  'Sorcery',
  'Fantasy',
  'Space',
  'Time Loop',
  'Isekai',
  'School Life',
  'Revenge',
  'Court Intrigue',
  'Overlord Lead',
  'Found Family',
  'Mystery',
  'Sports',
];
const NOTES = [
  'Left off at the market arc',
  'great art, slow plot',
  'recap by PanelPilot',
  'dropped then resumed',
  'check the sequel',
];

const now = '2026-01-01T00:00:00.000Z';
const cats = ['reading-a', 'reading-b', 'watching-c'];
const mkEntry = (i: number): Entry => {
  const words = Array.from({ length: 2 + rnd(3) }, () => pick(WORDS));
  const cat = cats[rnd(3)]!;
  return EntrySchema.parse({
    id: `e${String(i).padStart(4, '0')}`,
    library: cat.startsWith('reading') ? 'reading' : 'watching',
    categoryId: cat,
    title: words.join(' '),
    altTitles: rnd(4) === 0 ? [pick(WORDS) + ' ' + pick(WORDS)] : [],
    tags: Array.from({ length: rnd(4) }, () => pick(TAGS)).filter((t, k, a) => a.indexOf(t) === k),
    notes: {
      general: rnd(3) === 0 ? pick(NOTES) : null,
      extra1: rnd(5) === 0 ? pick(NOTES) : null,
      extra2: null,
    },
    createdAt: now,
    updatedAt: now,
  });
};
const corpus = Array.from({ length: 400 }, (_, i) => mkEntry(i));

const queries = [
  'crimson',
  'CRIMSON',
  'crim',
  'cri',
  'crimsn',
  'cirmson',
  'crimson tower',
  'tower crimson',
  'tower crim',
  'marlow',
  'marlow’s',
  'marlows',
  "marlow's",
  'cafe',
  'café',
  'caf',
  'ünder',
  'under',
  'faerie',
  'fáerie',
  'wandering',
  'wandring',
  'wand',
  'lantern',
  'lanterns',
  'lanter',
  'sorcery',
  'sorcrey',
  'sorc',
  'slow burn',
  'slow-burn',
  'burn',
  'cozy town',
  'cozy tow',
  'romance',
  'romanc',
  'time loop',
  'loop time',
  'left off',
  'left off market',
  'panel',
  'panelpilot',
  'recap',
  'dropped resumed',
  '12',
  '1',
  're:zero',
  'zero',
  'mr pell',
  'mr. pell',
  'the',
  'of the',
  'a',
  'zzzz',
  'xyzzy plugh',
  '!!!',
  '   ',
  'tale',
  'tales of',
  'academy sorcery',
  'quiet harbor',
  'quiet harbour',
  'glass meridian',
  'meridian glass',
  'moonlit orchard',
  'paper lanterns',
  'sword arts',
  'rank board',
  'dungeon',
  'dungon',
  'dungeons',
  'villainess reborn',
  'overlord',
  'overlord lead',
  'found family',
  'court',
  'intrigue court',
  'ashen throne',
  'verdant crown',
  'frozen oath',
  'scarlet contract',
  'gilded archive',
  'velvet covenant',
  'eternal garden',
  'lunar saga',
  'clockwork',
  'nebula',
  'maple brigade',
  'season two',
  'season',
  'sea',
  'sports',
  'mystery tale',
];

const dir = join(dirname(fileURLToPath(import.meta.url)), '../go/internal/search/testdata');
mkdirSync(dir, { recursive: true });
const tmp = mkdtempSync(join(os.tmpdir(), 'lk-search-'));
const store = new Store(tmp);
await store.open();
await store.createCategory({ library: 'reading', name: 'A' });
await store.createCategory({ library: 'reading', name: 'B' });
await store.createCategory({ library: 'watching', name: 'C' });
await store.insertEntries(corpus);
const index = new SearchIndex(store);

const run = (index: SearchIndex) =>
  Object.fromEntries(
    queries.map((q) => [
      q,
      index.search(q, 5000).map((h) => ({ id: h.id, score: h.score, terms: [...h.terms].sort() })),
    ]),
  );
const before = run(index);

type Op =
  | { op: 'update'; id: string; patch: Record<string, unknown> }
  | { op: 'delete'; id: string }
  | { op: 'insert'; entry: Entry }
  | { op: 'move'; id: string; to: string };
const ops: Op[] = [
  { op: 'update', id: 'e0003', patch: { title: 'Crimson Lantern Reborn' } },
  {
    op: 'update',
    id: 'e0010',
    patch: { tags: ['Sorcery', 'Space'], notes: { general: 'recap by PanelPilot' } },
  },
  { op: 'update', id: 'e0020', patch: { altTitles: ['Quiet Harbour'] } },
  { op: 'delete', id: 'e0005' },
  { op: 'delete', id: 'e0006' },
  {
    op: 'insert',
    entry: EntrySchema.parse({
      ...mkEntry(900),
      id: 'n0900',
      title: 'Wandering Crimson Quill',
      categoryId: 'reading-a',
      library: 'reading',
    }),
  },
  {
    op: 'insert',
    entry: EntrySchema.parse({
      ...mkEntry(901),
      id: 'n0901',
      title: 'Glass Meridian Two',
      categoryId: 'watching-c',
      library: 'watching',
    }),
  },
  { op: 'update', id: 'e0030', patch: { status: 'completed', score: 9 } }, // no searchable change
];
const movable = corpus.find((e) => e.library === 'reading' && e.categoryId === 'reading-a')!;
ops.push({ op: 'move', id: movable.id, to: 'reading-b' });
for (const o of ops) {
  if (o.op === 'update') await store.updateEntry(o.id, o.patch);
  else if (o.op === 'delete') await store.deleteEntry(o.id);
  else if (o.op === 'insert') await store.insertEntries([o.entry]);
  else await store.moveEntry(o.id, o.to);
}
// MiniSearch discards documents lazily, so the *incrementally edited* Node index keeps slightly stale
// BM25 statistics until it is vacuumed. The Go index recomputes them exactly, so the reference for
// "after the edits" is a freshly built index over the same data.
const after = run(new SearchIndex(store));

writeFileSync(join(dir, 'corpus.json'), JSON.stringify({ entries: corpus }, null, 1) + '\n');
writeFileSync(
  join(dir, 'rankings.json'),
  JSON.stringify({ queries, before, ops, after }, null, 1) + '\n',
);
const hits = Object.values(before).filter((r) => r.length).length;
console.log(
  `corpus=${corpus.length} queries=${queries.length} (${hits} with hits) ops=${ops.length}`,
);
await store.close();
rmSync(tmp, { recursive: true, force: true });

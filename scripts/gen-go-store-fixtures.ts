/**
 * Writes a realistic data folder with the Node store to go/internal/store/testdata/nodedata/.
 * The Go tests open it and re-serialize every file; the bytes must be identical, which proves the
 * Go store reads and writes exactly the same on-disk format. All content is invented.
 *   npx tsx scripts/gen-go-store-fixtures.ts
 */
import { mkdirSync, rmSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Store } from '../packages/server/src/index.ts';
import { parseLink } from '../packages/shared/src/index.ts';

const dir = join(dirname(fileURLToPath(import.meta.url)), '../go/internal/store/testdata/nodedata');
rmSync(dir, { recursive: true, force: true });
mkdirSync(dir, { recursive: true });

const store = new Store(dir, { appVersion: '0.1.0' });
await store.open();
const reading = await store.createCategory({ library: 'reading', name: 'Romance' });
const harem = await store.createCategory({ library: 'reading', name: 'Harem' });
const anime = await store.createCategory({ library: 'watching', name: 'Anime' });
await store.createCategory({ library: 'watching', name: 'Slice of Life' });

const a = await store.createEntry({
  title: 'Moonlit Orchard',
  categoryId: reading.id,
  status: 'in_progress',
  score: 8,
  tags: ['Slow Burn', 'slow burn', '  Cozy  Town '],
  sources: ['YouTube', 'Inkreader'],
  links: [
    { ...parseLink('youtu.be/abc123XYZ', 'youtube')!, slot: 'primary' },
    { ...parseLink('(S1 #4) Orchard Recap (3/3/26) By: PanelPilot', 'youtube')!, slot: 'other' },
  ],
  notes: { general: 'Tom & Jerry <Reborn> "quoted"', extra1: 'ch 12', extra2: null },
  consumedUpTo: { raw: '11', number: 11, season: null, kind: 'number' },
  external: {
    anilistId: 101,
    averageScore: 79.5,
    description: 'Line Separator and éè accents',
    genres: ['Romance', 'Drama'],
    nested: { b: 1, a: [2, { z: 1, y: 2 }] },
  },
  favorite: true,
});
await store.setProgress(a.id, { set: 'S2 E3' });
await store.setProgress(a.id, { delta: 0 }).catch(() => undefined); // non-numeric: rejected
const b = await store.createEntry({
  title: 'Paper Lanterns',
  categoryId: harem.id,
  tags: ['Cozy Town', 'Comedy'],
  totalUnits: 24,
  altTitles: ['Lanterns of Paper'],
});
await store.setProgress(b.id, { delta: 3 });
await store.updateEntry(b.id, { status: 'on_hold', season: 'S1' });
const c = await store.createEntry({
  title: 'Café Nebula',
  categoryId: anime.id,
  status: 'completed',
  score: 10,
  tags: ['Comedy', 'Space'],
  startedAt: '2026-01-02',
  completedAt: '2026-02-03',
  importRef: {
    file: 'sample.xlsx',
    sheet: 'Anime',
    row: 4,
    importedAt: '2026-01-01T00:00:00.000Z',
  },
});
await store.createEntry({ title: 'Quiet Harbor', categoryId: anime.id, tags: [] });
await store.mergeTags(['Slow Burn'], 'Cozy Town');
await store.updateTag('Comedy', { color: '#112233', hidden: true });
await store.saveSettings({
  ...store.settings,
  theme: 'light',
  labelOverrides: { extra1: 'Left off', extra2: 'Extras' },
  hiddenTags: ['Space'],
  savedViews: [{ id: 'v1', name: 'Cozy', query: { tags: 'Cozy Town' } }],
});
void c;
await store.close();
rmSync(join(dir, 'backups'), { recursive: true, force: true });
for (const d of ['exports', 'imports', 'logs'])
  rmSync(join(dir, d), { recursive: true, force: true });
console.log('wrote', dir);

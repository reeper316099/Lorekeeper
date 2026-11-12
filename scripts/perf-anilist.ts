/**
 * Times AniList matching and account import against an in-process fake AniList (no network, no real
 * sleeping), to compare with the Go port (`PERF=1 go test -run TestPerfAniList -v ./internal/anilist`).
 *   npx tsx scripts/perf-anilist.ts
 */
import { mkdtempSync, rmSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { performance } from 'node:perf_hooks';
import { AniListClient } from '../packages/server/src/anilist/client.ts';
import { JobRegistry } from '../packages/server/src/anilist/jobs.ts';
import { AniListService } from '../packages/server/src/anilist/service.ts';
import type { AniListEntry, AniMedia } from '../packages/server/src/anilist/queries.ts';
import { Store } from '../packages/server/src/index.ts';

const W = ['Silent', 'Crimson', 'Hollow', 'Gilded', 'Fallen', 'Wandering', 'Eternal', 'Ashen'];
const N = ['Crown', 'Garden', 'Oath', 'Tower', 'Contract', 'Blade', 'Archive', 'Throne'];
const title = (i: number) => `${W[i % 8]} ${N[(i >> 3) % 8]} ${i}`;
const media = (i: number): AniMedia => ({
  id: 1000 + i,
  idMal: null,
  type: 'MANGA',
  format: 'MANGA',
  status: 'RELEASING',
  episodes: null,
  chapters: 100,
  volumes: null,
  countryOfOrigin: 'KR',
  seasonYear: null,
  averageScore: 80,
  isAdult: false,
  siteUrl: `https://anilist.co/manga/${1000 + i}`,
  title: { romaji: title(i), english: null, native: null },
  synonyms: [],
  startDate: { year: 2020 },
  description: 'A <i>story</i>',
  coverImage: { large: `https://s4.anilist.co/c/${i}.jpg`, extraLarge: null },
  bannerImage: null,
  genres: ['Fantasy'],
  tags: [],
});

const n = Number(process.argv[2] ?? 300);
const catalog = Array.from({ length: n }, (_, i) => media(i));
const list: AniListEntry[] = catalog.map((m, i) => ({
  status: 'CURRENT',
  progress: i % 50,
  progressVolumes: null,
  repeat: 0,
  notes: null,
  score: (i % 10) + 1,
  startedAt: null,
  completedAt: null,
  media: m,
}));
const fake = (async (url: string | URL | Request, init?: RequestInit) => {
  if (String(url).startsWith('https://s4.anilist.co/'))
    return new Response(new Uint8Array([9, 9]), { headers: { 'content-type': 'image/jpeg' } });
  const { query, variables } = JSON.parse(String(init?.body)) as {
    query: string;
    variables: { search: string };
  };
  const json = (data: unknown) =>
    new Response(JSON.stringify({ data }), { headers: { 'content-type': 'application/json' } });
  if (query.includes('MediaListCollection'))
    return json({
      MediaListCollection: {
        hasNextChunk: false,
        lists: [{ name: 'All', isCustomList: false, entries: list }],
      },
    });
  const q = variables.search.toLowerCase();
  return json({ Page: { media: catalog.filter((m) => m.title.romaji!.toLowerCase() === q) } });
}) as typeof fetch;

const dir = mkdtempSync(path.join(os.tmpdir(), 'lk-pani-'));
const store = new Store(dir);
await store.open();
const cat = await store.createCategory({ library: 'reading', name: 'Misc.' });
await store.insertEntries(
  catalog.map((m, i) => store.buildEntry({ title: title(i), categoryId: cat.id })),
);
const jobs = new JobRegistry();
const svc = new AniListService(
  store,
  new AniListClient({ dataDir: dir, fetch: fake, sleep: async () => {}, maxPerMinute: 1_000_000 }),
  jobs,
);

console.log(`Node AniList timings, ${n} titles\n`);
let t = performance.now();
const job = svc.startMatching();
while (jobs.get(job.id)!.state === 'running') await new Promise((r) => setTimeout(r, 5));
console.log(
  `${'matching job (search+rank+apply+cover)'.padEnd(46)} ${(performance.now() - t).toFixed(1).padStart(8)} ms  ${JSON.stringify(jobs.get(job.id)!.result)}`,
);

t = performance.now();
const pv = await svc.previewList({
  username: 'x',
  types: ['MANGA'],
  mangaCategoryId: cat.id,
  strategy: 'fill',
});
console.log(
  `${'account import preview'.padEnd(46)} ${(performance.now() - t).toFixed(1).padStart(8)} ms  ${JSON.stringify(pv.counts)}`,
);
await store.close();
rmSync(dir, { recursive: true, force: true });

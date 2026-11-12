/**
 * Generates go/internal/anilist/testdata/anilist-parity.json from the real Node AniList service: a
 * fake AniList world (invented titles), scenarios driven through the service (matching job, review
 * actions, unmatch, cache clearing, account import), and everything they produced: entries, queue,
 * job results, and the names of the files left in anilist/cache and covers. The Go service must do
 * the same against the same world.
 *   npx tsx scripts/gen-go-anilist-fixtures.ts
 */
import { mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { AniListClient } from '../packages/server/src/anilist/client.ts';
import { JobRegistry } from '../packages/server/src/anilist/jobs.ts';
import { AniListService } from '../packages/server/src/anilist/service.ts';
import { rank, planSearch, parseAniListRef } from '../packages/server/src/anilist/matcher.ts';
import type { AniListEntry, AniMedia } from '../packages/server/src/anilist/queries.ts';
import { Store } from '../packages/server/src/index.ts';

const OUT = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  '../go/internal/anilist/testdata/anilist-parity.json',
);

const media = (id: number, title: string, o: Partial<AniMedia> = {}): AniMedia => ({
  id,
  idMal: null,
  type: 'MANGA',
  format: 'MANGA',
  status: 'RELEASING',
  episodes: null,
  chapters: 120,
  volumes: null,
  countryOfOrigin: 'KR',
  seasonYear: null,
  averageScore: 80,
  isAdult: false,
  siteUrl: `https://anilist.co/manga/${id}`,
  title: { romaji: title, english: null, native: null },
  synonyms: [],
  startDate: { year: 2020 },
  description: 'A <i>story</i><br>line &amp; more &quot;quoted&quot; it&#039;s',
  coverImage: {
    large: `https://s4.anilist.co/file/c/${id}-large.jpg`,
    extraLarge: `https://s4.anilist.co/file/c/${id}-xl.jpg`,
  },
  bannerImage: null,
  genres: ['Fantasy'],
  tags: [
    { name: 'Time Loop', rank: 80, isMediaSpoiler: false },
    { name: 'Twist', rank: 90, isMediaSpoiler: true },
    { name: 'Low Rank', rank: 20, isMediaSpoiler: false },
  ],
  ...o,
});

const item = (m: AniMedia, o: Partial<AniListEntry> = {}): AniListEntry => ({
  status: 'CURRENT',
  progress: 12,
  progressVolumes: null,
  repeat: 0,
  notes: 'my note',
  score: 8,
  startedAt: { year: 2024, month: 3, day: 9 },
  completedAt: { year: null, month: null, day: null },
  media: m,
  ...o,
});

const catalog: AniMedia[] = [
  media(1, 'Lantern of the Hollow Crown', {
    synonyms: ['Hollow Crown Lantern'],
    idMal: 5001,
    bannerImage: 'https://s4.anilist.co/file/b/1.jpg',
    title: {
      romaji: 'Lantern of the Hollow Crown',
      english: 'The Hollow Crown',
      native: '空洞の王冠',
    },
  }),
  media(2, 'Twin Stars of Ember', { chapters: 40 }),
  media(3, 'Twin Stars of Embers', { chapters: 41 }),
  media(5, 'Quiet Orchard Tale'),
  media(6, 'Quiet Orchard Tales'),
  media(7, 'Shared Cover Tale'),
  media(8, 'Novel of the Pale Moon', { format: 'NOVEL', countryOfOrigin: 'JP', chapters: null }),
  media(9, 'Cinder Chronicle: Ashes', { countryOfOrigin: 'CN' }),
  media(21, 'Moonlit Show', {
    type: 'ANIME',
    format: 'TV',
    episodes: 24,
    chapters: null,
    countryOfOrigin: 'JP',
    siteUrl: 'https://anilist.co/anime/21',
  }),
  media(22, 'Paper Dragons', {
    type: 'ANIME',
    format: 'TV',
    episodes: 12,
    chapters: null,
    countryOfOrigin: 'CN',
    seasonYear: 2023,
    siteUrl: 'https://anilist.co/anime/22',
  }),
];
const lists = {
  reader_one: {
    MANGA: [
      item(media(10, 'Brand New Manhwa'), {
        status: 'COMPLETED',
        score: 9,
        completedAt: { year: 2025, month: 1, day: 2 },
      }),
      item(media(11, 'Already Tracked Series', { synonyms: ['Alt Name'] }), {
        status: 'PLANNING',
        progress: 0,
        score: 0,
        notes: null,
      }),
      item(media(12, 'Chinese Comic', { countryOfOrigin: 'CN' }), { status: 'PAUSED', score: 7.5 }),
      item(media(13, 'Novel Entry', { format: 'NOVEL', countryOfOrigin: 'JP', chapters: null }), {
        status: 'DROPPED',
        progress: 3.5,
      }),
    ],
    ANIME: [
      item(
        media(20, 'Moonlit Show', {
          type: 'ANIME',
          format: 'TV',
          episodes: 24,
          chapters: null,
          countryOfOrigin: 'CN',
          siteUrl: 'https://anilist.co/anime/20',
        }),
        { status: 'REPEATING' },
      ),
    ],
  },
};
const world = { catalog, lists };

/** A tiny fake AniList that understands the three queries the app sends. */
function fakeAniList(): typeof fetch {
  return (async (url: string | URL | Request, init?: RequestInit) => {
    const u = String(url);
    if (u.startsWith('https://s4.anilist.co/'))
      return new Response(new Uint8Array([9, 9]), { headers: { 'content-type': 'image/jpeg' } });
    const { query, variables } = JSON.parse(String(init?.body)) as {
      query: string;
      variables: {
        userName: string;
        type: 'ANIME' | 'MANGA';
        id: number;
        search: string;
        country: string | null;
      };
    };
    const json = (data: unknown) =>
      new Response(JSON.stringify({ data }), { headers: { 'content-type': 'application/json' } });
    const fail = (message: string, status: number) =>
      new Response(JSON.stringify({ errors: [{ message, status }] }), { status });
    if (query.includes('MediaListCollection')) {
      const l = (lists as Record<string, Record<string, AniListEntry[]>>)[variables.userName];
      if (!l) return fail('User not found', 404);
      const entries = l[variables.type] ?? [];
      return json({
        MediaListCollection: {
          hasNextChunk: false,
          lists: [
            { name: 'All', isCustomList: false, entries },
            { name: 'Faves', isCustomList: true, entries: entries.slice(0, 1) },
          ],
        },
      });
    }
    if (query.includes('Media(id')) {
      const m = catalog.find((x) => x.id === variables.id);
      return m ? json({ Media: m }) : fail('Not Found.', 404);
    }
    const q = String(variables.search).toLowerCase();
    const hits = catalog.filter(
      (m) =>
        m.type === variables.type &&
        (!variables.country || m.countryOfOrigin === variables.country) &&
        [m.title.romaji, m.title.english, ...m.synonyms].some(
          (t) => t && (t.toLowerCase().includes(q) || q.includes(t.toLowerCase())),
        ),
    );
    return json({ Page: { media: hits } });
  }) as typeof fetch;
}

type Step = Record<string, unknown> & { do: string };
interface Scenario {
  name: string;
  categories: { library: 'reading' | 'watching'; name: string }[];
  entries: { title: string; category: string; extra?: Record<string, unknown> }[];
  steps: Step[];
}
const scenarios: Scenario[] = [
  {
    name: 'matching and review actions',
    categories: [
      { library: 'reading', name: 'Misc.' },
      { library: 'watching', name: 'Anime' },
      { library: 'watching', name: 'TV Shows' },
    ],
    entries: [
      { title: 'Lantern of the Hollow Crown', category: 'reading-misc', extra: { tags: ['mine'] } },
      { title: 'Twin Stars of Ember', category: 'reading-misc' },
      { title: 'A Show With No Match', category: 'reading-misc' },
      { title: 'Lantern of the Hollow Crown (TV)', category: 'watching-tv-shows' },
      {
        title: 'Cinder Chronicle: Ashes (Remastered)',
        category: 'reading-misc',
        extra: { mediaType: 'manhua' },
      },
      { title: 'Novel of the Pale Moon', category: 'reading-misc' },
      { title: 'Moonlit Show', category: 'watching-anime' },
    ],
    steps: [
      { do: 'match' },
      { do: 'queue', status: 'pending' },
      { do: 'queue', status: 'unlinkable' },
      { do: 'suggestedTags', title: 'Lantern of the Hollow Crown' },
      { do: 'confirm', title: 'Twin Stars of Ember', mediaId: 3 },
      { do: 'set', title: 'A Show With No Match', ref: 'https://anilist.co/manga/2/Twin' },
      { do: 'search', title: 'Moonlit Show', q: 'Paper Dragons' },
      { do: 'match' },
      { do: 'never', title: 'Cinder Chronicle: Ashes (Remastered)' },
      { do: 'queue', status: 'never' },
      { do: 'unmatch', title: 'Twin Stars of Ember' },
      { do: 'unmatch', title: 'Lantern of the Hollow Crown' },
      { do: 'clear' },
    ],
  },
  {
    name: 'shared cover survives one unmatch',
    categories: [{ library: 'reading', name: 'Misc.' }],
    entries: [
      { title: 'Shared Cover Tale', category: 'reading-misc' },
      { title: 'Shared Cover Tale Copy', category: 'reading-misc' },
    ],
    steps: [
      { do: 'confirm', title: 'Shared Cover Tale', mediaId: 7 },
      { do: 'confirm', title: 'Shared Cover Tale Copy', mediaId: 7 },
      { do: 'unmatch', title: 'Shared Cover Tale' },
      { do: 'unmatch', title: 'Shared Cover Tale Copy' },
    ],
  },
  {
    name: 'account import',
    categories: [
      { library: 'reading', name: 'Misc.' },
      { library: 'watching', name: 'Anime' },
    ],
    entries: [
      {
        title: 'Already tracked series',
        category: 'reading-misc',
        extra: { notes: { general: 'keep me', extra1: null, extra2: null } },
      },
    ],
    steps: [
      {
        do: 'preview',
        options: {
          username: 'reader_one',
          types: ['MANGA', 'ANIME'],
          animeCategoryId: 'watching-anime',
          mangaCategoryId: 'reading-misc',
        },
      },
      { do: 'commit' },
      {
        do: 'preview',
        options: {
          username: 'reader_one',
          types: ['MANGA', 'ANIME'],
          animeCategoryId: 'watching-anime',
          mangaCategoryId: 'reading-misc',
        },
      },
      {
        do: 'preview',
        options: {
          username: 'reader_one',
          types: ['MANGA'],
          strategy: 'overwrite',
          mangaCategoryId: 'reading-misc',
        },
      },
      { do: 'commit' },
      {
        do: 'preview',
        options: {
          username: 'reader_one',
          types: ['ANIME'],
          strategy: 'skip',
          animeCategoryId: 'watching-anime',
        },
      },
      {
        do: 'preview',
        options: {
          username: 'reader_one',
          types: ['ANIME'],
          strategy: 'keep_both',
          animeCategoryId: 'watching-anime',
        },
      },
      {
        do: 'preview',
        options: { username: 'reader_one', types: ['MANGA'], mangaCategoryId: 'does-not-exist' },
      },
    ],
  },
];

const skip = new Set(['id', 'createdAt', 'updatedAt']);
const norm = (v: unknown): unknown => {
  if (Array.isArray(v)) return v.map(norm);
  if (v && typeof v === 'object')
    return Object.fromEntries(
      Object.entries(v as Record<string, unknown>)
        .filter(([k, x]) => !skip.has(k) && !((k === 'channel' || k === 'date') && x === null))
        .map(([k, x]) => [k, norm(x)]),
    );
  return v;
};
const files = (dir: string) => {
  try {
    return readdirSync(dir).sort();
  } catch {
    return [];
  }
};

const results: unknown[] = [];
for (const sc of scenarios) {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'lk-ani-'));
  const store = new Store(dir);
  await store.open();
  for (const c of sc.categories) await store.createCategory(c);
  for (const e of sc.entries)
    await store.createEntry({ title: e.title, categoryId: e.category, ...e.extra } as never);
  const jobs = new JobRegistry();
  const svc = new AniListService(
    store,
    new AniListClient({ dataDir: dir, fetch: fakeAniList(), sleep: async () => {}, maxRetries: 1 }),
    jobs,
  );
  const byTitle = (t: string) => store.all().find((e) => e.title === t)!;
  const waitJob = async (id: string) => {
    for (let i = 0; i < 500; i++) {
      const j = jobs.get(id)!;
      if (j.state !== 'running') return j;
      await new Promise((r) => setTimeout(r, 10));
    }
    throw new Error('job did not finish');
  };
  let previewId = '';
  const stepResults: unknown[] = [];
  for (const st of sc.steps) {
    let out: unknown = null;
    const title = st.title as string;
    try {
      switch (st.do) {
        case 'match': {
          const job = await waitJob(svc.startMatching().id);
          out = { state: job.state, result: job.result, done: job.done, total: job.total };
          break;
        }
        case 'queue':
          out = (await svc.queueItems(st.status as never)).map(
            ({ entry, updatedAt: _u, ...it }) => ({ title: entry.title, ...it }),
          );
          break;
        case 'suggestedTags':
          out = norm(await svc.addSuggestedTags(byTitle(title).id));
          break;
        case 'confirm':
          out = norm(await svc.confirm(byTitle(title).id, st.mediaId as number));
          break;
        case 'set':
          out = norm(await svc.setByRef(byTitle(title).id, st.ref as string));
          break;
        case 'search':
          out = await svc.candidatesFor(byTitle(title).id, st.q as string);
          break;
        case 'never':
          await svc.never(byTitle(title).id);
          break;
        case 'unmatch':
          out = norm(await svc.unmatch(byTitle(title).id));
          break;
        case 'clear':
          out = await svc.clearCaches();
          break;
        case 'preview': {
          const pv = await svc.previewList(st.options as never);
          previewId = pv.previewId;
          const rest: Record<string, unknown> = { ...pv };
          delete rest.previewId;
          out = rest;
          break;
        }
        case 'commit': {
          const job = await waitJob(svc.startListImport(previewId).id);
          const result: Record<string, unknown> = {
            ...((job.result ?? {}) as Record<string, unknown>),
          };
          delete result.undoToken;
          out = { state: job.state, result };
          break;
        }
      }
    } catch (err) {
      const e = err as { status?: number; code?: string };
      out = { error: { status: e.status ?? null, code: e.code ?? null } };
    }
    stepResults.push({
      step: st,
      out,
      entries: (norm(store.all()) as { title: string }[]).sort((a, b) =>
        a.title.localeCompare(b.title),
      ),
      cache: files(path.join(dir, 'anilist', 'cache')),
      covers: files(path.join(dir, 'covers')),
    });
  }
  await store.close();
  rmSync(dir, { recursive: true, force: true });
  results.push({
    name: sc.name,
    setup: { categories: sc.categories, entries: sc.entries },
    results: stepResults,
  });
}

// matcher functions on their own (no network)
const e1 = {
  title: 'Lantern of the Hollow Crown',
  altTitles: [] as string[],
  library: 'reading',
  mediaType: 'manhwa',
} as never;
const matcher = {
  ranked: rank(
    e1,
    [
      catalog[0]!,
      catalog[1]!,
      media(30, 'Lantern of the Hollow Crown', { countryOfOrigin: 'JP' }),
      media(31, 'Lantern of the Hollow Crown', { format: 'NOVEL' }),
      media(32, 'Lantern of the Hollow Crown', { format: 'MUSIC' }),
      media(33, 'Lantern Hollow', { format: 'SPECIAL' }),
    ],
    planSearch(e1),
    8,
  ),
  refs: [
    '12345',
    'https://anilist.co/manga/987/Some-Title/',
    'anilist.co/anime/55',
    'nope',
    ' 7 ',
  ].map((r) => ({ in: r, out: parseAniListRef(r) })),
  keys: [
    AniListClient.hashKey('search', {
      search: 'Lantern of the Hollow Crown',
      type: 'MANGA',
      country: 'KR',
      perPage: 8,
    }),
    AniListClient.hashKey('search', {
      search: 'Café <&> “quoted”',
      type: 'ANIME',
      perPage: 8,
    }),
  ],
};
mkdirSync(path.dirname(OUT), { recursive: true });
writeFileSync(OUT, JSON.stringify({ world, scenarios: results, matcher }) + '\n');
console.log(`wrote ${OUT}: ${results.length} scenarios`);

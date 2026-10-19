import { mkdtemp, rm } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { buildApp, Store, type StoreOptions } from '../src/index.js';
import type { AniMedia } from '../src/anilist/queries.js';

export const TOKEN = 'test-token-123';

export async function tmpDir() {
  const dir = await mkdtemp(path.join(os.tmpdir(), 'lorekeeper-'));
  return { dir, cleanup: () => rm(dir, { recursive: true, force: true }) };
}

export async function makeStore(opts: StoreOptions = {}) {
  const t = await tmpDir();
  const store = new Store(t.dir, opts);
  await store.open();
  return { store, ...t };
}

export async function makeApp(opts: StoreOptions = {}) {
  const s = await makeStore(opts);
  const app = await buildApp({ store: s.store, token: TOKEN });
  const call = async (
    method: string,
    url: string,
    payload?: unknown,
    token: string | null = TOKEN,
  ) => {
    const res = await app.inject({
      method: method as 'GET',
      url: `/api/v1${url}`,
      payload: payload as object | undefined,
      headers: token ? { authorization: `Bearer ${token}` } : {},
    });
    return { status: res.statusCode, body: res.body ? JSON.parse(res.body) : null, res };
  };
  return { ...s, app, call };
}

/** An AniList media fixture with invented content. */
export const media = (id: number, title: string, o: Partial<AniMedia> = {}): AniMedia => ({
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
  description: 'A <i>story</i><br>line',
  coverImage: {
    large: `https://s4.anilist.co/file/c/${id}-large.jpg`,
    extraLarge: `https://s4.anilist.co/file/c/${id}-xl.jpg`,
  },
  bannerImage: null,
  genres: ['Fantasy'],
  tags: [
    { name: 'Time Loop', rank: 80, isMediaSpoiler: false },
    { name: 'Twist', rank: 90, isMediaSpoiler: true },
  ],
  ...o,
});

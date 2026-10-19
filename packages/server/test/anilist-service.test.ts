import { promises as fs } from 'node:fs';
import path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { buildApp } from '../src/index.js';
import { AniListClient } from '../src/anilist/client.js';
import { registerAniListRoutes } from '../src/anilist/routes.js';
import { registerIoRoutes } from '../src/io/routes.js';
import type { AniListEntry, AniMedia } from '../src/anilist/queries.js';
import { makeStore, media, TOKEN } from './helpers.js';

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const c of cleanups.splice(0)) await c();
});

interface World {
  catalog: AniMedia[];
  lists: Record<string, Record<'ANIME' | 'MANGA', AniListEntry[]>>;
  offline: boolean;
  delayMs: number;
  searches: string[];
}

/** A tiny fake AniList that understands the three queries the app sends. */
function fakeAniList(world: World): typeof fetch {
  return (async (url: string | URL | Request, init?: RequestInit) => {
    const u = String(url);
    if (world.offline) throw new TypeError('fetch failed');
    if (world.delayMs) await new Promise((r) => setTimeout(r, world.delayMs));
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
      const l = world.lists[variables.userName];
      if (!l) return fail('User not found', 404);
      const entries = l[variables.type as 'ANIME' | 'MANGA'] ?? [];
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
      const m = world.catalog.find((x) => x.id === variables.id);
      return m ? json({ Media: m }) : fail('Not Found.', 404);
    }
    world.searches.push(variables.search);
    const q = String(variables.search).toLowerCase();
    const hits = world.catalog.filter(
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

async function setup(world: Partial<World> = {}) {
  const w: World = { catalog: [], lists: {}, offline: false, delayMs: 0, searches: [], ...world };
  const s = await makeStore();
  const app = await buildApp({
    store: s.store,
    token: TOKEN,
    extend: (a, st) => {
      registerIoRoutes(a, st);
      registerAniListRoutes({
        client: () =>
          new AniListClient({
            dataDir: st.dir,
            fetch: fakeAniList(w),
            sleep: async () => {},
            maxRetries: 1,
          }),
      })(a, st);
    },
  });
  cleanups.push(async () => {
    await app.close();
    await s.store.close();
    await s.cleanup();
  });
  const call = async (method: string, url: string, payload?: unknown, auth = true) => {
    const r = await app.inject({
      method: method as 'GET',
      url: `/api/v1${url}`,
      payload: payload as object | undefined,
      headers: auth ? { authorization: `Bearer ${TOKEN}` } : {},
    });
    return {
      status: r.statusCode,
      body:
        r.body && r.headers['content-type']?.toString().includes('json')
          ? JSON.parse(r.body)
          : null,
      raw: r.rawPayload,
      res: r,
    };
  };
  const waitJob = async (id: string) => {
    for (let i = 0; i < 200; i++) {
      const j = (await call('GET', `/anilist/jobs/${id}`)).body;
      if (j.state !== 'running') return j;
      await new Promise((r) => setTimeout(r, 10));
    }
    throw new Error('job did not finish');
  };
  const reading = await s.store.createCategory({ library: 'reading', name: 'Misc.' });
  const anime = await s.store.createCategory({ library: 'watching', name: 'Anime' });
  const tv = await s.store.createCategory({ library: 'watching', name: 'TV Shows' });
  return { ...s, w, call, waitJob, reading, anime, tv };
}

describe('AniList matching', () => {
  it('auto-confirms clear matches, queues ambiguous ones, skips unlinkable categories', async () => {
    const t = await setup({
      catalog: [
        media(1, 'Lantern of the Hollow Crown', {
          synonyms: ['Hollow Crown Lantern'],
          title: {
            romaji: 'Lantern of the Hollow Crown',
            english: 'The Hollow Crown',
            native: '空洞の王冠',
          },
        }),
        media(2, 'Twin Stars of Ember', { chapters: 40 }),
        media(3, 'Twin Stars of Embers', { chapters: 41 }),
      ],
    });
    const a = await t.store.createEntry({
      title: 'Lantern of the Hollow Crown',
      categoryId: t.reading.id,
      tags: ['mine'],
    });
    const b = await t.store.createEntry({ title: 'Twin Stars of Ember', categoryId: t.reading.id });
    const c = await t.store.createEntry({
      title: 'A Show With No Match',
      categoryId: t.reading.id,
    });
    const tvShow = await t.store.createEntry({
      title: 'Lantern of the Hollow Crown',
      categoryId: t.tv.id,
    });

    const run = await t.call('POST', '/anilist/match/run', {});
    const job = await t.waitJob(run.body.id);
    expect(job.state).toBe('done');
    expect(job.result).toMatchObject({ autoConfirmed: 1, queued: 2, unlinkable: 1 });

    const matched = t.store.getEntry(a.id)!;
    expect(matched.external).toMatchObject({
      anilistId: 1,
      matchLocked: true,
      countryOfOrigin: 'KR',
      cover: '1.jpg',
      format: 'MANGA',
    });
    expect(matched.external.description).toBe('A story\nline'); // HTML stripped
    expect(matched.altTitles).toEqual(
      expect.arrayContaining(['The Hollow Crown', '空洞の王冠', 'Hollow Crown Lantern']),
    );
    expect(matched.totalUnits).toBe(120);
    expect(matched.mediaType).toBe('manhwa');
    expect(
      matched.links.some((l) => l.kind === 'anilist' && l.url === 'https://anilist.co/manga/1'),
    ).toBe(true);
    expect(matched.tags).toEqual(['mine']); // AniList tags are only suggestions
    await expect(fs.access(path.join(t.dir, 'covers', '1.jpg'))).resolves.toBeUndefined();

    const pending = (await t.call('GET', '/anilist/queue?status=pending')).body;
    expect(pending.map((p: { entry: { id: string } }) => p.entry.id).sort()).toEqual(
      [b.id, c.id].sort(),
    );
    expect(
      pending
        .find((p: { entry: { id: string } }) => p.entry.id === b.id)
        .candidates.map((x: { id: number }) => x.id)
        .sort(),
    ).toEqual([2, 3]);
    expect((await t.call('GET', '/anilist/queue?status=unlinkable')).body[0].entry.id).toBe(
      tvShow.id,
    );

    // suggested tags never apply on their own, only on request
    const added = (await t.call('POST', `/anilist/entries/${a.id}/suggested-tags`)).body;
    expect(added.tags).toEqual(expect.arrayContaining(['mine', 'Fantasy', 'Time Loop']));
    expect(added.tags).not.toContain('Twist'); // spoiler tag

    // review actions
    expect(
      (await t.call('POST', `/anilist/match/${b.id}/confirm`, { mediaId: 3 })).body.external
        .anilistId,
    ).toBe(3);
    expect(
      (
        await t.call('POST', `/anilist/match/${c.id}/set`, {
          ref: 'https://anilist.co/manga/2/Twin',
        })
      ).body.external.anilistId,
    ).toBe(2);
    expect((await t.call('POST', `/anilist/match/${c.id}/set`, { ref: 'nonsense' })).status).toBe(
      400,
    );
    expect((await t.call('POST', `/anilist/match/${c.id}/set`, { ref: '999999' })).status).toBe(
      404,
    );
    const st = (await t.call('GET', '/anilist/status')).body;
    expect(st.matched).toBe(3);
  });

  it('remembers reject / never and does not ask again; retry re-searches', async () => {
    const t = await setup({
      catalog: [media(5, 'Quiet Orchard Tale'), media(6, 'Quiet Orchard Tales')],
    });
    const e = await t.store.createEntry({ title: 'Quiet Orchard Tale', categoryId: t.reading.id });
    await t.waitJob((await t.call('POST', '/anilist/match/run', {})).body.id);
    expect((await t.call('GET', '/anilist/queue')).body).toHaveLength(1);
    await t.call('POST', `/anilist/match/${e.id}/never`);
    const before = t.w.searches.length;
    const job = await t.waitJob((await t.call('POST', '/anilist/match/run', {})).body.id);
    expect(t.w.searches.length).toBe(before); // never = never searched again
    expect(job.state).toBe('done');
    expect((await t.call('GET', '/anilist/queue?status=never')).body).toHaveLength(1);
    const retry = await t.call('POST', `/anilist/match/${e.id}/retry`);
    expect(retry.body.length).toBeGreaterThan(0);
    expect((await t.call('GET', '/anilist/queue')).body).toHaveLength(1);
    await t.call('POST', `/anilist/match/${e.id}/reject`);
    expect((await t.call('GET', '/anilist/queue?status=rejected')).body).toHaveLength(1);
    // unmatching clears the lock
    await t.call('POST', `/anilist/match/${e.id}/confirm`, { mediaId: 5 });
    const un = (await t.call('POST', `/anilist/entries/${e.id}/unmatch`)).body;
    expect(un.external.anilistId).toBeUndefined();
    expect(un.links.some((l: { kind: string }) => l.kind === 'anilist')).toBe(false);
  });

  it('handles offline and cancellation gracefully', async () => {
    const t = await setup({ catalog: [media(1, 'Some Title')], offline: true });
    await t.store.createEntry({ title: 'Some Title', categoryId: t.reading.id });
    const job = await t.waitJob((await t.call('POST', '/anilist/match/run', {})).body.id);
    expect(job.state).toBe('offline');
    expect((await t.call('GET', '/anilist/status')).body.offline).toBe(true);
    expect(
      (await t.call('GET', `/anilist/search?entryId=${t.store.all()[0]!.id}&q=x`)).status,
    ).toBe(503);

    t.w.offline = false;
    t.w.delayMs = 40;
    for (let i = 0; i < 6; i++)
      await t.store.createEntry({ title: `Slow Title ${i}`, categoryId: t.reading.id });
    const run = (await t.call('POST', '/anilist/match/run', {})).body;
    await new Promise((r) => setTimeout(r, 60));
    expect((await t.call('POST', `/anilist/jobs/${run.id}/cancel`)).body.cancelled).toBe(true);
    const done = await t.waitJob(run.id);
    expect(done.state).toBe('cancelled');
    expect(done.done).toBeLessThan(done.total);
  });

  it('requires auth and serves covers via ?token only for safe file names', async () => {
    const t = await setup({ catalog: [media(1, 'Exact Title Here')] });
    await t.store.createEntry({ title: 'Exact Title Here', categoryId: t.reading.id });
    await t.waitJob((await t.call('POST', '/anilist/match/run', {})).body.id);
    expect((await t.call('GET', '/anilist/status', undefined, false)).status).toBe(401);
    expect((await t.call('GET', '/anilist/cover/1.jpg', undefined, false)).status).toBe(401);
    const ok = await t.call('GET', `/anilist/cover/1.jpg?token=${TOKEN}`, undefined, false);
    expect(ok.status).toBe(200);
    expect(ok.res.headers['content-type']).toBe('image/jpeg');
    expect(
      (
        await t.call(
          'GET',
          `/anilist/cover/..%2Fconfig%2Fsettings.json?token=${TOKEN}`,
          undefined,
          false,
        )
      ).status,
    ).toBe(400);
    expect(
      (
        await t.call(
          'GET',
          `/anilist/image?url=${encodeURIComponent('https://evil.example/a.jpg')}&token=${TOKEN}`,
          undefined,
          false,
        )
      ).status,
    ).toBe(400);
    expect(
      (
        await t.call(
          'GET',
          `/anilist/image?url=${encodeURIComponent('https://s4.anilist.co/x.jpg')}&token=${TOKEN}`,
          undefined,
          false,
        )
      ).status,
    ).toBe(200);
  });
});

describe('AniList account import', () => {
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

  it('previews without writing, maps fields, dedupes, commits with covers, and undoes', async () => {
    const t = await setup({
      lists: {
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
            item(media(12, 'Chinese Comic', { countryOfOrigin: 'CN' }), { status: 'PAUSED' }),
          ],
          ANIME: [
            item(
              media(20, 'Moonlit Show', {
                type: 'ANIME',
                format: 'TV',
                episodes: 24,
                chapters: null,
                countryOfOrigin: 'CN',
              }),
              { status: 'REPEATING' },
            ),
          ],
        },
      },
    });
    const mine = await t.store.createEntry({
      title: 'Already tracked series',
      categoryId: t.reading.id,
      notes: { general: 'keep me', extra1: null, extra2: null },
    });
    const body = {
      username: 'reader_one',
      types: ['MANGA', 'ANIME'],
      animeCategoryId: t.anime.id,
      mangaCategoryId: t.reading.id,
    };

    const pv = (await t.call('POST', '/anilist/import/preview', body)).body;
    expect(pv.counts).toMatchObject({ fetched: 4, create: 3, update: 1 }); // custom list duplicate was ignored
    expect(t.store.all()).toHaveLength(1);

    const job = await t.waitJob(
      (await t.call('POST', '/anilist/import/commit', { previewId: pv.previewId })).body.id,
    );
    expect(job.state).toBe('done');
    expect(job.result).toMatchObject({ created: 3, updated: 1 });

    const all = t.store.all();
    const fresh = all.find((e) => e.title === 'Brand New Manhwa')!;
    expect(fresh).toMatchObject({
      library: 'reading',
      status: 'completed',
      score: 9,
      mediaType: 'manhwa',
      completedAt: '2025-01-02',
      startedAt: '2024-03-09',
    });
    expect(fresh.current).toMatchObject({ raw: '12', number: 12 });
    expect(fresh.notes.general).toBe('my note');
    expect(fresh.external).toMatchObject({ anilistId: 10, cover: '10.jpg' });
    expect(all.find((e) => e.title === 'Chinese Comic')).toMatchObject({
      mediaType: 'manhua',
      status: 'on_hold',
    });
    expect(all.find((e) => e.title === 'Moonlit Show')).toMatchObject({
      library: 'watching',
      mediaType: 'donghua',
      status: 'repeating',
      totalUnits: 24,
    });
    const merged = t.store.getEntry(mine.id)!;
    expect(merged.notes.general).toBe('keep me'); // never overwrites notes by default
    expect(merged.external.anilistId).toBe(11); // but is linked to AniList
    expect(merged.altTitles).toContain('Alt Name');
    expect(all).toHaveLength(4); // the existing entry was merged, not duplicated

    // a second import changes nothing
    const again = (await t.call('POST', '/anilist/import/preview', body)).body;
    expect(again.counts).toMatchObject({ create: 0, update: 0 });

    expect((await t.call('POST', `/import/undo/${job.result.undoToken}`)).status).toBe(200);
    expect(t.store.all().map((e) => e.id)).toEqual([mine.id]);
    expect(t.store.getEntry(mine.id)!.external.anilistId).toBeUndefined();
  });

  it('reports unknown users and validates usernames', async () => {
    const t = await setup({});
    const r = await t.call('POST', '/anilist/import/preview', {
      username: 'nobody_here',
      types: ['ANIME'],
      animeCategoryId: t.anime.id,
    });
    expect(r.status).toBe(404);
    expect(r.body.error.code).toBe('USER_NOT_FOUND');
    expect(
      (await t.call('POST', '/anilist/import/preview', { username: 'bad name!', types: ['ANIME'] }))
        .status,
    ).toBe(400);
    expect((await t.call('POST', '/anilist/import/commit', { previewId: 'nope' })).status).toBe(
      404,
    );
  });

  it('shows offline status for username import', async () => {
    const t = await setup({ offline: true });
    const r = await t.call('POST', '/anilist/import/preview', {
      username: 'someone',
      types: ['ANIME'],
      animeCategoryId: t.anime.id,
    });
    expect(r.status).toBe(503);
    expect(r.body.error.code).toBe('OFFLINE');
  });
});

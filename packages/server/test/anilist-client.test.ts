import { promises as fs } from 'node:fs';
import path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import {
  AniListClient,
  AniListError,
  CancelledError,
  OfflineError,
  RateLimitError,
} from '../src/anilist/client.js';
import { parseAniListRef, planSearch, rank, shouldAutoConfirm } from '../src/anilist/matcher.js';
import { media, tmpDir } from './helpers.js';

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const c of cleanups.splice(0)) await c();
});

const ok = (data: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'content-type': 'application/json', ...headers },
  });

/** A client on a fake clock: sleeping just advances time. */
async function setup(opts: { fetch: typeof fetch; maxPerMinute?: number; ttlMs?: number }) {
  const t = await tmpDir();
  cleanups.push(t.cleanup);
  const clock = { now: 1_000_000, sleeps: [] as number[] };
  const client = new AniListClient({
    dataDir: t.dir,
    fetch: opts.fetch,
    maxPerMinute: opts.maxPerMinute ?? 30,
    ttlMs: opts.ttlMs,
    now: () => clock.now,
    sleep: async (ms, signal) => {
      if (signal?.aborted) throw new CancelledError();
      clock.sleeps.push(ms);
      clock.now += ms;
    },
  });
  return { client, clock, dir: t.dir };
}

describe('AniListClient rate limiting & retries', () => {
  it('never exceeds the per-minute budget and keeps order', async () => {
    const seen: number[] = [];
    const { client, clock } = await setup({
      maxPerMinute: 3,
      fetch: (async () => {
        seen.push(clock.now);
        return ok({ n: 1 });
      }) as typeof fetch,
    });
    const start = clock.now;
    await Promise.all(Array.from({ length: 7 }, () => client.request('q', {})));
    for (let i = 3; i < seen.length; i++)
      expect(seen[i]! - seen[i - 3]!).toBeGreaterThanOrEqual(60_000);
    expect(clock.now - start).toBeGreaterThanOrEqual(120_000);
  });

  it('adopts the lower limit AniList reports in its headers', async () => {
    const { client } = await setup({
      maxPerMinute: 60,
      fetch: (async () =>
        ok({}, { 'x-ratelimit-limit': '30', 'x-ratelimit-remaining': '29' })) as typeof fetch,
    });
    await client.request('q', {});
    expect(client.currentLimit).toBe(30);
  });

  it('waits out a 429 using Retry-After, then succeeds', async () => {
    let calls = 0;
    const { client, clock } = await setup({
      fetch: (async () =>
        ++calls === 1
          ? new Response('{}', { status: 429, headers: { 'retry-after': '7' } })
          : ok({ done: true })) as typeof fetch,
    });
    expect(await client.request('q', {})).toEqual({ done: true });
    expect(calls).toBe(2);
    expect(Math.max(...clock.sleeps)).toBeGreaterThanOrEqual(7000);
  });

  it('gives up with RateLimitError after repeated 429s', async () => {
    const { client } = await setup({
      fetch: (async () =>
        new Response('{}', { status: 429, headers: { 'retry-after': '1' } })) as typeof fetch,
    });
    await expect(client.request('q', {})).rejects.toBeInstanceOf(RateLimitError);
  });

  it('retries 5xx with exponential backoff', async () => {
    let calls = 0;
    const { client, clock } = await setup({
      fetch: (async () =>
        ++calls < 3 ? new Response('x', { status: 503 }) : ok({ fine: 1 })) as typeof fetch,
    });
    await client.request('q', {});
    expect(clock.sleeps.filter((s) => s === 1000 || s === 2000)).toEqual([1000, 2000]);
  });

  it('does not retry GraphQL/4xx errors', async () => {
    let calls = 0;
    const { client } = await setup({
      fetch: (async () => {
        calls++;
        return new Response(JSON.stringify({ errors: [{ message: 'Not Found.', status: 404 }] }), {
          status: 404,
        });
      }) as typeof fetch,
    });
    await expect(client.request('q', {})).rejects.toMatchObject({ status: 404 });
    await expect(client.request('q', {})).rejects.toBeInstanceOf(AniListError);
    expect(calls).toBe(2);
  });

  it('reports offline after retries and can be cancelled', async () => {
    const { client } = await setup({
      fetch: (async () => {
        throw new TypeError('fetch failed');
      }) as typeof fetch,
    });
    await expect(client.request('q', {})).rejects.toBeInstanceOf(OfflineError);
    expect(client.offline).toBe(true);
    const ctl = new AbortController();
    ctl.abort();
    await expect(client.request('q', {}, ctl.signal)).rejects.toBeInstanceOf(CancelledError);
  });
});

describe('AniListClient cache & images', () => {
  it('serves from the disk cache within the TTL and stale data when offline', async () => {
    let calls = 0;
    let offline = false;
    const { client, clock } = await setup({
      ttlMs: 1000,
      fetch: (async () => {
        calls++;
        if (offline) throw new TypeError('down');
        return ok({ v: calls });
      }) as typeof fetch,
    });
    const a = await client.cachedRequest('media-1', 'q', {});
    const b = await client.cachedRequest('media-1', 'q', {});
    expect([a.cached, b.cached, calls]).toEqual([false, true, 1]);
    clock.now += 5000;
    offline = true;
    const stale = await client.cachedRequest<{ v: number }>('media-1', 'q', {});
    expect(stale).toMatchObject({ cached: true, stale: true, data: { v: 1 } });
    await expect(client.cachedRequest('media-2', 'q', {})).rejects.toBeInstanceOf(OfflineError);
  });

  it('only fetches images from the allow-listed host and caches them', async () => {
    let calls = 0;
    const { client, dir } = await setup({
      fetch: (async () => {
        calls++;
        return new Response(new Uint8Array([1, 2, 3]), {
          headers: { 'content-type': 'image/jpeg' },
        });
      }) as typeof fetch,
    });
    await expect(client.fetchImage('https://evil.example/x.jpg')).rejects.toThrow(/not allowed/);
    await expect(client.fetchImage('http://s4.anilist.co/x.jpg')).rejects.toThrow(/not allowed/);
    const name = await client.saveCover(42, 'https://s4.anilist.co/file/c/42.jpg');
    expect(name).toBe('42.jpg');
    expect([...(await fs.readFile(path.join(dir, 'covers', name)))]).toEqual([1, 2, 3]);
    await client.fetchImage('https://s4.anilist.co/file/c/42.jpg');
    expect(calls).toBe(1);
  });
});

describe('matcher', () => {
  const entry = (title: string, mediaType: string, library = 'reading') =>
    ({ title, altTitles: [] as string[], library, mediaType }) as never;

  it('decides what is linkable', () => {
    expect(planSearch(entry('x', 'manhwa'))).toMatchObject({
      linkable: true,
      type: 'MANGA',
      country: 'KR',
    });
    expect(planSearch(entry('x', 'manhua'))).toMatchObject({ country: 'CN' });
    expect(planSearch(entry('x', 'unknown'))).toMatchObject({ type: 'MANGA', country: null });
    expect(planSearch(entry('x', 'donghua', 'watching'))).toMatchObject({
      type: 'ANIME',
      country: 'CN',
    });
    expect(planSearch(entry('x', 'anime', 'watching'))).toMatchObject({ type: 'ANIME' });
    for (const t of ['tv', 'kdrama', 'cartoon', 'documentary'])
      expect(planSearch(entry('x', t, 'watching')).linkable).toBe(false);
  });

  it('ranks by title, country and format', () => {
    const e = entry('Lantern of the Hollow Crown', 'manhwa');
    const plan = planSearch(e);
    const ranked = rank(
      e,
      [
        media(1, 'Lantern of the Hollow Crown', { countryOfOrigin: 'JP' }),
        media(2, 'Lantern of the Hollow Crown', { countryOfOrigin: 'KR' }),
        media(3, 'Lantern of the Hollow Crown', { format: 'NOVEL', countryOfOrigin: 'KR' }),
        media(4, 'Something else entirely'),
      ],
      plan,
    );
    expect(ranked.map((c) => c.id)).toEqual([2, 3, 1, 4]);
    expect(ranked[0]!.confidence).toBe(1);
  });

  it('matches alternate titles', () => {
    const e = {
      title: 'Crown of Ash',
      altTitles: ['Kuroi Ou'],
      library: 'reading',
      mediaType: 'unknown',
    } as never;
    const c = rank(
      e,
      [
        media(9, 'Different Name', {
          title: { romaji: 'Different Name', english: null, native: 'Kuroi Ou' },
        }),
      ],
      planSearch(e),
    );
    expect(c[0]!.confidence).toBeGreaterThan(0.95);
  });

  it('auto-confirms only with a clear margin above the threshold', () => {
    const c = (confidence: number) => ({ confidence }) as never;
    expect(shouldAutoConfirm([c(0.99), c(0.5)], 0.92)).toBe(true);
    expect(shouldAutoConfirm([c(0.99), c(0.97)], 0.92)).toBe(false);
    expect(shouldAutoConfirm([c(0.9)], 0.92)).toBe(false);
    expect(shouldAutoConfirm([], 0.92)).toBe(false);
  });

  it('parses AniList references', () => {
    expect(parseAniListRef('12345')).toEqual({ id: 12345 });
    expect(parseAniListRef('https://anilist.co/manga/987/Some-Title/')).toEqual({
      id: 987,
      type: 'MANGA',
    });
    expect(parseAniListRef('anilist.co/anime/55')).toEqual({ id: 55, type: 'ANIME' });
    expect(parseAniListRef('not a ref')).toBeNull();
  });
});

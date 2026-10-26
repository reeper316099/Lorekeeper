import { createHash } from 'node:crypto';
import { promises as fs } from 'node:fs';
import path from 'node:path';
import { atomicWrite, stableStringify } from '../storage/atomic.js';

export class OfflineError extends Error {
  code = 'OFFLINE';
  constructor(message = 'AniList is unreachable (offline?)') {
    super(message);
  }
}
export class RateLimitError extends Error {
  code = 'RATE_LIMITED';
  constructor(public retryAfterSec: number) {
    super(`AniList rate limit hit; retry in ${retryAfterSec}s`);
  }
}
export class AniListError extends Error {
  code = 'ANILIST_ERROR';
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export class CancelledError extends Error {
  code = 'CANCELLED';
  constructor() {
    super('Cancelled');
  }
}

export interface ClientOptions {
  dataDir: string;
  endpoint?: string;
  fetch?: typeof fetch;
  /** AniList currently allows 30/min (90/min normally). The real limit from response headers wins if lower. */
  maxPerMinute?: number;
  ttlMs?: number;
  maxRetries?: number;
  timeoutMs?: number;
  now?: () => number;
  sleep?: (ms: number, signal?: AbortSignal) => Promise<void>;
}

export interface Cached<T> {
  data: T;
  cached: boolean;
  stale: boolean;
}

const WEEK = 7 * 24 * 3600_000;
export const IMAGE_HOSTS = new Set(['s4.anilist.co']);
const EXT_BY_TYPE: Record<string, string> = {
  'image/jpeg': 'jpg',
  'image/png': 'png',
  'image/webp': 'webp',
  'image/gif': 'gif',
};

const defaultSleep = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    if (signal?.aborted) return reject(new CancelledError());
    const t = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(t);
      reject(new CancelledError());
    };
    signal?.addEventListener('abort', onAbort, { once: true });
  });

/**
 * Rate-limited, cached, cancellable AniList GraphQL client.
 * Never logs query variables (they can contain titles from the user's library).
 */
export class AniListClient {
  private limit: number;
  private stamps: number[] = [];
  private blockedUntil = 0;
  private gate: Promise<void> = Promise.resolve();
  private readonly o: Required<Omit<ClientOptions, 'dataDir' | 'fetch'>> & {
    dataDir: string;
    fetch: typeof fetch;
  };
  /** Set when the last network attempt failed because AniList could not be reached. */
  offline = false;

  constructor(opts: ClientOptions) {
    this.o = {
      dataDir: opts.dataDir,
      // LOREKEEPER_ANILIST_ENDPOINT lets development and tests point at a local mock.
      endpoint:
        opts.endpoint ?? process.env.LOREKEEPER_ANILIST_ENDPOINT ?? 'https://graphql.anilist.co',
      fetch: opts.fetch ?? ((...a) => fetch(...a)),
      maxPerMinute: opts.maxPerMinute ?? 30,
      ttlMs: opts.ttlMs ?? WEEK,
      maxRetries: opts.maxRetries ?? 3,
      timeoutMs: opts.timeoutMs ?? 15_000,
      now: opts.now ?? Date.now,
      sleep: opts.sleep ?? defaultSleep,
    };
    this.limit = this.o.maxPerMinute;
  }

  get currentLimit(): number {
    return this.limit;
  }

  private async acquire(signal?: AbortSignal): Promise<void> {
    for (;;) {
      if (signal?.aborted) throw new CancelledError();
      const now = this.o.now();
      this.stamps = this.stamps.filter((t) => now - t < 60_000);
      if (this.blockedUntil > now) {
        await this.o.sleep(this.blockedUntil - now, signal);
      } else if (this.stamps.length >= this.limit) {
        await this.o.sleep(this.stamps[0]! + 60_000 - now + 5, signal);
      } else {
        this.stamps.push(now);
        return;
      }
    }
  }

  /** FIFO queue so concurrent callers share the rate budget in order. */
  private async schedule(signal?: AbortSignal): Promise<void> {
    const prev = this.gate;
    let release!: () => void;
    this.gate = new Promise<void>((r) => (release = r));
    try {
      await prev;
      await this.acquire(signal);
    } finally {
      release();
    }
  }

  async request<T>(
    query: string,
    variables: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<T> {
    let lastErr: unknown;
    for (let attempt = 0; attempt <= this.o.maxRetries; attempt++) {
      await this.schedule(signal);
      let res: Response;
      try {
        const timeout = AbortSignal.timeout(this.o.timeoutMs);
        res = await this.o.fetch(this.o.endpoint, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
          body: JSON.stringify({ query, variables }),
          signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
        });
      } catch (err) {
        if (signal?.aborted) throw new CancelledError();
        lastErr = err;
        if (attempt === this.o.maxRetries) {
          this.offline = true;
          throw new OfflineError();
        }
        await this.o.sleep(1000 * 2 ** attempt, signal);
        continue;
      }
      this.offline = false;

      const hdrLimit = Number(res.headers.get('x-ratelimit-limit'));
      if (hdrLimit > 0) this.limit = Math.min(this.o.maxPerMinute, hdrLimit);
      if (res.headers.get('x-ratelimit-remaining') === '0') {
        const reset = Number(res.headers.get('x-ratelimit-reset'));
        if (reset > 0) this.blockedUntil = Math.max(this.blockedUntil, reset * 1000);
      }

      if (res.status === 429) {
        const retryAfter = Number(res.headers.get('retry-after')) || 60;
        this.blockedUntil = this.o.now() + retryAfter * 1000;
        lastErr = new RateLimitError(retryAfter);
        if (attempt === this.o.maxRetries) throw lastErr;
        continue;
      }
      if (res.status >= 500) {
        lastErr = new AniListError(`AniList server error (${res.status})`, res.status);
        if (attempt === this.o.maxRetries) throw lastErr;
        await this.o.sleep(1000 * 2 ** attempt, signal);
        continue;
      }
      const json = (await res.json().catch(() => null)) as {
        data?: T;
        errors?: { message: string; status?: number }[];
      } | null;
      if (!res.ok || json?.errors?.length) {
        const e = json?.errors?.[0];
        throw new AniListError(
          e?.message ?? `AniList request failed (${res.status})`,
          e?.status ?? res.status,
        );
      }
      return json!.data as T;
    }
    throw lastErr instanceof Error ? lastErr : new OfflineError();
  }

  // ---------- disk cache ----------
  private cachePath(key: string) {
    return path.join(this.o.dataDir, 'anilist', 'cache', `${key.replace(/[^\w.-]+/g, '_')}.json`);
  }

  static hashKey(prefix: string, parts: unknown): string {
    return `${prefix}-${createHash('sha1').update(JSON.stringify(parts)).digest('hex').slice(0, 20)}`;
  }

  /** Store a response we already have (e.g. media from a search) so later lookups hit the cache. */
  async primeCache(key: string, data: unknown): Promise<void> {
    await atomicWrite(this.cachePath(key), stableStringify({ fetchedAt: this.o.now(), data }));
  }

  /** Fetch with a TTL disk cache. When offline, an expired entry is served (stale) rather than failing. */
  async cachedRequest<T>(
    key: string,
    query: string,
    variables: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<Cached<T>> {
    const file = this.cachePath(key);
    let hit: { fetchedAt: number; data: T } | null = null;
    try {
      hit = JSON.parse(await fs.readFile(file, 'utf8'));
    } catch {
      /* no cache */
    }
    if (hit && this.o.now() - hit.fetchedAt < this.o.ttlMs)
      return { data: hit.data, cached: true, stale: false };
    try {
      const data = await this.request<T>(query, variables, signal);
      await atomicWrite(file, stableStringify({ fetchedAt: this.o.now(), data }));
      return { data, cached: false, stale: false };
    } catch (err) {
      if (hit && (err instanceof OfflineError || err instanceof RateLimitError))
        return { data: hit.data, cached: true, stale: true };
      throw err;
    }
  }

  // ---------- images (allow-listed hosts only, cached on disk) ----------
  static allowedImage(url: string): URL | null {
    try {
      const u = new URL(url);
      return u.protocol === 'https:' && IMAGE_HOSTS.has(u.hostname) ? u : null;
    } catch {
      return null;
    }
  }

  private imageBase(href: string): string {
    return path.join(
      this.o.dataDir,
      'anilist',
      'cache',
      `img-${createHash('sha1').update(href).digest('hex').slice(0, 20)}`,
    );
  }

  // ---------- cleanup ----------
  /** Delete a cached cover (covers/<id>.<ext>). Only plain cover file names are accepted. */
  async removeCover(file: string): Promise<boolean> {
    if (!/^\d+\.(jpg|png|webp|gif)$/.test(file)) return false;
    return fs.rm(path.join(this.o.dataDir, 'covers', file)).then(
      () => true,
      () => false,
    );
  }

  /** Forget everything cached about one title: its metadata and the artwork downloaded for it. */
  async purgeMedia(id: number): Promise<void> {
    const file = this.cachePath(`media-${id}`);
    try {
      const cached = JSON.parse(await fs.readFile(file, 'utf8')) as {
        data?: {
          Media?: {
            coverImage?: { large?: string | null; extraLarge?: string | null };
            bannerImage?: string | null;
          };
        };
      };
      const m = cached.data?.Media;
      for (const url of [m?.coverImage?.large, m?.coverImage?.extraLarge, m?.bannerImage]) {
        const u = url ? AniListClient.allowedImage(url) : null;
        if (!u) continue;
        for (const ext of Object.values(EXT_BY_TYPE))
          await fs.rm(`${this.imageBase(u.href)}.${ext}`, { force: true });
      }
    } catch {
      /* nothing cached for this title */
    }
    await fs.rm(file, { force: true });
  }

  async cacheStats(): Promise<{ files: number; bytes: number }> {
    const dir = path.join(this.o.dataDir, 'anilist', 'cache');
    let files = 0;
    let bytes = 0;
    for (const name of await fs.readdir(dir).catch(() => [] as string[])) {
      const st = await fs.stat(path.join(dir, name)).catch(() => null);
      if (st?.isFile()) {
        files++;
        bytes += st.size;
      }
    }
    return { files, bytes };
  }

  /** Remove every cached response and image. Covers of matched titles live in covers/ and are untouched. */
  async clearCache(): Promise<{ files: number; bytes: number }> {
    const stats = await this.cacheStats();
    const dir = path.join(this.o.dataDir, 'anilist', 'cache');
    for (const name of await fs.readdir(dir).catch(() => [] as string[]))
      await fs.rm(path.join(dir, name), { force: true });
    return stats;
  }

  /** Delete cover files that no entry references any more. */
  async pruneCovers(inUse: Set<string>): Promise<number> {
    const dir = path.join(this.o.dataDir, 'covers');
    let removed = 0;
    for (const name of await fs.readdir(dir).catch(() => [] as string[])) {
      if (!inUse.has(name) && (await this.removeCover(name))) removed++;
    }
    return removed;
  }

  async fetchImage(
    url: string,
    signal?: AbortSignal,
  ): Promise<{ bytes: Buffer; contentType: string }> {
    const u = AniListClient.allowedImage(url);
    if (!u) throw new AniListError('Image host not allowed', 400);
    const base = this.imageBase(u.href);
    for (const [type, ext] of Object.entries(EXT_BY_TYPE)) {
      try {
        return { bytes: await fs.readFile(`${base}.${ext}`), contentType: type };
      } catch {
        /* try next */
      }
    }
    let res: Response;
    try {
      res = await this.o.fetch(u.href, {
        signal: signal
          ? AbortSignal.any([signal, AbortSignal.timeout(this.o.timeoutMs)])
          : AbortSignal.timeout(this.o.timeoutMs),
      });
    } catch {
      if (signal?.aborted) throw new CancelledError();
      throw new OfflineError();
    }
    const contentType = (res.headers.get('content-type') ?? '').split(';')[0]!.trim();
    const ext = EXT_BY_TYPE[contentType];
    if (!res.ok || !ext) throw new AniListError(`Image fetch failed (${res.status})`, res.status);
    const bytes = Buffer.from(await res.arrayBuffer());
    await atomicWrite(`${base}.${ext}`, bytes);
    return { bytes, contentType };
  }

  /** Cache a cover for a confirmed match as covers/<anilistId>.<ext>; returns the file name. */
  async saveCover(anilistId: number, url: string, signal?: AbortSignal): Promise<string> {
    const { bytes, contentType } = await this.fetchImage(url, signal);
    const name = `${anilistId}.${EXT_BY_TYPE[contentType] ?? 'jpg'}`;
    await atomicWrite(path.join(this.o.dataDir, 'covers', name), bytes);
    return name;
  }
}

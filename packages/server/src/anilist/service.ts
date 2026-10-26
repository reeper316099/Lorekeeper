import { promises as fs } from 'node:fs';
import path from 'node:path';
import { randomUUID } from 'node:crypto';
import { uuidv7 } from 'uuidv7';
import { EntrySchema, normalizeTitle, type Entry, type Status } from '@lorekeeper/shared';
import { HttpError, type Store } from '../storage/store.js';
import { beginUndoable, mergeInto, type Strategy } from '../io/importer.js';
import {
  AniListClient,
  AniListError,
  CancelledError,
  OfflineError,
  RateLimitError,
} from './client.js';
import type { Job, JobRegistry } from './jobs.js';
import { parseAniListRef, planSearch, rank, shouldAutoConfirm, type Candidate } from './matcher.js';
import {
  BY_ID_QUERY,
  LIST_QUERY,
  SEARCH_QUERY,
  type AniDate,
  type AniListEntry,
  type AniMedia,
  type ListCollection,
} from './queries.js';

export type QueueStatus = 'pending' | 'confirmed' | 'rejected' | 'never' | 'unlinkable';
export interface QueueItem {
  status: QueueStatus;
  candidates: Candidate[];
  suggestedTags: string[];
  auto?: boolean;
  reason?: string;
  updatedAt: string;
}
interface QueueFile {
  items: Record<string, QueueItem>;
}

const STATUS_MAP: Record<AniListEntry['status'], Status> = {
  CURRENT: 'in_progress',
  PLANNING: 'planned',
  COMPLETED: 'completed',
  DROPPED: 'dropped',
  PAUSED: 'on_hold',
  REPEATING: 'repeating',
};

const dateOf = (d: AniDate | null): string | undefined =>
  d?.year
    ? `${d.year}-${String(d.month ?? 1).padStart(2, '0')}-${String(d.day ?? 1).padStart(2, '0')}`
    : undefined;

const stripHtml = (s: string | null): string | undefined =>
  s
    ? s
        .replace(/<br\s*\/?>/gi, '\n')
        .replace(/<[^>]+>/g, '')
        .replace(/&amp;/g, '&')
        .replace(/&quot;/g, '"')
        .replace(/&#039;/g, "'")
        .trim() || undefined
    : undefined;

/** Genres plus well-ranked, non-spoiler tags. Offered as suggestions, never applied automatically. */
export function suggestedTagsFor(m: AniMedia): string[] {
  const tags = m.tags.filter((t) => !t.isMediaSpoiler && (t.rank ?? 0) >= 60).map((t) => t.name);
  return [...new Set([...m.genres, ...tags])].slice(0, 20);
}

export interface ListImportOptions {
  username: string;
  types: ('ANIME' | 'MANGA')[];
  strategy?: Strategy;
  animeCategoryId?: string;
  mangaCategoryId?: string;
}

interface Prepared {
  creates: Entry[];
  updates: Entry[];
  unchanged: number;
  skipped: number;
  covers: Map<string, string>; // entry id -> remote cover url
  warnings: string[];
  username: string;
  createdAt: number;
}

export class AniListService {
  private queue: QueueFile | null = null;
  private previews = new Map<string, Prepared>();

  constructor(
    private store: Store,
    readonly client: AniListClient,
    readonly jobs: JobRegistry,
  ) {}

  // ---------------- queue persistence ----------------
  private get queueFile() {
    return path.join(this.store.dir, 'anilist', 'match-queue.json');
  }

  async loadQueue(): Promise<QueueFile> {
    if (this.queue) return this.queue;
    try {
      const raw = JSON.parse(await fs.readFile(this.queueFile, 'utf8')) as QueueFile;
      this.queue = { items: raw.items ?? {} };
    } catch {
      this.queue = { items: {} };
    }
    return this.queue;
  }

  private async saveQueue(): Promise<void> {
    if (this.queue) await this.store.writeJson(this.queueFile, this.queue);
  }

  private async setItem(
    entryId: string,
    item: Partial<QueueItem> & { status: QueueStatus },
  ): Promise<QueueItem> {
    const q = await this.loadQueue();
    const next: QueueItem = {
      candidates: [],
      suggestedTags: [],
      ...q.items[entryId],
      ...item,
      updatedAt: new Date().toISOString(),
    };
    q.items[entryId] = next;
    return next;
  }

  async counts() {
    const q = await this.loadQueue();
    const out: Record<QueueStatus, number> = {
      pending: 0,
      confirmed: 0,
      rejected: 0,
      never: 0,
      unlinkable: 0,
    };
    for (const [id, it] of Object.entries(q.items)) if (this.store.getEntry(id)) out[it.status]++;
    const entries = this.store.all();
    return {
      queue: out,
      matched: entries.filter((e) => e.external.anilistId).length,
      total: entries.length,
    };
  }

  // ---------------- searching ----------------
  async searchMedia(
    search: string,
    type: 'ANIME' | 'MANGA',
    country: string | null,
    signal?: AbortSignal,
  ) {
    const vars = { search, type, country, perPage: 8 };
    const r = await this.client.cachedRequest<{ Page: { media: AniMedia[] } }>(
      AniListClient.hashKey('search', vars),
      SEARCH_QUERY,
      vars,
      signal,
    );
    for (const m of r.data.Page.media) await this.client.primeCache(`media-${m.id}`, { Media: m });
    return { media: r.data.Page.media, stale: r.stale };
  }

  async mediaById(id: number, signal?: AbortSignal): Promise<AniMedia> {
    const r = await this.client.cachedRequest<{ Media: AniMedia }>(
      `media-${id}`,
      BY_ID_QUERY,
      { id },
      signal,
    );
    return r.data.Media;
  }

  private async searchFor(entry: Entry, signal?: AbortSignal) {
    const plan = planSearch(entry);
    if (!plan.linkable) return { plan, candidates: [] as Candidate[], media: [] as AniMedia[] };
    let { media } = await this.searchMedia(entry.title, plan.type!, plan.country ?? null, signal);
    if (media.length === 0 && /[(:[]/.test(entry.title)) {
      const shorter = entry.title
        .replace(/\(.*?\)|\[.*?\]/g, '')
        .split(':')[0]!
        .trim();
      if (shorter && shorter !== entry.title)
        ({ media } = await this.searchMedia(shorter, plan.type!, plan.country ?? null, signal));
    }
    return { plan, media, candidates: rank(entry, media, plan, 5) };
  }

  /** Candidates for one entry (also used by "pick another" with a custom query). */
  async candidatesFor(entryId: string, query?: string, signal?: AbortSignal): Promise<Candidate[]> {
    const entry = this.requireEntry(entryId);
    const plan = planSearch(entry);
    if (!plan.linkable) return [];
    if (query?.trim()) {
      const { media } = await this.searchMedia(query.trim(), plan.type!, null, signal);
      return rank(entry, media, { ...plan, country: null }, 8);
    }
    return (await this.searchFor(entry, signal)).candidates;
  }

  private requireEntry(id: string): Entry {
    const e = this.store.getEntry(id);
    if (!e) throw new HttpError(404, 'NOT_FOUND', 'Entry not found');
    return e;
  }

  // ---------------- applying a match ----------------
  async applyMatch(
    entryId: string,
    media: AniMedia,
    opts: { confidence: number; auto?: boolean; signal?: AbortSignal },
  ): Promise<Entry> {
    const entry = this.requireEntry(entryId);
    let cover: string | undefined = entry.external.cover;
    const coverUrl = media.coverImage?.extraLarge ?? media.coverImage?.large;
    if (coverUrl) {
      try {
        cover = await this.client.saveCover(media.id, coverUrl, opts.signal);
      } catch (err) {
        if (err instanceof CancelledError) throw err; // otherwise keep going without a cover
      }
    }
    const titles = [
      media.title.romaji,
      media.title.english,
      media.title.native,
      ...media.synonyms,
    ].filter((t): t is string => !!t);
    const alt = [
      ...new Set([
        ...entry.altTitles,
        ...titles.filter((t) => normalizeTitle(t) !== normalizeTitle(entry.title)),
      ]),
    ];
    const countryType =
      media.countryOfOrigin === 'KR'
        ? 'manhwa'
        : media.countryOfOrigin === 'CN'
          ? 'manhua'
          : media.countryOfOrigin === 'JP'
            ? 'manga'
            : null;
    let mediaType = entry.mediaType;
    if (entry.library === 'reading' && mediaType === 'unknown')
      mediaType = media.format === 'NOVEL' ? 'novel' : (countryType ?? 'unknown');
    if (entry.library === 'watching' && mediaType === 'unknown')
      mediaType = media.countryOfOrigin === 'CN' ? 'donghua' : 'anime';

    const links = entry.links.some((l) => l.kind === 'anilist' || l.url === media.siteUrl)
      ? entry.links
      : [
          ...entry.links,
          ...(media.siteUrl
            ? [{ kind: 'anilist' as const, url: media.siteUrl, label: 'AniList', raw: null }]
            : []),
        ];
    const total = media.type === 'MANGA' ? media.chapters : media.episodes;
    const patch = {
      altTitles: alt,
      mediaType,
      links,
      ...(entry.totalUnits === null && total ? { totalUnits: total } : {}),
      external: {
        anilistId: media.id,
        ...(media.idMal ? { malId: media.idMal } : {}),
        matchConfidence: opts.confidence,
        matchLocked: true,
        ...(cover ? { cover } : {}),
        ...(media.bannerImage ? { bannerUrl: media.bannerImage } : {}),
        ...(media.format ? { format: media.format } : {}),
        ...(media.countryOfOrigin ? { countryOfOrigin: media.countryOfOrigin } : {}),
        ...(media.status ? { apiStatus: media.status } : {}),
        ...((media.seasonYear ?? media.startDate?.year)
          ? { year: (media.seasonYear ?? media.startDate?.year)! }
          : {}),
        ...(media.averageScore ? { averageScore: media.averageScore } : {}),
        ...(stripHtml(media.description) ? { description: stripHtml(media.description) } : {}),
      },
    };
    const updated = await this.store.updateEntry(entryId, patch);
    await this.setItem(entryId, {
      status: 'confirmed',
      auto: opts.auto ?? false,
      suggestedTags: suggestedTagsFor(media),
      candidates: [],
    });
    await this.saveQueue();
    return updated;
  }

  async confirm(entryId: string, mediaId: number): Promise<Entry> {
    const q = await this.loadQueue();
    const cand = q.items[entryId]?.candidates.find((c) => c.id === mediaId);
    return this.applyMatch(entryId, await this.mediaById(mediaId), {
      confidence: cand?.confidence ?? 1,
    });
  }

  async setByRef(entryId: string, ref: string): Promise<Entry> {
    const parsed = parseAniListRef(ref);
    if (!parsed) throw new HttpError(400, 'VALIDATION', 'Paste an AniList URL or numeric ID');
    try {
      return await this.applyMatch(entryId, await this.mediaById(parsed.id), { confidence: 1 });
    } catch (err) {
      if (err instanceof AniListError && err.status === 404)
        throw new HttpError(404, 'NOT_FOUND', 'AniList has no title with that ID');
      throw err;
    }
  }

  async reject(entryId: string): Promise<void> {
    await this.setItem(entryId, { status: 'rejected', candidates: [] });
    await this.saveQueue();
  }

  async never(entryId: string): Promise<void> {
    await this.setItem(entryId, { status: 'never', candidates: [] });
    await this.saveQueue();
  }

  /** Remove a link (and its lock) so the entry can be matched again. */
  async unmatch(entryId: string): Promise<Entry> {
    const e = this.requireEntry(entryId);
    const external = { ...e.external };
    for (const k of [
      'anilistId',
      'malId',
      'matchConfidence',
      'matchLocked',
      'cover',
      'bannerUrl',
      'format',
      'countryOfOrigin',
      'apiStatus',
      'year',
      'averageScore',
      'description',
    ])
      delete (external as Record<string, unknown>)[k];
    const q = await this.loadQueue();
    delete q.items[entryId];
    await this.saveQueue();
    const out = await this.store.updateEntry(entryId, {
      links: e.links.filter((l) => l.kind !== 'anilist'),
    });
    // `external` is merged shallowly by updateEntry, so replace it through the store directly.
    await this.store.replaceEntries([{ ...out, external }]);

    // Free what only this link needed: the cover file and the cached metadata/artwork.
    const others = this.store.all().filter((o) => o.id !== entryId);
    const { cover, anilistId } = e.external;
    if (cover && !others.some((o) => o.external.cover === cover))
      await this.client.removeCover(cover);
    if (anilistId && !others.some((o) => o.external.anilistId === anilistId))
      await this.client.purgeMedia(anilistId);
    return this.store.getEntry(entryId)!;
  }

  /** Clear cached AniList responses/images and drop covers nothing points at. */
  async clearCaches(): Promise<{ files: number; bytes: number; coversRemoved: number }> {
    const stats = await this.client.clearCache();
    const inUse = new Set(
      this.store
        .all()
        .map((e) => e.external.cover)
        .filter((c): c is string => !!c),
    );
    return { ...stats, coversRemoved: await this.client.pruneCovers(inUse) };
  }

  async addSuggestedTags(entryId: string): Promise<Entry> {
    const entry = this.requireEntry(entryId);
    const q = await this.loadQueue();
    let tags = q.items[entryId]?.suggestedTags ?? [];
    if (!tags.length && entry.external.anilistId)
      tags = suggestedTagsFor(await this.mediaById(entry.external.anilistId));
    return this.store.updateEntry(entryId, { tags: [...entry.tags, ...tags] });
  }

  // ---------------- matching job ----------------
  startMatching(entryIds?: string[]): Job {
    const targets = this.store
      .all()
      .filter((e) =>
        entryIds ? entryIds.includes(e.id) : !e.external.anilistId && !e.external.matchLocked,
      )
      .map((e) => e.id);
    const { job, signal } = this.jobs.create('match', targets.length);
    void this.runMatching(job, signal, targets, !!entryIds);
    return job;
  }

  private async runMatching(
    job: Job,
    signal: AbortSignal,
    ids: string[],
    explicit: boolean,
  ): Promise<void> {
    const q = await this.loadQueue();
    const threshold = this.store.settings.autoConfirmThreshold;
    let autoConfirmed = 0;
    let queued = 0;
    let unlinkable = 0;
    try {
      for (const id of ids) {
        if (signal.aborted) throw new CancelledError();
        const entry = this.store.getEntry(id);
        const prev = q.items[id];
        const skip =
          !entry ||
          entry.external.anilistId ||
          (!explicit &&
            prev &&
            ['never', 'rejected', 'confirmed', 'pending'].includes(prev.status));
        if (skip || !entry) {
          this.jobs.update(job, { done: job.done + 1 });
          continue;
        }
        const plan = planSearch(entry);
        if (!plan.linkable) {
          await this.setItem(id, { status: 'unlinkable', reason: plan.reason });
          unlinkable++;
        } else {
          const { candidates, media } = await this.searchFor(entry, signal);
          if (shouldAutoConfirm(candidates, threshold)) {
            const top = media.find((m) => m.id === candidates[0]!.id)!;
            await this.applyMatch(id, top, {
              confidence: candidates[0]!.confidence,
              auto: true,
              signal,
            });
            autoConfirmed++;
          } else {
            await this.setItem(id, { status: 'pending', candidates });
            queued++;
          }
        }
        this.jobs.update(job, { done: job.done + 1, message: `${entry.title}` });
        if (job.done % 10 === 0) await this.saveQueue();
      }
      await this.saveQueue();
      this.jobs.update(job, {
        state: 'done',
        message: null,
        result: { autoConfirmed, queued, unlinkable },
      });
    } catch (err) {
      await this.saveQueue().catch(() => undefined);
      const result = { autoConfirmed, queued, unlinkable };
      if (err instanceof CancelledError)
        this.jobs.update(job, { state: 'cancelled', message: 'Cancelled', result });
      else if (err instanceof OfflineError)
        this.jobs.update(job, {
          state: 'offline',
          message: 'AniList is unreachable. Check your connection and run matching again.',
          result,
        });
      else if (err instanceof RateLimitError)
        this.jobs.update(job, {
          state: 'error',
          message: `Rate limited by AniList. Try again in ${err.retryAfterSec}s.`,
          result,
        });
      else this.jobs.update(job, { state: 'error', message: (err as Error).message, result });
    }
  }

  async queueItems(status: QueueStatus) {
    const q = await this.loadQueue();
    return Object.entries(q.items)
      .filter(([id, it]) => it.status === status && this.store.getEntry(id))
      .map(([id, it]) => ({ entry: this.store.getEntry(id)!, ...it }))
      .sort((a, b) => a.entry.title.localeCompare(b.entry.title));
  }

  // ---------------- import from an AniList account ----------------
  private async fetchList(
    username: string,
    type: 'ANIME' | 'MANGA',
    signal?: AbortSignal,
  ): Promise<AniListEntry[]> {
    const out = new Map<number, AniListEntry>();
    for (let chunk = 1; chunk < 50; chunk++) {
      const data = await this.client.request<ListCollection>(
        LIST_QUERY,
        { userName: username, type, chunk },
        signal,
      );
      const col = data.MediaListCollection;
      for (const list of col.lists) {
        if (list.isCustomList) continue; // custom lists repeat entries already in the status lists
        for (const e of list.entries) if (!out.has(e.media.id)) out.set(e.media.id, e);
      }
      if (!col.hasNextChunk) break;
    }
    return [...out.values()];
  }

  private toEntry(
    e: AniListEntry,
    categoryId: string,
    library: 'reading' | 'watching',
    now: string,
  ): Entry {
    const m = e.media;
    const title = m.title.romaji ?? m.title.english ?? m.title.native ?? `AniList #${m.id}`;
    const alt = [m.title.english, m.title.native, ...m.synonyms].filter(
      (t): t is string => !!t && normalizeTitle(t) !== normalizeTitle(title),
    );
    const mediaType =
      library === 'reading'
        ? m.format === 'NOVEL'
          ? 'novel'
          : m.countryOfOrigin === 'KR'
            ? 'manhwa'
            : m.countryOfOrigin === 'CN'
              ? 'manhua'
              : 'manga'
        : m.countryOfOrigin === 'CN'
          ? 'donghua'
          : 'anime';
    const progress = e.progress ?? 0;
    return EntrySchema.parse({
      id: uuidv7(),
      library,
      categoryId,
      title,
      altTitles: [...new Set(alt)],
      mediaType,
      status: STATUS_MAP[e.status],
      score: e.score && e.score > 0 ? Math.min(10, Math.round(e.score)) : null,
      current:
        progress > 0
          ? { raw: String(progress), number: progress, season: null, kind: 'number' }
          : undefined,
      totalUnits: (library === 'reading' ? m.chapters : m.episodes) ?? null,
      links: m.siteUrl ? [{ kind: 'anilist', url: m.siteUrl, label: 'AniList', raw: null }] : [],
      notes: { general: e.notes?.trim() || null, extra1: null, extra2: null },
      external: {
        anilistId: m.id,
        ...(m.idMal ? { malId: m.idMal } : {}),
        matchConfidence: 1,
        matchLocked: true,
        ...(m.bannerImage ? { bannerUrl: m.bannerImage } : {}),
        ...(m.format ? { format: m.format } : {}),
        ...(m.countryOfOrigin ? { countryOfOrigin: m.countryOfOrigin } : {}),
        ...(m.status ? { apiStatus: m.status } : {}),
        ...((m.seasonYear ?? m.startDate?.year)
          ? { year: (m.seasonYear ?? m.startDate?.year)! }
          : {}),
        ...(m.averageScore ? { averageScore: m.averageScore } : {}),
        ...(stripHtml(m.description) ? { description: stripHtml(m.description) } : {}),
      },
      startedAt: dateOf(e.startedAt),
      completedAt: dateOf(e.completedAt),
      createdAt: now,
      updatedAt: now,
    });
  }

  async previewList(opts: ListImportOptions, signal?: AbortSignal) {
    const strategy = opts.strategy ?? 'fill';
    const now = new Date().toISOString();
    const warnings: string[] = [];
    const cats = this.store.categories;
    const pick = (
      id: string | undefined,
      library: 'reading' | 'watching',
      fallbackName: string,
    ) => {
      const c =
        cats.find((x) => x.id === id && x.library === library) ??
        cats.find((x) => x.library === library && x.name.toLowerCase() === fallbackName) ??
        cats.find((x) => x.library === library);
      return c;
    };
    const prepared: Prepared = {
      creates: [],
      updates: [],
      unchanged: 0,
      skipped: 0,
      covers: new Map(),
      warnings,
      username: opts.username,
      createdAt: Date.now(),
    };
    const existing = this.store.all();
    const byAniId = new Map(
      existing
        .filter((e) => e.external.anilistId)
        .map((e) => [`${e.library}:${e.external.anilistId}`, e]),
    );
    const byTitle = new Map<string, Entry>();
    for (const e of existing)
      for (const t of [e.title, ...e.altTitles])
        if (!byTitle.has(`${e.library}:${normalizeTitle(t)}`))
          byTitle.set(`${e.library}:${normalizeTitle(t)}`, e);
    const touched = new Map<string, Entry>();
    let total = 0;

    for (const type of opts.types) {
      const library = type === 'ANIME' ? 'watching' : 'reading';
      const cat = pick(
        type === 'ANIME' ? opts.animeCategoryId : opts.mangaCategoryId,
        library,
        type === 'ANIME' ? 'anime' : 'misc.',
      );
      if (!cat) {
        warnings.push(
          `No ${library} category exists to import ${type.toLowerCase()} into. Create one first.`,
        );
        continue;
      }
      let list: AniListEntry[];
      try {
        list = await this.fetchList(opts.username, type, signal);
      } catch (err) {
        if (err instanceof AniListError && err.status === 404)
          throw new HttpError(
            404,
            'USER_NOT_FOUND',
            `AniList has no public list for “${opts.username}”.`,
          );
        throw err;
      }
      for (const item of list) {
        total++;
        const incoming = this.toEntry(item, cat.id, library, now);
        const hit =
          byAniId.get(`${library}:${item.media.id}`) ??
          [incoming.title, ...incoming.altTitles]
            .map((t) => byTitle.get(`${library}:${normalizeTitle(t)}`))
            .find(Boolean);
        const coverUrl = item.media.coverImage?.extraLarge ?? item.media.coverImage?.large;
        if (hit && strategy !== 'keep_both') {
          const cur = touched.get(hit.id) ?? hit;
          if (strategy === 'skip') {
            prepared.skipped++;
            continue;
          }
          const merged = mergeInto(cur, incoming, strategy);
          const enriched = this.enrich(merged ?? cur, incoming, strategy === 'overwrite');
          if (!merged && enriched === cur) {
            prepared.unchanged++;
          } else {
            touched.set(hit.id, enriched);
            if (coverUrl && !enriched.external.cover) prepared.covers.set(hit.id, coverUrl);
          }
          continue;
        }
        prepared.creates.push(incoming);
        if (coverUrl) prepared.covers.set(incoming.id, coverUrl);
      }
    }
    prepared.updates = [...touched.values()];
    const previewId = randomUUID();
    this.previews.set(previewId, prepared);
    for (const [k, v] of this.previews)
      if (Date.now() - v.createdAt > 30 * 60_000) this.previews.delete(k);
    return {
      previewId,
      username: opts.username,
      counts: {
        fetched: total,
        create: prepared.creates.length,
        update: prepared.updates.length,
        unchanged: prepared.unchanged,
        skipped: prepared.skipped,
      },
      sample: prepared.creates.slice(0, 12).map((e) => ({
        title: e.title,
        status: e.status,
        score: e.score,
        progress: e.current.raw,
        library: e.library,
      })),
      warnings,
    };
  }

  /** After merging fields, make sure the AniList identity is attached to an existing entry too. */
  private enrich(e: Entry, incoming: Entry, overwrite: boolean): Entry {
    const external = overwrite
      ? { ...e.external, ...incoming.external }
      : { ...incoming.external, ...e.external };
    const alt = [...new Set([...e.altTitles, ...incoming.altTitles])].filter(
      (t) => normalizeTitle(t) !== normalizeTitle(e.title),
    );
    const links = e.links.some((l) => l.kind === 'anilist')
      ? e.links
      : [...e.links, ...incoming.links.filter((l) => l.kind === 'anilist')];
    const next: Entry = {
      ...e,
      external,
      altTitles: alt,
      links,
      totalUnits: e.totalUnits ?? incoming.totalUnits,
    };
    const same =
      JSON.stringify([next.external, next.altTitles, next.links, next.totalUnits]) ===
      JSON.stringify([e.external, e.altTitles, e.links, e.totalUnits]);
    if (same) return e;
    return { ...next, updatedAt: new Date().toISOString() };
  }

  startListImport(previewId: string): Job {
    const prepared = this.previews.get(previewId);
    if (!prepared) throw new HttpError(404, 'NOT_FOUND', 'Preview expired. Run the preview again.');
    const steps = prepared.covers.size + 1;
    const { job, signal } = this.jobs.create('import', steps);
    void (async () => {
      try {
        const urls = [...prepared.covers.entries()];
        const files = new Map<string, string>();
        let next = 0;
        const worker = async () => {
          while (next < urls.length) {
            const [id, url] = urls[next++]!;
            const anilistId = [...prepared.creates, ...prepared.updates].find((e) => e.id === id)
              ?.external.anilistId;
            try {
              if (anilistId) files.set(id, await this.client.saveCover(anilistId, url, signal));
            } catch (err) {
              if (err instanceof CancelledError) throw err; // a missing cover is not fatal
            }
            this.jobs.update(job, { done: job.done + 1, message: 'Downloading covers' });
          }
        };
        await Promise.all([worker(), worker(), worker()]);
        if (signal.aborted) throw new CancelledError();

        const withCover = (e: Entry): Entry =>
          files.has(e.id) ? { ...e, external: { ...e.external, cover: files.get(e.id) } } : e;
        const creates = prepared.creates.map(withCover);
        const updates = prepared.updates.map(withCover);
        const cats = [...new Set([...creates, ...updates].map((e) => e.categoryId))];
        const undo = await beginUndoable(this.store, cats);
        if (creates.length) await this.store.insertEntries(creates);
        if (updates.length) await this.store.replaceEntries(updates);
        this.previews.delete(previewId);
        this.jobs.update(job, {
          state: 'done',
          done: steps,
          message: null,
          result: {
            created: creates.length,
            updated: updates.length,
            unchanged: prepared.unchanged,
            skipped: prepared.skipped,
            undoToken: undo.id,
          },
        });
      } catch (err) {
        if (err instanceof CancelledError)
          this.jobs.update(job, {
            state: 'cancelled',
            message: 'Cancelled. Nothing was imported.',
          });
        else if (err instanceof OfflineError)
          this.jobs.update(job, { state: 'offline', message: 'AniList is unreachable.' });
        else this.jobs.update(job, { state: 'error', message: (err as Error).message });
      }
    })();
    return job;
  }
}

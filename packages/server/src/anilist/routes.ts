import { promises as fs } from 'node:fs';
import path from 'node:path';
import type { FastifyInstance } from 'fastify';
import { z } from 'zod';
import { HttpError, type Store } from '../storage/store.js';
import {
  AniListClient,
  CancelledError,
  OfflineError,
  RateLimitError,
  type ClientOptions,
} from './client.js';
import { JobRegistry } from './jobs.js';
import { AniListService } from './service.js';

const CONTENT_TYPES: Record<string, string> = {
  jpg: 'image/jpeg',
  png: 'image/png',
  webp: 'image/webp',
  gif: 'image/gif',
};

export interface AniListRouteOptions {
  /** Test seam: supply a client (e.g. with a mocked fetch). */
  client?: (store: Store) => AniListClient;
  clientOptions?: Partial<ClientOptions>;
}

/** Map network-ish failures to friendly HTTP errors the UI can show as "offline". */
function friendly<T>(p: Promise<T>): Promise<T> {
  return p.catch((err) => {
    if (err instanceof OfflineError)
      throw new HttpError(503, 'OFFLINE', 'AniList is unreachable. Check your connection.');
    if (err instanceof RateLimitError)
      throw new HttpError(429, 'RATE_LIMITED', err.message, { retryAfterSec: err.retryAfterSec });
    if (err instanceof CancelledError) throw new HttpError(499, 'CANCELLED', 'Cancelled');
    throw err;
  });
}

export function registerAniListRoutes(opts: AniListRouteOptions = {}) {
  return (app: FastifyInstance, store: Store): void => {
    const client =
      opts.client?.(store) ?? new AniListClient({ dataDir: store.dir, ...opts.clientOptions });
    const jobs = new JobRegistry((job) => store.emit('job', job));
    const svc = new AniListService(store, client, jobs);
    const idParam = (req: { params: unknown }) => (req.params as { id: string }).id;

    app.get('/anilist/status', async () => ({
      ...(await svc.counts()),
      offline: client.offline,
      requestsPerMinute: client.currentLimit,
      autoConfirmThreshold: store.settings.autoConfirmThreshold,
      matchJob: jobs.running('match') ?? null,
      importJob: jobs.running('import') ?? null,
    }));

    app.post('/anilist/match/run', async (req) => {
      const b = z.object({ entryIds: z.array(z.string()).optional() }).parse(req.body ?? {});
      const running = jobs.running('match');
      if (running) return running;
      return svc.startMatching(b.entryIds);
    });

    app.get('/anilist/jobs/:id', async (req) => {
      const j = jobs.get(idParam(req));
      if (!j) throw new HttpError(404, 'NOT_FOUND', 'Job not found');
      return j;
    });
    app.post('/anilist/jobs/:id/cancel', async (req) => ({ cancelled: jobs.cancel(idParam(req)) }));

    app.get('/anilist/queue', async (req) => {
      const { status } = z
        .object({
          status: z
            .enum(['pending', 'confirmed', 'rejected', 'never', 'unlinkable'])
            .default('pending'),
        })
        .parse(req.query);
      return svc.queueItems(status);
    });

    app.get('/anilist/search', async (req) => {
      const q = z.object({ entryId: z.string(), q: z.string().optional() }).parse(req.query);
      return friendly(svc.candidatesFor(q.entryId, q.q));
    });

    app.post('/anilist/match/:id/confirm', async (req) => {
      const { mediaId } = z.object({ mediaId: z.number().int().positive() }).parse(req.body);
      return friendly(svc.confirm(idParam(req), mediaId));
    });
    app.post('/anilist/match/:id/set', async (req) => {
      const { ref } = z.object({ ref: z.string().min(1) }).parse(req.body);
      return friendly(svc.setByRef(idParam(req), ref));
    });
    app.post('/anilist/match/:id/reject', async (req) => {
      await svc.reject(idParam(req));
      return { ok: true };
    });
    app.post('/anilist/match/:id/never', async (req) => {
      await svc.never(idParam(req));
      return { ok: true };
    });
    app.post('/anilist/match/:id/retry', async (req) => {
      const id = idParam(req);
      const cands = await friendly(svc.candidatesFor(id));
      const q = await svc.loadQueue();
      q.items[id] = {
        status: 'pending',
        candidates: cands,
        suggestedTags: [],
        updatedAt: new Date().toISOString(),
      };
      await store.writeJson(path.join(store.dir, 'anilist', 'match-queue.json'), q);
      return cands;
    });
    app.post('/anilist/entries/:id/unmatch', async (req) => svc.unmatch(idParam(req)));
    app.post('/anilist/entries/:id/suggested-tags', async (req) =>
      friendly(svc.addSuggestedTags(idParam(req))),
    );

    const ListBody = z.object({
      username: z
        .string()
        .trim()
        .min(1)
        .max(40)
        .regex(/^[\w-]+$/, 'Usernames use letters, numbers, _ and -'),
      types: z
        .array(z.enum(['ANIME', 'MANGA']))
        .min(1)
        .default(['ANIME', 'MANGA']),
      strategy: z.enum(['skip', 'fill', 'overwrite', 'keep_both']).optional(),
      animeCategoryId: z.string().optional(),
      mangaCategoryId: z.string().optional(),
    });
    app.post('/anilist/import/preview', async (req) =>
      friendly(svc.previewList(ListBody.parse(req.body))),
    );
    app.post('/anilist/import/commit', async (req) =>
      svc.startListImport(z.object({ previewId: z.string() }).parse(req.body).previewId),
    );

    // Images: <img> cannot send an Authorization header, so these accept ?token= (see app.ts).
    app.get('/anilist/cover/:file', async (req, reply) => {
      const file = (req.params as { file: string }).file;
      const m = /^(\d+)\.(jpg|png|webp|gif)$/.exec(file);
      if (!m) throw new HttpError(400, 'VALIDATION', 'Invalid cover name');
      const bytes = await fs.readFile(path.join(store.dir, 'covers', file)).catch(() => null);
      if (!bytes) throw new HttpError(404, 'NOT_FOUND', 'No such cover');
      return reply
        .header('content-type', CONTENT_TYPES[m[2]!]!)
        .header('cache-control', 'private, max-age=86400')
        .send(bytes);
    });
    app.get('/anilist/image', async (req, reply) => {
      const { url } = z.object({ url: z.string().url() }).parse(req.query);
      if (!AniListClient.allowedImage(url))
        throw new HttpError(400, 'VALIDATION', 'Image host is not allowed');
      const { bytes, contentType } = await friendly(client.fetchImage(url));
      return reply
        .header('content-type', contentType)
        .header('cache-control', 'private, max-age=86400')
        .send(bytes);
    });
  };
}

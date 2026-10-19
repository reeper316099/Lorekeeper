import { timingSafeEqual, randomUUID } from 'node:crypto';
import Fastify, { type FastifyInstance } from 'fastify';
import cors from '@fastify/cors';
import { z, ZodError } from 'zod';
import {
  EntrySchema,
  LibrarySchema,
  ScalesSchema,
  SettingsSchema,
  StatusSchema,
  suggestTagMerges,
  titleSimilarity,
} from '@lorekeeper/shared';
import { EntryQuerySchema, SearchIndex, normalizeQueryKeys, queryEntries } from '../search.js';
import { overview } from '../stats.js';
import { HttpError, type Store } from '../storage/store.js';
import { isValidBackupName } from '../storage/backups.js';

export interface AppOptions {
  store: Store;
  token: string;
  /** Exact origins allowed by CORS (the packaged app origin, Vite dev server). */
  allowedOrigins?: string[];
  logger?: boolean;
  /** Extra route modules (importers/exporters) registered under /api/v1. */
  extend?: (app: FastifyInstance, store: Store) => void | Promise<void>;
}

const ProgressOp = z
  .object({ delta: z.number().int().optional(), set: z.union([z.string(), z.number()]).optional() })
  .refine((v) => v.delta !== undefined || v.set !== undefined, 'delta or set required');

export async function buildApp(opts: AppOptions): Promise<FastifyInstance> {
  const { store } = opts;
  const app = Fastify({
    logger: opts.logger ? { level: 'info', redact: ['req.headers.authorization'] } : false,
    genReqId: () => randomUUID(),
  });
  const index = new SearchIndex(store);
  const tokenBuf = Buffer.from(opts.token);
  const allowed = opts.allowedOrigins ?? [];

  await app.register(cors, {
    origin: (origin, cb) => cb(null, !origin || allowed.includes(origin)),
    allowedHeaders: ['Authorization', 'Content-Type'],
    methods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'],
  });

  app.addHook('onRequest', async (req, reply) => {
    reply.header('x-request-id', req.id);
    if (req.method === 'OPTIONS') return;
    const url = req.url.split('?')[0]!;
    const header = req.headers.authorization?.replace(/^Bearer\s+/i, '');
    const qs = url.endsWith('/events') ? (req.query as { token?: string }).token : undefined;
    const given = Buffer.from(header ?? qs ?? '');
    if (given.length !== tokenBuf.length || !timingSafeEqual(given, tokenBuf))
      throw new HttpError(401, 'UNAUTHORIZED', 'Missing or invalid token');
  });

  app.setErrorHandler((err: Error, req, reply) => {
    if (err instanceof ZodError) {
      return reply
        .status(400)
        .send({ error: { code: 'VALIDATION', message: 'Invalid request', details: err.issues } });
    }
    if (err instanceof HttpError) {
      return reply
        .status(err.status)
        .send({ error: { code: err.code, message: err.message, details: err.details } });
    }
    const status = (err as { statusCode?: number }).statusCode ?? 500;
    if (status >= 500) req.log.error({ err: err.message }, 'unhandled');
    return reply.status(status).send({
      error: {
        code: status >= 500 ? 'INTERNAL' : 'BAD_REQUEST',
        message: status >= 500 ? 'Internal error' : err.message,
        details: null,
      },
    });
  });
  app.setNotFoundHandler((_req, reply) =>
    reply
      .status(404)
      .send({ error: { code: 'NOT_FOUND', message: 'Route not found', details: null } }),
  );

  const route = async (api: FastifyInstance) => {
    api.get('/health', async () => ({ ok: true }));
    api.get('/meta', async () => ({
      ...store.meta,
      entries: store.all().length,
      dataDir: store.dir,
    }));

    // ----- entries -----
    api.get('/entries', async (req) =>
      queryEntries(
        store,
        index,
        EntryQuerySchema.parse(normalizeQueryKeys(req.query as Record<string, unknown>)),
      ),
    );
    api.post('/entries', async (req, reply) => {
      const body = z
        .object({ title: z.string().min(1), categoryId: z.string().min(1) })
        .passthrough()
        .parse(req.body);
      const rest = { ...(body as Record<string, unknown>) };
      for (const k of ['id', 'createdAt', 'updatedAt', 'library']) delete rest[k];
      reply.status(201);
      return store.createEntry(rest as never);
    });
    api.post('/entries/restore', async (req, reply) => {
      reply.status(201);
      return store.restoreEntry(EntrySchema.parse(req.body));
    });
    api.get('/entries/:id/activity', async (req) => {
      const id = (req.params as { id: string }).id;
      return (await store.readActivity()).filter((a) => a.entryId === id).reverse();
    });
    api.post('/entries/bulk', async (req) => {
      const body = z
        .object({
          ids: z.array(z.string()).min(1),
          action: z.enum(['set_status', 'add_tag', 'remove_tag', 'move', 'delete']),
          value: z.string().optional(),
        })
        .parse(req.body);
      if (body.action === 'set_status') StatusSchema.parse(body.value);
      if (body.action !== 'delete' && !body.value)
        throw new HttpError(400, 'VALIDATION', 'value required');
      return store.bulk(body);
    });
    api.get('/entries/:id', async (req) => {
      const e = store.getEntry((req.params as { id: string }).id);
      if (!e) throw new HttpError(404, 'NOT_FOUND', 'Entry not found');
      return e;
    });
    api.patch('/entries/:id', async (req) =>
      store.updateEntry(
        (req.params as { id: string }).id,
        z.record(z.string(), z.unknown()).parse(req.body),
      ),
    );
    api.delete('/entries/:id', async (req) => store.deleteEntry((req.params as { id: string }).id));
    api.post('/entries/:id/progress', async (req) =>
      store.setProgress((req.params as { id: string }).id, ProgressOp.parse(req.body)),
    );
    api.post('/entries/:id/move', async (req) =>
      store.moveEntry(
        (req.params as { id: string }).id,
        z.object({ categoryId: z.string() }).parse(req.body).categoryId,
      ),
    );
    api.get('/entries/:id/similar', async (req) => {
      const e = store.getEntry((req.params as { id: string }).id);
      if (!e) throw new HttpError(404, 'NOT_FOUND', 'Entry not found');
      return store
        .all()
        .filter(
          (o) =>
            o.id !== e.id && o.library === e.library && titleSimilarity(o.title, e.title) >= 0.8,
        )
        .map((o) => ({ id: o.id, title: o.title }));
    });

    // ----- search -----
    api.get('/search', async (req) => {
      const { q, limit } = z
        .object({
          q: z.string().default(''),
          limit: z.coerce.number().int().min(1).max(100).default(20),
        })
        .parse(req.query);
      const hits = index.search(q, limit);
      const byId = new Map(store.all().map((e) => [e.id, e]));
      const lc = q.trim().toLowerCase();
      return {
        entries: hits.map((h) => ({ entry: byId.get(h.id)!, score: h.score, terms: h.terms })),
        tags: lc ? store.tags.filter((t) => t.name.toLowerCase().includes(lc)).slice(0, 8) : [],
        categories: lc ? store.categories.filter((c) => c.name.toLowerCase().includes(lc)) : [],
      };
    });

    // ----- categories -----
    api.get('/categories', async () =>
      [...store.categories]
        .sort((a, b) => a.library.localeCompare(b.library) || a.order - b.order)
        .map((c) => ({ ...c, count: store.entriesOf(c.id).length })),
    );
    api.post('/categories', async (req, reply) => {
      const b = z
        .object({
          library: LibrarySchema,
          name: z.string().min(1),
          color: z
            .string()
            .regex(/^#[0-9a-fA-F]{6}$/)
            .optional(),
        })
        .parse(req.body);
      reply.status(201);
      return store.createCategory(b);
    });
    api.post('/categories/reorder', async (req) => {
      const b = z.object({ library: LibrarySchema, ids: z.array(z.string()) }).parse(req.body);
      await store.reorderCategories(b.library, b.ids);
      return { ok: true };
    });
    api.patch('/categories/:id', async (req) =>
      store.updateCategory(
        (req.params as { id: string }).id,
        z
          .object({
            name: z.string().min(1).optional(),
            color: z
              .string()
              .regex(/^#[0-9a-fA-F]{6}$/)
              .optional(),
            order: z.number().int().optional(),
          })
          .parse(req.body),
      ),
    );
    api.delete('/categories/:id', async (req) => {
      const { moveTo } = z.object({ moveTo: z.string().optional() }).parse(req.query);
      await store.deleteCategory((req.params as { id: string }).id, moveTo);
      return { ok: true };
    });

    // ----- tags -----
    api.get('/tags', async () => {
      const usage = store.tagUsage();
      return store.tags
        .map((t) => ({ ...t, count: usage.get(t.name) ?? 0 }))
        .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
    });
    api.get('/tags/suggestions', async () => suggestTagMerges(store.tagUsage()));
    api.post('/tags/merge', async (req) => {
      const b = z
        .object({ from: z.array(z.string()).min(1), to: z.string().min(1) })
        .parse(req.body);
      return store.mergeTags(b.from, b.to);
    });
    api.patch('/tags/:name', async (req) =>
      store.updateTag(
        decodeURIComponent((req.params as { name: string }).name),
        z
          .object({
            name: z.string().optional(),
            color: z.string().optional(),
            hidden: z.boolean().optional(),
          })
          .parse(req.body),
      ),
    );

    // ----- stats -----
    api.get('/stats/overview', async (req) =>
      overview(store, z.object({ library: LibrarySchema.optional() }).parse(req.query).library),
    );

    // ----- settings & scales -----
    api.get('/settings', async () => store.settings);
    api.put('/settings', async (req) => store.saveSettings(SettingsSchema.parse(req.body)));
    api.get('/scales', async () => store.scales);
    api.put('/scales', async (req) => store.saveScales(ScalesSchema.parse(req.body)));

    // ----- backups -----
    api.get('/backups', async () => store.listBackups());
    api.post('/backups', async (_req, reply) => {
      reply.status(201);
      return store.backup('manual');
    });
    api.post('/backup/restore', async (req) => {
      const { name } = z.object({ name: z.string() }).parse(req.body);
      if (!isValidBackupName(name)) throw new HttpError(400, 'VALIDATION', 'Invalid backup name');
      await store.restoreBackup(name);
      return { ok: true };
    });

    // ----- realtime -----
    api.get('/events', (req, reply) => {
      reply.hijack();
      const res = reply.raw;
      const origin = req.headers.origin;
      res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
        // The reply is hijacked, so CORS headers must be set by hand.
        ...(origin && allowed.includes(origin)
          ? { 'Access-Control-Allow-Origin': origin, Vary: 'Origin' }
          : {}),
      });
      const send = (event: string, data: unknown) =>
        res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
      send('hello', { version: store.version });
      const on = (ev: string) => {
        const h = (d?: unknown) => send(ev, { version: store.version, ...(d as object) });
        store.on(ev, h);
        return () => store.off(ev, h);
      };
      const offs = ['change', 'external-change', 'file-error', 'file-restored'].map(on);
      const ping = setInterval(() => res.write(': ping\n\n'), 25000);
      req.raw.on('close', () => {
        clearInterval(ping);
        offs.forEach((f) => f());
      });
    });

    await opts.extend?.(api, store);
  };

  await app.register(route, { prefix: '/api/v1' });
  return app;
}

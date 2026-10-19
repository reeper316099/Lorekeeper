import { promises as fs } from 'node:fs';
import path from 'node:path';
import multipart from '@fastify/multipart';
import type { FastifyInstance } from 'fastify';
import { z } from 'zod';
import { LibrarySchema, StatusSchema } from '@lorekeeper/shared';
import { EntryQuerySchema, SearchIndex, normalizeQueryKeys, queryEntries } from '../search.js';
import { HttpError, type Store } from '../storage/store.js';
import { createBackup, readBackup } from '../storage/backups.js';
import { zipSync } from 'fflate';
import { commitImport, previewImport, stageUpload, undoImport } from './importer.js';
import { csvTemplate, exportCsv, exportXlsx } from './export.js';
import { FIELDS } from './columns.js';

const FieldSchema = z.enum(FIELDS as [string, ...string[]]).nullable();
const OptionsSchema = z.object({
  strategy: z.enum(['skip', 'fill', 'overwrite', 'keep_both']).optional(),
  library: LibrarySchema.optional(),
  category: z.string().optional(),
  sheets: z
    .record(
      z.string(),
      z.object({
        include: z.boolean().optional(),
        library: LibrarySchema.optional(),
        category: z.string().optional(),
        mapping: z.array(FieldSchema).optional(),
      }),
    )
    .optional(),
  statusMap: z.record(z.string(), StatusSchema).optional(),
});

export function registerIoRoutes(app: FastifyInstance, store: Store): void {
  void app.register(multipart, { limits: { fileSize: 50 * 1024 * 1024, files: 1 } });

  app.post('/import/upload', async (req, reply) => {
    const file = await req.file();
    if (!file) throw new HttpError(400, 'VALIDATION', 'No file uploaded');
    if (!/\.(xlsx|xlsm|csv|tsv|txt)$/i.test(file.filename))
      throw new HttpError(400, 'VALIDATION', 'Unsupported file type');
    const data = await file.toBuffer();
    const staged = await stageUpload(store, file.filename, data);
    reply.status(201);
    return { importId: staged.id, fileName: staged.fileName };
  });

  /** Parsed rows + mapping + warnings, no writes to the library. */
  app.post('/import/preview', async (req) => {
    const b = z
      .object({ importId: z.string(), options: OptionsSchema.default({}) })
      .parse(req.body);
    return previewImport(store, b.importId, b.options as never);
  });
  app.post('/import/commit', async (req) => {
    const b = z
      .object({ importId: z.string(), options: OptionsSchema.default({}) })
      .parse(req.body);
    return commitImport(store, b.importId, b.options as never);
  });
  app.post('/import/undo/:id', async (req) => undoImport(store, (req.params as { id: string }).id));

  const index = new SearchIndex(store);
  const send = (
    reply: import('fastify').FastifyReply,
    name: string,
    type: string,
    body: Buffer | Uint8Array,
  ) =>
    reply
      .header('content-type', type)
      .header('content-disposition', `attachment; filename="${name}"`)
      .send(Buffer.from(body));

  app.get('/export/xlsx', async (req, reply) => {
    const { library } = z.object({ library: LibrarySchema }).parse(req.query);
    return send(
      reply,
      `${library}-tracker.xlsx`,
      'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      await exportXlsx(store, library),
    );
  });

  app.get('/export/csv', async (req, reply) => {
    const raw = normalizeQueryKeys(req.query as Record<string, unknown>);
    const { library } = z.object({ library: LibrarySchema }).parse(raw);
    const q = EntryQuerySchema.parse({ ...raw, library });
    const all = [];
    for (let page = 1; ; page++) {
      const r = queryEntries(store, index, { ...q, limit: 1000, page });
      all.push(...r.items);
      if (all.length >= r.total || r.items.length === 0) break;
    }
    return send(reply, `${library}.csv`, 'text/csv; charset=utf-8', exportCsv(store, library, all));
  });

  app.get('/export/csv-template', async (req, reply) => {
    const { library } = z.object({ library: LibrarySchema }).parse(req.query);
    return send(reply, `${library}-template.csv`, 'text/csv; charset=utf-8', csvTemplate(library));
  });

  /** Full backup zip of config/ + data/ + activity/. */
  app.get('/export/json', async (_req, reply) => {
    const info = await createBackup(store.dir, 'export', 1000);
    const files = await readBackup(store.dir, info.name);
    return send(reply, info.name, 'application/zip', zipSync(files));
  });

  app.get('/imports/:id/report', async (req) => {
    const id = (req.params as { id: string }).id;
    if (!/^\d{8}-\d{6}-[0-9a-f]{6}$/.test(id))
      throw new HttpError(400, 'VALIDATION', 'Invalid import id');
    const text = await fs
      .readFile(path.join(store.importsDir, id, 'import-report.json'), 'utf8')
      .catch(() => null);
    if (!text) throw new HttpError(404, 'NOT_FOUND', 'No report');
    return JSON.parse(text);
  });
}

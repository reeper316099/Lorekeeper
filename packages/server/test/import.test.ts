import { readFileSync, existsSync, readdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, describe, expect, it } from 'vitest';
import { buildApp, Store } from '../src/index.js';
import { commitImport, previewImport, stageUpload, undoImport } from '../src/io/importer.js';
import { exportCsv, exportXlsx } from '../src/io/export.js';
import { readCsv, readXlsx } from '../src/io/read.js';
import { registerIoRoutes } from '../src/io/routes.js';
import { parseScore } from '@lorekeeper/shared';
import { TOKEN, makeStore } from './helpers.js';

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', '..', '..');
const SYN = path.join(root, 'fixtures', 'synthetic');
const PRIVATE = path.join(root, 'private-fixtures');
const expected = JSON.parse(readFileSync(path.join(SYN, 'expected.json'), 'utf8'));

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const c of cleanups.splice(0)) await c();
});
async function fresh() {
  const s = await makeStore();
  cleanups.push(async () => {
    await s.store.close();
    await s.cleanup();
  });
  return s.store;
}

async function importFile(store: Store, file: string, opts = {}) {
  const staged = await stageUpload(store, path.basename(file), readFileSync(file));
  return commitImport(store, staged.id, opts);
}

type Summary = {
  total: number;
  byCategory: Record<string, number>;
  byRawStatus: Record<string, number>;
  tagsRaw: number;
  tagsNormalized: number;
};
function expectCounts(r: Awaited<ReturnType<typeof commitImport>>, exp: Summary) {
  expect(r.summary.entries).toBe(exp.total);
  expect(r.counts.created).toBe(exp.total);
  expect(r.summary.byCategory).toEqual(exp.byCategory);
  expect(r.summary.byRawStatus).toEqual(exp.byRawStatus);
  expect(r.summary.distinctTagsRaw).toBe(exp.tagsRaw);
  expect(r.summary.distinctTagsNormalized).toBe(exp.tagsNormalized);
}

describe('synthetic fixtures', () => {
  const readingFile = path.join(SYN, 'Reading_Tracker.xlsx');
  const watchingFile = path.join(SYN, 'Watching_Tracker.xlsx');

  it('reading workbook: exact counts and structure', async () => {
    const store = await fresh();
    const r = await importFile(store, readingFile);
    expectCounts(r, expected.reading);
    expect(store.categories.filter((c) => c.library === 'reading').map((c) => c.name)).toEqual([
      'Romance',
      'Isekai',
      'Action',
      'Fantasy',
      'Misc.',
    ]);
    expect(store.categories.find((c) => c.name === 'Romance')!.color).toBe('#8B2252');
    expect(r.warnings.filter((w) => w.code === 'SHEET_SKIPPED').map((w) => w.sheet)).toEqual([
      'Add Category - Instructions',
    ]);
    expect(r.warnings.some((w) => w.code === 'NEAR_DUPLICATE')).toBe(true);
    expect(r.counts.categoriesCreated).toBe(5);
  });

  it('watching workbook: exact counts, blank statuses warn, theme colors fall back', async () => {
    const store = await fresh();
    const r = await importFile(store, watchingFile);
    expectCounts(r, expected.watching);
    expect(r.warnings.filter((w) => w.code === 'BLANK_STATUS')).toHaveLength(6);
    expect(store.all().filter((e) => e.status === 'planned')).toHaveLength(6);
    expect(store.categories).toHaveLength(16);
    expect(store.categories.find((c) => c.name === 'Action Donghua')!.color).toBe('#B71C1C');
    expect(store.categories.find((c) => c.name === 'Romance Donghua')!.color).toBe('#8B2252');
    expect(store.all().find((e) => e.categoryId === 'watching-action-donghua')!.mediaType).toBe(
      'donghua',
    );
  });

  it('preserves raw progress text, parses scores/sources/links', async () => {
    const store = await fresh();
    await importFile(store, readingFile);
    await importFile(store, watchingFile);
    const all = store.all();
    const raws = new Set(all.flatMap((e) => [e.current.raw, e.consumedUpTo.raw]));
    for (const must of [
      'End of S1',
      '49?',
      'Beginning of S2',
      'S2 E1',
      'Done',
      'Completed',
      'End',
      'S1 E13',
    ])
      expect(raws.has(must), must).toBe(true);
    expect(all.find((e) => e.current.raw === 'S2 E1')!.current).toMatchObject({
      season: 2,
      number: 1,
    });
    expect(all.find((e) => e.consumedUpTo.raw === '49?')!.consumedUpTo).toMatchObject({
      number: 49,
      kind: 'text',
    });
    const reading = all.filter((e) => e.library === 'reading');
    expect(reading.some((e) => e.sources.join() === 'YouTube,Inkreader')).toBe(true);
    expect(reading.every((e) => e.sources.length > 0)).toBe(true);
    const links = reading.flatMap((e) => e.links);
    expect(
      links.some(
        (l) =>
          l.url === 'https://tapas.example/episode/5' || l.url?.startsWith('https://tapas.example'),
      ),
    ).toBe(true);
    expect(links.filter((l) => l.url).every((l) => /^https?:\/\//.test(l.url!))).toBe(true);
    const desc = links.find((l) => l.url === null && l.label)!;
    expect(desc.channel).toBeTruthy();
    expect(desc.date).toMatch(/\d+\/\d+\/26/);
    expect(reading.some((e) => e.notes.extra1)).toBe(true);
    expect(reading.some((e) => e.score !== null)).toBe(true);
    expect(parseScore(' 9 - Amazing')).toBe(9);
  });

  it('scores parse across all three label sets, incl. the leading-space variant', async () => {
    const sheets = await readXlsx(readFileSync(readingFile));
    const col = sheets[0]!.mapping.indexOf('score');
    const cells = sheets[0]!.rows
      .map((r) => r.cells[col])
      .filter((x): x is string => typeof x === 'string');
    expect(cells.some((c) => c.startsWith(' '))).toBe(true);
    expect(cells.some((c) => /Above Average|Awful/.test(c))).toBe(true);
    expect(cells.every((c) => parseScore(c) !== null)).toBe(true);
    const w = await readXlsx(readFileSync(watchingFile));
    const wc = w[0]!.rows
      .map((r) => r.cells[w[0]!.mapping.indexOf('score')])
      .filter(Boolean) as string[];
    expect(wc.every((c) => parseScore(c) !== null)).toBe(true);
  });

  it('is idempotent on re-import and never overwrites by default', async () => {
    const store = await fresh();
    await importFile(store, readingFile);
    const target = store.all()[0]!;
    await store.updateEntry(target.id, { notes: { general: 'MY EDIT' } });
    const before = JSON.stringify(store.all());
    const again = await importFile(store, readingFile);
    expect(again.counts).toMatchObject({
      created: 0,
      updated: 0,
      unchanged: 66,
      categoriesCreated: 0,
    });
    expect(JSON.stringify(store.all())).toBe(before);
    expect(store.getEntry(target.id)!.notes.general).toBe('MY EDIT');
  });

  it('strategies: skip, overwrite, keep_both', async () => {
    const store = await fresh();
    await importFile(store, readingFile);
    const t = store.all()[0]!;
    await store.updateEntry(t.id, { score: 1, notes: { general: 'edited' } });
    expect((await importFile(store, readingFile, { strategy: 'skip' })).counts.skipped).toBe(66);
    expect(store.getEntry(t.id)!.notes.general).toBe('edited');
    const ow = await importFile(store, readingFile, { strategy: 'overwrite' });
    expect(ow.counts.updated).toBeGreaterThan(0);
    expect(store.getEntry(t.id)!.notes.general).not.toBe('edited');
    await importFile(store, readingFile, { strategy: 'keep_both' });
    expect(store.all()).toHaveLength(132);
  });

  it('undo restores the exact pre-import state', async () => {
    const store = await fresh();
    await importFile(store, readingFile);
    const snapshot = JSON.stringify({ e: store.all(), c: store.categories });
    const r = await importFile(store, watchingFile);
    expect(store.all()).toHaveLength(114);
    await undoImport(store, r.undoToken);
    expect(JSON.stringify({ e: store.all(), c: store.categories })).toBe(snapshot);
    await expect(undoImport(store, r.undoToken)).rejects.toThrow(/already/);
    // and it persisted to disk
    const reopened = new Store(store.dir);
    await reopened.open();
    expect(JSON.stringify({ e: reopened.all(), c: reopened.categories })).toBe(snapshot);
  });

  it('preview does not write', async () => {
    const store = await fresh();
    const staged = await stageUpload(store, 'Reading_Tracker.xlsx', readFileSync(readingFile));
    const p = await previewImport(store, staged.id, {});
    expect(p.counts.create).toBe(66);
    expect(store.all()).toHaveLength(0);
    expect(store.categories).toHaveLength(0);
    expect(p.tagSuggestions.length).toBeGreaterThan(0);
    expect(p.sheets.find((s) => s.name === 'Action')!.mapping.every((m) => m.field !== null)).toBe(
      true,
    );
    expect(p.warnings.some((w) => w.code === 'STRAY_COLUMN')).toBe(false);
  });

  it('round trip: import → export xlsx → re-import is lossless', async () => {
    for (const file of [readingFile, watchingFile]) {
      const a = await fresh();
      await importFile(a, file);
      const library = a.all()[0]!.library;
      const buf = await exportXlsx(a, library);
      const b = await fresh();
      const staged = await stageUpload(b, 'roundtrip.xlsx', buf);
      const r = await commitImport(b, staged.id, {});
      expect(r.counts.created).toBe(a.all().length);
      const norm = (s: Store) =>
        s
          .all()
          .map(({ id: _i, createdAt: _c, updatedAt: _u, importRef: _r, ...rest }) => rest)
          .sort((x, y) => x.title.localeCompare(y.title));
      expect(norm(b)).toEqual(norm(a));
      const cats = (s: Store) => s.categories.map((c) => [c.library, c.name, c.color, c.order]);
      expect(cats(b)).toEqual(cats(a));
    }
  });

  it('xlsx export has banner, frozen header, dropdown validations', async () => {
    const a = await fresh();
    await importFile(a, readingFile);
    const buf = await exportXlsx(a, 'reading');
    const ExcelJS = (await import('exceljs')).default;
    const wb = new ExcelJS.Workbook();
    await wb.xlsx.load(buf as never);
    const ws = wb.getWorksheet('Romance')!;
    expect(ws.getCell('A1').value).toBe('Romance Tracker');
    expect(ws.getCell('A2').value).toBe('Title');
    expect(ws.getCell('C2').value).toBe('Score (1–10)');
    expect(ws.views[0]).toMatchObject({ state: 'frozen', ySplit: 2 });
    const dv = (
      ws as unknown as { dataValidations: { model: Record<string, { formulae: string[] }> } }
    ).dataValidations.model;
    const rules = Object.values(dv).map((v) => v.formulae[0]);
    expect(rules.some((f) => f!.includes('Plan to Read') && f!.includes('Re-Reading'))).toBe(true);
    expect(rules.some((f) => f!.includes('10 - Masterpiece'))).toBe(true);
    expect(wb.getWorksheet('Add Category - Instructions')).toBeUndefined();
  });
});

describe('csv', () => {
  it('handles BOM, quoted newlines and quotes', async () => {
    const sheets = readCsv(
      readFileSync(path.join(SYN, 'sample-reading.csv')),
      'sample-reading.csv',
    );
    const s = sheets[0]!;
    expect(s.library).toBe('reading');
    expect(s.headers[0]).toBe('Title');
    expect(s.rows).toHaveLength(2);
    expect(s.rows[0]!.cells[0]).toBe('Café "Noir" Chronicle');
    expect(String(s.rows[0]!.cells[6])).toContain('\n');
  });
  it('sniffs semicolons and falls back to Windows-1252', () => {
    const semi = readCsv(readFileSync(path.join(SYN, 'sample-semicolon.csv')), 'x.csv')[0]!;
    expect(semi.library).toBe('watching');
    expect(semi.rows[0]!.cells[3]).toBe('Fantasy');
    const cp = readCsv(readFileSync(path.join(SYN, 'sample-cp1252.csv')), 'x.csv')[0]!;
    expect(cp.rows[0]!.cells[0]).toBe('École Maudite');
  });
  it('imports a CSV into the filename category and exports back with BOM', async () => {
    const store = await fresh();
    const r = await importFile(store, path.join(SYN, 'sample-reading.csv'));
    expect(r.counts.created).toBe(2);
    expect(store.categories.map((c) => c.name)).toEqual(['sample-reading']);
    const out = exportCsv(store, 'reading', store.all());
    expect([...out.subarray(0, 3)]).toEqual([0xef, 0xbb, 0xbf]);
    const back = readCsv(out, 'back.csv')[0]!;
    expect(back.rows).toHaveLength(2);
  });
  it('warns about non-empty unlabeled and unknown columns', async () => {
    const store = await fresh();
    const staged = await stageUpload(
      store,
      'x.csv',
      Buffer.from('Title,Status,Currently on Chapter,,Mystery\nA,Reading,3,stray,huh\n'),
    );
    const p = await previewImport(store, staged.id, {});
    expect(p.warnings.map((w) => w.code)).toEqual(
      expect.arrayContaining(['STRAY_COLUMN', 'UNKNOWN_COLUMN']),
    );
  });
  it('requires a library when headers do not reveal one', async () => {
    const store = await fresh();
    const staged = await stageUpload(store, 'x.csv', Buffer.from('Title,Status\nA,Reading\n'));
    const p = await previewImport(store, staged.id, {});
    expect(p.warnings.map((w) => w.code)).toContain('NO_LIBRARY');
    const ok = await commitImport(store, staged.id, { library: 'reading', category: 'Misc.' });
    expect(ok.counts.created).toBe(1);
  });
});

describe('import API', () => {
  it('upload → preview → commit → undo, with auth', async () => {
    const store = await fresh();
    const app = await buildApp({ store, token: TOKEN, extend: registerIoRoutes });
    cleanups.push(() => app.close());
    const fd = new FormData();
    fd.append('file', new Blob([readFileSync(path.join(SYN, 'Watching_Tracker.xlsx'))]), 'w.xlsx');
    const res = new Response(fd);
    const payload = Buffer.from(await res.arrayBuffer());
    const headers = {
      'content-type': res.headers.get('content-type')!,
      authorization: `Bearer ${TOKEN}`,
    };
    const noAuth = await app.inject({
      method: 'POST',
      url: '/api/v1/import/upload',
      payload,
      headers: { 'content-type': headers['content-type'] },
    });
    expect(noAuth.statusCode).toBe(401);
    const up = await app.inject({ method: 'POST', url: '/api/v1/import/upload', payload, headers });
    expect(up.statusCode).toBe(201);
    const { importId } = up.json();
    const auth = { authorization: `Bearer ${TOKEN}` };
    const pv = await app.inject({
      method: 'POST',
      url: '/api/v1/import/preview',
      payload: { importId },
      headers: auth,
    });
    expect(pv.json().counts.create).toBe(48);
    const cm = await app.inject({
      method: 'POST',
      url: '/api/v1/import/commit',
      payload: { importId, options: { strategy: 'fill' } },
      headers: auth,
    });
    expect(cm.json().counts.created).toBe(48);
    const x = await app.inject({
      method: 'GET',
      url: '/api/v1/export/xlsx?library=watching',
      headers: auth,
    });
    expect(x.statusCode).toBe(200);
    expect(x.headers['content-type']).toContain('spreadsheetml');
    const csv = await app.inject({
      method: 'GET',
      url: '/api/v1/export/csv?library=watching&status=completed',
      headers: auth,
    });
    expect(csv.body.split('\r\n').length).toBe(1 + 18);
    const zip = await app.inject({ method: 'GET', url: '/api/v1/export/json', headers: auth });
    expect(zip.rawPayload.subarray(0, 2).toString()).toBe('PK');
    const tpl = await app.inject({
      method: 'GET',
      url: '/api/v1/export/csv-template?library=reading',
      headers: auth,
    });
    expect(tpl.body).toContain('Watched on YouTube Up To Chapter');
    const un = await app.inject({
      method: 'POST',
      url: `/api/v1/import/undo/${importId}`,
      headers: auth,
    });
    expect(un.statusCode).toBe(200);
    expect(store.all()).toHaveLength(0);
    const bad = await app.inject({
      method: 'POST',
      url: '/api/v1/import/undo/..%2F..',
      headers: auth,
    });
    expect(bad.statusCode).toBeGreaterThanOrEqual(400);
  });
});

// Real workbooks are optional: skipped on CI and other machines.
const realFiles = existsSync(PRIVATE)
  ? readdirSync(PRIVATE).filter((f) => /\.xlsx$/i.test(f) && !f.startsWith('~$'))
  : [];
describe.skipIf(realFiles.length === 0)('private fixtures (local only)', () => {
  it('imports the real workbooks with the documented counts', async () => {
    const store = await fresh();
    for (const f of realFiles) {
      const sheets = await readXlsx(readFileSync(path.join(PRIVATE, f)));
      const lib = sheets.find((s) => s.library)?.library;
      const exp = lib === 'reading' ? expected.reading : expected.watching;
      const r = await importFile(store, path.join(PRIVATE, f));
      expect(r.summary.entries, f).toBe(exp.total);
      expect(r.summary.byCategory).toEqual(exp.byCategory);
      expect(r.summary.byRawStatus).toEqual(exp.byRawStatus);
      expect(r.summary.distinctTagsRaw).toBe(exp.tagsRaw);
      expect(r.summary.distinctTagsNormalized).toBe(exp.tagsNormalized);
      const again = await importFile(store, path.join(PRIVATE, f));
      expect(again.counts.created).toBe(0);
    }
  });
});

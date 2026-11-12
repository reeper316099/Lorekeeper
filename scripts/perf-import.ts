/**
 * Times the import/export hot paths on the synthetic workbooks (and a large generated one), to compare
 * with the Go port (`PERF=1 go test -run TestPerfImport -v ./internal/importexport`).
 *   npx tsx scripts/perf-import.ts
 */
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { performance } from 'node:perf_hooks';
import ExcelJS from 'exceljs';
import { Store } from '../packages/server/src/index.ts';
import { buildPlan, commitImport, stageUpload } from '../packages/server/src/io/importer.ts';
import { exportXlsx, exportCsv } from '../packages/server/src/io/export.ts';
import { readXlsx } from '../packages/server/src/io/read.ts';

const SYN = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'fixtures', 'synthetic');
const reading = readFileSync(path.join(SYN, 'Reading_Tracker.xlsx'));
const watching = readFileSync(path.join(SYN, 'Watching_Tracker.xlsx'));

async function big(n: number): Promise<Buffer> {
  const wb = new ExcelJS.Workbook();
  const ws = wb.addWorksheet('Big');
  ws.addRow([
    'Title',
    'Status',
    'Score (1–10)',
    'Genres / Tags',
    'Currently on Chapter',
    'General Notes',
  ]);
  const W = ['Silent', 'Crimson', 'Hollow', 'Gilded', 'Fallen', 'Wandering', 'Eternal', 'Ashen'];
  const N = ['Crown', 'Garden', 'Oath', 'Tower', 'Contract', 'Blade', 'Archive', 'Throne'];
  for (let i = 0; i < n; i++)
    ws.addRow([
      `${W[i % 8]} ${N[(i >> 3) % 8]} ${i}`,
      ['Reading', 'Completed', 'On Hold'][i % 3],
      `${(i % 10) + 1} - Good`,
      `Tag ${i % 40}, Tag ${(i * 7) % 40}`,
      i % 300,
      `Notes ${i}`,
    ]);
  return Buffer.from(await wb.xlsx.writeBuffer());
}

const med = (xs: number[]) => xs.sort((a, b) => a - b)[Math.floor(xs.length / 2)]!;
const time = async <T>(label: string, runs: number, fn: () => Promise<T>) => {
  const s: number[] = [];
  let out!: T;
  for (let i = 0; i < runs; i++) {
    const t = performance.now();
    out = await fn();
    s.push(performance.now() - t);
  }
  console.log(`${label.padEnd(46)} ${med(s).toFixed(1).padStart(8)} ms p50  (${runs} runs)`);
  return out;
};
const fresh = async () => {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'lk-pi-'));
  const s = new Store(dir);
  await s.open();
  return {
    s,
    done: async () => {
      await s.close();
      rmSync(dir, { recursive: true, force: true });
    },
  };
};

console.log('Node import/export timings\n');
await time('read reading workbook (66 rows, 6 sheets)', 7, () => readXlsx(reading));
await time('read watching workbook (48 rows)', 7, () => readXlsx(watching));
{
  const f = await fresh();
  const sheets = await readXlsx(reading);
  await time('plan reading import', 7, () => buildPlan(f.s, 'r.xlsx', sheets));
  await f.done();
}
await time('commit reading import (stage+plan+write)', 5, async () => {
  const f = await fresh();
  const st = await stageUpload(f.s, 'r.xlsx', reading);
  await commitImport(f.s, st.id, {});
  await f.done();
});
{
  const f = await fresh();
  const st = await stageUpload(f.s, 'r.xlsx', reading);
  await commitImport(f.s, st.id, {});
  await time('export reading xlsx (5 sheets)', 5, () => exportXlsx(f.s, 'reading'));
  await time('export reading csv', 7, async () => exportCsv(f.s, 'reading', f.s.all()));
  await f.done();
}
for (const n of [2000, 10000]) {
  const file = await big(n);
  await time(`read ${n}-row workbook`, 3, () => readXlsx(file));
  await time(`import ${n}-row workbook (stage+plan+commit)`, 3, async () => {
    const f = await fresh();
    const st = await stageUpload(f.s, 'big.xlsx', file);
    await commitImport(f.s, st.id, {});
    await f.done();
  });
}

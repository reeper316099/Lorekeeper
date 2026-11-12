/**
 * Reads the XLSX and CSV exports written by the Go exporter with the Node importer and checks that
 * the entries survive unchanged, and that the workbook has the banner, frozen header and dropdowns.
 *   cd go && INTEROP_OUT=/tmp/go-exports go test -run TestWriteInteropExports ./internal/importexport
 *   npx tsx scripts/check-go-exports.ts /tmp/go-exports
 */
import { readFileSync, mkdtempSync, rmSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import ExcelJS from 'exceljs';
import { Store } from '../packages/server/src/index.ts';
import { commitImport, stageUpload } from '../packages/server/src/io/importer.ts';

const dir = process.argv[2];
if (!dir) throw new Error('usage: check-go-exports.ts <folder>');
const expected = JSON.parse(readFileSync(path.join(dir, 'expected.json'), 'utf8')) as Record<
  string,
  unknown[]
>;
const problems: string[] = [];
const skip = new Set(['id', 'createdAt', 'updatedAt', 'importRef']);
const norm = (e: object) =>
  JSON.stringify(sortKeys(Object.fromEntries(Object.entries(e).filter(([k]) => !skip.has(k)))));
const sortKeys = (v: unknown): unknown =>
  Array.isArray(v)
    ? v.map(sortKeys)
    : v && typeof v === 'object'
      ? Object.fromEntries(
          Object.entries(v as object)
            .sort(([a], [b]) => (a < b ? -1 : 1))
            .map(([k, x]) => [k, sortKeys(x)]),
        )
      : v;

for (const lib of ['reading', 'watching']) {
  for (const ext of ['xlsx', 'csv']) {
    const tmp = mkdtempSync(path.join(os.tmpdir(), 'lk-chk-'));
    const store = new Store(tmp);
    await store.open();
    const staged = await stageUpload(
      store,
      `${lib}.${ext}`,
      readFileSync(path.join(dir, `${lib}.${ext}`)),
    );
    const r = await commitImport(store, staged.id, {});
    const want = (expected[lib] as object[]).map(norm).sort();
    const got = store.all().map(norm).sort();
    if (r.counts.created !== want.length)
      problems.push(`${lib}.${ext}: created ${r.counts.created}, want ${want.length}`);
    if (JSON.stringify(got) !== JSON.stringify(want)) {
      const i = got.findIndex((g, k) => g !== want[k]);
      problems.push(`${lib}.${ext}: entries differ at ${i}\n  node: ${got[i]}\n  go:   ${want[i]}`);
    }
    await store.close();
    rmSync(tmp, { recursive: true, force: true });
  }
  const wb = new ExcelJS.Workbook();
  await wb.xlsx.load(readFileSync(path.join(dir, `${lib}.xlsx`)) as never);
  const ws = wb.worksheets[0]!;
  if (!String(ws.getCell('A1').value).endsWith(' Tracker'))
    problems.push(`${lib}: banner is ${ws.getCell('A1').value}`);
  if (ws.getCell('A2').value !== 'Title') problems.push(`${lib}: header row`);
  if (ws.views[0]?.state !== 'frozen' || ws.views[0]?.ySplit !== 2)
    problems.push(`${lib}: frozen header ${JSON.stringify(ws.views[0])}`);
  const dv = (
    ws as unknown as { dataValidations: { model: Record<string, { formulae: string[] }> } }
  ).dataValidations.model;
  if (!Object.values(dv).some((v) => v.formulae[0]!.includes('10 - Masterpiece')))
    problems.push(`${lib}: no score dropdown`);
  if (!ws.properties.tabColor) problems.push(`${lib}: no tab color`);
  console.log(
    `${lib}: ${wb.worksheets.length} sheets, ${expected[lib]!.length} entries read back from xlsx and csv`,
  );
}
if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('Node read the Go-written XLSX and CSV exports cleanly.');

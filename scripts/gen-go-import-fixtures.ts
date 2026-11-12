/**
 * Generates go/internal/importexport/testdata/node-import.json from the real Node importer: for a set of
 * scenarios (synthetic workbooks, CSV files with awkward content, re-imports with every strategy) it
 * records the preview, the committed report, the resulting entries/categories and the CSV export.
 * The Go importer must produce the same results. All content is invented.
 *   npm run fixtures:generate && npx tsx scripts/gen-go-import-fixtures.ts
 */
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { Store } from '../packages/server/src/index.ts';
import { commitImport, previewImport, stageUpload } from '../packages/server/src/io/importer.ts';
import { exportCsv } from '../packages/server/src/io/export.ts';

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const OUT = path.join(root, 'go/internal/importexport/testdata/node-import.json');

type File = { fileName: string; path?: string; text?: string; bytesHex?: string };
type Options = Record<string, unknown>;
type Step =
  | { do: 'commit'; file: File; options?: Options }
  | { do: 'edit'; index: number; patch: Record<string, unknown> };
interface Scenario {
  name: string;
  steps: Step[];
}

const syn = (n: string): File => ({ fileName: n, path: `fixtures/synthetic/${n}` });
const csv = (fileName: string, text: string): File => ({ fileName, text });

const HARBOR = [
  'Title,Status,Score (1–10),Genres / Tags,Currently on Chapter,Reading Method,General Notes',
  'The Quiet Harbor,Reading,7 - Good,"Cozy, cozy,Slow Burn",12,YouTube,"Line one',
  'line two"',
  'Quiet Harbour,Completed,9,Comedy;Cozy,End,ClipHub;YouTube,',
  'quiet harbor,Dropped,abc,,3?,,',
  '"Café ""Noir"" Tale",Paused,,Mystery,S2 E1,,note',
  'Lantern Quest,Wishlist,5,,,,',
  ' Lantern Quest ,Reading,,,,,',
  '   ,Reading,,,,,',
  ',,,,,,',
  'Marlow’s Plan,,8,Romance,,,',
].join('\r\n');

const scenarios: Scenario[] = [
  { name: 'reading workbook', steps: [{ do: 'commit', file: syn('Reading_Tracker.xlsx') }] },
  { name: 'watching workbook', steps: [{ do: 'commit', file: syn('Watching_Tracker.xlsx') }] },
  {
    name: 'both workbooks into one library',
    steps: [
      { do: 'commit', file: syn('Reading_Tracker.xlsx') },
      { do: 'commit', file: syn('Watching_Tracker.xlsx') },
    ],
  },
  {
    name: 're-import is idempotent and keeps edits',
    steps: [
      { do: 'commit', file: syn('Reading_Tracker.xlsx') },
      { do: 'edit', index: 0, patch: { notes: { general: 'MY EDIT' } } },
      { do: 'commit', file: syn('Reading_Tracker.xlsx') },
    ],
  },
  {
    name: 'strategies skip, overwrite, keep_both',
    steps: [
      { do: 'commit', file: syn('Reading_Tracker.xlsx') },
      { do: 'edit', index: 0, patch: { score: 1, notes: { general: 'edited' } } },
      { do: 'commit', file: syn('Reading_Tracker.xlsx'), options: { strategy: 'skip' } },
      { do: 'commit', file: syn('Reading_Tracker.xlsx'), options: { strategy: 'overwrite' } },
      { do: 'commit', file: syn('Reading_Tracker.xlsx'), options: { strategy: 'keep_both' } },
    ],
  },
  { name: 'csv reading sample', steps: [{ do: 'commit', file: syn('sample-reading.csv') }] },
  { name: 'csv semicolon sample', steps: [{ do: 'commit', file: syn('sample-semicolon.csv') }] },
  { name: 'csv windows-1252 sample', steps: [{ do: 'commit', file: syn('sample-cp1252.csv') }] },
  {
    name: 'csv with duplicates, unknown statuses, bad scores, blank rows',
    steps: [{ do: 'commit', file: csv('harbor.csv', HARBOR), options: { library: 'reading' } }],
  },
  {
    name: 'csv status map and forced category',
    steps: [
      {
        do: 'commit',
        file: csv('harbor.csv', HARBOR),
        options: {
          library: 'reading',
          category: 'Cozy Stuff',
          statusMap: { Wishlist: 'planned', Paused: 'dropped' },
        },
      },
    ],
  },
  {
    name: 'csv needs a library',
    steps: [{ do: 'commit', file: csv('nolib.csv', 'Title,Status\nA,Reading\nB,Finished\n') }],
  },
  {
    name: 'csv with BOM, category column, stray and unknown columns',
    steps: [
      {
        do: 'commit',
        file: {
          fileName: 'cols.csv',
          text:
            '﻿Title,Status,Currently on Episode,,Mystery,Category,Link\n' +
            'Glass Meridian,Watching,4,stray,huh,Anime,youtu.be/abc123\n' +
            'Paper Lanterns,Finished,12,,,Donghua,https://example.com/a.\n' +
            'Café Nebula,On Hold,,,,,"see https://example.org/x (3/3/26) By: PanelPilot"\n',
        },
      },
    ],
  },
  {
    name: 'csv tab separated',
    steps: [
      {
        do: 'commit',
        file: csv(
          'tabs.tsv',
          'Title\tStatus\tCurrently on Chapter\nOne\tReading\t5\nTwo\tCompleted\t9\n',
        ),
        options: { library: 'reading' },
      },
    ],
  },
];

const skipKeys = new Set(['id', 'createdAt', 'updatedAt']);
const normalize = (v: unknown): unknown => {
  if (Array.isArray(v)) return v.map(normalize);
  if (v && typeof v === 'object')
    return Object.fromEntries(
      Object.entries(v as Record<string, unknown>)
        .filter(([k]) => !skipKeys.has(k) && k !== 'importedAt')
        .map(([k, x]) => [k, normalize(x)]),
    );
  return v;
};

const bytesOf = (f: File): Buffer =>
  f.path ? readFileSync(path.join(root, f.path)) : Buffer.from(f.text!, 'utf8');

const out: unknown[] = [];
for (const sc of scenarios) {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'lk-imp-'));
  const store = new Store(dir);
  await store.open();
  const steps: unknown[] = [];
  for (const [si, st] of sc.steps.entries()) {
    const last = si === sc.steps.length - 1; // entries and CSV only for the final state, to keep the file small
    if (st.do === 'edit') {
      await store.updateEntry(store.all()[st.index]!.id, st.patch);
      steps.push({ do: 'edit' });
      continue;
    }
    const staged = await stageUpload(store, st.file.fileName, bytesOf(st.file));
    const preview = await previewImport(store, staged.id, (st.options ?? {}) as never);
    const report = await commitImport(store, staged.id, (st.options ?? {}) as never);
    const previewRest = { ...preview } as Record<string, unknown>;
    delete previewRest.importId;
    const libs = [...new Set(store.all().map((e) => e.library))];
    steps.push({
      do: 'commit',
      preview: normalize(previewRest),
      report: normalize({
        counts: report.counts,
        summary: report.summary,
        warnings: report.warnings,
        tagSuggestions: report.tagSuggestions,
        strategy: report.strategy,
      }),
      entries: last ? normalize(store.all()) : undefined,
      categories: last ? store.categories : undefined,
      csv: !last
        ? undefined
        : Object.fromEntries(
            libs.map((l) => [
              l,
              exportCsv(
                store,
                l,
                store.all().filter((e) => e.library === l),
              ).toString('base64'),
            ]),
          ),
    });
  }
  await store.close();
  rmSync(dir, { recursive: true, force: true });
  out.push({
    name: sc.name,
    steps: sc.steps.map((s) =>
      s.do === 'edit' ? s : { ...s, file: { ...s.file, text: s.file.text, path: s.file.path } },
    ),
    results: steps,
  });
}
mkdirSync(path.dirname(OUT), { recursive: true });
writeFileSync(OUT, JSON.stringify({ scenarios: out }) + '\n');
console.log(`wrote ${OUT}: ${scenarios.length} scenarios`);

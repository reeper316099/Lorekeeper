/**
 * Generates go/internal/shared/testdata/parity.json: the output of the TypeScript parsers for a
 * corpus of invented inputs. The Go port's tests (go/internal/shared/parity_test.go) must produce
 * the same JSON for the same inputs. Re-run after changing a parser or the corpus:
 *   npx tsx scripts/gen-go-parity.ts
 */
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  EntrySchema,
  CategorySchema,
  TagSchema,
  ScalesSchema,
  SettingsSchema,
  MetaSchema,
  ActivityRecordSchema,
  DEFAULT_SCALES,
  parseProgress,
  progressCell,
  behindBy,
  parseLink,
  splitTags,
  suggestTagMerges,
  normalizeTitle,
  titleSimilarity,
  isNearDuplicateTitle,
  parseStatus,
  parseScore,
  parseSources,
  cleanText,
  readableTextColor,
  slugify,
  levenshtein,
  damerau,
} from '../packages/shared/src/index.js';

const NBSP = ' ';
const cells: unknown[] = [
  null,
  '',
  '  ',
  ' ',
  0,
  1,
  7,
  7.5,
  10,
  15,
  46,
  111,
  1.5,
  100,
  123,
  '007',
  ' 111 ',
  '12',
  'x',
  true,
  'End of S1',
  'Ended of S2',
  'Beginning of S2',
  'Start of Season 4',
  'begin s3',
  'S2 E1',
  's3e12',
  'Season 2, Episode 4',
  'S1-E10',
  's1 ep 2',
  '49?',
  '12 ?',
  'Done',
  'Completed',
  'Complete',
  'Finished',
  'Finished?',
  'End',
  'Ended',
  'N/A',
  'A few chapters after Season 3?',
  'ep 5',
  '7 - Good',
  ' 9 - Amazing',
  '10 - Masterpiece',
  '8/10',
  'Great',
  '0 - none',
  '00',
  `${NBSP}12${NBSP}`,
  `\u3000S2${NBSP}E3`,
  '﻿44',
  'Plan to Read',
  'plan  to   watch',
  'On Hold',
  'ON HOLD',
  'Re-Reading',
  'rewatching',
  'Dropped',
  'paused',
  'in progress',
  'backlog',
  '???',
  'Youtube, Inkreader',
  'YouTube',
  'yt; Inkreader / ClipHub',
  'a,,b; ;',
  ' Action,  Isekai , ',
  'Quest-like, Quest-Like',
  'Romance,Slapstick',
  'Tag\nOther',
  'Café, café',
  'ünï, ÜNÏ',
  'youtu.be/nQ5pQCTXeB0?si=abc',
  ' tapas.io/episode/2',
  'https://example.com/a',
  'see https://example.com/a.',
  'https://www.youtube.com/watch?v=abc123',
  'http://anilist.co/anime/1',
  'https://example.org/path(1)',
  '(S1 #full) Some Title (3/3/26) By: PanelPilot',
  'Notes only, no link',
  'www.example.com',
  'example',
  'by: someone',
  'Title (12/25/2025)',
  'bad url https://',
  'a b.com',
  'The Quiet Harbor',
  '  The Last Door ',
  'A Tale of Two Clocks',
  'An Ode',
  '“Quoted’s” Plan!',
  'Ünïcode Café',
  'Ｔｈｅ Ｑｕｉｅｔ',
  '日本語タイトル',
  'THE',
  '!!!',
  'Mr. Marlow',
  'Mr Marlow',
  'Misc.',
  'Sci-Fi',
  'Slice of Life',
  '  Weird__Name  ',
  '日本',
  '---',
  'Ça va?',
  '#B71C1C',
  '#FFEB3B',
  'B71C1C',
  '#fff',
  'zzz',
  '#000000',
  '#ffffff',
  '#808080',
];

const titlePairs: [string, string][] = [
  ['Quiet Harbor Without Mr. Pell', 'Can You Do Quiet Harbor Without Mr Pelle?'],
  ['Silent Crown of Ash', 'Frozen Garden of Seven Moons'],
  ['The Marlow Plan', 'Marlow Plan'],
  ['A', 'a'],
  ['', ''],
  ['Ünï', 'Uni'],
  ['Clockwork Orchard Season 2', 'Clockwork Orchard 2nd Season'],
  ['Hollow Lantern Chronicles Of Dusk', 'Hollow Lantern Chronicle Of Dusk'],
  ['One Two Three Four Five', 'One Two Three Four Six'],
  ['kitten', 'sitting'],
  ['flaw', 'lawn'],
];

const tagUsage: [string, number][][] = [
  [
    ['Sorcery', 10],
    ['Sorcrey', 1],
    ['Fantasy', 8],
    ['Fantsy', 1],
    ['Rank Board', 5],
    ['Rankk Board', 1],
    ['Sword Arts', 3],
    ['Sword Artss', 1],
    ['Quest-like', 4],
    ['Quest-Like', 1],
    ['Sunny Days', 5],
    ['Sunny Days(loud)', 1],
    ['Romance', 9],
  ],
  [
    ['a', 1],
    ['A', 1],
    ['b', 2],
  ],
  [
    ['Slice of Life', 3],
    ['Slice-of-life', 3],
    ['Slice Of Life!', 3],
  ],
  [
    ['Café', 2],
    ['Cafe', 5],
    ['Cafè', 1],
  ],
  [
    ['ab', 1],
    ['ac', 1],
    ['abcd', 1],
    ['abdc', 1],
  ],
  [
    ['abcdefgh', 2],
    ['abcdefxy', 1],
    ['abcdexyz', 1],
    ['abxdefgh', 1],
  ],
  [
    ['abcde', 2],
    ['abxyz', 1],
    ['abcxe', 1],
  ],
  [],
];

const progressPairs: [unknown, unknown][] = [
  [111, 110],
  [111, 'End'],
  [5, 5],
  ['S2 E4', 'S2 E1'],
  ['S2 E4', 'S1 E1'],
  [null, 3],
  [3, null],
  [2.5, 1],
];

const out: Record<string, { in: unknown; out: unknown }[]> = {};
const run = <T>(name: string, inputs: T[], fn: (x: T) => unknown) => {
  out[name] = inputs.map((x) => ({ in: x ?? null, out: fn(x) ?? null }));
};

run('parseProgress', cells, parseProgress);
run('progressCell', cells, (x) => progressCell(parseProgress(x)));
run('behindBy', progressPairs, ([a, b]) => behindBy(parseProgress(a), parseProgress(b)));
run('parseLink', cells, (x) => parseLink(x));
run('parseLinkYouTube', cells, (x) => parseLink(x, 'youtube'));
run('splitTags', cells, splitTags);
run('parseSources', cells, parseSources);
run('parseStatus', cells, parseStatus);
run('parseScore', cells, parseScore);
run('cleanText', cells, cleanText);
run('normalizeTitle', cells.filter((c) => typeof c === 'string') as string[], normalizeTitle);
run('slugify', cells.filter((c) => typeof c === 'string') as string[], slugify);
run('readableTextColor', cells.filter((c) => typeof c === 'string') as string[], readableTextColor);
run('titleSimilarity', titlePairs, ([a, b]) => titleSimilarity(a, b));
run('isNearDuplicateTitle', titlePairs, ([a, b]) => isNearDuplicateTitle(a, b));
run('levenshtein', titlePairs, ([a, b]) => levenshtein(a, b));
run('damerau', titlePairs, ([a, b]) => damerau(a, b));
run('suggestTagMerges', tagUsage, (u) => suggestTagMerges(new Map(u)));

const dir = join(dirname(fileURLToPath(import.meta.url)), '../go/internal/shared/testdata');
mkdirSync(dir, { recursive: true });
writeFileSync(join(dir, 'parity.json'), JSON.stringify(out, null, 1) + '\n');
console.log(
  'wrote parity.json:',
  Object.entries(out)
    .map(([k, v]) => `${k}=${v.length}`)
    .join(' '),
);

// ---- schema cases -------------------------------------------------------------------------
const prog = { raw: '46', number: 46, season: null, kind: 'number' };
const link = (o: object = {}) => ({
  kind: 'youtube',
  url: 'https://youtu.be/x',
  label: null,
  raw: 'youtu.be/x',
  channel: null,
  date: null,
  ...o,
});
const entry = (o: object = {}) => ({
  id: 'e1',
  library: 'reading',
  categoryId: 'c1',
  title: 'Quiet Harbor',
  createdAt: '2026-01-01T00:00:00.000Z',
  updatedAt: '2026-01-02T00:00:00.000Z',
  ...o,
});
const schemas = {
  EntrySchema,
  CategorySchema,
  TagSchema,
  ScalesSchema,
  SettingsSchema,
  MetaSchema,
  ActivityRecordSchema,
};
const schemaInputs: [keyof typeof schemas, unknown][] = [
  ['EntrySchema', entry()],
  [
    'EntrySchema',
    entry({
      altTitles: ['A'],
      mediaType: 'manhwa',
      status: 'in_progress',
      score: 8,
      tags: ['x', 'y'],
      season: 'S2',
      current: prog,
      consumedUpTo: { ...prog, raw: 'End', number: null, kind: 'end' },
      totalUnits: 120,
      sources: ['YouTube'],
      links: [link(), link({ slot: 'other', kind: 'other', url: null })],
      notes: { general: 'g', extra1: null, extra2: 'e' },
      external: { anilistId: 5, custom: { a: 1 } },
      favorite: true,
      startedAt: '2026-01-01',
      completedAt: '2026-02-01',
      importRef: { file: 'f.xlsx', sheet: 'S', row: 4, importedAt: '2026-01-01T00:00:00.000Z' },
    }),
  ],
  ['EntrySchema', { ...entry(), extraneous: 1 }],
  ['EntrySchema', entry({ id: '' })],
  ['EntrySchema', entry({ library: 'music' })],
  ['EntrySchema', entry({ title: '' })],
  ['EntrySchema', entry({ mediaType: 'comic' })],
  ['EntrySchema', entry({ status: 'finished' })],
  ['EntrySchema', entry({ score: 11 })],
  ['EntrySchema', entry({ score: 0 })],
  ['EntrySchema', entry({ score: 7.5 })],
  ['EntrySchema', entry({ current: { ...prog, kind: 'weird' } })],
  ['EntrySchema', entry({ links: [link({ kind: 'blog' })] })],
  ['EntrySchema', entry({ links: [link({ slot: 'side' })] })],
  [
    'CategorySchema',
    { id: 'c1', library: 'reading', name: 'Romance', slug: 'romance', color: '#8B2252', order: 0 },
  ],
  [
    'CategorySchema',
    {
      id: 'c1',
      library: 'reading',
      name: 'Romance',
      slug: 'romance',
      color: '#8B2252',
      order: 3,
      icon: 'heart',
    },
  ],
  [
    'CategorySchema',
    { id: 'c1', library: 'reading', name: 'Romance', slug: 'romance', color: '8B2252', order: 0 },
  ],
  [
    'CategorySchema',
    {
      id: 'c1',
      library: 'reading',
      name: 'Romance',
      slug: 'romance',
      color: '#8B2252',
      order: 1.5,
    },
  ],
  [
    'CategorySchema',
    { id: 'c1', library: 'games', name: 'Romance', slug: 'romance', color: '#8B2252', order: 0 },
  ],
  ['TagSchema', { id: 't1', name: 'Action' }],
  ['TagSchema', { id: 't1', name: 'Action', color: '#fff', aliases: ['Acción'], hidden: true }],
  ['TagSchema', { id: '', name: 'Action' }],
  ['ScalesSchema', DEFAULT_SCALES],
  [
    'ScalesSchema',
    { ...DEFAULT_SCALES, reading: { ...DEFAULT_SCALES.reading, scoreLabels: ['a'] } },
  ],
  ['SettingsSchema', {}],
  ['SettingsSchema', { theme: 'light', labelOverrides: { extra2: 'Notes' }, backup: { keep: 5 } }],
  [
    'SettingsSchema',
    { savedViews: [{ id: 'v', name: 'N', query: { q: 'x' } }], hiddenTags: ['a'] },
  ],
  ['SettingsSchema', { theme: 'blue' }],
  ['SettingsSchema', { density: 'tiny' }],
  ['SettingsSchema', { defaultLibrary: 'books' }],
  ['SettingsSchema', { autoConfirmThreshold: 1.5 }],
  ['SettingsSchema', { backup: { keep: 0 } }],
  ['MetaSchema', { schemaVersion: 1, appVersion: '0.1.0', createdAt: 'a', lastOpenedAt: 'b' }],
  ['MetaSchema', { schemaVersion: 1.5, appVersion: '0.1.0', createdAt: 'a', lastOpenedAt: 'b' }],
  ['ActivityRecordSchema', { at: 't', entryId: 'e1', kind: 'progress', from: 1, to: 2 }],
  ['ActivityRecordSchema', { at: 't', entryId: 'e1', kind: 'created' }],
  ['ActivityRecordSchema', { at: 't', entryId: 'e1', kind: 'deleted' }],
];
const schemaCases = schemaInputs.map(([schema, input]) => {
  const r = schemas[schema].safeParse(input);
  return { schema, input, ok: r.success, output: r.success ? r.data : null };
});
writeFileSync(join(dir, 'schemas.json'), JSON.stringify(schemaCases, null, 1) + '\n');
console.log(
  `wrote schemas.json: ${schemaCases.length} cases (${schemaCases.filter((c) => c.ok).length} valid)`,
);

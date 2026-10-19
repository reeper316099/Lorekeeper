/**
 * Deterministic synthetic fixtures. All titles/notes are invented; the structure and quirks mirror
 * the real private workbooks (banner row + header row 2, one sheet per category, Instructions
 * sheet, mixed progress cells, messy tags, ...). Same seed → same content.
 */
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import ExcelJS from 'exceljs';
import iconv from 'iconv-lite';

const here = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(here, '..', 'fixtures', 'synthetic');

function rng(seed: number) {
  return () => {
    seed |= 0;
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const rand = rng(20240607);
const pick = <T>(xs: readonly T[]): T => xs[Math.floor(rand() * xs.length)]!;
const int = (a: number, b: number) => a + Math.floor(rand() * (b - a + 1));

const A = [
  'Silent',
  'Crimson',
  'Hollow',
  'Gilded',
  'Fallen',
  'Wandering',
  'Eternal',
  'Ashen',
  'Velvet',
  'Iron',
  'Lunar',
  'Broken',
  'Hidden',
  'Scarlet',
  'Verdant',
  'Frozen',
];
const B = [
  'Crown',
  'Garden',
  'Oath',
  'Tower',
  'Contract',
  'Blade',
  'Archive',
  'Throne',
  'Lantern',
  'Covenant',
  'Harbor',
  'Saga',
  'Requiem',
  'Orchard',
  'Gate',
  'Chronicle',
];
const C = [
  'of the Duchess',
  'of Ash',
  'Reborn',
  'in Bloom',
  'Without Mercy',
  'of Seven Moons',
  'at Dawn',
  'Unbound',
  'for Two',
  'Beyond the Veil',
];

const usedTitles = new Set<string>();
function title(extra = ''): string {
  for (;;) {
    const t = `${pick(A)} ${pick(B)} ${pick(C)}${extra}`.trim();
    const k = t.toLowerCase();
    if (!usedTitles.has(k)) {
      usedTitles.add(k);
      return t;
    }
  }
}

const READ_TAGS = [
  'Romance',
  'Action',
  'Fantasy',
  'Isekai',
  'Overlord Lead',
  'Rank Board',
  'Regression',
  'Slapstick',
  'Sorcery',
  'Sorcrey',
  'Soorcery',
  'Fantsy',
  'Rankk Board',
  'Sword Artss',
  'Sword Arts',
  'Quest-like',
  'Quest-Like',
  'Sunny Days',
  'Sunny Nights',
  'Sunny Scenes',
  'Sunnyish Scenes',
  'Sunny Days(loud)',
  'Sunny Days(quiet)',
  'Sorcery-ish',
  'Second Life',
  'Payback',
  'Ensemble',
  'Comedy',
  'Drama',
  'School',
  'Catacomb',
  'Spire Climb',
  'Alchemy',
  'Duchess Lead',
  'Long Fuse',
  'Unbeatable',
];
const WATCH_TAGS = [
  'Fantasy',
  'Sorcery',
  'Romance',
  'Action',
  'Isekai',
  'Comedy',
  'Drama',
  'Mystery',
  'Supernatural',
  'Sci-Fi',
  'Slice of Life',
  'School',
  'Adventure',
  'Sports',
];
if (new Set(READ_TAGS).size !== 36 || new Set(READ_TAGS.map((t) => t.toLowerCase())).size !== 35)
  throw new Error('reading tag vocabulary must be 36 raw / 35 normalized');
if (new Set(WATCH_TAGS).size !== 14) throw new Error('watching tag vocabulary must be 14');

/** Row i gets 3 tags; the first 12 rows cover the whole vocabulary. Formatting is deliberately messy. */
function tagCell(vocab: string[], i: number): string {
  const tags = [0, 1, 2].map((k) => vocab[(i * 3 + k) % vocab.length]!);
  if (i >= 12) tags.push(vocab[int(0, vocab.length - 1)]!);
  switch (i % 5) {
    case 0:
      return tags.join(', ');
    case 1:
      return tags.join(',');
    case 2:
      return tags.join(', ') + ', ';
    case 3:
      return ' ' + tags.join(' , ');
    default:
      return tags.map((t) => (i % 2 ? (t.toUpperCase() === t ? t : t) : t)).join(', ');
  }
}

const SCORES_MAIN = [
  '1 - Awful',
  '2 - Terrible',
  '3 - Bad',
  '4 - Below Average',
  '5 - Average',
  '6 - Above Average',
  '7 - Good',
  '8 - Great',
  '9 - Amazing',
  '10 - Masterpiece',
];
const SCORES_NEW = [
  '1 - Trash',
  '2 - Very Bad',
  '3 - Bad',
  '4 - Below Average',
  '5 - Mid',
  '6 - Decent',
  '7 - Good',
  '8 - Great',
  '9 - Amazing',
  '10 - Masterpiece',
];
const SCORES_WATCH = [
  '1 - Terrible',
  '2 - Bad',
  '3 - Poor',
  '4 - Mediocre',
  '5 - Average',
  '6 - Decent',
  '7 - Good',
  '8 - Great',
  '9 - Excellent',
  '10 - Masterpiece',
];

const CHANNELS = ['RecapRiver', 'PanelPilot', 'NightOwlTales'];
function youtubeCell(i: number): string | null {
  switch (i % 6) {
    case 0:
      return `youtu.be/${'abcdefghijk'
        .split('')
        .map((_, k) => 'AbC0dEf1GhI2jKl3'[(i + k) % 16])
        .join('')}?si=zzTop${i}`;
    case 1:
      return `https://www.youtube.com/watch?v=Fx${i}QwErTy12`;
    case 2:
      return `(S1 #full) ${title(' Recap')} (${int(1, 12)}/${int(1, 28)}/26) By: ${pick(CHANNELS)}`;
    case 3:
      return `www.youtube.com/playlist?list=PLsynthetic${i}`;
    default:
      return null;
  }
}

const NOTE_A = [
  'Strong start',
  'Slow middle but worth it',
  'Art is gorgeous',
  'Great side characters',
  'Predictable yet fun',
  'Fantastic fight scenes',
];
const LEFT_OFF = [
  'Left off at the harbor festival',
  'Left off after the council vote',
  'Ended with the ferry departure',
  'Left off right before the time skip',
  'Left off as the apprentice arrives',
];

type Row = (string | number | null)[];

function banner(ws: ExcelJS.Worksheet, text: string, cols: number, color: string) {
  ws.mergeCells(1, 1, 1, cols);
  const c = ws.getCell(1, 1);
  c.value = text;
  c.font = { bold: true, size: 16, color: { argb: 'FFFFFFFF' } };
  c.fill = { type: 'pattern', pattern: 'solid', fgColor: { argb: `FF${color.slice(1)}` } };
}

function sheet(
  wb: ExcelJS.Workbook,
  name: string,
  color: string | { theme: number },
  headers: string[],
  rows: Row[],
  extraCols = 0,
) {
  const ws = wb.addWorksheet(name, {
    properties: { tabColor: typeof color === 'string' ? { argb: `FF${color.slice(1)}` } : color },
    views: [{ state: 'frozen', ySplit: 2 }],
  });
  banner(ws, `${name} Tracker`, headers.length, typeof color === 'string' ? color : '#8B2252');
  ws.getRow(2).values = [...headers, ...Array.from({ length: extraCols }, () => null)];
  rows.forEach((r, i) => (ws.getRow(i + 3).values = r));
  headers.forEach((_, i) => (ws.getColumn(i + 1).width = 20));
  return ws;
}

function instructions(wb: ExcelJS.Workbook) {
  const ws = wb.addWorksheet('Add Category - Instructions');
  ws.getCell('A1').value = 'How to add a new category';
  ws.getCell('A3').value = '1. Press Alt+F11 and paste the macro below.';
  ws.getCell('A4').value = "Sub AddCategory()  ' synthetic placeholder, not a real macro";
  ws.getCell('G2').value = 'Rankings of Good:';
  ['Good', 'Pretty Good', 'Really Good', 'REALLY good'].forEach(
    (t, i) => (ws.getCell(`G${3 + i}`).value = t),
  );
}

// ---------------------------------------------------------------- reading workbook
const R_HEAD = [
  'Title',
  'Status',
  'Score (1–10)',
  'Genres / Tags',
  'Currently on Chapter',
  'Watched on YouTube Up To Chapter',
  'YouTube Link',
  'Reading Method',
  'General Notes',
  'Extra Notes 1',
  'Extra Notes 2',
  'Other Links',
];
const READ_PROGRESS_CURRENT: (string | number)[] = [
  'Beginning of S2',
  'Finished?',
  'S2 E1',
  'End of S1',
];
const READ_PROGRESS_DONE: (string | number)[] = ['Completed', 'Done', 'Finished'];
const READ_PROGRESS_YT: (string | number | null)[] = ['End', 'End of S2', 'Completed', '49?'];

function readingRows(n: number, completedIdx: Set<number>, offset: number, near?: string): Row[] {
  return Array.from({ length: n }, (_, k) => {
    const i = offset + k;
    const done = completedIdx.has(i);
    const ci = [...completedIdx].indexOf(i);
    const cur = int(20, 330);
    const t = near && k === 0 ? near : title();
    const hasYT = done || i % 4 !== 3;
    const scoreSet = i % 3 === 0 ? SCORES_MAIN : SCORES_NEW;
    let score: string | null = i % 7 === 6 ? null : scoreSet[int(4, 9)]!;
    if (score && i % 5 === 4) score = ` ${score}`; // leading-space variant
    const current: string | number = done
      ? READ_PROGRESS_DONE[ci % 3]!
      : i % 17 === 5
        ? READ_PROGRESS_CURRENT[i % 4]!
        : cur;
    const consumed: string | number | null = !hasYT
      ? null
      : done
        ? READ_PROGRESS_YT[ci % 3]!
        : i % 13 === 4
          ? READ_PROGRESS_YT[3]!
          : typeof current === 'number'
            ? current - (i % 3 === 0 ? 1 : 0)
            : null;
    return [
      t,
      done ? 'Completed' : 'Reading',
      score,
      tagCell(READ_TAGS, i),
      current,
      consumed,
      youtubeCell(i),
      ['Youtube', 'YouTube', 'Youtube, Inkreader', 'Youtube'][i % 4]!,
      `${pick(NOTE_A)}.`,
      done ? null : pick(LEFT_OFF),
      i % 9 === 0 ? 'Check the side story' : null,
      i % 11 === 0
        ? `manhwa-site.example/series/${i}`
        : i % 11 === 5
          ? ` tapas.example/episode/${i}`
          : null,
    ] as Row;
  });
}

async function buildReading() {
  const wb = new ExcelJS.Workbook();
  wb.created = wb.modified = new Date('2024-01-01T00:00:00Z');
  // 9 completed spread across the 66 rows
  const completed = new Set([2, 8, 15, 23, 31, 40, 47, 55, 61]);
  const near = 'Quartz Hollow Gathering';
  const romance = readingRows(32, completed, 0, near);
  const action = readingRows(33, completed, 32);
  action[5]![0] = 'Quartz Hollow Gatherings'; // near-duplicate title (warning only)
  usedTitles.add('quartz hollow gatherings');
  const fantasy = readingRows(1, completed, 65);
  sheet(wb, 'Romance', '#8B2252', R_HEAD, romance);
  sheet(wb, 'Isekai', '#4A148C', R_HEAD, []);
  sheet(wb, 'Action', '#B71C1C', R_HEAD, action, 3);
  sheet(wb, 'Fantasy', '#004D40', R_HEAD, fantasy);
  sheet(wb, 'Misc.', '#1A237E', R_HEAD, []);
  instructions(wb);
  await wb.xlsx.writeFile(path.join(OUT, 'Reading_Tracker.xlsx'));
}

// ---------------------------------------------------------------- watching workbook
const W_HEAD = [
  'Title',
  'Status',
  'Score (1–10)',
  'Genres / Tags',
  'Season',
  'Currently on Episode',
  'Watched Until Episode',
  'Watch Platform',
  'Link',
  'General Notes',
  'Extra Notes 1',
  'Extra Notes 2',
];

function watchingRows(statuses: (string | null)[], offset: number): Row[] {
  return statuses.map((status, k) => {
    const i = offset + k;
    const done = status === 'Finished';
    const season: string | null =
      i % 6 === 0 ? 'N/A' : i % 10 === 3 ? 'Done' : i % 8 === 2 ? 'S2' : null;
    const current: string | number | null = done
      ? 'Done'
      : i % 12 === 7
        ? 'S2 E1'
        : status === 'Dropped' || status === 'On Hold'
          ? int(3, 40)
          : int(1, 230);
    const consumed: string | number | null =
      i % 12 === 7 ? 'S1 E13' : done && i % 4 === 0 ? 'Done' : null;
    return [
      title(),
      status,
      i % 6 === 5 ? null : SCORES_WATCH[int(5, 8)]!,
      tagCell(WATCH_TAGS, i),
      season,
      current,
      consumed,
      i % 3 === 0 ? 'OtterTV' : i % 3 === 1 ? 'StreamBox' : 'ClipHub',
      i % 5 === 0 ? `ottertv.example/series/${i}` : null,
      i % 2 ? `${pick(NOTE_A)}.` : null,
      null,
      null,
    ] as Row;
  });
}

async function buildWatching() {
  const wb = new ExcelJS.Workbook();
  wb.created = wb.modified = new Date('2024-01-01T00:00:00Z');
  const statuses: (string | null)[] = [
    ...Array(18).fill('Finished'),
    ...Array(14).fill('Watching'),
    ...Array(7).fill('On Hold'),
    ...Array(3).fill('Dropped'),
    ...Array(6).fill(null),
  ];
  // shuffle deterministically
  for (let i = statuses.length - 1; i > 0; i--) {
    const j = Math.floor(rand() * (i + 1));
    [statuses[i], statuses[j]] = [statuses[j]!, statuses[i]!];
  }
  const rows = watchingRows(statuses, 0);
  const sheets: [string, string | { theme: number }, Row[]][] = [
    ['Anime', '#781E82', rows.slice(0, 43)],
    ['Action Anime', '#B71C1C', []],
    ['Romance Anime', '#8B2252', []],
    ['Donghua', '#0F7B6C', []],
    ['Action Donghua', { theme: 8 }, rows.slice(43, 47)],
    ['Romance Donghua', { theme: 8 }, []],
    ['TV Shows', '#1565C0', []],
    ['Cartoons', '#F57F17', []],
    ['Sci-Fi', '#0D47A1', rows.slice(47, 48)],
    ['K-Drama', '#00897B', []],
    ['Horror', '#212121', []],
    ['Comedy', '#E66400', []],
    ['Documentary', '#4E342E', []],
    ['Slice of Life', '#2E7D32', []],
    ['Thriller', '#3E2723', []],
    ['Misc.', '#1A237E', []],
  ];
  for (const [n, c, r] of sheets) sheet(wb, n, c, W_HEAD, r);
  const ws = wb.addWorksheet('Add Category - Instructions');
  ws.getCell('A1').value = 'Synthetic instructions sheet';
  await wb.xlsx.writeFile(path.join(OUT, 'Watching_Tracker.xlsx'));
}

// ---------------------------------------------------------------- csv samples
function writeCsv() {
  const head =
    'Title,Status,Score (1–10),Genres / Tags,Currently on Chapter,Reading Method,General Notes\r\n';
  const utf8 = Buffer.concat([
    Buffer.from([0xef, 0xbb, 0xbf]),
    Buffer.from(
      head +
        '"Café ""Noir"" Chronicle",Reading,7 - Good,"Romance, Sorcery",12,Youtube,"Line one\nline two"\r\n' +
        'Plain Title,Completed,9,"Action,Slapstick",Done,"Youtube, Inkreader",\r\n',
      'utf8',
    ),
  ]);
  writeFileSync(path.join(OUT, 'sample-reading.csv'), utf8);
  writeFileSync(
    path.join(OUT, 'sample-semicolon.csv'),
    'Title;Status;Score (1–10);Genres / Tags;Currently on Episode;Watch Platform\r\nSemicolon Show;Watching;8;Fantasy;4;StreamBox\r\n',
  );
  writeFileSync(
    path.join(OUT, 'sample-cp1252.csv'),
    iconv.encode(
      'Title,Status,Currently on Chapter\r\nÉcole Maudite,Reading,3\r\nNaïve Hero,Plan to Read,\r\n',
      'win1252',
    ),
  );
}

mkdirSync(OUT, { recursive: true });
await buildReading();
await buildWatching();
writeCsv();
writeFileSync(
  path.join(OUT, 'expected.json'),
  JSON.stringify(
    {
      reading: {
        total: 66,
        byCategory: { Romance: 32, Action: 33, Fantasy: 1 },
        byRawStatus: { Reading: 57, Completed: 9 },
        tagsRaw: 36,
        tagsNormalized: 35,
      },
      watching: {
        total: 48,
        byCategory: { Anime: 43, 'Action Donghua': 4, 'Sci-Fi': 1 },
        byRawStatus: { Finished: 18, Watching: 14, 'On Hold': 7, Dropped: 3, '(blank)': 6 },
        tagsRaw: 14,
        tagsNormalized: 14,
      },
    },
    null,
    2,
  ) + '\n',
);
console.log('fixtures written to', OUT);

import ExcelJS from 'exceljs';
import Papa from 'papaparse';
import {
  readableTextColor,
  progressCell,
  scoreLabel,
  statusLabel,
  type Category,
  type Entry,
  type Library,
  type Link,
} from '@lorekeeper/shared';
import type { Store } from '../storage/store.js';
import { LAYOUTS, type Field } from './columns.js';

function linkText(l: Link): string {
  return l.raw ?? l.url ?? l.label ?? '';
}

/** Cell value for one field of an entry. */
export function cellFor(store: Store, e: Entry, f: Field): string | number | null {
  switch (f) {
    case 'title':
      return e.title;
    case 'status':
      return statusLabel(store.scales, e.library, e.status) || null;
    case 'score':
      return scoreLabel(store.scales, e.library, e.score) || null;
    case 'tags':
      return e.tags.length ? e.tags.join(', ') : null;
    case 'season':
      return e.season;
    case 'current':
      return progressCell(e.current);
    case 'consumed':
      return progressCell(e.consumedUpTo);
    case 'sources':
      return e.sources.length ? e.sources.join(', ') : null;
    case 'general':
      return e.notes.general;
    case 'extra1':
      return e.notes.extra1;
    case 'extra2':
      return e.notes.extra2;
    case 'youtubeLink':
    case 'otherLinks':
    case 'link': {
      const primary = e.links.filter(
        (l) => l.slot === 'primary' || (!l.slot && l.kind === 'youtube'),
      );
      const primaryFallback = primary.length ? primary : [];
      const other = e.links.filter((l) => !primaryFallback.includes(l));
      if (f === 'youtubeLink') return primaryFallback[0] ? linkText(primaryFallback[0]) : null;
      if (f === 'otherLinks') return other.length ? other.map(linkText).join('\n') : null;
      // watching "Link": every link, primary first (there is no Other Links column)
      const all = [...primaryFallback, ...other].map(linkText);
      return all.length ? all.join('\n') : null;
    }
    default:
      return null;
  }
}

function safeSheetName(name: string, used: Set<string>): string {
  const base =
    name
      .replace(/[\\/*?:[\]]/g, ' ')
      .trim()
      .slice(0, 31) || 'Sheet';
  let n = base;
  for (let i = 2; used.has(n.toLowerCase()); i++) n = `${base.slice(0, 28)} ${i}`;
  used.add(n.toLowerCase());
  return n;
}

const tint = (hex: string, amount: number) => {
  const n = parseInt(hex.slice(1), 16);
  const mix = (c: number) => Math.round(c + (255 - c) * amount);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
    .map(mix)
    .map((x) => x.toString(16).padStart(2, '0'))
    .join('')
    .toUpperCase();
};

export async function exportXlsx(store: Store, library: Library): Promise<Buffer> {
  const layout = LAYOUTS[library];
  const wb = new ExcelJS.Workbook();
  wb.creator = 'Lorekeeper';
  const used = new Set<string>();
  const cats = store.categories
    .filter((c) => c.library === library)
    .sort((a, b) => a.order - b.order);
  const statusList = Object.values(store.scales[library].statusLabels);
  const scoreList = store.scales[library].scoreLabels;
  const lastCol = layout.headers.length;

  for (const cat of cats) {
    const color = cat.color.slice(1).toUpperCase();
    const ws = wb.addWorksheet(safeSheetName(cat.name, used), {
      properties: { tabColor: { argb: `FF${color}` } },
      views: [{ state: 'frozen', ySplit: 2, xSplit: 0 }],
    });
    ws.mergeCells(1, 1, 1, lastCol);
    const banner = ws.getCell(1, 1);
    banner.value = `${cat.name} Tracker`;
    banner.font = {
      bold: true,
      size: 16,
      color: { argb: `FF${readableTextColor(cat.color).slice(1).toUpperCase()}` },
    };
    banner.fill = { type: 'pattern', pattern: 'solid', fgColor: { argb: `FF${color}` } };
    banner.alignment = { vertical: 'middle', horizontal: 'center' };
    ws.getRow(1).height = 30;

    const hdr = ws.getRow(2);
    layout.headers.forEach((h, i) => {
      const c = hdr.getCell(i + 1);
      c.value = h;
      c.font = {
        bold: true,
        color: { argb: `FF${readableTextColor(cat.color).slice(1).toUpperCase()}` },
      };
      c.fill = { type: 'pattern', pattern: 'solid', fgColor: { argb: `FF${tint(cat.color, 0)}` } };
      c.alignment = { vertical: 'middle', horizontal: 'center', wrapText: true };
      c.border = { bottom: { style: 'thin', color: { argb: 'FFFFFFFF' } } };
    });
    hdr.height = 22;
    layout.widths.forEach((w, i) => (ws.getColumn(i + 1).width = w));

    const entries = [...store.entriesOf(cat.id)].sort((a, b) =>
      a.title.localeCompare(b.title, undefined, { sensitivity: 'base' }),
    );
    const lastRow = Math.max(entries.length + 2, library === 'reading' ? 200 : 1000);
    const shade = `FF${tint(cat.color, 0.9)}`;
    for (let r = 3; r <= lastRow; r++) {
      const row = ws.getRow(r);
      const e = entries[r - 3];
      for (let c = 1; c <= lastCol; c++) {
        const cell = row.getCell(c);
        if (e) cell.value = cellFor(store, e, layout.fields[c - 1]!);
        cell.alignment = { vertical: 'top', wrapText: true };
        if ((r - 3) % 2 === 1)
          cell.fill = { type: 'pattern', pattern: 'solid', fgColor: { argb: shade } };
      }
    }
    // ExcelJS supports range-based validations at runtime but does not type them.
    const dv = (ws as unknown as { dataValidations: { add(range: string, rule: object): void } })
      .dataValidations;
    const si = layout.fields.indexOf('status') + 1;
    const sc = layout.fields.indexOf('score') + 1;
    const colL = (n: number) => String.fromCharCode(64 + n);
    dv.add(`${colL(si)}3:${colL(si)}${lastRow}`, {
      type: 'list',
      allowBlank: true,
      formulae: [`"${statusList.join(',')}"`],
    });
    dv.add(`${colL(sc)}3:${colL(sc)}${lastRow}`, {
      type: 'list',
      allowBlank: true,
      formulae: [`"${scoreList.join(',')}"`],
    });
  }
  return Buffer.from(await wb.xlsx.writeBuffer());
}

export function csvRows(store: Store, library: Library, entries: Entry[], withCategory: boolean) {
  const layout = LAYOUTS[library];
  const headers = withCategory ? [...layout.headers, 'Category'] : layout.headers;
  const catName = new Map(store.categories.map((c) => [c.id, c.name]));
  const data = entries.map((e) => {
    const row: (string | number | null)[] = layout.fields.map((f) => cellFor(store, e, f));
    if (withCategory) row.push(catName.get(e.categoryId) ?? '');
    return row;
  });
  return { headers, data };
}

/** UTF-8 with BOM so Excel opens it cleanly. */
export function exportCsv(
  store: Store,
  library: Library,
  entries: Entry[],
  withCategory = true,
): Buffer {
  const { headers, data } = csvRows(store, library, entries, withCategory);
  const text = Papa.unparse({ fields: headers, data }, { newline: '\r\n' });
  return Buffer.concat([Buffer.from([0xef, 0xbb, 0xbf]), Buffer.from(text, 'utf8')]);
}

export function csvTemplate(library: Library): Buffer {
  return Buffer.concat([
    Buffer.from([0xef, 0xbb, 0xbf]),
    Buffer.from(
      Papa.unparse({ fields: [...LAYOUTS[library].headers, 'Category'], data: [] }) + '\r\n',
      'utf8',
    ),
  ]);
}

export type { Category };

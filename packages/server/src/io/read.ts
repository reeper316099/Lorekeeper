import { Buffer } from 'node:buffer';
import ExcelJS from 'exceljs';
import { strFromU8, strToU8, unzipSync, zipSync } from 'fflate';
import iconv from 'iconv-lite';
import Papa from 'papaparse';
import { SEED_CATEGORIES, type Library } from '@lorekeeper/shared';
import { detectLibrary, matchHeader, type Field } from './columns.js';

export interface ParsedRow {
  /** 1-based spreadsheet row number */
  row: number;
  cells: unknown[];
}

export interface ParsedSheet {
  name: string;
  /** "#RRGGBB" from the tab color, if it is a plain RGB color */
  color: string | null;
  headerRow: number;
  headers: string[];
  mapping: (Field | null)[];
  library: Library | null;
  rows: ParsedRow[];
  skipped?: string;
}

const SKIP_NAME = /instruction|setup/i;

/** Flatten ExcelJS cell values (rich text, hyperlinks, formulas, dates) to plain primitives. */
export function plainCell(v: unknown): unknown {
  if (v === null || v === undefined) return null;
  if (v instanceof Date) return v.toISOString().slice(0, 10);
  if (typeof v === 'object') {
    const o = v as Record<string, unknown>;
    if (Array.isArray(o.richText))
      return (o.richText as { text: string }[]).map((r) => r.text).join('');
    if ('result' in o) return plainCell(o.result);
    if ('text' in o) return plainCell(o.text);
    if ('hyperlink' in o) return String(o.hyperlink);
    if ('error' in o) return null;
    return String(v);
  }
  return v;
}

const isBlank = (v: unknown) =>
  v === null || v === undefined || (typeof v === 'string' && v.trim() === '');

/** Find the header row (has Title and Status) within the first few rows. */
export function findHeaderRow(rows: unknown[][]): number {
  for (let i = 0; i < Math.min(rows.length, 6); i++) {
    const keys = (rows[i] ?? []).map((c) =>
      String(c ?? '')
        .trim()
        .toLowerCase(),
    );
    if (keys.includes('title') && keys.includes('status')) return i;
  }
  return -1;
}

function build(name: string, color: string | null, grid: unknown[][]): ParsedSheet {
  const base = { name, color, headerRow: 0, headers: [], mapping: [], library: null, rows: [] };
  if (SKIP_NAME.test(name)) return { ...base, skipped: 'Instructions/setup sheet' };
  const h = findHeaderRow(grid);
  if (h < 0) return { ...base, skipped: 'No header row with Title and Status' };
  const headerCells = grid[h]!;
  let width = headerCells.length;
  while (width > 0 && isBlank(headerCells[width - 1])) width--;
  // Keep stray columns too (so we can warn about them) — scan data for the real width.
  const rows: ParsedRow[] = [];
  let maxData = width;
  for (let i = h + 1; i < grid.length; i++) {
    const cells = grid[i] ?? [];
    let last = cells.length;
    while (last > 0 && isBlank(cells[last - 1])) last--;
    if (last === 0) continue;
    maxData = Math.max(maxData, last);
    rows.push({ row: i + 1, cells });
  }
  const headers = Array.from({ length: maxData }, (_, i) => String(headerCells[i] ?? '').trim());
  const mapping = headers.map((x) => matchHeader(x));
  return {
    name,
    color,
    headerRow: h + 1,
    headers,
    mapping,
    library: detectLibrary(headers),
    rows,
  };
}

function argbToHex(argb: unknown): string | null {
  if (typeof argb !== 'string' || argb.length < 6) return null;
  const hex = argb.slice(-6);
  return /^[0-9a-f]{6}$/i.test(hex) ? `#${hex.toUpperCase()}` : null;
}

/**
 * ExcelJS expands whole-column data validations (e.g. B33:B1048576) into one object per cell,
 * which takes seconds. Import never needs validations, so drop them from the sheet XML first.
 */
export function stripDataValidations(data: Uint8Array): Uint8Array {
  const files = unzipSync(new Uint8Array(data));
  let changed = false;
  for (const [name, bytes] of Object.entries(files)) {
    if (!/^xl\/worksheets\/[^/]+\.xml$/.test(name)) continue;
    const xml = strFromU8(bytes);
    const out = xml.replace(/<dataValidations[\s\S]*?<\/dataValidations>/g, '');
    if (out !== xml) {
      files[name] = strToU8(out);
      changed = true;
    }
  }
  return changed ? zipSync(files) : data;
}

export async function readXlsx(data: Buffer | Uint8Array): Promise<ParsedSheet[]> {
  const wb = new ExcelJS.Workbook();
  await wb.xlsx.load(stripDataValidations(data) as never);
  const out: ParsedSheet[] = [];
  wb.eachSheet((ws) => {
    if (SKIP_NAME.test(ws.name)) {
      out.push(build(ws.name, null, []));
      return;
    }
    const grid: unknown[][] = [];
    ws.eachRow((row, r) => {
      const cells: unknown[] = [];
      row.eachCell((cell, c) => {
        const v = plainCell(cell.value);
        if (v !== null) cells[c - 1] = v;
      });
      grid[r - 1] = cells;
    });
    const color = argbToHex((ws.properties.tabColor as { argb?: string } | undefined)?.argb);
    out.push(build(ws.name, color, grid));
  });
  return out;
}

export function decodeText(buf: Buffer | Uint8Array): string {
  const b = Buffer.from(buf);
  const body = b[0] === 0xef && b[1] === 0xbb && b[2] === 0xbf ? b.subarray(3) : b;
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(body);
  } catch {
    return iconv.decode(Buffer.from(body), 'win1252');
  }
}

export function readCsv(data: Buffer | Uint8Array, fileName: string): ParsedSheet[] {
  const text = decodeText(data);
  const parsed = Papa.parse<string[]>(text, {
    skipEmptyLines: 'greedy',
    delimitersToGuess: [',', ';', '\t'],
  });
  const name = fileName.replace(/\.[^.]+$/, '') || 'Imported';
  return [build(name, null, parsed.data)];
}

/** Fallback color for a category name when the tab color is missing or themed. */
export function seedColor(library: Library, name: string): string | null {
  return (
    SEED_CATEGORIES.find(
      (c) => c.library === library && c.name.toLowerCase() === name.toLowerCase(),
    )?.color ?? null
  );
}

export async function readSpreadsheet(
  data: Buffer | Uint8Array,
  fileName: string,
): Promise<ParsedSheet[]> {
  const lower = fileName.toLowerCase();
  if (/\.(xlsx|xlsm)$/.test(lower)) return readXlsx(data);
  if (/\.(csv|tsv|txt)$/.test(lower)) return readCsv(data, fileName);
  throw new Error('Unsupported file type (use .xlsx, .xlsm, .csv or .tsv)');
}

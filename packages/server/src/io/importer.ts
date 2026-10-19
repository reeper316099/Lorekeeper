import { promises as fs } from 'node:fs';
import path from 'node:path';
import { randomBytes } from 'node:crypto';
import { uuidv7 } from 'uuidv7';
import {
  DEFAULT_CATEGORY_COLOR,
  EntrySchema,
  NEW_CATEGORY_PALETTE,
  WATCHING_MEDIA_TYPE_BY_CATEGORY,
  cleanText,
  normalizeTitle,
  parseLink,
  parseProgress,
  parseScore,
  parseSources,
  parseStatus,
  slugify,
  splitTags,
  suggestTagMerges,
  isNearDuplicateTitle,
  type Category,
  type Entry,
  type Library,
  type Link,
  type Status,
} from '@lorekeeper/shared';
import { atomicWrite, stableStringify } from '../storage/atomic.js';
import type { Store } from '../storage/store.js';
import { HttpError } from '../storage/store.js';
import type { Field } from './columns.js';
import { readSpreadsheet, seedColor, type ParsedSheet } from './read.js';

export type Strategy = 'skip' | 'fill' | 'overwrite' | 'keep_both';

export interface ImportOptions {
  strategy?: Strategy;
  /** Force a library for sheets whose headers do not reveal it (CSV). */
  library?: Library;
  /** Force a category name (CSV without a Category column). */
  category?: string;
  sheets?: Record<
    string,
    { include?: boolean; library?: Library; category?: string; mapping?: (Field | null)[] }
  >;
  /** Map raw unknown status text → status (the "let me pick" step). */
  statusMap?: Record<string, Status>;
}

export interface ImportWarning {
  code:
    | 'BLANK_STATUS'
    | 'UNKNOWN_STATUS'
    | 'BAD_SCORE'
    | 'STRAY_COLUMN'
    | 'UNKNOWN_COLUMN'
    | 'NEAR_DUPLICATE'
    | 'DUPLICATE_IN_FILE'
    | 'SHEET_SKIPPED'
    | 'NO_LIBRARY'
    | 'CATEGORY_DIFFERS';
  sheet: string;
  row?: number;
  title?: string;
  message: string;
}

export interface SheetPlan {
  name: string;
  skipped?: string;
  library: Library | null;
  libraryDetected: Library | null;
  color: string | null;
  headerRow: number;
  mapping: { col: number; header: string; field: Field | null }[];
  rows: number;
  categoryExists: boolean;
  categoryName: string;
}

export interface ImportPlan {
  sheets: SheetPlan[];
  newCategories: { library: Library; name: string; color: string }[];
  creates: Entry[];
  updates: Entry[];
  unchanged: number;
  skipped: number;
  warnings: ImportWarning[];
  summary: {
    entries: number;
    byCategory: Record<string, number>;
    byRawStatus: Record<string, number>;
    byStatus: Record<string, number>;
    distinctTagsRaw: number;
    distinctTagsNormalized: number;
  };
  tagSuggestions: ReturnType<typeof suggestTagMerges>;
}

type Cells = Partial<Record<Field, unknown>>;

/** Resolve existing or planned categories without touching the store. */
class CategoryResolver {
  readonly pending: { library: Library; name: string; color: string; slug: string; id: string }[] =
    [];
  constructor(private existing: Category[]) {}

  resolve(library: Library, name: string, color: string | null): { id: string; isNew: boolean } {
    const lc = name.trim().toLowerCase();
    const have = this.existing.find((c) => c.library === library && c.name.toLowerCase() === lc);
    if (have) return { id: have.id, isNew: false };
    const planned = this.pending.find((c) => c.library === library && c.name.toLowerCase() === lc);
    if (planned) return { id: planned.id, isNew: true };
    let slug = slugify(name);
    const base = slug;
    const taken = (s: string) =>
      [...this.existing, ...this.pending].some((c) => c.library === library && c.slug === s);
    for (let i = 2; taken(slug); i++) slug = `${base}-${i}`;
    const entry = {
      library,
      name: name.trim(),
      slug,
      id: `${library}-${slug}`,
      color:
        color ?? seedColor(library, name) ?? NEW_CATEGORY_PALETTE[lc] ?? DEFAULT_CATEGORY_COLOR,
    };
    this.pending.push(entry);
    return { id: entry.id, isNew: true };
  }
}

function cellsOf(mapping: (Field | null)[], cells: unknown[]): Cells {
  const out: Cells = {};
  mapping.forEach((f, i) => {
    if (f && out[f] === undefined) out[f] = cells[i];
  });
  return out;
}

function fieldsEmpty(e: Entry) {
  return {
    status: e.status === null,
    score: e.score === null,
    tags: e.tags.length === 0,
    season: e.season === null,
    current: e.current.raw === null,
    consumedUpTo: e.consumedUpTo.raw === null,
    sources: e.sources.length === 0,
    links: e.links.length === 0,
    general: !e.notes.general,
    extra1: !e.notes.extra1,
    extra2: !e.notes.extra2,
  };
}

/** Merge an incoming entry into an existing one. Returns null if nothing changed. */
export function mergeInto(
  existing: Entry,
  incoming: Entry,
  mode: 'fill' | 'overwrite',
): Entry | null {
  const empty = fieldsEmpty(existing);
  const take = (key: keyof typeof empty, hasIncoming: boolean) =>
    hasIncoming && (mode === 'overwrite' || empty[key]);
  const next: Entry = { ...existing, notes: { ...existing.notes } };
  if (take('status', incoming.status !== null)) next.status = incoming.status;
  if (take('score', incoming.score !== null)) next.score = incoming.score;
  if (take('tags', incoming.tags.length > 0)) next.tags = incoming.tags;
  if (take('season', incoming.season !== null)) next.season = incoming.season;
  if (take('current', incoming.current.raw !== null)) next.current = incoming.current;
  if (take('consumedUpTo', incoming.consumedUpTo.raw !== null))
    next.consumedUpTo = incoming.consumedUpTo;
  if (take('sources', incoming.sources.length > 0)) next.sources = incoming.sources;
  if (take('links', incoming.links.length > 0)) next.links = incoming.links;
  if (take('general', !!incoming.notes.general)) next.notes.general = incoming.notes.general;
  if (take('extra1', !!incoming.notes.extra1)) next.notes.extra1 = incoming.notes.extra1;
  if (take('extra2', !!incoming.notes.extra2)) next.notes.extra2 = incoming.notes.extra2;
  if (!existing.importRef && incoming.importRef) next.importRef = incoming.importRef;
  const same =
    stableStringify({ ...next, updatedAt: '', importRef: undefined }) ===
    stableStringify({ ...existing, updatedAt: '', importRef: undefined });
  if (same) return null;
  next.updatedAt = new Date().toISOString();
  return next;
}

export async function buildPlan(
  store: Store,
  fileName: string,
  sheets: ParsedSheet[],
  opts: ImportOptions = {},
): Promise<ImportPlan> {
  const strategy = opts.strategy ?? 'fill';
  const now = new Date().toISOString();
  const resolver = new CategoryResolver(store.categories);
  const warnings: ImportWarning[] = [];
  const sheetPlans: SheetPlan[] = [];
  const creates: Entry[] = [];
  const updates = new Map<string, Entry>();
  let unchanged = 0;
  let skipped = 0;

  // library + normalized title → entry (existing and newly created, so in-file dupes are caught)
  const byKey = new Map<string, Entry>();
  for (const e of store.all()) byKey.set(`${e.library}\u0000${normalizeTitle(e.title)}`, e);
  const pools = new Map<Library, Entry[]>();
  const libPool = (lib: Library) => {
    let p = pools.get(lib);
    if (!p) pools.set(lib, (p = store.all().filter((e) => e.library === lib)));
    return p;
  };

  const rawTags = new Set<string>();
  const normTags = new Set<string>();
  const byCategory: Record<string, number> = {};
  const byRawStatus: Record<string, number> = {};
  const byStatus: Record<string, number> = {};
  let total = 0;

  for (const sheet of sheets) {
    const sopts = opts.sheets?.[sheet.name];
    const library = sopts?.library ?? sheet.library ?? opts.library ?? null;
    const mapping = sopts?.mapping ?? sheet.mapping;
    const color = sheet.color ?? null;
    const sp: SheetPlan = {
      name: sheet.name,
      library,
      libraryDetected: sheet.library,
      color,
      headerRow: sheet.headerRow,
      mapping: sheet.headers.map((header, col) => ({ col, header, field: mapping[col] ?? null })),
      rows: sheet.rows.length,
      categoryExists: false,
      categoryName: sopts?.category ?? opts.category ?? sheet.name,
    };
    sheetPlans.push(sp);

    if (sheet.skipped || sopts?.include === false) {
      sp.skipped = sheet.skipped ?? 'Excluded';
      warnings.push({
        code: 'SHEET_SKIPPED',
        sheet: sheet.name,
        message: `Skipped: ${sp.skipped}`,
      });
      continue;
    }
    if (!library) {
      sp.skipped = 'Library could not be detected';
      warnings.push({
        code: 'NO_LIBRARY',
        sheet: sheet.name,
        message: 'Choose reading or watching for this sheet.',
      });
      continue;
    }
    if (!mapping.includes('title')) {
      sp.skipped = 'No Title column';
      warnings.push({
        code: 'SHEET_SKIPPED',
        sheet: sheet.name,
        message: 'No Title column mapped.',
      });
      continue;
    }
    sp.categoryExists = store.categories.some(
      (c) => c.library === library && c.name.toLowerCase() === sp.categoryName.toLowerCase(),
    );
    // Always create the sheet's category, even when empty, to preserve structure.
    if (!mapping.includes('category')) resolver.resolve(library, sp.categoryName, color);

    sheet.headers.forEach((h, i) => {
      if (mapping[i]) return;
      const hasData = sheet.rows.some(
        (r) =>
          !(r.cells[i] === null || r.cells[i] === undefined || String(r.cells[i]).trim() === ''),
      );
      if (!h && hasData)
        warnings.push({
          code: 'STRAY_COLUMN',
          sheet: sheet.name,
          message: `Unlabeled column ${i + 1} contains data and was ignored.`,
        });
      else if (h)
        warnings.push({
          code: 'UNKNOWN_COLUMN',
          sheet: sheet.name,
          message: `Column "${h}" is not recognised and was ignored.`,
        });
    });

    for (const r of sheet.rows) {
      const c = cellsOf(mapping, r.cells);
      const title = cleanText(c.title);
      if (!title) continue;
      total++;

      const catName = cleanText(c.category) ?? sp.categoryName;
      const cat = resolver.resolve(library, catName, cleanText(c.category) ? null : color);
      byCategory[catName] = (byCategory[catName] ?? 0) + 1;

      // status
      const rawStatus = cleanText(c.status);
      byRawStatus[rawStatus ?? '(blank)'] = (byRawStatus[rawStatus ?? '(blank)'] ?? 0) + 1;
      const ps = parseStatus(rawStatus);
      let status: Status;
      if (!ps.ok) {
        status = opts.statusMap?.[ps.raw] ?? 'planned';
        if (!opts.statusMap?.[ps.raw])
          warnings.push({
            code: 'UNKNOWN_STATUS',
            sheet: sheet.name,
            row: r.row,
            title,
            message: `Unknown status "${ps.raw}" → Planned. Pick a mapping to override.`,
          });
      } else {
        status = ps.status;
        if (ps.blank)
          warnings.push({
            code: 'BLANK_STATUS',
            sheet: sheet.name,
            row: r.row,
            title,
            message: 'Blank status imported as Planned.',
          });
      }
      byStatus[status] = (byStatus[status] ?? 0) + 1;

      // score
      const rawScore = cleanText(c.score);
      const score = parseScore(rawScore);
      if (rawScore && score === null)
        warnings.push({
          code: 'BAD_SCORE',
          sheet: sheet.name,
          row: r.row,
          title,
          message: `Could not read score "${rawScore}".`,
        });

      // tags
      const split = splitTags(c.tags);
      for (const t of String(c.tags ?? '').split(/[,;\n]/)) {
        const tt = t.replace(/\s+/g, ' ').trim();
        if (tt) {
          rawTags.add(tt);
          normTags.add(tt.toLowerCase());
        }
      }

      const links: Link[] = [];
      const yt = parseLink(c.youtubeLink, 'youtube');
      if (yt) links.push({ ...yt, slot: 'primary' });
      const lk = parseLink(c.link, 'other');
      if (lk) links.push({ ...lk, slot: 'primary' });
      const ol = parseLink(c.otherLinks, 'other');
      if (ol) links.push({ ...ol, slot: 'other' });

      const base = {
        id: uuidv7(),
        library,
        categoryId: cat.id,
        title,
        mediaType:
          library === 'watching'
            ? (WATCHING_MEDIA_TYPE_BY_CATEGORY[catName.toLowerCase()] ?? 'unknown')
            : 'unknown',
        status,
        score,
        tags: store.canonicalTags(split),
        season: cleanText(c.season),
        current: parseProgress(c.current),
        consumedUpTo: parseProgress(c.consumed),
        sources: parseSources(c.sources),
        links,
        notes: {
          general: cleanText(c.general),
          extra1: cleanText(c.extra1),
          extra2: cleanText(c.extra2),
        },
        createdAt: now,
        updatedAt: now,
        importRef: { file: fileName, sheet: sheet.name, row: r.row, importedAt: now },
      };
      const incoming = EntrySchema.parse(base);

      const key = `${library}\u0000${normalizeTitle(title)}`;
      const existing = updates.get(byKey.get(key)?.id ?? '') ?? byKey.get(key);
      if (existing && strategy !== 'keep_both') {
        if (creates.includes(existing))
          warnings.push({
            code: 'DUPLICATE_IN_FILE',
            sheet: sheet.name,
            row: r.row,
            title,
            message: 'Same title appears more than once in this import.',
          });
        if (strategy === 'skip') {
          skipped++;
          continue;
        }
        if (existing.categoryId !== incoming.categoryId)
          warnings.push({
            code: 'CATEGORY_DIFFERS',
            sheet: sheet.name,
            row: r.row,
            title,
            message: 'Existing entry is in another category; it was not moved.',
          });
        const merged = mergeInto(existing, incoming, strategy);
        if (!merged) {
          unchanged++;
        } else if (creates.includes(existing)) {
          creates[creates.indexOf(existing)] = merged;
          byKey.set(key, merged);
        } else {
          updates.set(merged.id, merged);
          byKey.set(key, merged);
        }
        continue;
      }

      // new entry: fuzzy near-duplicate warnings against everything in the same library
      // Fuzzy scan is quadratic, so it is skipped for very large libraries.
      const pool = libPool(library);
      for (const o of pool.length > 3000 ? [] : pool) {
        if (normalizeTitle(o.title) === normalizeTitle(title)) continue;
        if (isNearDuplicateTitle(o.title, title)) {
          warnings.push({
            code: 'NEAR_DUPLICATE',
            sheet: sheet.name,
            row: r.row,
            title,
            message: `Similar to existing title "${o.title}".`,
          });
          break;
        }
      }
      creates.push(incoming);
      libPool(library).push(incoming);
      byKey.set(key, incoming);
    }
  }

  const usage = new Map<string, number>();
  for (const e of [...store.all(), ...creates])
    for (const t of e.tags) usage.set(t, (usage.get(t) ?? 0) + 1);

  return {
    sheets: sheetPlans,
    newCategories: resolver.pending.map(({ library, name, color }) => ({ library, name, color })),
    creates,
    updates: [...updates.values()],
    unchanged,
    skipped,
    warnings,
    summary: {
      entries: total,
      byCategory,
      byRawStatus,
      byStatus,
      distinctTagsRaw: rawTags.size,
      distinctTagsNormalized: normTags.size,
    },
    tagSuggestions: suggestTagMerges(usage),
  };
}

export const publicPlan = (p: ImportPlan) => ({
  sheets: p.sheets,
  newCategories: p.newCategories,
  counts: {
    create: p.creates.length,
    update: p.updates.length,
    unchanged: p.unchanged,
    skipped: p.skipped,
  },
  warnings: p.warnings,
  summary: p.summary,
  tagSuggestions: p.tagSuggestions,
});

// ---------------- persistence: preview / commit / undo ----------------

const ID_RE = /^\d{8}-\d{6}-[0-9a-f]{6}$/;

function importDir(store: Store, id: string): string {
  if (!ID_RE.test(id)) throw new HttpError(400, 'VALIDATION', 'Invalid import id');
  return path.join(store.importsDir, id);
}

export function newId(): string {
  const d = new Date();
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}-${randomBytes(3).toString('hex')}`;
}

export async function stageUpload(store: Store, fileName: string, data: Buffer) {
  const id = newId();
  const dir = importDir(store, id);
  await fs.mkdir(dir, { recursive: true });
  const safeName = path.basename(fileName).replace(/[^\w.\- ]+/g, '_') || 'upload';
  await atomicWrite(path.join(dir, `source${path.extname(safeName).toLowerCase()}`), data);
  await atomicWrite(path.join(dir, 'meta.json'), stableStringify({ fileName: safeName }));
  return { id, fileName: safeName };
}

async function loadStaged(store: Store, id: string) {
  const dir = importDir(store, id);
  const meta = JSON.parse(
    (await fs.readFile(path.join(dir, 'meta.json'), 'utf8').catch(() => 'null')) ?? 'null',
  );
  if (!meta) throw new HttpError(404, 'NOT_FOUND', 'Unknown import id');
  const file = (await fs.readdir(dir)).find((f) => f.startsWith('source.'))!;
  const data = await fs.readFile(path.join(dir, file));
  return {
    dir,
    fileName: meta.fileName as string,
    sheets: await readSpreadsheet(data, meta.fileName),
  };
}

export async function previewImport(store: Store, id: string, opts: ImportOptions) {
  const { fileName, sheets } = await loadStaged(store, id);
  return { importId: id, fileName, ...publicPlan(await buildPlan(store, fileName, sheets, opts)) };
}

interface Manifest {
  existed: string[];
  created: string[];
  undone?: boolean;
}

export async function commitImport(store: Store, id: string, opts: ImportOptions) {
  const { dir, fileName, sheets } = await loadStaged(store, id);
  const preview = await buildPlan(store, fileName, sheets, opts);

  // Snapshot every file the import can touch (exactly restorable by undo).
  const snap = path.join(dir, 'snapshot');
  const rels = new Set<string>(['config/categories.json', 'config/tags.json']);
  const targetCats = new Set<string>(
    [...preview.creates, ...preview.updates].map((e) => e.categoryId),
  );
  for (const c of store.categories)
    if (targetCats.has(c.id)) rels.add(`data/${c.library}/${c.slug}.json`);
  const manifest: Manifest = { existed: [], created: [] };
  for (const rel of rels) {
    const src = path.join(store.dir, rel);
    const exists = await fs.access(src).then(
      () => true,
      () => false,
    );
    if (exists) {
      await fs.mkdir(path.dirname(path.join(snap, rel)), { recursive: true });
      await fs.copyFile(src, path.join(snap, rel));
      manifest.existed.push(rel);
    }
  }
  await store.backup('pre-import');

  // Create categories, then re-plan against the real ones so ids line up.
  for (const nc of preview.newCategories) {
    const cat = await store.ensureCategory(nc.library, nc.name, nc.color);
    manifest.created.push(`data/${cat.library}/${cat.slug}.json`);
  }
  await atomicWrite(path.join(dir, 'manifest.json'), stableStringify(manifest));
  const plan = await buildPlan(store, fileName, sheets, opts);
  const realCreated = plan.newCategories.length;
  if (realCreated)
    throw new HttpError(500, 'IMPORT_INCONSISTENT', 'Categories did not resolve after creation');
  await store.insertEntries(plan.creates);
  if (plan.updates.length) await store.replaceEntries(plan.updates);

  const report = {
    importId: id,
    file: fileName,
    committedAt: new Date().toISOString(),
    strategy: opts.strategy ?? 'fill',
    counts: {
      created: plan.creates.length,
      updated: plan.updates.length,
      unchanged: plan.unchanged,
      skipped: plan.skipped,
      categoriesCreated: preview.newCategories.length,
    },
    summary: plan.summary,
    warnings: plan.warnings,
    tagSuggestions: plan.tagSuggestions,
    createdIds: plan.creates.map((e) => e.id),
    updatedIds: plan.updates.map((e) => e.id),
  };
  await atomicWrite(path.join(dir, 'import-report.json'), stableStringify(report));
  return { ...report, undoToken: id };
}

export async function undoImport(store: Store, id: string) {
  const dir = importDir(store, id);
  const manifest: Manifest | null = JSON.parse(
    (await fs.readFile(path.join(dir, 'manifest.json'), 'utf8').catch(() => 'null')) ?? 'null',
  );
  if (!manifest) throw new HttpError(404, 'NOT_FOUND', 'Nothing to undo for this import');
  if (manifest.undone) throw new HttpError(409, 'ALREADY_UNDONE', 'Import was already undone');
  for (const rel of manifest.existed) {
    await atomicWrite(
      path.join(store.dir, rel),
      await fs.readFile(path.join(dir, 'snapshot', rel)),
    );
  }
  for (const rel of manifest.created) await fs.rm(path.join(store.dir, rel), { force: true });
  await atomicWrite(
    path.join(dir, 'manifest.json'),
    stableStringify({ ...manifest, undone: true }),
  );
  await store.loadAll();
  store.emit('change');
  return { ok: true };
}

/**
 * Snapshot every file an operation may touch so `undoImport(store, id)` can restore it exactly.
 * Used by importers other than the spreadsheet one (e.g. AniList list import).
 */
export async function beginUndoable(
  store: Store,
  targetCategoryIds: string[],
): Promise<{ id: string; dir: string }> {
  const id = newId();
  const dir = path.join(store.importsDir, id);
  const snap = path.join(dir, 'snapshot');
  const rels = new Set<string>(['config/categories.json', 'config/tags.json']);
  for (const c of store.categories)
    if (targetCategoryIds.includes(c.id)) rels.add(`data/${c.library}/${c.slug}.json`);
  const manifest: Manifest = { existed: [], created: [] };
  for (const rel of rels) {
    const src = path.join(store.dir, rel);
    if (
      await fs.access(src).then(
        () => true,
        () => false,
      )
    ) {
      await fs.mkdir(path.dirname(path.join(snap, rel)), { recursive: true });
      await fs.copyFile(src, path.join(snap, rel));
      manifest.existed.push(rel);
    }
  }
  await fs.mkdir(dir, { recursive: true });
  await atomicWrite(path.join(dir, 'manifest.json'), stableStringify(manifest));
  await store.backup('pre-anilist-import');
  return { id, dir };
}

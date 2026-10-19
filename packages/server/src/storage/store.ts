import { EventEmitter } from 'node:events';
import { promises as fs } from 'node:fs';
import path from 'node:path';
import chokidar, { type FSWatcher } from 'chokidar';
import { uuidv7 } from 'uuidv7';
import type { ZodType } from 'zod';
import {
  CategoriesFileSchema,
  DEFAULT_CATEGORY_COLOR,
  DEFAULT_SCALES,
  EntryFileSchema,
  EntrySchema,
  MetaSchema,
  NEW_CATEGORY_PALETTE,
  SCHEMA_VERSION,
  ScalesSchema,
  SettingsSchema,
  TagsFileSchema,
  WATCHING_MEDIA_TYPE_BY_CATEGORY,
  parseProgress,
  slugify,
  type ActivityRecord,
  type Category,
  type Entry,
  type Library,
  type Meta,
  type Progress,
  type Scales,
  type Settings,
  type Status,
  type Tag,
} from '@lorekeeper/shared';
import { KeyedMutex, atomicWrite, stableStringify } from './atomic.js';
import {
  createBackup,
  latestBackupText,
  listBackups,
  restoreBackupFiles,
  type BackupInfo,
} from './backups.js';
import { migrate, type Migration } from './migrations.js';

export class HttpError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details?: unknown,
  ) {
    super(message);
  }
}

export interface StoreOptions {
  appVersion?: string;
  migrations?: Migration[];
  schemaVersion?: number;
  watch?: boolean;
}

export type EntryInput = Partial<Omit<Entry, 'id' | 'createdAt' | 'updatedAt'>> & {
  title: string;
  categoryId: string;
};

function describeJsonError(text: string, err: unknown): string {
  const msg = err instanceof Error ? err.message : String(err);
  const m = /position (\d+)/.exec(msg);
  if (!m) return msg;
  const pos = Number(m[1]);
  const before = text.slice(0, pos);
  const line = before.split('\n').length;
  const col = pos - before.lastIndexOf('\n');
  return `${msg} (line ${line}, column ${col})`;
}

export class Store extends EventEmitter {
  readonly dir: string;
  meta!: Meta;
  settings!: Settings;
  scales!: Scales;
  categories: Category[] = [];
  tags: Tag[] = [];
  /** bumps on every change; caches key off it */
  version = 0;

  private byCat = new Map<string, Entry[]>();
  private mutex = new KeyedMutex();
  private lastWritten = new Map<string, string>();
  private watcher?: FSWatcher;
  private allCache?: { v: number; list: Entry[] };

  constructor(
    dir: string,
    private opts: StoreOptions = {},
  ) {
    super();
    this.dir = path.resolve(dir);
  }

  // ---------- paths ----------
  private p(...segs: string[]) {
    return path.join(this.dir, ...segs);
  }
  private catFile(c: Category) {
    return this.p('data', c.library, `${c.slug}.json`);
  }

  // ---------- lifecycle ----------
  async open(): Promise<void> {
    await fs.mkdir(this.dir, { recursive: true });
    for (const d of [
      'config',
      'data/reading',
      'data/watching',
      'activity',
      'backups',
      'exports',
      'imports',
      'logs',
    ])
      await fs.mkdir(this.p(d), { recursive: true });

    const now = new Date().toISOString();
    const metaText = await fs.readFile(this.p('meta.json'), 'utf8').catch(() => null);
    let meta: Meta | null = null;
    if (metaText) {
      try {
        meta = MetaSchema.parse(JSON.parse(metaText));
      } catch {
        await this.quarantine(this.p('meta.json'));
      }
    }
    const target = this.opts.schemaVersion ?? SCHEMA_VERSION;
    const fresh = !meta;
    this.meta = meta ?? {
      schemaVersion: target,
      appVersion: this.opts.appVersion ?? '0.1.0',
      createdAt: now,
      lastOpenedAt: now,
    };
    if (!fresh) {
      const v = await migrate(this.dir, this.meta.schemaVersion, {
        target,
        migrations: this.opts.migrations,
        backup: (r) => createBackup(this.dir, r, this.keep()),
      });
      this.meta.schemaVersion = v;
    }
    this.meta.lastOpenedAt = now;
    this.meta.appVersion = this.opts.appVersion ?? this.meta.appVersion;
    await this.writeJson(this.p('meta.json'), this.meta);

    await this.loadAll();

    const hours = this.settings.backup.onLaunchHours;
    const latest = (await listBackups(this.dir))[0];
    const stale = !latest || Date.now() - Date.parse(latest.createdAt) > hours * 3600_000;
    if (stale && this.all().length > 0) await this.backup('launch');
    if (this.opts.watch) this.startWatching();
  }

  async close(): Promise<void> {
    await this.watcher?.close();
    this.watcher = undefined;
  }

  private keep() {
    return this.settings?.backup.keep ?? 20;
  }

  async loadAll(): Promise<void> {
    this.settings = await this.readValidated(
      'config/settings.json',
      SettingsSchema,
      SettingsSchema.parse({}),
    );
    this.scales = await this.readValidated('config/scales.json', ScalesSchema, DEFAULT_SCALES);
    this.categories = await this.readValidated('config/categories.json', CategoriesFileSchema, []);
    this.tags = await this.readValidated('config/tags.json', TagsFileSchema, []);
    this.byCat.clear();
    for (const c of this.categories) {
      this.byCat.set(
        c.id,
        await this.readValidated(
          `data/${c.library}/${c.slug}.json`,
          EntryFileSchema,
          [] as Entry[],
        ),
      );
    }
    this.bump();
  }

  // ---------- file IO ----------
  async writeJson(file: string, data: unknown): Promise<void> {
    const text = stableStringify(data);
    await this.mutex.run(file, async () => {
      this.lastWritten.set(file, text);
      await atomicWrite(file, text);
    });
  }

  private async quarantine(file: string): Promise<string | null> {
    try {
      const dest = this.p('backups', 'corrupt', `${path.basename(file)}.${Date.now()}.bad`);
      await fs.mkdir(path.dirname(dest), { recursive: true });
      await fs.copyFile(file, dest);
      return dest;
    } catch {
      return null;
    }
  }

  private async readValidated<T>(rel: string, schema: ZodType<T>, fallback: T): Promise<T> {
    const file = this.p(rel);
    const text = await fs.readFile(file, 'utf8').catch(() => null);
    if (text === null) {
      await this.writeJson(file, fallback);
      return fallback;
    }
    const parsed = this.tryParse(text, schema);
    if (parsed.ok) return parsed.value;
    const q = await this.quarantine(file);
    this.emit('file-error', { file: rel, message: parsed.message, quarantined: q });
    const bak = await latestBackupText(this.dir, rel);
    if (bak) {
      const again = this.tryParse(bak, schema);
      if (again.ok) {
        await this.writeJson(file, again.value);
        this.emit('file-restored', { file: rel });
        return again.value;
      }
    }
    await this.writeJson(file, fallback);
    return fallback;
  }

  private tryParse<T>(
    text: string,
    schema: ZodType<T>,
  ): { ok: true; value: T } | { ok: false; message: string } {
    let json: unknown;
    try {
      json = JSON.parse(text);
    } catch (e) {
      return { ok: false, message: describeJsonError(text, e) };
    }
    const r = schema.safeParse(json);
    if (r.success) return { ok: true, value: r.data };
    const issue = r.error.issues[0];
    return { ok: false, message: `${issue?.path.join('.') ?? ''}: ${issue?.message ?? 'invalid'}` };
  }

  // ---------- watching ----------
  startWatching(): void {
    this.watcher = chokidar.watch([this.p('config'), this.p('data')], {
      ignoreInitial: true,
      ignored: (f) => f.endsWith('.tmp'),
      awaitWriteFinish: { stabilityThreshold: 150, pollInterval: 50 },
    });
    const onFile = (f: string) => void this.onExternalChange(f);
    this.watcher.on('add', onFile).on('change', onFile);
  }

  private async onExternalChange(file: string): Promise<void> {
    if (!file.endsWith('.json')) return;
    const text = await fs.readFile(file, 'utf8').catch(() => null);
    if (text === null || this.lastWritten.get(file) === text) return;
    const rel = path.relative(this.dir, file).split(path.sep).join('/');
    const apply = <T>(schema: ZodType<T>, set: (v: T) => void) => {
      const r = this.tryParse(text, schema);
      if (!r.ok) {
        this.emit('file-error', { file: rel, message: r.message, quarantined: null, kept: true });
        return;
      }
      this.lastWritten.set(file, text);
      set(r.value);
      this.bump();
      this.emit('external-change', { file: rel });
    };
    if (rel === 'config/settings.json') apply(SettingsSchema, (v) => (this.settings = v));
    else if (rel === 'config/scales.json') apply(ScalesSchema, (v) => (this.scales = v));
    else if (rel === 'config/tags.json') apply(TagsFileSchema, (v) => (this.tags = v));
    else if (rel === 'config/categories.json')
      apply(CategoriesFileSchema, (v) => (this.categories = v));
    else {
      const cat = this.categories.find((c) => this.catFile(c) === file);
      if (cat) apply(EntryFileSchema, (v) => this.byCat.set(cat.id, v));
    }
  }

  // ---------- change plumbing ----------
  private bump(): void {
    this.version++;
    this.allCache = undefined;
  }

  private async commit(catIds: string[], event = 'change'): Promise<void> {
    this.bump();
    for (const id of new Set(catIds)) {
      const c = this.categories.find((x) => x.id === id);
      if (c) await this.writeJson(this.catFile(c), this.byCat.get(id) ?? []);
    }
    this.emit(event);
  }

  async backup(reason: string): Promise<BackupInfo> {
    const b = await createBackup(this.dir, reason, this.keep());
    this.emit('backup', b);
    return b;
  }

  async listBackups(): Promise<BackupInfo[]> {
    return listBackups(this.dir);
  }

  async restoreBackup(name: string): Promise<void> {
    await this.backup('pre-restore');
    await restoreBackupFiles(this.dir, name);
    this.lastWritten.clear();
    await this.loadAll();
    this.emit('change');
  }

  // ---------- entries ----------
  all(): Entry[] {
    if (this.allCache?.v === this.version) return this.allCache.list;
    const list = [...this.byCat.values()].flat();
    this.allCache = { v: this.version, list };
    return list;
  }

  getEntry(id: string): Entry | undefined {
    return this.all().find((e) => e.id === id);
  }

  entriesOf(categoryId: string): Entry[] {
    return this.byCat.get(categoryId) ?? [];
  }

  private requireCategory(id: string): Category {
    const c = this.categories.find((x) => x.id === id);
    if (!c) throw new HttpError(404, 'NOT_FOUND', `Category not found: ${id}`);
    return c;
  }

  private requireEntry(id: string): { entry: Entry; cat: Category; list: Entry[]; idx: number } {
    for (const [catId, list] of this.byCat) {
      const idx = list.findIndex((e) => e.id === id);
      if (idx >= 0) return { entry: list[idx]!, cat: this.requireCategory(catId), list, idx };
    }
    throw new HttpError(404, 'NOT_FOUND', `Entry not found: ${id}`);
  }

  inferMediaType(cat: Category): Entry['mediaType'] {
    return cat.library === 'watching'
      ? (WATCHING_MEDIA_TYPE_BY_CATEGORY[cat.name.toLowerCase()] ?? 'unknown')
      : 'unknown';
  }

  /** Build a full entry (no persistence). */
  buildEntry(input: EntryInput, now = new Date().toISOString()): Entry {
    const cat = this.requireCategory(input.categoryId);
    const tags = this.canonicalTags(input.tags ?? []);
    return EntrySchema.parse({
      mediaType: this.inferMediaType(cat),
      ...input,
      tags,
      id: uuidv7(),
      library: cat.library,
      createdAt: now,
      updatedAt: now,
    });
  }

  async createEntry(input: EntryInput): Promise<Entry> {
    const entry = this.buildEntry(input);
    const list = this.byCat.get(entry.categoryId)!;
    list.push(entry);
    await this.registerTags(entry.tags);
    await this.commit([entry.categoryId]);
    await this.logActivity({ at: entry.createdAt, entryId: entry.id, kind: 'created' });
    return entry;
  }

  /** Insert many pre-built entries in one write (importer). */
  async insertEntries(entries: Entry[]): Promise<void> {
    const touched: string[] = [];
    for (const e of entries) {
      this.byCat.get(e.categoryId)!.push(e);
      touched.push(e.categoryId);
    }
    await this.registerTags(entries.flatMap((e) => e.tags));
    await this.commit(touched);
  }

  async replaceEntries(entries: Entry[]): Promise<void> {
    const touched: string[] = [];
    for (const e of entries) {
      const { list, idx } = this.requireEntry(e.id);
      list[idx] = e;
      touched.push(e.categoryId);
    }
    await this.registerTags(entries.flatMap((e) => e.tags));
    await this.commit(touched);
  }

  async updateEntry(id: string, patch: Record<string, unknown>): Promise<Entry> {
    const { entry, list, idx } = this.requireEntry(id);
    const next: Record<string, unknown> = { ...entry };
    for (const [k, v] of Object.entries(patch)) {
      if (['id', 'library', 'categoryId', 'createdAt'].includes(k)) continue;
      const cur = (entry as Record<string, unknown>)[k];
      next[k] =
        v && typeof v === 'object' && !Array.isArray(v) && cur && typeof cur === 'object'
          ? { ...(cur as object), ...(v as object) }
          : v;
    }
    if (patch.tags) next.tags = this.canonicalTags(patch.tags as string[]);
    next.updatedAt = new Date().toISOString();
    const parsed = EntrySchema.parse(next);
    list[idx] = parsed;
    await this.registerTags(parsed.tags);
    await this.commit([parsed.categoryId]);
    if (parsed.status !== entry.status)
      await this.logActivity({
        at: parsed.updatedAt,
        entryId: id,
        kind: 'status',
        from: entry.status,
        to: parsed.status,
      });
    return parsed;
  }

  async setProgress(id: string, op: { delta?: number; set?: string | number }): Promise<Entry> {
    const { entry } = this.requireEntry(id);
    let current: Progress;
    if (op.set !== undefined) current = parseProgress(op.set);
    else {
      const base = entry.current.number ?? 0;
      if (
        entry.current.raw !== null &&
        entry.current.number === null &&
        entry.current.kind !== null
      )
        throw new HttpError(
          422,
          'UNPARSEABLE_PROGRESS',
          'Current progress is not numeric; use set.',
        );
      const n = Math.max(0, base + (op.delta ?? 0));
      current = { raw: String(n), number: n, season: entry.current.season, kind: 'number' };
    }
    const patch: Record<string, unknown> = { current };
    if (entry.status === null || entry.status === 'planned') patch.status = 'in_progress';
    const out = await this.updateEntry(id, patch);
    await this.logActivity({
      at: out.updatedAt,
      entryId: id,
      kind: 'progress',
      from: entry.current.raw,
      to: current.raw,
    });
    return out;
  }

  /** Re-insert an exact entry (undo of a delete). No-op if the id already exists. */
  async restoreEntry(entry: Entry): Promise<Entry> {
    const parsed = EntrySchema.parse(entry);
    if (this.all().some((e) => e.id === parsed.id)) return this.getEntry(parsed.id)!;
    const list = this.byCat.get(parsed.categoryId);
    if (!list) throw new HttpError(404, 'NOT_FOUND', `Category not found: ${parsed.categoryId}`);
    list.push(parsed);
    await this.registerTags(parsed.tags);
    await this.commit([parsed.categoryId]);
    return parsed;
  }

  async deleteEntry(id: string): Promise<Entry> {
    const { entry, list, idx } = this.requireEntry(id);
    list.splice(idx, 1);
    await this.commit([entry.categoryId]);
    return entry;
  }

  async deleteEntries(ids: string[]): Promise<number> {
    await this.backup('pre-bulk-delete');
    const set = new Set(ids);
    const touched: string[] = [];
    let n = 0;
    for (const [catId, list] of this.byCat) {
      const kept = list.filter((e) => !set.has(e.id));
      if (kept.length !== list.length) {
        n += list.length - kept.length;
        this.byCat.set(catId, kept);
        touched.push(catId);
      }
    }
    await this.commit(touched);
    return n;
  }

  /** Atomic two-file move with in-memory + disk rollback if the second write fails. */
  async moveEntry(id: string, toCategoryId: string): Promise<Entry> {
    const { entry, cat: from, list, idx } = this.requireEntry(id);
    const to = this.requireCategory(toCategoryId);
    if (from.id === to.id) return entry;
    if (from.library !== to.library)
      throw new HttpError(422, 'LIBRARY_MISMATCH', 'Cannot move between libraries.');
    const moved: Entry = { ...entry, categoryId: to.id, updatedAt: new Date().toISOString() };
    const dest = this.byCat.get(to.id)!;
    dest.push(moved);
    list.splice(idx, 1);
    this.bump();
    try {
      await this.writeJson(this.catFile(to), dest); // write dest first: a crash duplicates, never loses
      await this.writeJson(this.catFile(from), list);
    } catch (err) {
      dest.pop();
      list.splice(idx, 0, entry);
      this.bump();
      await this.writeJson(this.catFile(to), dest).catch(() => undefined);
      throw err;
    }
    this.emit('change');
    return moved;
  }

  async bulk(op: {
    ids: string[];
    action: 'set_status' | 'add_tag' | 'remove_tag' | 'move' | 'delete';
    value?: string;
  }): Promise<{ affected: number }> {
    if (op.action === 'delete') return { affected: await this.deleteEntries(op.ids) };
    let affected = 0;
    for (const id of op.ids) {
      const e = this.getEntry(id);
      if (!e) continue;
      if (op.action === 'set_status') await this.updateEntry(id, { status: op.value as Status });
      else if (op.action === 'add_tag')
        await this.updateEntry(id, { tags: [...e.tags, op.value!] });
      else if (op.action === 'remove_tag')
        await this.updateEntry(id, {
          tags: e.tags.filter((t) => t.toLowerCase() !== op.value!.toLowerCase()),
        });
      else if (op.action === 'move') await this.moveEntry(id, op.value!);
      affected++;
    }
    return { affected };
  }

  // ---------- categories ----------
  private async saveCategories() {
    this.bump();
    await this.writeJson(this.p('config', 'categories.json'), this.categories);
    this.emit('change');
  }

  async createCategory(input: {
    library: Library;
    name: string;
    color?: string;
  }): Promise<Category> {
    const name = input.name.trim();
    let slug = slugify(name);
    const base = slug;
    for (
      let i = 2;
      this.categories.some((c) => c.library === input.library && c.slug === slug);
      i++
    )
      slug = `${base}-${i}`;
    const cat: Category = {
      id: `${input.library}-${slug}`,
      library: input.library,
      name,
      slug,
      color: input.color ?? NEW_CATEGORY_PALETTE[name.toLowerCase()] ?? DEFAULT_CATEGORY_COLOR,
      order: this.categories.filter((c) => c.library === input.library).length,
    };
    this.categories.push(cat);
    this.byCat.set(cat.id, []);
    await this.writeJson(this.catFile(cat), []);
    await this.saveCategories();
    return cat;
  }

  async updateCategory(
    id: string,
    patch: { name?: string; color?: string; order?: number },
  ): Promise<Category> {
    const cat = this.requireCategory(id);
    const oldFile = this.catFile(cat);
    if (patch.color) cat.color = patch.color;
    if (patch.order !== undefined) cat.order = patch.order;
    if (patch.name && patch.name.trim() !== cat.name) {
      cat.name = patch.name.trim();
      let slug = slugify(cat.name);
      const base = slug;
      for (
        let i = 2;
        this.categories.some((c) => c !== cat && c.library === cat.library && c.slug === slug);
        i++
      )
        slug = `${base}-${i}`;
      if (slug !== cat.slug) {
        cat.slug = slug;
        await this.writeJson(this.catFile(cat), this.byCat.get(id) ?? []);
        await fs.rm(oldFile, { force: true });
      }
    }
    await this.saveCategories();
    return cat;
  }

  async reorderCategories(library: Library, ids: string[]): Promise<void> {
    ids.forEach((id, i) => {
      const c = this.categories.find((x) => x.id === id && x.library === library);
      if (c) c.order = i;
    });
    await this.saveCategories();
  }

  async deleteCategory(id: string, moveTo?: string): Promise<void> {
    const cat = this.requireCategory(id);
    const entries = this.byCat.get(id) ?? [];
    if (entries.length) {
      if (!moveTo)
        throw new HttpError(409, 'CATEGORY_NOT_EMPTY', 'Category has entries; pass moveTo.');
      for (const e of [...entries]) await this.moveEntry(e.id, moveTo);
    }
    await this.backup('pre-category-delete');
    this.categories = this.categories.filter((c) => c.id !== id);
    this.byCat.delete(id);
    await fs.rm(this.catFile(cat), { force: true });
    await this.saveCategories();
  }

  async ensureCategory(library: Library, name: string, color?: string): Promise<Category> {
    const found = this.categories.find(
      (c) => c.library === library && c.name.toLowerCase() === name.trim().toLowerCase(),
    );
    return found ?? this.createCategory({ library, name, color });
  }

  // ---------- tags ----------
  canonicalTags(names: string[]): string[] {
    const out: string[] = [];
    const seen = new Set<string>();
    for (const raw of names) {
      const t = raw.replace(/\s+/g, ' ').trim();
      if (!t) continue;
      const lc = t.toLowerCase();
      const hit = this.tags.find(
        (x) => x.name.toLowerCase() === lc || x.aliases.some((a) => a.toLowerCase() === lc),
      );
      const name = hit?.name ?? t;
      if (seen.has(name.toLowerCase())) continue;
      seen.add(name.toLowerCase());
      out.push(name);
    }
    return out;
  }

  private async registerTags(names: string[]): Promise<void> {
    let changed = false;
    for (const n of names) {
      if (!this.tags.some((t) => t.name.toLowerCase() === n.toLowerCase())) {
        this.tags.push({ id: slugify(n), name: n, aliases: [] });
        changed = true;
      }
    }
    if (changed) await this.writeJson(this.p('config', 'tags.json'), this.tags);
  }

  tagUsage(): Map<string, number> {
    const m = new Map<string, number>();
    for (const e of this.all()) for (const t of e.tags) m.set(t, (m.get(t) ?? 0) + 1);
    return m;
  }

  async updateTag(
    name: string,
    patch: { name?: string; color?: string; hidden?: boolean },
  ): Promise<Tag> {
    const tag = this.tags.find((t) => t.name.toLowerCase() === name.toLowerCase());
    if (!tag) throw new HttpError(404, 'NOT_FOUND', `Tag not found: ${name}`);
    const oldName = tag.name;
    if (patch.color !== undefined) tag.color = patch.color;
    if (patch.hidden !== undefined) tag.hidden = patch.hidden;
    if (patch.name && patch.name.trim() && patch.name.trim() !== oldName) {
      const newName = patch.name.trim();
      if (this.tags.some((t) => t !== tag && t.name.toLowerCase() === newName.toLowerCase()))
        throw new HttpError(409, 'TAG_EXISTS', 'A tag with that name exists; merge instead.');
      tag.name = newName;
      tag.id = slugify(newName);
      await this.rewriteTags((tags) => tags.map((t) => (t === oldName ? newName : t)));
    }
    await this.writeJson(this.p('config', 'tags.json'), this.tags);
    this.bump();
    this.emit('change');
    return tag;
  }

  /** Merge `from` tags into `to`. Remembers the old names as aliases so imports auto-resolve. */
  async mergeTags(from: string[], to: string): Promise<{ affected: number }> {
    let target = this.tags.find((t) => t.name.toLowerCase() === to.toLowerCase());
    if (!target) {
      target = { id: slugify(to), name: to.trim(), aliases: [] };
      this.tags.push(target);
    }
    const fromLc = new Set(
      from.map((f) => f.toLowerCase()).filter((f) => f !== target!.name.toLowerCase()),
    );
    const affected = await this.rewriteTags((tags) =>
      tags.map((t) => (fromLc.has(t.toLowerCase()) ? target!.name : t)),
    );
    for (const f of from) {
      if (
        f.toLowerCase() !== target.name.toLowerCase() &&
        !target.aliases.some((a) => a.toLowerCase() === f.toLowerCase())
      )
        target.aliases.push(f);
    }
    for (const t of this.tags.filter((t) => fromLc.has(t.name.toLowerCase()))) {
      for (const a of t.aliases) if (!target.aliases.includes(a)) target.aliases.push(a);
    }
    this.tags = this.tags.filter((t) => !fromLc.has(t.name.toLowerCase()));
    await this.writeJson(this.p('config', 'tags.json'), this.tags);
    this.bump();
    this.emit('change');
    return { affected };
  }

  private async rewriteTags(fn: (tags: string[]) => string[]): Promise<number> {
    const touched = new Set<string>();
    let n = 0;
    for (const [catId, list] of this.byCat) {
      list.forEach((e, i) => {
        const next = this.dedupe(fn(e.tags));
        if (next.join('\u0000') !== e.tags.join('\u0000')) {
          list[i] = { ...e, tags: next, updatedAt: new Date().toISOString() };
          touched.add(catId);
          n++;
        }
      });
    }
    await this.commit([...touched]);
    return n;
  }

  private dedupe(tags: string[]): string[] {
    const seen = new Set<string>();
    return tags.filter((t) =>
      seen.has(t.toLowerCase()) ? false : (seen.add(t.toLowerCase()), true),
    );
  }

  // ---------- settings ----------
  async saveSettings(next: Settings): Promise<Settings> {
    this.settings = SettingsSchema.parse(next);
    await this.writeJson(this.p('config', 'settings.json'), this.settings);
    this.emit('change');
    return this.settings;
  }

  async saveScales(next: Scales): Promise<Scales> {
    this.scales = ScalesSchema.parse(next);
    await this.writeJson(this.p('config', 'scales.json'), this.scales);
    this.emit('change');
    return this.scales;
  }

  // ---------- activity ----------
  async logActivity(rec: ActivityRecord): Promise<void> {
    const file = this.p('activity', `${rec.at.slice(0, 7)}.jsonl`);
    await this.mutex.run(file, () => fs.appendFile(file, JSON.stringify(rec) + '\n'));
  }

  async readActivity(sinceIso?: string): Promise<ActivityRecord[]> {
    const files = (await fs.readdir(this.p('activity')).catch(() => [] as string[]))
      .filter((f) => f.endsWith('.jsonl'))
      .sort();
    const out: ActivityRecord[] = [];
    for (const f of files) {
      if (sinceIso && f.slice(0, 7) < sinceIso.slice(0, 7)) continue;
      const text = await fs.readFile(this.p('activity', f), 'utf8').catch(() => '');
      for (const line of text.split('\n')) {
        if (!line.trim()) continue;
        try {
          const r = JSON.parse(line) as ActivityRecord;
          if (!sinceIso || r.at >= sinceIso) out.push(r);
        } catch {
          /* skip a torn line */
        }
      }
    }
    return out;
  }

  // ---------- helpers for importers ----------
  get importsDir() {
    return this.p('imports');
  }
  get exportsDir() {
    return this.p('exports');
  }
  categoryFileFor(c: Category) {
    return this.catFile(c);
  }
}

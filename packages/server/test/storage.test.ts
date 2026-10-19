import { promises as fs } from 'node:fs';
import path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { KeyedMutex, atomicWrite, stableStringify } from '../src/storage/atomic.js';
import { migrate } from '../src/storage/migrations.js';
import { Store } from '../src/storage/store.js';
import { createBackup, isSafeZipPath, listBackups } from '../src/storage/backups.js';
import { makeStore, tmpDir } from './helpers.js';

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const c of cleanups.splice(0)) await c();
});
const mk = async (o = {}) => {
  const s = await makeStore(o);
  cleanups.push(async () => {
    await s.store.close();
    await s.cleanup();
  });
  return s;
};

describe('atomic IO', () => {
  it('writes stable key-ordered pretty JSON', () => {
    expect(stableStringify({ b: 1, a: { d: 1, c: undefined } })).toBe(
      '{\n  "a": {\n    "d": 1\n  },\n  "b": 1\n}\n',
    );
  });
  it('never leaves tmp files and replaces atomically', async () => {
    const t = await tmpDir();
    cleanups.push(t.cleanup);
    const f = path.join(t.dir, 'x.json');
    await Promise.all(Array.from({ length: 20 }, (_, i) => atomicWrite(f, `{"i":${i}}`)));
    expect(JSON.parse(await fs.readFile(f, 'utf8'))).toHaveProperty('i');
    expect((await fs.readdir(t.dir)).filter((n) => n.endsWith('.tmp'))).toEqual([]);
  });
  it('retries a rename that Windows briefly refuses (EPERM/EBUSY)', async () => {
    const { vi } = await import('vitest');
    const t = await tmpDir();
    cleanups.push(t.cleanup);
    const real = fs.rename.bind(fs);
    let failures = 2;
    const spy = vi.spyOn(fs, 'rename').mockImplementation(async (a, b) => {
      if (failures-- > 0)
        throw Object.assign(new Error('locked'), { code: failures ? 'EPERM' : 'EBUSY' });
      return real(a, b);
    });
    try {
      await atomicWrite(path.join(t.dir, 'y.json'), '{"ok":true}');
      expect(JSON.parse(await fs.readFile(path.join(t.dir, 'y.json'), 'utf8'))).toEqual({
        ok: true,
      });
      failures = 99;
      await expect(atomicWrite(path.join(t.dir, 'z.json'), '{}')).rejects.toThrow('locked');
      expect((await fs.readdir(t.dir)).filter((n) => n.endsWith('.tmp'))).toEqual([]);
      spy.mockImplementation(async () => {
        throw Object.assign(new Error('nope'), { code: 'ENOENT' });
      });
      await expect(atomicWrite(path.join(t.dir, 'w.json'), '{}')).rejects.toThrow('nope'); // other errors are not retried
    } finally {
      spy.mockRestore();
    }
  });
  it('mutex serializes per key', async () => {
    const m = new KeyedMutex();
    const order: number[] = [];
    await Promise.all([
      m.run('k', async () => {
        await new Promise((r) => setTimeout(r, 20));
        order.push(1);
      }),
      m.run('k', async () => {
        order.push(2);
      }),
      m
        .run('k', async () => {
          throw new Error('x');
        })
        .catch(() => order.push(3)),
      m.run('k', async () => {
        order.push(4);
      }),
    ]);
    expect(order).toEqual([1, 2, 3, 4]);
  });
});

describe('Store', () => {
  it('creates the documented layout', async () => {
    const { store, dir } = await mk();
    for (const f of [
      'meta.json',
      'config/settings.json',
      'config/categories.json',
      'config/tags.json',
      'config/scales.json',
    ])
      await expect(fs.access(path.join(dir, f))).resolves.toBeUndefined();
    expect(store.meta.schemaVersion).toBe(1);
  });

  it('concurrent writes all land and persist', async () => {
    const { store, dir } = await mk();
    const cat = await store.createCategory({ library: 'reading', name: 'Romance' });
    await Promise.all(
      Array.from({ length: 25 }, (_, i) =>
        store.createEntry({ title: `T${i}`, categoryId: cat.id }),
      ),
    );
    const onDisk = JSON.parse(
      await fs.readFile(path.join(dir, 'data/reading/romance.json'), 'utf8'),
    );
    expect(onDisk).toHaveLength(25);
    const reopened = new Store(dir);
    await reopened.open();
    expect(reopened.all()).toHaveLength(25);
  });

  it('quarantines a corrupt file and restores from the latest backup', async () => {
    const { store, dir } = await mk();
    const cat = await store.createCategory({ library: 'reading', name: 'Action' });
    await store.createEntry({ title: 'Keep me', categoryId: cat.id });
    await store.backup('test');
    await fs.writeFile(path.join(dir, 'data/reading/action.json'), '{ not json');
    const again = new Store(dir);
    const errors: unknown[] = [];
    again.on('file-error', (e) => errors.push(e));
    await again.open();
    expect(errors).toHaveLength(1);
    expect(again.all().map((e) => e.title)).toEqual(['Keep me']);
    expect((await fs.readdir(path.join(dir, 'backups/corrupt'))).length).toBe(1);
  });

  it('corrupt file with no backup falls back to empty without crashing', async () => {
    const { store, dir } = await mk();
    await store.createCategory({ library: 'reading', name: 'Action' });
    await fs.writeFile(path.join(dir, 'data/reading/action.json'), 'garbage');
    const again = new Store(dir);
    await again.open();
    expect(again.all()).toEqual([]);
  });

  it('moves entries between categories atomically', async () => {
    const { store, dir } = await mk();
    const a = await store.createCategory({ library: 'reading', name: 'A' });
    const b = await store.createCategory({ library: 'reading', name: 'B' });
    const e = await store.createEntry({ title: 'X', categoryId: a.id });
    await store.moveEntry(e.id, b.id);
    expect(store.entriesOf(a.id)).toHaveLength(0);
    expect(
      JSON.parse(await fs.readFile(path.join(dir, 'data/reading/b.json'), 'utf8')),
    ).toHaveLength(1);
    await expect(
      store.moveEntry(e.id, (await store.createCategory({ library: 'watching', name: 'W' })).id),
    ).rejects.toThrow(/libraries/i);
  });

  it('rolls back a move when the second write fails', async () => {
    const { store } = await mk();
    const a = await store.createCategory({ library: 'reading', name: 'A' });
    const b = await store.createCategory({ library: 'reading', name: 'B' });
    const e = await store.createEntry({ title: 'X', categoryId: a.id });
    const real = store.writeJson.bind(store);
    let n = 0;
    store.writeJson = async (f, d) => {
      if (++n === 2) throw new Error('disk full');
      return real(f, d);
    };
    await expect(store.moveEntry(e.id, b.id)).rejects.toThrow('disk full');
    store.writeJson = real;
    expect(store.entriesOf(a.id).map((x) => x.id)).toEqual([e.id]);
    expect(store.entriesOf(b.id)).toHaveLength(0);
  });

  it('progress delta writes activity and starts planned entries', async () => {
    const { store } = await mk();
    const c = await store.createCategory({ library: 'watching', name: 'Anime' });
    const e = await store.createEntry({ title: 'Show', categoryId: c.id });
    expect(e.mediaType).toBe('anime');
    const out = await store.setProgress(e.id, { delta: 1 });
    expect(out.current.number).toBe(1);
    expect(out.status).toBe('in_progress');
    const act = await store.readActivity();
    expect(act.map((a) => a.kind)).toEqual(
      expect.arrayContaining(['created', 'progress', 'status']),
    );
  });

  it('merges tags, remembers aliases and canonicalizes later input', async () => {
    const { store } = await mk();
    const c = await store.createCategory({ library: 'reading', name: 'A' });
    const e1 = await store.createEntry({
      title: 'one',
      categoryId: c.id,
      tags: ['Sorcery', 'Overlord Lead'],
    });
    const e2 = await store.createEntry({
      title: 'two',
      categoryId: c.id,
      tags: ['Sorcrey', 'Sorcery'],
    });
    await store.mergeTags(['Sorcrey'], 'Sorcery');
    expect(store.getEntry(e2.id)!.tags).toEqual(['Sorcery']);
    expect(store.getEntry(e1.id)!.tags).toEqual(['Sorcery', 'Overlord Lead']);
    expect(store.tags.find((t) => t.name === 'Sorcery')!.aliases).toContain('Sorcrey');
    expect(store.canonicalTags(['sorcrey', 'new'])).toEqual(['Sorcery', 'new']);
  });

  it('reloads hand edits and keeps memory on invalid edits', async () => {
    const { store, dir } = await mk({ watch: true });
    const c = await store.createCategory({ library: 'reading', name: 'A' });
    const e = await store.createEntry({ title: 'Orig', categoryId: c.id });
    const file = path.join(dir, 'data/reading/a.json');
    const wait = (ev: string) => new Promise<unknown>((r) => store.once(ev, r));
    await new Promise((r) => setTimeout(r, 400));
    const edited = JSON.parse(await fs.readFile(file, 'utf8'));
    edited[0].title = 'Hand edited';
    let p = wait('external-change');
    await fs.writeFile(file, JSON.stringify(edited));
    await p;
    expect(store.getEntry(e.id)!.title).toBe('Hand edited');
    p = wait('file-error');
    await fs.writeFile(file, '{ broken');
    const err = (await p) as { message: string; kept: boolean };
    expect(err.kept).toBe(true);
    expect(store.getEntry(e.id)!.title).toBe('Hand edited');
  });
});

describe('backups & migrations', () => {
  it('creates, lists, prunes and restores backups', async () => {
    const { store, dir } = await mk();
    const c = await store.createCategory({ library: 'reading', name: 'A' });
    const e = await store.createEntry({ title: 'Before', categoryId: c.id });
    const b = await store.backup('manual');
    await store.updateEntry(e.id, { title: 'After' });
    await store.restoreBackup(b.name);
    expect(store.getEntry(e.id)!.title).toBe('Before');
    for (let i = 0; i < 4; i++) await createBackup(dir, 'x', 3);
    expect((await listBackups(dir)).length).toBe(3);
  });
  it('rejects unsafe zip paths', () => {
    for (const p of ['../x', '/etc/passwd', 'config/../../x', 'C:/x', 'misc/file.json', 'data\\x'])
      expect(isSafeZipPath(p)).toBe(false);
    expect(isSafeZipPath('data/reading/a.json')).toBe(true);
  });
  it('runs ordered migrations after a backup and refuses newer data', async () => {
    const { store, dir } = await mk();
    const ran: number[] = [];
    let backed = 0;
    const v = await migrate(dir, 1, {
      target: 3,
      backup: async () => {
        backed++;
      },
      migrations: [
        {
          from: 2,
          to: 3,
          run: async () => {
            ran.push(2);
          },
        },
        {
          from: 1,
          to: 2,
          run: async () => {
            ran.push(1);
          },
        },
      ],
    });
    expect({ v, ran, backed }).toEqual({ v: 3, ran: [1, 2], backed: 1 });
    expect(JSON.parse(await fs.readFile(path.join(dir, 'meta.json'), 'utf8')).schemaVersion).toBe(
      3,
    );
    await expect(migrate(dir, 9, { backup: async () => {} })).rejects.toThrow(/newer/);
    void store;
  });
});

describe('write coalescing & incremental search', () => {
  it('coalesces bursts of writes but persists the final state', async () => {
    const { store, dir } = await mk();
    const c = await store.createCategory({ library: 'reading', name: 'Burst' });
    const e = await store.createEntry({ title: 'Counter', categoryId: c.id });
    const real = store.writeJson.bind(store);
    let writes = 0;
    store.writeJson = async (f, d) => {
      writes++;
      await new Promise((r) => setTimeout(r, 15));
      return real(f, d);
    };
    await Promise.all(Array.from({ length: 30 }, () => store.setProgress(e.id, { delta: 1 })));
    store.writeJson = real;
    expect(writes).toBeLessThan(10);
    const onDisk = JSON.parse(await fs.readFile(path.join(dir, 'data/reading/burst.json'), 'utf8'));
    expect(onDisk[0].current.number).toBe(30);
  });

  it('keeps the search index in sync after edits, deletes and adds', async () => {
    const { store } = await mk();
    const { SearchIndex } = await import('../src/search.js');
    const c = await store.createCategory({ library: 'reading', name: 'Idx' });
    const a = await store.createEntry({ title: 'Quartz Lantern', categoryId: c.id });
    const b = await store.createEntry({ title: 'Amber Harbor', categoryId: c.id });
    const idx = new SearchIndex(store);
    expect(idx.search('quartz').map((h) => h.id)).toEqual([a.id]);
    await store.updateEntry(a.id, { title: 'Violet Orchard' });
    expect(idx.search('quartz')).toEqual([]);
    expect(idx.search('violet').map((h) => h.id)).toEqual([a.id]);
    await store.deleteEntry(b.id);
    expect(idx.search('amber')).toEqual([]);
    const n = await store.createEntry({ title: 'Amber Fresh', categoryId: c.id });
    expect(idx.search('amber').map((h) => h.id)).toEqual([n.id]);
    expect(idx.search('orchrd').map((h) => h.id)).toEqual([a.id]); // one typo still finds it
  });
});

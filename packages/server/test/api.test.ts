import { afterEach, describe, expect, it } from 'vitest';
import { makeApp } from './helpers.js';

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const c of cleanups.splice(0)) await c();
});
async function setup() {
  const a = await makeApp();
  cleanups.push(async () => {
    await a.app.close();
    await a.store.close();
    await a.cleanup();
  });
  const rom = (await a.call('POST', '/categories', { library: 'reading', name: 'Romance' })).body;
  const act = (await a.call('POST', '/categories', { library: 'reading', name: 'Action' })).body;
  return { ...a, rom, act };
}

describe('auth & errors', () => {
  it('rejects missing/bad tokens on every route class', async () => {
    const { call } = await setup();
    for (const [m, u] of [
      ['GET', '/health'],
      ['GET', '/entries'],
      ['POST', '/entries'],
      ['GET', '/stats/overview'],
      ['GET', '/backups'],
    ] as const) {
      expect((await call(m, u, undefined, null)).status).toBe(401);
      const bad = await call(m, u, undefined, 'nope');
      expect(bad.status).toBe(401);
      expect(bad.body.error.code).toBe('UNAUTHORIZED');
    }
    expect((await call('GET', '/health')).body).toEqual({ ok: true });
  });
  it('validates bodies and returns a consistent error shape', async () => {
    const { call } = await setup();
    const r = await call('POST', '/entries', { title: '' });
    expect(r.status).toBe(400);
    expect(r.body.error).toMatchObject({ code: 'VALIDATION' });
    expect(r.res.headers['x-request-id']).toBeTruthy();
    expect((await call('GET', '/entries/nope')).status).toBe(404);
    expect((await call('GET', '/nowhere')).body.error.code).toBe('NOT_FOUND');
    expect((await call('POST', '/backup/restore', { name: '../../etc/passwd' })).status).toBe(400);
  });
});

describe('entries', () => {
  it('CRUD, progress, move, bulk', async () => {
    const { call, rom, act } = await setup();
    const created = await call('POST', '/entries', {
      title: 'Alpha',
      categoryId: rom.id,
      tags: ['Romance', 'romance', 'Sorcery'],
      status: 'in_progress',
    });
    expect(created.status).toBe(201);
    const id = created.body.id;
    expect(created.body.tags).toEqual(['Romance', 'Sorcery']);
    expect(
      (await call('PATCH', `/entries/${id}`, { score: 8, notes: { extra1: 'Left off at ch 5' } }))
        .body,
    ).toMatchObject({ score: 8, notes: { extra1: 'Left off at ch 5', general: null } });
    expect((await call('PATCH', `/entries/${id}`, { score: 11 })).status).toBe(400);
    expect(
      (await call('POST', `/entries/${id}/progress`, { set: 'S2 E1' })).body.current,
    ).toMatchObject({ season: 2, number: 1 });
    expect(
      (await call('POST', `/entries/${id}/progress`, { set: 'End of S1' })).body.current.kind,
    ).toBe('end');
    expect((await call('POST', `/entries/${id}/progress`, { delta: 1 })).status).toBe(422);
    expect((await call('POST', `/entries/${id}/progress`, { set: 10 })).body.current.number).toBe(
      10,
    );
    expect((await call('POST', `/entries/${id}/progress`, { delta: 2 })).body.current.number).toBe(
      12,
    );
    expect(
      (await call('POST', `/entries/${id}/move`, { categoryId: act.id })).body.categoryId,
    ).toBe(act.id);
    const b = await call('POST', '/entries/bulk', {
      ids: [id],
      action: 'add_tag',
      value: 'Overlord Lead',
    });
    expect(b.body.affected).toBe(1);
    expect((await call('GET', `/entries/${id}`)).body.tags).toContain('Overlord Lead');
    expect((await call('DELETE', `/entries/${id}`)).status).toBe(200);
    expect((await call('GET', '/entries')).body.total).toBe(0);
  });

  it('filters, tag modes, behind, sort, pagination and search', async () => {
    const { call, rom } = await setup();
    const mk = (o: object) => call('POST', '/entries', { categoryId: rom.id, ...o });
    await mk({
      title: 'Marlow’s Plan',
      tags: ['Romance', 'Overlord Lead'],
      score: 9,
      status: 'completed',
    });
    await mk({
      title: 'Sorcery Academy',
      tags: ['Sorcery', 'Romance'],
      score: 6,
      status: 'in_progress',
      current: { raw: '111', number: 111, season: null, kind: 'number' },
      consumedUpTo: { raw: '110', number: 110, season: null, kind: 'number' },
    });
    await mk({ title: 'Third', tags: ['Sorcery'], notes: { general: 'has notes' } });
    const q = async (qs: string) => (await call('GET', `/entries?${qs}`)).body;
    expect((await q('tags=Romance,Sorcery&tagMode=all')).total).toBe(1);
    expect((await q('tags[]=Romance&tags[]=Sorcery&tagMode=any')).total).toBe(3);
    expect(
      (await q('tags=Sorcery&excludeTags=Romance')).items.map((e: { title: string }) => e.title),
    ).toEqual(['Third']);
    expect((await q('behind=true')).items[0].title).toBe('Sorcery Academy');
    expect((await q('hasNotes=true')).total).toBe(1);
    expect((await q('minScore=7')).total).toBe(1);
    expect((await q('status=completed,in_progress')).total).toBe(2);
    expect((await q('sort=score&order=desc')).items[0].title).toBe('Marlow’s Plan');
    expect((await q('limit=2&page=2')).items).toHaveLength(1);
    expect((await q('q=sorcerx')).items[0].title).toBe('Sorcery Academy');
    expect((await q('q=marlows')).total + (await q('q=marlow')).total).toBeGreaterThan(0);
    const s = (await call('GET', '/search?q=acad')).body;
    expect(s.entries[0].entry.title).toBe('Sorcery Academy');
  });
});

describe('categories, tags, stats, settings, backups', () => {
  it('category rename, reorder, delete-with-move', async () => {
    const { call, rom, act } = await setup();
    await call('POST', '/entries', { title: 'E', categoryId: rom.id });
    expect((await call('DELETE', `/categories/${rom.id}`)).status).toBe(409);
    expect(
      (await call('PATCH', `/categories/${rom.id}`, { name: 'Love', color: '#112233' })).body,
    ).toMatchObject({ name: 'Love', slug: 'love' });
    expect((await call('DELETE', `/categories/${rom.id}?moveTo=${act.id}`)).status).toBe(200);
    expect((await call('GET', '/entries')).body.items[0].categoryId).toBe(act.id);
    expect(
      (await call('POST', '/categories', { library: 'reading', name: 'Harem' })).body.color,
    ).toBe('#880E4F');
  });
  it('tag suggestions and merge', async () => {
    const { call, rom } = await setup();
    await call('POST', '/entries', {
      title: 'a',
      categoryId: rom.id,
      tags: ['Sorcery', 'Fantasy'],
    });
    await call('POST', '/entries', { title: 'b', categoryId: rom.id, tags: ['Sorcery'] });
    await call('POST', '/entries', { title: 'c', categoryId: rom.id, tags: ['Sorcrey'] });
    const sug = (await call('GET', '/tags/suggestions')).body;
    expect(sug).toEqual([expect.objectContaining({ from: 'Sorcrey', to: 'Sorcery' })]);
    expect(
      (await call('POST', '/tags/merge', { from: ['Sorcrey'], to: 'Sorcery' })).body.affected,
    ).toBe(1);
    const tags = (await call('GET', '/tags')).body;
    expect(tags.find((t: { name: string }) => t.name === 'Sorcery').count).toBe(3);
  });
  it('stats overview', async () => {
    const { call, rom } = await setup();
    await call('POST', '/entries', {
      title: 'a',
      categoryId: rom.id,
      score: 8,
      status: 'completed',
      tags: ['x', 'y'],
    });
    await call('POST', '/entries', {
      title: 'b',
      categoryId: rom.id,
      score: 6,
      status: 'in_progress',
      tags: ['x', 'y'],
    });
    const s = (await call('GET', '/stats/overview')).body;
    expect(s.totals).toMatchObject({ entries: 2, completed: 1, avgScore: 7 });
    expect(s.byStatus).toEqual({ completed: 1, in_progress: 1 });
    expect(s.tagCooccurrence[0]).toMatchObject({ a: 'x', b: 'y', count: 2 });
    expect(Object.values(s.heatmap).reduce((a: number, b) => a + (b as number), 0)).toBeGreaterThan(
      0,
    );
  });
  it('settings, scales and backups round trip', async () => {
    const { call, rom } = await setup();
    const st = (await call('GET', '/settings')).body;
    expect((await call('PUT', '/settings', { ...st, accent: '#00ff00' })).body.accent).toBe(
      '#00ff00',
    );
    expect((await call('PUT', '/settings', { theme: 'neon' })).status).toBe(400);
    const e = (await call('POST', '/entries', { title: 'keep', categoryId: rom.id })).body;
    const b = (await call('POST', '/backups')).body;
    await call('DELETE', `/entries/${e.id}`);
    expect((await call('POST', '/backup/restore', { name: b.name })).status).toBe(200);
    expect((await call('GET', '/entries')).body.total).toBe(1);
    expect((await call('GET', '/backups')).body.length).toBeGreaterThanOrEqual(2);
    expect((await call('GET', '/scales')).body.watching.statusLabels.completed).toBe('Finished');
  });
});

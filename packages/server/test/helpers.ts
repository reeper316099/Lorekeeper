import { mkdtemp, rm } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { buildApp, Store, type StoreOptions } from '../src/index.js';

export const TOKEN = 'test-token-123';

export async function tmpDir() {
  const dir = await mkdtemp(path.join(os.tmpdir(), 'lorekeeper-'));
  return { dir, cleanup: () => rm(dir, { recursive: true, force: true }) };
}

export async function makeStore(opts: StoreOptions = {}) {
  const t = await tmpDir();
  const store = new Store(t.dir, opts);
  await store.open();
  return { store, ...t };
}

export async function makeApp(opts: StoreOptions = {}) {
  const s = await makeStore(opts);
  const app = await buildApp({ store: s.store, token: TOKEN });
  const call = async (
    method: string,
    url: string,
    payload?: unknown,
    token: string | null = TOKEN,
  ) => {
    const res = await app.inject({
      method: method as 'GET',
      url: `/api/v1${url}`,
      payload: payload as object | undefined,
      headers: token ? { authorization: `Bearer ${token}` } : {},
    });
    return { status: res.statusCode, body: res.body ? JSON.parse(res.body) : null, res };
  };
  return { ...s, app, call };
}

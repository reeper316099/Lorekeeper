import { promises as fs } from 'node:fs';
import path from 'node:path';
import { SCHEMA_VERSION } from '@lorekeeper/shared';
import { atomicWrite, stableStringify } from './atomic.js';

export interface Migration {
  from: number;
  to: number;
  run(dataDir: string): Promise<void>;
}

/** Ordered list. Add new migrations here when SCHEMA_VERSION is bumped. */
export const MIGRATIONS: Migration[] = [];

export async function migrate(
  dataDir: string,
  current: number,
  opts: {
    target?: number;
    migrations?: Migration[];
    backup: (reason: string) => Promise<unknown>;
  },
): Promise<number> {
  const target = opts.target ?? SCHEMA_VERSION;
  const list = (opts.migrations ?? MIGRATIONS).slice().sort((a, b) => a.from - b.from);
  if (current > target) {
    throw new Error(`Data was written by a newer version (schema ${current} > ${target}).`);
  }
  let v = current;
  if (v === target) return v;
  await opts.backup(`pre-migration-v${v}-to-v${target}`);
  while (v < target) {
    const m = list.find((x) => x.from === v);
    if (!m) throw new Error(`No migration from schema ${v}`);
    await m.run(dataDir);
    v = m.to;
    const metaFile = path.join(dataDir, 'meta.json');
    const meta = JSON.parse(await fs.readFile(metaFile, 'utf8').catch(() => '{}'));
    await atomicWrite(metaFile, stableStringify({ ...meta, schemaVersion: v }));
  }
  return v;
}

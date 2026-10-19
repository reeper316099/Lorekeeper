import { promises as fs } from 'node:fs';
import path from 'node:path';

/** Stable-key, 2-space JSON so files are diff-friendly and hand-editable. */
export function stableStringify(value: unknown): string {
  const sort = (v: unknown): unknown => {
    if (Array.isArray(v)) return v.map(sort);
    if (v && typeof v === 'object') {
      return Object.fromEntries(
        Object.entries(v as Record<string, unknown>)
          .filter(([, x]) => x !== undefined)
          .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
          .map(([k, x]) => [k, sort(x)]),
      );
    }
    return v;
  };
  return JSON.stringify(sort(value), null, 2) + '\n';
}

/** Write to a sibling tmp file, fsync, then rename over the target. Never leaves a half file. */
export async function atomicWrite(file: string, content: string | Uint8Array): Promise<void> {
  await fs.mkdir(path.dirname(file), { recursive: true });
  const tmp = `${file}.${process.pid}.${Math.random().toString(36).slice(2)}.tmp`;
  const handle = await fs.open(tmp, 'w');
  try {
    await handle.writeFile(content);
    await handle.sync();
  } finally {
    await handle.close();
  }
  try {
    await renameWithRetry(tmp, file);
  } catch (err) {
    await fs.rm(tmp, { force: true });
    throw err;
  }
}

const RETRYABLE = new Set(['EPERM', 'EBUSY', 'EACCES']);

/**
 * Windows refuses to rename over a file that another handle (an antivirus scan, a search indexer, a
 * concurrent writer) has open for a moment. Retry briefly instead of failing the save.
 */
async function renameWithRetry(from: string, to: string, attempts = 12): Promise<void> {
  for (let i = 0; ; i++) {
    try {
      await fs.rename(from, to);
      return;
    } catch (err) {
      const code = (err as NodeJS.ErrnoException).code ?? '';
      if (i >= attempts - 1 || !RETRYABLE.has(code)) throw err;
      await new Promise((r) => setTimeout(r, 15 * (i + 1)));
    }
  }
}

/** Serializes async work per key (e.g. per file path). */
export class KeyedMutex {
  private tails = new Map<string, Promise<unknown>>();

  run<T>(key: string, fn: () => Promise<T>): Promise<T> {
    const prev = this.tails.get(key) ?? Promise.resolve();
    const next = prev.then(fn, fn);
    const tail = next.catch(() => undefined);
    this.tails.set(key, tail);
    void tail.then(() => {
      if (this.tails.get(key) === tail) this.tails.delete(key);
    });
    return next;
  }
}

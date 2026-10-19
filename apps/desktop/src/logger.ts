import { appendFileSync, mkdirSync, renameSync, rmSync, statSync, existsSync } from 'node:fs';
import path from 'node:path';

const MAX_BYTES = 1024 * 1024;
const KEEP = 3;

/** Tiny rotating file logger. Never pass entry content (titles, notes, tags) to it. */
export function createLogger(dataDir: string) {
  const dir = path.join(dataDir, 'logs');
  const file = path.join(dir, 'app.log');
  try {
    mkdirSync(dir, { recursive: true });
  } catch {
    /* logging must never crash the app */
  }
  const rotate = () => {
    try {
      if (!existsSync(file) || statSync(file).size < MAX_BYTES) return;
      rmSync(`${file}.${KEEP}`, { force: true });
      for (let i = KEEP - 1; i >= 1; i--)
        if (existsSync(`${file}.${i}`)) renameSync(`${file}.${i}`, `${file}.${i + 1}`);
      renameSync(file, `${file}.1`);
    } catch {
      /* ignore */
    }
  };
  return (level: 'info' | 'warn' | 'error', message: string) => {
    try {
      rotate();
      appendFileSync(file, `${new Date().toISOString()} ${level.toUpperCase()} ${message}\n`);
    } catch {
      /* ignore */
    }
  };
}
export type Logger = ReturnType<typeof createLogger>;

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { unzipSync, zipSync, strToU8, strFromU8 } from 'fflate';
import { atomicWrite } from './atomic.js';

export const BACKUP_DIRS = ['config', 'data', 'activity'] as const;
const NAME_RE = /^backup-\d{8}-\d{6}(?:-\d+)?\.zip$/;

export interface BackupInfo {
  name: string;
  size: number;
  createdAt: string;
  reason?: string;
}

export function isValidBackupName(name: string): boolean {
  return NAME_RE.test(name);
}

async function walk(dir: string, base = dir): Promise<string[]> {
  let names: import('node:fs').Dirent[];
  try {
    names = await fs.readdir(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  const out: string[] = [];
  for (const d of names) {
    const p = path.join(dir, d.name);
    if (d.isDirectory()) out.push(...(await walk(p, base)));
    else if (d.isFile() && !d.name.endsWith('.tmp')) out.push(path.relative(base, p));
  }
  return out;
}

function stamp(d = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`;
}

export async function createBackup(
  dataDir: string,
  reason: string,
  keep = 20,
): Promise<BackupInfo> {
  const files: Record<string, Uint8Array> = {};
  for (const top of BACKUP_DIRS) {
    for (const rel of await walk(path.join(dataDir, top), dataDir)) {
      files[rel.split(path.sep).join('/')] = new Uint8Array(
        await fs.readFile(path.join(dataDir, rel)),
      );
    }
  }
  const createdAt = new Date().toISOString();
  files['backup-info.json'] = strToU8(JSON.stringify({ reason, createdAt }));
  const dir = path.join(dataDir, 'backups');
  await fs.mkdir(dir, { recursive: true });
  const st0 = stamp();
  const same = (await listBackups(dataDir)).filter((b) => b.name.startsWith(`backup-${st0}`));
  const next = same.length
    ? Math.max(...same.map((b) => Number(/-(\d+)\.zip$/.exec(b.name)?.[1] ?? 0))) + 1
    : 0;
  const name = next ? `backup-${st0}-${next}.zip` : `backup-${st0}.zip`;
  await atomicWrite(path.join(dir, name), zipSync(files));
  await pruneBackups(dataDir, keep);
  const st = await fs.stat(path.join(dir, name));
  return { name, size: st.size, createdAt, reason };
}

export async function listBackups(dataDir: string): Promise<BackupInfo[]> {
  const dir = path.join(dataDir, 'backups');
  let names: string[];
  try {
    names = (await fs.readdir(dir)).filter(isValidBackupName);
  } catch {
    return [];
  }
  const out: BackupInfo[] = [];
  for (const name of names) {
    const st = await fs.stat(path.join(dir, name));
    out.push({ name, size: st.size, createdAt: st.mtime.toISOString() });
  }
  const key = (n: string) => {
    const m = /^backup-(\d{8}-\d{6})(?:-(\d+))?\.zip$/.exec(n)!;
    return `${m[1]}-${String(m[2] ?? 0).padStart(6, '0')}`;
  };
  return out.sort((a, b) => (key(a.name) < key(b.name) ? 1 : -1));
}

export async function pruneBackups(dataDir: string, keep: number): Promise<void> {
  const all = await listBackups(dataDir);
  for (const b of all.slice(keep))
    await fs.rm(path.join(dataDir, 'backups', b.name), { force: true });
}

/** Safe relative path inside a zip: no traversal, no absolute, only known top dirs. */
export function isSafeZipPath(p: string): boolean {
  if (p.includes('\0') || p.startsWith('/') || /^[a-zA-Z]:/.test(p) || p.includes('\\'))
    return false;
  const parts = p.split('/');
  if (parts.some((s) => s === '..' || s === '')) return false;
  return (BACKUP_DIRS as readonly string[]).includes(parts[0]!);
}

export async function readBackup(
  dataDir: string,
  name: string,
): Promise<Record<string, Uint8Array>> {
  if (!isValidBackupName(name)) throw new Error('Invalid backup name');
  const raw = await fs.readFile(path.join(dataDir, 'backups', name));
  const all = unzipSync(new Uint8Array(raw));
  const out: Record<string, Uint8Array> = {};
  for (const [k, v] of Object.entries(all)) if (isSafeZipPath(k)) out[k] = v;
  return out;
}

/** Replace config/, data/ and activity/ with the contents of a backup. */
export async function restoreBackupFiles(dataDir: string, name: string): Promise<string[]> {
  const files = await readBackup(dataDir, name);
  for (const top of BACKUP_DIRS)
    await fs.rm(path.join(dataDir, top), { recursive: true, force: true });
  for (const [rel, bytes] of Object.entries(files))
    await atomicWrite(path.join(dataDir, rel), bytes);
  return Object.keys(files);
}

/** Read one file's text from the newest backup that has it (used for corrupt-file recovery). */
export async function latestBackupText(dataDir: string, rel: string): Promise<string | null> {
  for (const b of await listBackups(dataDir)) {
    try {
      const files = await readBackup(dataDir, b.name);
      const hit = files[rel];
      if (hit) return strFromU8(hit);
    } catch {
      /* try older */
    }
  }
  return null;
}

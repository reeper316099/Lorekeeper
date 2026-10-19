import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';

export interface Bootstrap {
  /** Custom data folder chosen in Settings (stored beside Electron's default userData). */
  dataDir?: string;
}

/**
 * Data folder precedence: LOREKEEPER_DATA_DIR env (dev/tests) → folder chosen in Settings →
 * Electron's userData (%APPDATA%\<App>, ~/Library/Application Support/<App>, $XDG_CONFIG_HOME|~/.config/<App>).
 */
export function resolveDataDir(opts: {
  env?: string;
  bootstrapDir?: string;
  userData: string;
}): string {
  const pick = [opts.env, opts.bootstrapDir].find((p) => p && p.trim() && path.isAbsolute(p));
  return path.resolve(pick ?? opts.userData);
}

export function readBootstrap(userData: string): Bootstrap {
  try {
    const file = path.join(userData, 'bootstrap.json');
    if (!existsSync(file)) return {};
    const raw = JSON.parse(readFileSync(file, 'utf8')) as Bootstrap;
    return typeof raw.dataDir === 'string' ? { dataDir: raw.dataDir } : {};
  } catch {
    return {};
  }
}

/** Where Electron places userData per OS (used by docs and the path test). */
export function expectedUserData(
  platform: NodeJS.Platform,
  appName: string,
  env: { HOME?: string; APPDATA?: string; XDG_CONFIG_HOME?: string },
): string {
  if (platform === 'win32') return path.win32.join(env.APPDATA ?? '', appName);
  if (platform === 'darwin')
    return path.posix.join(env.HOME ?? '', 'Library', 'Application Support', appName);
  return path.posix.join(
    env.XDG_CONFIG_HOME ?? path.posix.join(env.HOME ?? '', '.config'),
    appName,
  );
}

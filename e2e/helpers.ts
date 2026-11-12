import { spawn, type ChildProcess } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdtempSync, rmSync } from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium, type Browser, type Page } from '@playwright/test';

export const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
export const FIXTURES = path.join(root, 'fixtures', 'synthetic');

/**
 * The specs run against the Go API server (go/cmd/server) serving the built web UI, driven in
 * Chromium with the `window.lorekeeper` bridge the desktop shell would provide.
 */
export const GO_SERVER =
  process.env.E2E_GO_SERVER ??
  path.join(
    root,
    'release-go',
    process.platform === 'win32' ? 'lorekeeper-server.exe' : 'lorekeeper-server',
  );

/** What the specs need from a running app, whichever backend it is. */
export interface E2EApp {
  close(): Promise<void>;
  /** Run a script in the page, bypassing the page's Content-Security-Policy (used to inject axe). */
  injectScript(source: string): Promise<void>;
}

export interface Session {
  tmp: string;
  running: E2EApp[];
  browser?: Browser;
}

export const newSession = (): Session => ({
  tmp: mkdtempSync(path.join(os.tmpdir(), 'lorekeeper-e2e-')),
  running: [],
});
export const endSession = async (s: Session) => {
  for (const a of s.running.splice(0)) await a.close().catch(() => undefined);
  await s.browser?.close().catch(() => undefined);
  rmSync(s.tmp, { recursive: true, force: true });
};

const freePort = () =>
  new Promise<number>((resolve, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address() as net.AddressInfo;
      srv.close(() => resolve(port));
    });
  });

async function launchGo(s: Session, size: { width: number; height: number }) {
  const port = await freePort();
  const token = randomBytes(16).toString('hex');
  const baseUrl = `http://127.0.0.1:${port}`;
  const server: ChildProcess = spawn(GO_SERVER, [], {
    stdio: 'ignore',
    env: {
      ...process.env,
      PORT: String(port),
      LOREKEEPER_TOKEN: token,
      LOREKEEPER_DATA_DIR: path.join(s.tmp, 'data'),
      LOREKEEPER_STATIC: path.join(root, 'apps', 'web', 'dist'),
    },
  });
  for (let i = 0; ; i++) {
    const ok = await fetch(`${baseUrl}/api/v1/health`, {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((r) => r.ok)
      .catch(() => false);
    if (ok) break;
    if (i > 100) throw new Error(`the Go server did not start (${GO_SERVER})`);
    await new Promise((r) => setTimeout(r, 100));
  }
  s.browser ??= await chromium.launch();
  const context = await s.browser.newContext({ viewport: size });
  // the bridge the desktop shell provides
  await context.addInitScript(
    ({ baseUrl, token, platform }) => {
      (window as unknown as { lorekeeper: object }).lorekeeper = {
        baseUrl,
        token,
        platform,
        openDataDir: async () => undefined,
        chooseDataDir: async () => ({ ok: false }),
        setTheme: () => undefined,
        onMenu: () => () => undefined,
      };
    },
    { baseUrl, token, platform: process.platform },
  );
  const page = await context.newPage();
  await page.goto(baseUrl);
  const app: E2EApp = {
    injectScript: async (source) => {
      await page.evaluate(source);
    },
    close: async () => {
      await context.close().catch(() => undefined);
      if (server.exitCode === null) {
        server.kill('SIGTERM');
        await new Promise((r) => {
          const t = setTimeout(() => {
            server.kill('SIGKILL');
            r(null);
          }, 10_000);
          server.once('exit', () => {
            clearTimeout(t);
            r(null);
          });
        });
      }
    },
  };
  return { app, page };
}

export async function launch(
  s: Session,
  size = { width: 1360, height: 900 },
): Promise<{ app: E2EApp; page: Page }> {
  const launched = await launchGo(s, size);
  s.running.push(launched.app);
  await launched.page.waitForSelector('.app');
  return launched;
}

/** Import a synthetic workbook through the real wizard. */
export async function importWorkbook(page: Page, file: string): Promise<void> {
  await page.getByRole('button', { name: 'Import / Export' }).click();
  const another = page.getByRole('button', { name: 'Import another' });
  if (await another.isVisible()) await another.click(); // wizard is still on its report step
  await page.setInputFiles('input[type=file]', path.join(FIXTURES, file));
  await page.getByRole('button', { name: 'Next: warnings' }).click();
  await page.getByRole('button', { name: 'Next: import' }).click();
  await page.getByRole('button', { name: /^Import \d+ titles$/ }).click();
  await page.getByText('Import complete').waitFor();
}

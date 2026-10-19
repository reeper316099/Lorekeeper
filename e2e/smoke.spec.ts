import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  _electron as electron,
  expect,
  test,
  type ElectronApplication,
  type Page,
} from '@playwright/test';

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const MAIN = path.join(root, 'apps', 'desktop', 'dist', 'main.cjs');
const WORKBOOK = path.join(root, 'fixtures', 'synthetic', 'Watching_Tracker.xlsx');

let tmp: string;
const running: ElectronApplication[] = [];
const env = () => ({
  ...process.env,
  LOREKEEPER_HEADLESS: '1',
  LOREKEEPER_USER_DATA: path.join(tmp, 'userData'),
  LOREKEEPER_DATA_DIR: path.join(tmp, 'data'),
});

async function launch(): Promise<{ app: ElectronApplication; page: Page }> {
  const app = await electron.launch({ args: [MAIN], env: env() as Record<string, string> });
  running.push(app);
  const page = await app.firstWindow();
  await page.waitForSelector('.app');
  return { app, page };
}

const api = (page: Page, route: string, token = true) =>
  page.evaluate(
    async ({ route, token }) => {
      const b = (window as unknown as { lorekeeper: { baseUrl: string; token: string } })
        .lorekeeper;
      const r = await fetch(`${b.baseUrl}/api/v1${route}`, {
        headers: token ? { Authorization: `Bearer ${b.token}` } : {},
      });
      return {
        status: r.status,
        bytes: (await r.clone().arrayBuffer()).byteLength,
        json: r.headers.get('content-type')?.includes('json') ? await r.json() : null,
      };
    },
    { route, token },
  );

test.beforeAll(() => {
  tmp = mkdtempSync(path.join(os.tmpdir(), 'lorekeeper-e2e-'));
});
test.afterEach(async () => {
  for (const a of running.splice(0)) await a.close().catch(() => undefined);
});
test.afterAll(() => rmSync(tmp, { recursive: true, force: true }));

test('first run → import → search → filter → edit → export → restart keeps data', async () => {
  let { app, page } = await launch();

  // --- app identity, OS app-data convention and renderer isolation
  const info = await app.evaluate(({ app }) => ({
    name: app.getName(),
    appData: app.getPath('appData'),
    platform: process.platform,
    home: process.env.HOME,
    xdg: process.env.XDG_CONFIG_HOME,
    roaming: process.env.APPDATA,
  }));
  expect(info.name).toBe('Lorekeeper');
  const expectedRoot =
    info.platform === 'win32'
      ? info.roaming!
      : info.platform === 'darwin'
        ? path.join(info.home!, 'Library', 'Application Support')
        : (info.xdg ?? path.join(info.home!, '.config'));
  expect(path.resolve(info.appData)).toBe(path.resolve(expectedRoot));
  expect(
    await page.evaluate(() => [
      typeof (window as never as { require?: unknown }).require,
      typeof (window as never as { process?: unknown }).process,
    ]),
  ).toEqual(['undefined', 'undefined']);
  expect((await api(page, '/health', false)).status).toBe(401);
  expect((await api(page, '/health')).status).toBe(200);

  // --- first run: empty library, offered an import
  await page.getByRole('button', { name: 'Library' }).first().click();
  await expect(page.getByText('Your library is empty')).toBeVisible();

  // --- import the synthetic workbook through the wizard
  await page.getByRole('button', { name: 'Import / Export' }).click();
  await page.setInputFiles('input[type=file]', WORKBOOK);
  await page.getByRole('button', { name: 'Next: warnings' }).click();
  await page.getByRole('button', { name: 'Next: import' }).click();
  await page.getByRole('button', { name: /^Import \d+ titles$/ }).click();
  await expect(page.getByText('Import complete')).toBeVisible();
  await page.getByRole('button', { name: 'Open library' }).click();
  await expect(page.getByText('48 titles')).toBeVisible();

  // --- search finds a known title
  const firstTitle = (await page.locator('.trow .cell-title button').first().textContent())!.trim();
  await page.getByLabel('Search titles').fill(firstTitle.split(' ')[0]!);
  await expect(
    page.locator('.trow .cell-title button', { hasText: firstTitle }).first(),
  ).toBeVisible();
  await page.getByLabel('Search titles').fill('');
  await expect(page.getByText('48 titles')).toBeVisible();

  // --- filter by tag narrows the list
  const filters = page.getByRole('search', { name: 'Library filters' });
  await filters.getByRole('button', { name: /^Tags/ }).click();
  await page.locator('[role=option]').first().click();
  await expect(page.getByText('48 titles')).toBeHidden();
  await page.keyboard.press('Escape');
  await filters.getByRole('button', { name: /^Clear/ }).click();
  await expect(page.getByText('48 titles')).toBeVisible();

  // --- edit progress on a title with a numeric position
  const row = page
    .locator('.trow', {
      has: page.locator('button[aria-label="Increase progress"]:not([disabled])'),
    })
    .first();
  const before = Number(await row.locator('.progress-cell strong').textContent());
  const title = (await row.locator('.cell-title button').textContent())!.trim();
  await row.getByRole('button', { name: 'Increase progress' }).click();
  await expect(row.locator('.progress-cell strong')).toHaveText(String(before + 1));

  // --- export works (XLSX is a zip: "PK")
  const exported = await api(page, '/export/xlsx?library=watching');
  expect(exported.status).toBe(200);
  expect(exported.bytes).toBeGreaterThan(5000);

  // --- a second instance on the same profile does not start
  const second = spawn(
    process.execPath.replace(/node$/, 'node'),
    [
      '-e',
      `require('child_process').spawn(require('electron'), [${JSON.stringify(MAIN)}], {env: process.env, stdio: 'ignore'}).on('exit', c => process.exit(c ?? 0))`,
    ],
    { cwd: root, env: env() as NodeJS.ProcessEnv, stdio: 'ignore' },
  );
  const code = await new Promise<number | null>((resolve) => {
    const t = setTimeout(() => {
      second.kill();
      resolve(null);
    }, 20_000);
    second.on('exit', (c) => {
      clearTimeout(t);
      resolve(c);
    });
  });
  expect(code).toBe(0);

  // --- restart: data persisted on disk
  await app.close();
  ({ app, page } = await launch());
  await page.getByRole('button', { name: 'Library' }).first().click();
  await expect(page.getByText('48 titles')).toBeVisible();
  const entries = await api(page, `/entries?q=${encodeURIComponent(title)}&limit=5`);
  expect(entries.json.items.find((e: { title: string }) => e.title === title).current.number).toBe(
    before + 1,
  );
  await app.close();
});

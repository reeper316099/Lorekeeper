import { expect, test, type Page } from '@playwright/test';
import { BACKEND, endSession, importWorkbook, launch, newSession, type Session } from './helpers';

// The Go counterpart of smoke.spec.ts (run with E2E_BACKEND=go): the same user journey against the
// Go API server, minus the checks that are about Electron itself.
test.skip(BACKEND !== 'go', 'runs only with E2E_BACKEND=go');

let s: Session;
test.beforeEach(() => {
  s = newSession();
});
test.afterEach(() => endSession(s));

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

test('first run → import → search → filter → edit → export → restart keeps data', async () => {
  const first = await launch(s);
  let page = first.page;

  expect((await api(page, '/health', false)).status).toBe(401);
  expect((await api(page, '/health')).status).toBe(200);

  await page.getByRole('button', { name: 'Library' }).first().click();
  await expect(page.getByText('Your library is empty')).toBeVisible();

  await importWorkbook(page, 'Watching_Tracker.xlsx');
  await page.getByRole('button', { name: 'Open library' }).click();
  await expect(page.getByText('48 titles')).toBeVisible();

  const firstTitle = (await page.locator('.trow .cell-title button').first().textContent())!.trim();
  await page.getByLabel('Search titles').fill(firstTitle.split(' ')[0]!);
  await expect(
    page.locator('.trow .cell-title button', { hasText: firstTitle }).first(),
  ).toBeVisible();
  await page.getByLabel('Search titles').fill('');
  await expect(page.getByText('48 titles')).toBeVisible();

  const filters = page.getByRole('search', { name: 'Library filters' });
  await filters.getByRole('button', { name: /^Tags/ }).click();
  await page.locator('[role=option]').first().click();
  await expect(page.getByText('48 titles')).toBeHidden();
  await page.keyboard.press('Escape');
  await filters.getByRole('button', { name: /^Clear/ }).click();
  await expect(page.getByText('48 titles')).toBeVisible();

  const row = page
    .locator('.trow', {
      has: page.locator('button[aria-label="Increase progress"]:not([disabled])'),
    })
    .first();
  const before = Number(await row.locator('.progress-cell strong').textContent());
  const title = (await row.locator('.cell-title button').textContent())!.trim();
  await row.getByRole('button', { name: 'Increase progress' }).click();
  await expect(row.locator('.progress-cell strong')).toHaveText(String(before + 1));

  const exported = await api(page, '/export/xlsx?library=watching');
  expect(exported.status).toBe(200);
  expect(exported.bytes).toBeGreaterThan(5000);

  // restart the server on the same data folder: everything persisted
  await first.app.close();
  page = (await launch(s)).page;
  await page.getByRole('button', { name: 'Library' }).first().click();
  await expect(page.getByText('48 titles')).toBeVisible();
  const entries = await api(page, `/entries?q=${encodeURIComponent(title)}&limit=5`);
  expect(entries.json.items.find((e: { title: string }) => e.title === title).current.number).toBe(
    before + 1,
  );
});

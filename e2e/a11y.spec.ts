import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { expect, test, type ElectronApplication, type Page } from '@playwright/test';
import { endSession, importWorkbook, launch, newSession, type Session } from './helpers';

const axeSource = readFileSync(
  createRequire(import.meta.url).resolve('axe-core/axe.min.js'),
  'utf8',
);

interface AxeResult {
  violations: { id: string; impact: string; help: string; nodes: { target: string[] }[] }[];
}

const found: string[] = [];
let s: Session;
test.beforeEach(() => {
  s = newSession(); // a fresh profile per test: the single-instance lock allows one app per profile
});
test.afterEach(() => endSession(s));
test.afterEach(() => {
  const all = found.splice(0);
  expect(all, all.join('\n')).toEqual([]);
});

async function audit(app: ElectronApplication, page: Page, label: string) {
  await page.waitForTimeout(350); // let entrance animations settle
  // The page's strict CSP (rightly) blocks injected <script> tags, so run axe through the main process instead.
  if (!(await page.evaluate(() => 'axe' in window)))
    await app.evaluate(
      ({ BrowserWindow }, src) =>
        BrowserWindow.getAllWindows()[0]!.webContents.executeJavaScript(src),
      axeSource,
    );
  const res = (await page.evaluate(() =>
    (window as unknown as { axe: { run(ctx: Document, o: object): Promise<unknown> } }).axe.run(
      document,
      {
        runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] },
      },
    ),
  )) as AxeResult;
  const lines = res.violations.map(
    (v) =>
      `${label}: [${v.impact}] ${v.id} — ${v.help} (${v.nodes.length}) e.g. ${v.nodes[0]?.target.join(' ')}`,
  );
  found.push(...lines);
}

const go = (page: Page, name: string | RegExp) =>
  page
    .getByRole('button', { name, exact: typeof name === 'string' })
    .first()
    .click();

for (const theme of ['dark', 'light'] as const) {
  test(`WCAG 2 AA audit of every screen (${theme})`, async () => {
    const { app, page } = await launch(s);
    await importWorkbook(page, 'Watching_Tracker.xlsx');
    await page.getByRole('button', { name: 'Settings' }).click();
    await page
      .getByRole('button', { name: theme[0]!.toUpperCase() + theme.slice(1), exact: true })
      .click();

    await go(page, 'Dashboard');
    await audit(app, page, `${theme} dashboard`);

    await go(page, 'Library');
    await expect(page.locator('.trow').first()).toBeVisible();
    await audit(app, page, `${theme} library table`);
    await page.getByRole('button', { name: 'Card view' }).click();
    await audit(app, page, `${theme} library cards`);
    await page.getByRole('button', { name: 'Table view' }).click();

    await page.locator('.trow .cell-title button').first().click();
    await expect(page.getByRole('dialog', { name: 'Title details' })).toBeVisible();
    await audit(app, page, `${theme} drawer`);
    await page.keyboard.press('Escape');

    await page.getByRole('button', { name: 'Open command palette' }).click();
    await expect(page.getByRole('dialog', { name: 'Command palette' })).toBeVisible();
    await audit(app, page, `${theme} palette`);
    await page.keyboard.press('Escape');

    await page.getByRole('button', { name: 'Add title' }).click();
    await audit(app, page, `${theme} quick add`);
    await page.keyboard.press('Escape');

    for (const name of ['Categories', 'Tags', 'Import / Export', 'AniList', 'Settings']) {
      await go(page, name);
      await audit(app, page, `${theme} ${name}`);
    }
  });
}

test('keyboard: skip link, focus is trapped in dialogs and restored, reduced motion is honoured', async () => {
  const { page } = await launch(s);
  await importWorkbook(page, 'Watching_Tracker.xlsx');
  await page.getByRole('button', { name: 'Library' }).first().click();
  await expect(page.locator('.trow').first()).toBeVisible();

  // skip link is the first stop and moves focus to the main region
  await page.reload(); // a fresh page: the first Tab stop must be the skip link
  await page.waitForSelector('.app');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('link', { name: 'Skip to main content' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('#main')).toBeFocused();

  const insideDialog = () =>
    page.evaluate(() => !!document.activeElement?.closest('[role=dialog]'));

  // command palette
  const paletteButton = page.getByRole('button', { name: 'Open command palette' });
  await paletteButton.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('combobox', { name: /Search titles and commands/ })).toBeFocused();
  for (let i = 0; i < 8; i++) {
    await page.keyboard.press('Tab');
    expect(await insideDialog()).toBe(true);
  }
  await page.keyboard.press('Escape');
  await expect(paletteButton).toBeFocused();

  // detail drawer opened from a row, closed with Escape
  const title = page.locator('.trow .cell-title button').first();
  await title.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('dialog', { name: 'Title details' })).toBeVisible();
  for (let i = 0; i < 40; i++) {
    await page.keyboard.press('Tab');
    expect(await insideDialog()).toBe(true);
  }
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog', { name: 'Title details' })).toBeHidden();
  await expect(title).toBeFocused();

  // quick add via the N shortcut puts the cursor in the title field
  await page.locator('body').click({ position: { x: 1, y: 1 } });
  await page.keyboard.press('n');
  await expect(page.getByLabel('Title', { exact: true })).toBeFocused();
  await page.keyboard.press('Escape');

  // prefers-reduced-motion switches transitions off
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const ms = await page.evaluate(
    () => parseFloat(getComputedStyle(document.querySelector('.btn')!).transitionDuration) * 1000,
  );
  expect(ms).toBeLessThan(1);
});

import path from 'node:path';
import { test, type Page } from '@playwright/test';
import { endSession, importWorkbook, launch, newSession, root } from './helpers';

// Regenerates docs/screenshots from the synthetic fixtures only:  npm run screenshots
test.skip(
  !process.env.SCREENSHOTS,
  'set SCREENSHOTS=1 (npm run screenshots) to regenerate the README images',
);

const out = (name: string) => path.join(root, 'docs', 'screenshots', `${name}.png`);
const nav = (page: Page, name: string) =>
  page.getByRole('button', { name, exact: true }).first().click();

test('capture screenshots', async () => {
  const s = newSession();
  try {
    const { page } = await launch(s, { width: 1360, height: 860 });
    await importWorkbook(page, 'Watching_Tracker.xlsx');
    await importWorkbook(page, 'Reading_Tracker.xlsx');
    const api = (route: string, body?: object) =>
      page.evaluate(
        async ({ route, body }) => {
          const b = (window as unknown as { lorekeeper: { baseUrl: string; token: string } })
            .lorekeeper;
          const r = await fetch(`${b.baseUrl}/api/v1${route}`, {
            method: body ? 'POST' : 'GET',
            headers: { Authorization: `Bearer ${b.token}`, 'Content-Type': 'application/json' },
            body: body ? JSON.stringify(body) : undefined,
          });
          return r.json();
        },
        { route, body },
      );
    const inProgress = (await api('/entries?status=in_progress&limit=12')).items as {
      id: string;
      current: { kind: string | null; number: number | null };
    }[];
    for (const e of inProgress.filter((x) => x.current.number !== null).slice(0, 8))
      await api(`/entries/${e.id}/progress`, { delta: 1 });

    await nav(page, 'Settings');
    await page.getByRole('button', { name: 'Dark', exact: true }).click();
    await nav(page, 'Dashboard');
    await page.waitForTimeout(900);
    await page.screenshot({ path: out('dashboard-dark') });

    await nav(page, 'Library');
    await page.locator('.trow').first().waitFor();
    await page.waitForTimeout(300);
    await page.screenshot({ path: out('library-table') });

    await page.getByRole('button', { name: 'Card view' }).click();
    await page.waitForTimeout(500);
    await page.screenshot({ path: out('library-cards') });
    await page.getByRole('button', { name: 'Table view' }).click();

    await page.locator('.trow .cell-title button').nth(2).click();
    await page.getByRole('dialog', { name: 'Title details' }).waitFor();
    await page.waitForTimeout(400);
    await page.screenshot({ path: out('detail-drawer') });
    await page.keyboard.press('Escape');

    await nav(page, 'Tags');
    await page.waitForTimeout(500);
    await page.screenshot({ path: out('tag-cleanup') });

    await nav(page, 'Import / Export');
    await page.setInputFiles(
      'input[type=file]',
      path.join(root, 'fixtures', 'synthetic', 'Reading_Tracker.xlsx'),
    );
    await page.getByText('Review mapping').first().waitFor();
    await page.waitForTimeout(500);
    await page.screenshot({ path: out('import-wizard') });

    await nav(page, 'Settings');
    await page.getByRole('button', { name: 'Light', exact: true }).click();
    await nav(page, 'Dashboard');
    await page.waitForTimeout(900);
    await page.screenshot({ path: out('dashboard-light') });
    await nav(page, 'Library');
    await page.locator('.trow').first().waitFor();
    await page.getByRole('button', { name: 'Card view' }).click();
    await page.waitForTimeout(500);
    await page.screenshot({ path: out('library-cards-light') });
  } finally {
    await endSession(s);
  }
});

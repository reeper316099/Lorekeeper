import { expect, test } from '@playwright/test';
import { endSession, importWorkbook, launch, newSession } from './helpers';

test('links can be added, edited and removed from the detail drawer', async () => {
  const s = newSession();
  try {
    const { page } = await launch(s);
    await importWorkbook(page, 'Watching_Tracker.xlsx');
    await page.getByRole('button', { name: 'Library' }).first().click();
    const title = (await page.locator('.trow .cell-title button').first().textContent())!.trim();
    await page.locator('.trow .cell-title button').first().click();
    const drawer = page.getByRole('dialog', { name: 'Title details' });
    const before = await drawer.locator('a[target=_blank]').count();

    // a scheme-less YouTube link becomes a clickable https link
    await drawer.getByRole('combobox', { name: 'Link type' }).selectOption('youtube');
    await drawer.getByLabel('Add a link').fill('youtu.be/abc123XYZ?si=zz');
    await drawer.getByRole('button', { name: 'Add', exact: true }).click();
    const added = drawer.locator('a[href="https://youtu.be/abc123XYZ?si=zz"]');
    await expect(added).toBeVisible();

    // plain text is kept as a note
    await drawer.getByLabel('Add a link').fill('(S1 #full) Some recap (3/3/26) By: RecapRiver');
    await drawer.getByRole('button', { name: 'Add', exact: true }).click();
    await expect(drawer.getByText('RecapRiver').first()).toBeVisible();

    // edit the first one
    await drawer.getByRole('button', { name: /^Edit link https:\/\/youtu\.be\/abc123XYZ/ }).click();
    await drawer.getByLabel('Edit link', { exact: true }).fill('example.org/series/42');
    await drawer.getByRole('button', { name: 'Save' }).click();
    await expect(drawer.locator('a[href="https://example.org/series/42"]')).toBeVisible();
    await expect(added).toBeHidden();

    // persisted through the API, then removed
    const read = () =>
      page.evaluate(async (t) => {
        const b = (window as unknown as { lorekeeper: { baseUrl: string; token: string } })
          .lorekeeper;
        const r = await fetch(`${b.baseUrl}/api/v1/entries?q=${encodeURIComponent(t)}&limit=1`, {
          headers: { Authorization: `Bearer ${b.token}` },
        });
        return (await r.json()).items[0].links as { url: string | null; channel?: string | null }[];
      }, title);
    await expect
      .poll(async () => (await read()).some((l) => l.url === 'https://example.org/series/42'))
      .toBe(true);
    expect((await read()).some((l) => l.channel === 'RecapRiver')).toBe(true);

    await drawer.getByRole('button', { name: /^Remove link https:\/\/example\.org/ }).click();
    await expect
      .poll(async () => (await read()).some((l) => l.url === 'https://example.org/series/42'))
      .toBe(false);
    expect(await drawer.locator('a[target=_blank]').count()).toBe(before);
  } finally {
    await endSession(s);
  }
});

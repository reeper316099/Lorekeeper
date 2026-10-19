import { mkdtempSync, rmSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { _electron as electron, type ElectronApplication, type Page } from '@playwright/test';

export const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
export const MAIN = path.join(root, 'apps', 'desktop', 'dist', 'main.cjs');
export const FIXTURES = path.join(root, 'fixtures', 'synthetic');

export interface Session {
  tmp: string;
  running: ElectronApplication[];
}

export const newSession = (): Session => ({
  tmp: mkdtempSync(path.join(os.tmpdir(), 'lorekeeper-e2e-')),
  running: [],
});
export const endSession = async (s: Session) => {
  for (const a of s.running.splice(0)) await a.close().catch(() => undefined);
  rmSync(s.tmp, { recursive: true, force: true });
};

export const sessionEnv = (s: Session) => ({
  ...process.env,
  LOREKEEPER_HEADLESS: '1',
  LOREKEEPER_USER_DATA: path.join(s.tmp, 'userData'),
  LOREKEEPER_DATA_DIR: path.join(s.tmp, 'data'),
});

export async function launch(
  s: Session,
  size = { width: 1360, height: 900 },
): Promise<{ app: ElectronApplication; page: Page }> {
  const app = await electron.launch({ args: [MAIN], env: sessionEnv(s) as Record<string, string> });
  s.running.push(app);
  const page = await app.firstWindow();
  await page.setViewportSize(size);
  await page.waitForSelector('.app');
  return { app, page };
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

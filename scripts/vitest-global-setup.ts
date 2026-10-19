import { existsSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

/** The synthetic workbooks are generated, not committed. Build them if they are missing. */
export default function setup() {
  const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
  const dir = path.join(root, 'fixtures', 'synthetic');
  if (
    existsSync(path.join(dir, 'Reading_Tracker.xlsx')) &&
    existsSync(path.join(dir, 'Watching_Tracker.xlsx'))
  )
    return;
  execFileSync(
    process.execPath,
    ['--import', 'tsx', path.join(root, 'scripts', 'generate-fixtures.ts')],
    { stdio: 'inherit' },
  );
}

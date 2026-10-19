#!/usr/bin/env node
// Fails if a spreadsheet/CSV outside fixtures/synthetic/ (or anything in private-fixtures/) is
// staged (--staged) or tracked. Keeps personal data out of the public repo.
import { execFileSync } from 'node:child_process';

const staged = process.argv.includes('--staged');
const args = staged ? ['diff', '--cached', '--name-only', '--diff-filter=ACMR'] : ['ls-files'];
const files = execFileSync('git', args, { encoding: 'utf8' }).split('\n').filter(Boolean);

export function isForbidden(file) {
  const f = file.replace(/\\/g, '/');
  if (f.startsWith('private-fixtures/')) return true;
  // Generated workbooks are never committed (they are produced by `npm run fixtures:generate`).
  if (/\.(xlsx|xlsm|xls)$/i.test(f)) return true;
  if (/\.(csv|tsv)$/i.test(f)) return !f.startsWith('fixtures/synthetic/');
  return false;
}

const bad = files.filter(isForbidden);
if (bad.length) {
  console.error('Refusing: personal-data-risk files are tracked/staged:\n  ' + bad.join('\n  '));
  process.exit(1);
}
console.log(`check-no-spreadsheets: ok (${files.length} files)`);

/**
 * Opens a data folder that was written by the Go store with the Node store and prints what it
 * found; exits 1 on any problem. Used to prove the two implementations share one on-disk format.
 *   INTEROP_OUT=/tmp/go-folder go test -run TestWriteInteropFolder ./internal/store   (in go/)
 *   npx tsx scripts/check-go-data-dir.ts /tmp/go-folder
 */
import { Store } from '../packages/server/src/index.ts';

const dir = process.argv[2];
if (!dir) throw new Error('usage: check-go-data-dir.ts <folder>');
const problems: string[] = [];
const store = new Store(dir);
store.on('file-error', (e) => problems.push(`file-error ${JSON.stringify(e)}`));
await store.open();

const titles = store
  .all()
  .map((e) => e.title)
  .sort();
const check = (cond: boolean, msg: string) => cond || problems.push(msg);
check(titles.join('|') === 'Glass Meridian|Moved Along|Tom & Jerry <Reborn>', `titles: ${titles}`);
check(
  store.categories
    .map((c) => c.slug)
    .sort()
    .join() === 'anime,harem,love-stories',
  'category slugs',
);
const glass = store.all().find((e) => e.title === 'Glass Meridian')!;
check(glass.current.season === 2 && glass.current.number === 3, 'progress');
check(glass.tags.join() === 'Cozy Town', `tags after merge: ${glass.tags}`);
check(store.settings.theme === 'light', 'settings');
check((glass.external as Record<string, unknown>).extraKey !== undefined, 'passthrough external');
const tom = store.all().find((e) => e.title.startsWith('Tom'))!;
check(tom.notes.general === 'Line sep', 'U+2028 survived');
const backups = await store.listBackups();
check(backups.length >= 1, 'no backups listed');
const first = backups.find((b) => b.name);
if (first) {
  await store.restoreBackup(first.name); // reads the zip the Go store wrote
  check(
    store.all().length === 3,
    `entries after restoring a Go-written zip: ${store.all().length}`,
  );
}
const acts = await store.readActivity();
check(
  acts.some((a) => a.kind === 'created') && acts.some((a) => a.kind === 'progress'),
  'activity',
);
await store.close();

console.log(
  `entries=${titles.length} categories=${store.categories.length} backups=${backups.length} activity=${acts.length}`,
);
if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('Node opened the Go-written folder cleanly (incl. restoring a Go-written backup zip).');

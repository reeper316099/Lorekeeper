/**
 * Opens a data folder written by the Go AniList service with the Node one, with the network cut off:
 * the review queue must load, and searching must be answered from the cache files the Go side wrote
 * (same file names, same format), which proves both implementations share the on-disk format.
 *   cd go && INTEROP_OUT=/tmp/go-ani go test -run TestWriteInteropFolder ./internal/anilist
 *   npx tsx scripts/check-go-anilist.ts /tmp/go-ani
 */
import { readdirSync } from 'node:fs';
import path from 'node:path';
import { AniListClient } from '../packages/server/src/anilist/client.ts';
import { JobRegistry } from '../packages/server/src/anilist/jobs.ts';
import { AniListService } from '../packages/server/src/anilist/service.ts';
import { Store } from '../packages/server/src/index.ts';

const dir = process.argv[2];
if (!dir) throw new Error('usage: check-go-anilist.ts <folder>');
const problems: string[] = [];
const check = (ok: boolean, msg: string) => ok || problems.push(msg);

let requests = 0;
const offline = (async () => {
  requests++;
  throw new TypeError('network is off');
}) as typeof fetch;
const store = new Store(dir);
await store.open();
const svc = new AniListService(
  store,
  new AniListClient({ dataDir: dir, fetch: offline, sleep: async () => {}, maxRetries: 0 }),
  new JobRegistry(),
);

const counts = await svc.counts();
check(
  counts.matched === 1 && counts.queue.pending === 2 && counts.queue.unlinkable === 1,
  `counts ${JSON.stringify(counts)}`,
);
const pending = await svc.queueItems('pending');
check(pending.length === 2 && pending.every((p) => p.status === 'pending'), 'pending queue');
const twin = pending.find((p) => p.entry.title === 'Twin Stars of Ember')!;
check(!!twin && twin.candidates.length === 2, `twin candidates ${twin?.candidates.length}`);
const matched = store.all().find((e) => e.title === 'Lantern of the Hollow Crown')!;
check(matched.external.anilistId === 1 && matched.external.cover === '1.jpg', 'matched entry');

// Searches are served from the Go-written cache: same key, same file format.
const cands = await svc.candidatesFor(twin.entry.id).catch((e) => `error: ${e}`);
check(
  Array.isArray(cands) && cands.length === 2,
  `candidates from cache: ${JSON.stringify(cands)}`,
);
const media = await svc.mediaById(3).catch((e) => `error: ${e}`);
check(
  typeof media === 'object' && (media as { id: number }).id === 3,
  `media 3 from cache: ${JSON.stringify(media)}`,
);
check(requests === 0, `${requests} network requests were needed; the cache should have answered`);

const files = readdirSync(path.join(dir, 'anilist', 'cache'));
console.log(
  `cache files: ${files.length}, queue pending: ${pending.length}, network requests: ${requests}`,
);
await store.close();
if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('Node read the Go-written AniList queue and cache cleanly (offline).');

# Phase 2: storage engine

Status: **done** (awaiting approval for phase 3).

## What was done

`go/internal/store`, a port of `packages/server/src/storage/*`:

| File                       | Port of              | Contents                                                                                                                                      |
| -------------------------- | -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `atomic.go`                | `atomic.ts`          | `StableMarshal` (sorted keys, 2 spaces, no HTML escaping), `AtomicWrite` (tmp, fsync, rename with Windows retry), `KeyedMutex` (FIFO per key) |
| `backups.go`               | `backups.ts`         | zip backups of `config/ data/ activity/`, list (newest first), prune, safe-path check, restore, newest-backup lookup                          |
| `migrations.go`            | `migrations.ts`      | ordered migrations after a backup, refuses data from a newer schema                                                                           |
| `store.go`                 | `store.ts` (core)    | `Open`, `LoadAll`, quarantine + restore of corrupt files, write coalescing, events, backups                                                   |
| `entries.go`               | `store.ts` (entries) | create, insert, replace, update (patch merge), progress, restore, delete, bulk, two-file move with rollback                                   |
| `categories.go`, `tags.go` | `store.ts`           | category create/rename/reorder/delete, tag canonicalization, rename, merge with aliases, settings and scales                                  |
| `activity.go`              | `store.ts`           | append-only `activity/YYYY-MM.jsonl`, tolerant reader                                                                                         |
| `watch.go`                 | chokidar watcher     | `fsnotify`, 150 ms settle delay, own writes ignored, invalid hand edits reported and the old data kept                                        |

Also in `internal/shared`: `ActivityRecord.From/To` keep an explicit `null`; struct fields are declared in sorted JSON-key order (guarded by `TestJSONFieldsAreSorted`) so writing is a single pass.

### Concurrency model

Node's store is single-threaded, so "mutate memory, then await the write" was atomic for free. In Go:

- One `sync.RWMutex` guards the in-memory state. Entries are **immutable** once stored; every change replaces the entry and the category slice (copy on write). Readers get slices that stay valid while writes continue.
- Files are serialized **outside** the lock from an immutable snapshot, so a 5 MB write never blocks a read (slowest read during writes: 6-9 ms; an earlier draft that serialized under the read lock stalled reads for 540 ms, caught by the perf test).
- Read-modify-write operations (`SetProgress` with a delta, updates) run entirely under the write lock, so 30 concurrent "+1" requests give exactly 30 (a first draft lost updates; `TestCoalescesBurstsButPersistsFinalState` and `TestConcurrentReadersAndWriters` caught it).
- Write coalescing is the same algorithm as Node: while one write runs, any number of requests share one follow-up write of the latest data.
- `go test -race` is clean.

## How compatibility with Node is proven

1. **Node folder opens in Go.** `scripts/gen-go-store-fixtures.ts` makes a data folder with the _Node_ store (invented content: categories, entries with links, progress, aliases, passthrough `external` data, `&`, `<`, U+2028, accents; settings, scales, activity). `go/internal/store/testdata/nodedata/` is committed. `TestOpensNodeWrittenDataFolder` opens it with no `file-error`.
2. **Byte-identical re-serialization.** `TestReserializingNodeFilesIsByteIdentical` parses every file with the Go schemas and writes it back: all 10 files are identical to what Node wrote. `TestOpenDoesNotRewriteNodeFiles`: opening a Node folder rewrites nothing except `meta.json`.
3. **Go folder opens in Node.** `INTEROP_OUT=/tmp/x go test -run TestWriteInteropFolder ./internal/store` writes a folder with the Go store (create, move between categories, merge tags, rename a category, backup); `npx tsx scripts/check-go-data-dir.ts /tmp/x` opens it with the Node store, checks the content, and restores the **zip backup the Go store wrote**. Result: `Node opened the Go-written folder cleanly`.

## Tests

33 Go tests in `internal/store` pass (2 more are skipped unless asked: the perf run and the interop writer), plus 34 in `internal/shared` (phase 1 tests, still green).

Ported from the 18 tests in `storage.test.ts` (16 here; the search-index test belongs to phase 3, and the stable-stringify test is joined by escaping checks):

- stable JSON, no tmp files after 20 concurrent writes, retry on EPERM/EBUSY and no retry on ENOENT, per-key FIFO mutex
- documented layout, concurrent writes persist, corrupt file quarantined and restored from the newest backup, corrupt file without backup falls back to empty
- move between categories, move rollback when the second write fails, progress delta writes activity and starts planned entries, tag merge with aliases
- hand edits reload and invalid edits keep memory, backups create/list/prune/restore, unsafe zip paths, ordered migrations
- write coalescing

Added for the Go version: invalid-schema file is quarantined too, patch merging and protected keys, bulk operations and the pre-delete backup, category lifecycle (slug collisions, rename moves the file, delete needs `moveTo`), tag rename, settings and scales persistence, activity reader with torn lines, launch backup rules, newer-schema refusal, own writes not reported as external changes, a concurrent reader/writer stress test, and the three compatibility tests above.

## Timing vs Node (same machine, warm caches)

Test suites (not directly comparable, see note):

| Measure           | Node (`storage.test.ts`) | Go (`internal/store`)                |
| ----------------- | ------------------------ | ------------------------------------ |
| Tests             | 18                       | 33                                   |
| Wall time, 3 runs | 4.03s, 4.46s, 5.32s      | 5.1s, 5.6s, 8.6s (7.5s with `-race`) |

The Go suite is bigger and has more deliberate waits (two watcher tests wait 0.7s each to prove nothing fires, a 1.2s stress test). It is not faster as a suite; the operations are, below.

`scripts/perf.ts` storage rows vs the Go port (`PERF=1 go test -run TestPerfStore -v ./internal/store`), 10,000 entries, same data and operations:

| Operation                                 | Node (3 runs, p50)           | Go (6 runs, p50)            |
| ----------------------------------------- | ---------------------------- | --------------------------- |
| Bulk insert, 1 write per category         | 272-373 ms                   | 92-161 ms                   |
| Cold open (read + validate all JSON)      | 595-664 ms                   | 189-281 ms                  |
| PATCH one entry (persist the big file)    | 143-165 ms                   | 54-75 ms                    |
| 20 concurrent progress writes (coalesced) | 257-349 ms                   | 184-358 ms (one run: 2.2 s) |
| Slowest read while writes were in flight  | 2.7 ms (search, earlier run) | 5.7-9 ms (`All()`)          |

Roughly 2-3x faster on open and on single writes, about the same for the coalesced burst. The 2.2 s outlier was one run in about ten; it is a disk stall (every write fsyncs, and Go's `File.Sync` on macOS is a bit slower than Node's plain `fsync`: 4.3 ms vs 1.1 ms per small write in a probe). I did not weaken durability to hide it; if it shows up in the app, switching macOS to plain `fsync` is a one-line change.

## Differences from the Node store, on purpose

- API is Go-shaped: `CreateEntry(map)` / `UpdateEntry(id, patch map)` take JSON-like maps (the HTTP layer will pass request bodies through), events use `On(func(Event))` instead of `EventEmitter`, `All()` iterates categories in order (Node used Map insertion order, which is the same in practice).
- Entry `create`/`update` go through the same JSON validation as files, so an invalid patch is rejected with a `*shared.ValidationError` and the entry is left untouched.
- `activity` records keep an explicit `from: null` (Node did too); fixed in `ActivityRecord` after the byte-identity test flagged it.
- A single backup file read is capped at 512 MB (zip-bomb guard); Node had no cap.
- Tag rename compares `t == oldName` exactly, as Node did.

## Known gaps / next

- Search index, stats and the HTTP layer are phase 3 (they read through `All()`/`EntriesOf()`; no store changes expected).
- `uuid` v7 comes from `github.com/google/uuid` (same format as `uuidv7`).
- CI runs the Go tests on 3 OS; the Node-opens-Go-folder check is a manual command for now (it needs both toolchains), documented above.

# Phase 5: AniList

Status: **done** (awaiting approval for phase 6).

## What was done

`go/internal/anilist`, a port of `packages/server/src/anilist/*` (v1: no login, no OAuth; M8 stays on hold):

| File         | Port of      | Contents                                                                                                                                                                                                                                |
| ------------ | ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `queries.go` | `queries.ts` | the three GraphQL queries (identical text) and typed `Media` / `ListEntry`                                                                                                                                                              |
| `client.go`  | `client.ts`  | FIFO rate limiter (30/min, lowered by response headers), 429 / `Retry-After`, 5xx backoff 1-2-4 s, offline detection, cancellation, 7-day disk cache with stale-when-offline, allow-listed image proxy and cover files, cleanup helpers |
| `matcher.go` | `matcher.ts` | what is linkable, candidate ranking (title, country, format), auto-confirm margin, AniList URL parsing                                                                                                                                  |
| `jobs.go`    | `jobs.ts`    | in-memory job registry with cancellation and progress callbacks (published as store `job` events, so SSE works)                                                                                                                         |
| `service.go` | `service.ts` | review queue (`anilist/match-queue.json`), matching job, confirm / set / reject / never / retry, unmatch (frees cover and cache), clear cache, suggested tags, account import (preview then commit with covers, snapshot and undo)      |
| `routes.go`  | `routes.ts`  | all 18 `/anilist/*` routes                                                                                                                                                                                                              |

Also: `internal/routes.Extend` wires import/export and AniList together (what the Node server called `registerAllRoutes`); `cmd/server` uses it. Supporting changes: `Store.WriteJSON`, `Store.Emit`, `store.HTTPError.Details` (for `retryAfterSec`), `importexport.MergeInto` exported, `api.Download` can be inline with a cache header (covers and images).

## How parity with Node is proven

The tests never touch the real AniList; they use a small fake AniList that understands the three queries, identical in Node and Go.

1. **Node service vs Go service.** `scripts/gen-go-anilist-fixtures.ts` drives the real Node service through 3 scenarios (18 steps) against the fake world and records, after every step, the step's result, **every entry**, the names of all files in `anilist/cache` and `covers`. Scenarios: matching with review actions (auto-confirm, queue, unlinkable, suggested tags, confirm, set by URL, search with a custom query, rerun, never, unmatch, clear caches); a cover shared by two titles surviving one unmatch; and the account import (preview, commit with covers, repeat preview, overwrite, skip, keep both, missing category). `TestServiceMatchesNode` requires identical results, **including the cache file names** (so the SHA-1 cache keys and image hashes are the same as Node's). I checked the test has teeth by breaking the auto-confirm margin and the HTML stripping: both failed immediately.
2. **Matcher.** Ranking of six candidates (country and format penalties), URL parsing and cache keys equal Node's.
3. **Node reads Go's files.** `INTEROP_OUT=/tmp/x go test -run TestWriteInteropFolder ./internal/anilist` then `npx tsx scripts/check-go-anilist.ts /tmp/x`: the Node service, with the network cut off, loads the Go-written review queue and answers a search and a media lookup **from the Go-written cache** (0 network requests). That proves same file names, same file format, same queue format.
4. **Real HTTP.** A client test talks to a local HTTP server through `net/http` (the `LOREKEEPER_ANILIST_ENDPOINT` override), including a 404 GraphQL error.
5. **The real UI.** The React app against `cmd/server` (AniList pointed at a closed port): the AniList screen shows "1 of 4 titles linked", the 30/min note and all the queue tabs; "Match my library" ends with "AniList unreachable" after the real retries.

## Tests

`internal/anilist`: 36 tests plus 3 scenario sub-tests, all passing (2 more run on request: perf and the interop writer). Ported from the 23 tests of `anilist-client.test.ts` and `anilist-service.test.ts`: rate budget and 429/5xx/GraphQL handling, offline and cancellation, cache TTL and stale data, image allow-list, matcher, auto-confirm margin, review actions, never/reject/retry, unmatch freeing cover and cache (and keeping a shared cover), clearing the cache, offline/cancel jobs, auth and token-only image routes, the account import and its undo, unknown users, offline import. Added: concurrent callers sharing one budget, the exact request shape, a real timeout, oversized and non-image downloads, `retryAfterSec` in the 429 response, job events, empty-body match run, and cleanup helper edge cases. `go test -race ./...` passes for the whole module.

## Timing vs Node (same machine, warm caches)

| Measure           | Node (client + service tests) | Go (`internal/anilist`)     |
| ----------------- | ----------------------------- | --------------------------- |
| Tests             | 23                            | 36 (+ 3 scenario sub-tests) |
| Wall time, 3 runs | 2.40 s, 1.75 s, 1.73 s        | 2.73 s, 2.58 s, 2.65 s      |

Operations against the in-process fake AniList (`npx tsx scripts/perf-anilist.ts` vs `PERF=1 go test -run TestPerfAniList -v ./internal/anilist`), 300 titles, 3 runs each:

| Operation                                 | Node         | Go          |
| ----------------------------------------- | ------------ | ----------- |
| Matching job (search, rank, apply, cover) | 9.32-9.41 s  | 8.33-8.47 s |
| Account import preview (300 titles)       | 14.5-25.9 ms | 9.3-10.7 ms |

Matching is not faster in any way that matters: about 28 ms per title in both, because each confirmed match durably rewrites the category file (write + fsync) and appends to the activity log. In real use the 30 requests/minute AniList allows makes the network the bottleneck by two orders of magnitude (one search per 2 s). The preview, which is pure computation, is about 2x faster.

## Differences from the Node implementation, on purpose

- Cancellation uses Go contexts; the HTTP client is an interface (`Doer`) instead of an injected `fetch`. Each attempt has its own 15 s timeout, as before.
- Added safety caps that Node did not have: a downloaded image over 20 MB is refused (`413`), a GraphQL response over 32 MB is cut off.
- Entries Go writes always carry `channel` / `date` (as `null`) on links, like in phase 1; Node left them out. Reading either works.
- A 429 response now carries `details.retryAfterSec`, as the Node route did (the Go `HTTPError` gained a `Details` field for it).
- Review-queue rows are ordered by title with the same ICU-style collation as `localeCompare`.
- AniList itself was **never contacted** while porting or testing (fake world only). The request and response shapes are the same text and fields as the Node client; a first real run with a small library is the one check left for you.

## Known gaps / next

- The Wails shell (window, token bridge, menus, logger, single instance, data-dir lock) is phase 6; packaging, CI on 3 OS, e2e and the size comparison are phase 7.
- OAuth and two-way sync (M8) are not part of this port.

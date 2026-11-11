# Phase 3: search, stats and the HTTP API

Status: **done** (awaiting approval for phase 4).

## What was done

| Package             | Port of                   | Contents                                                                                                                                          |
| ------------------- | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/search`   | `search.ts` (MiniSearch)  | full-text index with MiniSearch's BM25 scoring, prefix and fuzzy matching; entry list query (filters, sort, pagination); text collation           |
| `internal/stats`    | `stats.ts`                | dashboard overview                                                                                                                                |
| `internal/api`      | `api/app.ts`              | router, bearer-token auth, CORS lock, uniform error shape, request ids, static web build, server-sent events, all non-import / non-AniList routes |
| `cmd/server`        | `dev.ts`                  | headless server (`go run ./cmd/server`), graceful shutdown                                                                                        |
| `internal/perfdata` | part of `scripts/perf.ts` | the synthetic 10,000-entry data set, so Go and Node timings use identical data                                                                    |

Routes ported (all under `/api/v1`): `health`, `meta`, `entries` (list, create, restore, bulk, get, patch, delete, progress, move, activity, similar), `search`, `categories` (list, create, reorder, patch, delete), `tags` (list, suggestions, merge, patch), `stats/overview`, `settings`, `scales`, `backups`, `backup/restore`, `events`. Importers/exporters and AniList plug in later through `Options.Extend` (phases 4 and 5).

### Search

MiniSearch has no Go equivalent, so the index is a port of the parts the app uses, with the same options the Node server passes: fields `title / alt / tags / notes` boosted 6/3/2/1, prefix matching, fuzzy matching (about one typo for words of 5-6 letters, two for 7 or more), every query word must match, BM25 with `k=1.2 b=0.7 d=0.5`, prefix weight 0.375 and fuzzy weight 0.45. The default tokenizer is reproduced exactly (including that field length counts _unique raw tokens_). No new dependency.

The index follows the store incrementally: it compares each category's list by identity and, inside changed categories, entries by a content fingerprint. So a hand-edit that keeps `updatedAt` is still picked up.

### HTTP layer

- A small router (about 100 lines) instead of `net/http`'s mux, to keep Fastify's behaviour: static segments beat `:params`, and anything unmatched is a JSON `404 NOT_FOUND` (the stdlib mux would answer 405 or plain text).
- Same error shape `{ "error": { code, message, details } }` with codes `UNAUTHORIZED`, `VALIDATION`, `BAD_REQUEST`, `NOT_FOUND`, `INTERNAL`, plus the store's own (`LIBRARY_MISMATCH`, `CATEGORY_NOT_EMPTY`, `UNPARSEABLE_PROGRESS`, `TAG_EXISTS`).
- `?token=` is accepted only on the same three read-only routes as before (`/events`, `/anilist/cover/*`, `/anilist/image`); constant-time token comparison.
- SSE: `hello`, then `change / external-change / file-error / file-restored / job` with the store version, ping every 25 s, a slow client can never block the store, and `Server.Close()` ends open streams so shutdown never hangs (the same issue the Node server needed `forceCloseConnections` for).
- Request bodies are limited to 1 MB by default (Fastify's default), larger per route for uploads later.

## How parity with Node is proven

1. **Search rankings.** `scripts/gen-go-search-fixtures.ts` runs the _real_ Node `SearchIndex` over an invented corpus of 400 entries and 93 queries (exact words, prefixes, typos, multi-word, punctuation, accents, numbers, nonsense). `TestRankingsMatchMiniSearch` requires the same ranked results with scores equal to 1e-9 and the same matched terms. All 77 queries that have hits match; `fuzzy "cirmson"` finds the same 625 entries in the 10,000-entry perf data.
2. **Incremental updates.** After 9 edits (title change, tag change, deletes, inserts, a move, a non-searchable change) the Go index, updated incrementally, ranks exactly like a freshly built MiniSearch index, and also like a freshly built Go index.
3. **API behaviour.** `api.test.ts` is ported test by test, plus checks for the router, CORS, query validation, SSE, static files and shutdown.
4. **The real UI.** I ran the actual React app (Vite dev server) against `go run ./cmd/server` on a copy of the Node-written fixture folder and clicked through it in the browser pane: library table (categories, scores, favorite star, progress), Tags page (alias, hidden flag), Dashboard (totals, average score, "Continue" shelf). No console errors. Pressing `+` on a title updated `romance.json` and the activity log on disk. This was a manual smoke test, not an automated one; the e2e suite arrives in phase 7.

## Tests

| Package  | Go tests (passing)     | Notes                                                                                                                                                                                            |
| -------- | ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `api`    | 16                     | ported `api.test.ts` (auth, validation, entries CRUD, filters/search, categories, tags, stats, settings/scales/backups, SSE, static) + router quirks, CORS, query validation, close, extend hook |
| `search` | 4                      | rankings vs MiniSearch, incremental updates, hand-edit detection, edge cases                                                                                                                     |
| `stats`  | 3                      | counts, tag ranking and co-occurrence, empty store gives arrays not `null`                                                                                                                       |
| `shared` | 12 (34 with sub-tests) | phase 1, still green                                                                                                                                                                             |
| `store`  | 33                     | phase 2, still green                                                                                                                                                                             |

`go test -race ./...` is clean. One regression found by writing the stats test and fixed earlier in the TypeScript dashboard code is now locked in for Go: "Needs a decision" lists only on-hold titles (dropped ones were wrongly included).

## Timing vs Node (same machine, warm caches)

Test files:

| Measure               | Node                          | Go                            |
| --------------------- | ----------------------------- | ----------------------------- |
| `api.test.ts` / `api` | 10 tests: 4.12s, 2.25s, 2.05s | 16 tests: 2.25s, 2.28s, 2.73s |
| search tests          | 2 (inside `storage.test.ts`)  | 4 tests: 1.2-1.5s             |
| stats tests           | none                          | 3 tests: 0.6-1.2s             |

`scripts/perf.ts` rows vs the Go port (`PERF=1 go test -run TestPerfSearchAndStats -v ./internal/stats`), 10,000 entries, p50:

| Operation                              | Node (3 runs)          | Go (6 runs)            |
| -------------------------------------- | ---------------------- | ---------------------- |
| First search (builds the index)        | 173-227 ms             | 60-155 ms              |
| Search "crimson tower" (warm)          | 1.5-1.9 ms             | 1.3-3.2 ms             |
| Fuzzy "cirmson" (warm)                 | 1.6-2.0 ms             | 0.9-1.4 ms             |
| Prefix "wand" (warm)                   | 1.4-1.7 ms             | 0.9-1.4 ms             |
| List sorted by title, page 1           | 1.6-1.7 ms (p95 29-30) | 0.1-0.2 ms (p95 26-40) |
| List, 2 tags + status filter           | 0.6-0.8 ms             | 0.7-1.0 ms             |
| Stats overview                         | 23-39 ms               | 6-13 ms                |
| PATCH one entry (persist the big file) | 128-140 ms             | 53-63 ms               |
| Search right after a write             | 1.2-2.4 ms             | 1.4-2.6 ms             |

Go is clearly faster for building the index, stats and writes. Warm multi-word search is about on par (sometimes a little slower than Node's trie-based lookups; everything is far below the 50 ms target). The p95 of a sorted list is the cost of re-sorting 10,000 titles after a change, and is similar in both.

Two performance bugs were found and fixed on the way by the perf run: sorting ~400-byte entries by value (now sorts pointers: stats 54 ms to 9 ms) and an unbounded edit-distance in fuzzy search (now bounded).

## Differences from the Node server, on purpose

- **Validation details**: `error.details` is `[{ "path": [...], "message": "..." }]`, not Zod's full issue objects. The web app only reads `code` and `message`.
- **Tie order** in search results with exactly equal scores is by insertion order, not MiniSearch's internal map order.
- **Exact BM25 statistics after deletes.** MiniSearch discards documents lazily, so an incrementally edited _Node_ index keeps slightly stale term statistics until it is vacuumed (scores differ in the 3rd decimal, rank order can swap between near-equal results). The Go index recomputes them, so it always equals a fresh build.
- **Lenient content type**: bodies are parsed as JSON whatever `Content-Type` says (Fastify answered 415).
- Empty body on routes that need one is `400 VALIDATION` ("Required"); Fastify's wording for a malformed body differs, the status and code do not.
- `Server.Close()` must be called before `http.Server.Shutdown` (the headless server does).

## Known gaps / next

- Import/export routes (`/import/*`, `/export/*`, `/imports/:id/report`) are phase 4; AniList routes are phase 5. Both are missing from the Go server for now, so those two screens of the UI will error against it.
- Search ranking for ties and the lazily vacuumed Node behaviour are documented above; if you want bit-for-bit Node ranking after deletes, say so (it would mean copying MiniSearch's lazy-discard quirk, which I advise against).

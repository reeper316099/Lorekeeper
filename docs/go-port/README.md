# Go + Wails port

Rewrite of the Node/Electron backend and desktop shell in Go + Wails v2. The React UI (`apps/web`) is kept as is.

Goals: much smaller installer and lower memory, **no change in behaviour or on-disk data**. Every phase ends with a summary and waits for approval before the next one starts. Each phase gets a page in this folder recording what was done and which tests pass, with timings next to the Node numbers ([BASELINE.md](BASELINE.md)).

## Decisions

- **Keep the localhost HTTP API** (`/api/v1`, per-launch bearer token, SSE `/events`). The Go server exposes the same routes, so the web app needs almost no changes. Wails hosts the UI and hands it the base URL and token, like the Electron preload does today.
- **Wails v2** (stable), not v3 (alpha).
- **On-disk format is frozen.** A data folder written by the Node app must open in the Go app and vice versa (same stable 2-space JSON, same `meta.schemaVersion`, same backups layout). This is the main acceptance test for every storage phase.
- **Node code stays in the repo until parity is proven** (final phase), so each phase can be checked against the original.
- Work happens on the `go-port` branch. Go code lives in `go/` (module `lorekeeper`); `docs/go-port/` holds these pages.
- Libraries: `excelize` (xlsx), `encoding/csv` + charset detection (csv), `fsnotify` (watcher), `archive/zip` (backups), a custom index that ports MiniSearch's scoring (decided in phase 3), stdlib `net/http` with a small custom router (to keep Fastify's 404 behaviour).
- Same rules as before: public repo, invented fixtures only, no committed .xlsx, you do the `git push`.

## Layout

```
go/
  go.mod
  internal/shared/    schemas, constants, parsers      (packages/shared)
  internal/store/     atomic writes, mutex, store, backups, migrations
  internal/search/    index + stats
  internal/api/       routes, auth, SSE
  internal/importexport/  import/export (xlsx, csv, json) + routes
  internal/anilist/   client, cache, matcher, queue, jobs
  cmd/server/         headless server (dev, tests, e2e)
  cmd/lorekeeper/     Wails desktop app (embeds apps/web/dist)
```

## Phases

| #   | Phase              | Scope                                                                                                                          | Done when                                                                               |
| --- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------- |
| 0   | Plan + baseline    | This page, Node timings                                                                                                        | You approve the plan                                                                    |
| 1   | Scaffold + shared  | Go module, CI job, Zod schemas -> structs + validation, constants, parsers (progress, links, tags, titles, aliases)            | Ported `parse.test.ts` cases pass; JSON round-trips identical to Node                   |
| 2   | Storage engine     | Atomic write, keyed mutex, coalesced writes, Store, backups (zip), migrations, corruption quarantine, watcher                  | Ported storage tests pass; Go opens Node data dir and Node opens Go's                   |
| 3   | Search, stats, API | Search index, stats, all non-import/non-AniList routes, bearer auth, CORS lock, SSE, error shape                               | Ported `api.test.ts` passes; perf script numbers within range                           |
| 4   | Import / export    | XLSX/CSV import (preview, commit, undo, strategies), XLSX/CSV/JSON export                                                      | Ported `import.test.ts` passes on synthetic fixtures; round-trip import->export->import |
| 5   | AniList            | GraphQL client, rate limiter, cache, covers, matcher, review queue, jobs, routes                                               | Ported AniList tests pass (mock server)                                                 |
| 6   | Wails shell        | Window, embedded UI, token bridge, menus, logger, single-instance + data-dir lock, data-dir resolution, open-external, dialogs | App runs on macOS and the web UI works end to end                                       |
| 7   | Packaging, CI, e2e | `wails build` matrix (mac/win/linux), installers, e2e + axe against the Go server, size/memory comparison, cutover decision    | CI green on 3 OS; size table filled in; you decide whether to remove the Node code      |

## Progress

- Phase 0: done ([BASELINE.md](BASELINE.md))
- Phase 1: done ([phase-1.md](phase-1.md))
- Phase 2: done ([phase-2.md](phase-2.md))
- Phase 3: done ([phase-3.md](phase-3.md))
- Phase 4: done ([phase-4.md](phase-4.md))

## Per-phase doc template

Each `phase-N.md` has: what was done, files added, tests (name, count, pass/fail), timings vs Node (same test area, same machine), known gaps, and anything that differs from the Node behaviour on purpose.

## Risks

- Excel round-trip fidelity (styles, validations, freeze panes) is the fiddliest part of phase 4.
- System webview differences (WebKit/WebView2): the UI uses `color-mix` and `oklab`; checked in phase 6.
- Playwright cannot drive a Wails window like it drives Electron, so e2e/a11y move to a browser against the Go server (phase 7).
- Search ranking will not be byte-identical to MiniSearch; tests assert the same orderings that matter (title over tags over notes, fuzzy and prefix hits), not scores.

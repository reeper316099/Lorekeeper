# Contributing / developer notes

## Layout

```
apps/web        React + Vite UI (TanStack Query for server state, Zustand for UI state)
packages/shared Zod schemas and pure parsers used by the UI (progress, links, tags, titles, status, score)
go/             the backend and the desktop app (one Go module)
  cmd/lorekeeper  Wails desktop shell (build tag `desktop`); cmd/server is the headless API for dev and e2e
  internal/       shared (schemas, parsers), store (JSON storage, backups, migrations), api, search, stats,
                  importexport (XLSX/CSV), anilist, desktop (window-free shell core: paths, lock, bridge, menu)
e2e/            Playwright tests that drive the UI against the Go API server
scripts/        synthetic fixture generator, spreadsheet guard
docs/           architecture, data format, AniList, distribution, and docs/go-port (how the Go port was done)
```

The product name lives in one constant: `AppName` in `go/internal/shared/constants.go` (and `APP_NAME` in
`packages/shared/src/constants.ts` for the UI).

## Working on it

```bash
npm install
npm run dev            # browser UI + Go API with a throwaway data folder (~/.lorekeeper-dev-go)
npm run verify         # what CI runs: lint, typecheck, TS tests, Go tests, e2e
```

- The UI only talks to `/api/v1`. Every body is validated; every route except static files needs the bearer token.
- Storage writes are atomic and coalesced; the in-memory store is the source of truth at runtime. Never write the JSON files
  directly from other code: go through `store.Store`. JSON structs declare their fields in sorted key order (a test
  enforces it) so output stays byte-identical to what earlier versions wrote.
- Parsers belong in `go/internal/shared` (and `packages/shared` for the UI) with tests. When ambiguous input is parsed,
  keep the raw text and add the parsed value beside it.
- Changing the on-disk format: bump the schema version, add an ordered migration in `go/internal/store/migrations.go` and
  test it. A backup is taken automatically before a migration runs.
- Golden files in `go/internal/*/testdata` were recorded from the original TypeScript backend (see `docs/go-port`). They are
  now fixed expectations: change them only on purpose, when behaviour is meant to change.
- UI changes must keep the accessibility e2e green in both themes (`e2e/a11y.spec.ts`).
- Desktop changes: run the self-test (`LOREKEEPER_SELFTEST=1 ./release-go/lorekeeper`); it needs the installed app closed.

## Privacy rule (important: this repo is public)

Never commit personal data. Spreadsheets and CSVs are rejected outside `fixtures/synthetic/`, generated workbooks are not
committed at all, and tests/fixtures/screenshots must use **invented** titles, tags and notes. Don't reuse strings from real
libraries, even as examples. Tests that need real files go in `private-fixtures/` (gitignored) and must skip when it is absent.

## Pull requests

Keep changes focused, add tests, run `npm run verify`, and describe user-visible changes.

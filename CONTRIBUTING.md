# Contributing / developer notes

## Layout

```
apps/web        React + Vite UI (TanStack Query for server state, Zustand for UI state)
apps/desktop    Electron main + sandboxed preload, bundling (esbuild) and electron-builder config
packages/server Fastify API, storage engine, importers/exporters, AniList client
packages/shared Zod schemas and pure parsers (progress, links, tags, titles, status, score)
e2e/            Playwright tests that drive the built Electron app
scripts/        fixture generator, perf check, spreadsheet guard
docs/           architecture, data format, AniList, distribution
```

The product name lives in one constant, `APP_NAME` in `packages/shared/src/constants.ts`.

## Working on it

```bash
npm install
npm run dev            # browser UI + API with a throwaway data folder (~/.lorekeeper-dev)
npm run verify         # what CI runs: lint, typecheck, unit/API tests, Electron e2e
```

- The UI only talks to `/api/v1`. Every body is validated with Zod; every route except static files needs the bearer token.
- Storage writes are atomic and coalesced; the in-memory store is the source of truth at runtime. Never write the JSON files
  directly from server code: go through `Store`.
- Parsers belong in `packages/shared` with tests. When ambiguous input is parsed, keep the raw text and add the parsed value
  beside it.
- Changing the on-disk format: bump `SCHEMA_VERSION`, add an ordered migration in `packages/server/src/storage/migrations.ts`
  and test it. A backup is taken automatically before a migration runs.
- UI changes must keep the accessibility e2e green in both themes (`e2e/a11y.spec.ts`).

## Privacy rule (important: this repo is public)

Never commit personal data. Spreadsheets and CSVs are rejected outside `fixtures/synthetic/`, generated workbooks are not
committed at all, and tests/fixtures/screenshots must use **invented** titles, tags and notes. Don't reuse strings from real
libraries, even as examples. Tests that need real files go in `private-fixtures/` (gitignored) and must skip when it is absent.

## Pull requests

Keep changes focused, add tests, run `npm run verify`, and describe user-visible changes.

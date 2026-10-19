# Architecture

```
Electron main ──spawns──▶ Fastify API (127.0.0.1:ephemeral, bearer token) ──▶ Store (in-memory truth) ──▶ JSON files
      │ preload (contextBridge: token + base URL)                      ▲ chokidar reloads hand edits, SSE /events
      └──▶ React renderer (talks only to /api/v1)
```

- `packages/shared`: Zod schemas, constants, pure parsers (progress, links, tags, titles, status, score).
- `packages/server`: `storage/` (atomic IO, backups, migrations, `Store`), `search.ts` (MiniSearch + filters),
  `stats.ts`, `api/app.ts` (routes), `io/` (spreadsheet reading, import planner/commit/undo, exporters).
- `apps/web`: React UI (TanStack Query for server state, Zustand for UI state), talks only to `/api/v1`.
- `apps/desktop`: Electron main (starts the Store + API on an ephemeral port, single-instance lock, window state, native menu), sandboxed preload bridge, esbuild bundle and electron-builder config. See `docs/DISTRIBUTION.md`.

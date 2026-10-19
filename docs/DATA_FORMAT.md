# Data format

All data lives in the data directory (`LOREKEEPER_DATA_DIR`, else the OS app-data folder). Everything is plain,
2-space-indented JSON with sorted keys, written atomically (tmp file → fsync → rename).

```
meta.json                 { schemaVersion, appVersion, createdAt, lastOpenedAt }
config/settings.json      theme, accent, density, hiddenTags, labelOverrides, backup policy
config/categories.json    [{ id, library, name, slug, color, order }]
config/tags.json          [{ id, name, aliases[], color?, hidden? }]  (aliases auto-resolve on import)
config/scales.json        status labels + 10 score labels per library (also used for XLSX dropdowns)
data/<library>/<slug>.json   Entry[] for one category (library = reading | watching)
activity/YYYY-MM.jsonl    append-only progress/status log (feeds the heatmap)
imports/<id>/             source copy, snapshot/, manifest.json, import-report.json (enables Undo)
backups/                  backup-YYYYMMDD-HHmmss.zip (config + data + activity), backups/corrupt/
exports/  logs/
```

## Entry

See `packages/shared/src/schemas.ts` (`EntrySchema`). Ambiguous spreadsheet cells keep their text in
`current.raw` / `consumedUpTo.raw`; `number`, `season` and `kind` are best-effort parses next to it.
`links[].raw` keeps the original cell text; `slot` records which column it came from.

## Hand-editing safely

The app watches `config/` and `data/`. Edit while it runs and it reloads the file; if your edit is invalid JSON or
fails validation it keeps the previous in-memory data and reports the file and line. Keep `id`, `createdAt` and
`categoryId` intact. A corrupt file at startup is copied to `backups/corrupt/` and restored from the newest backup.

## Import rules (summary)

Dedupe key is `library + normalized title` (NFKC, no punctuation/articles). Strategies: `skip`, `fill` (default:
only empty fields), `overwrite`, `keep_both`. Fuzzy near-duplicates are warnings only. Blank statuses import as
`planned` with a warning. Tags are only trimmed and case-deduplicated; merges are suggested, never automatic.

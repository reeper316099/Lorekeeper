# Phase 4: import / export

Status: **done** (awaiting approval for phase 5).

## What was done

`go/internal/importexport`, a port of `packages/server/src/io/*` (the package is named `importexport`, not `xlsxio` as the plan said, because it also does CSV and JSON):

| File          | Port of       | Contents                                                                                                                                                                            |
| ------------- | ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `columns.go`  | `columns.ts`  | header matching, library detection, export layouts                                                                                                                                  |
| `read.go`     | `read.ts`     | XLSX reader (`excelize`), CSV reader (own PapaParse-compatible parser), Windows-1252 fallback, tab colors, date cells                                                               |
| `importer.go` | `importer.ts` | plan (preview), commit with snapshot + backup, undo, strategies skip/fill/overwrite/keep_both, near-duplicate warnings, report                                                      |
| `export.go`   | `export.ts`   | XLSX in the original layout (banner, frozen header, shaded rows, dropdowns, tab colors), CSV with BOM, CSV template, JSON zip                                                       |
| `routes.go`   | `routes.ts`   | `/import/upload` (multipart), `/import/preview`, `/import/commit`, `/import/undo/:id`, `/export/xlsx`, `/export/csv`, `/export/csv-template`, `/export/json`, `/imports/:id/report` |

Also: `api.Download` (a handler result sent as a file) and `Options.Extend` wiring in `cmd/server`.

### Libraries

- **XLSX**: `github.com/xuri/excelize/v2` instead of ExcelJS. It reads rows lazily, so the Node workaround `stripDataValidations` (ExcelJS expands whole-column dropdowns into millions of objects) is not needed.
- **CSV**: a small parser in `read.go` that follows PapaParse where it matters: it guesses the line ending (`\r\n`, `\n`, `\r`) ignoring quoted text, guesses `,` `;` or tab by field-count consistency over the first 10 rows, keeps quoted fields byte for byte (including `\r\n` inside quotes, which Go's `encoding/csv` rewrites), and leaves a trailing empty row. Export quoting follows PapaParse's rules (quote on `"`, comma, CR, LF, BOM, or leading/trailing space).

## How parity with Node is proven

1. **Node importer vs Go importer.** `scripts/gen-go-import-fixtures.ts` runs the real Node importer over 13 scenarios and records the preview, the committed report (counts, summary, warnings, tag suggestions), the resulting entries and categories, and the CSV export: both synthetic workbooks (created by ExcelJS), both together, re-import with edits, all four strategies, three CSV samples (comma, semicolon, Windows-1252), a CSV with duplicates / unknown statuses / bad scores / blank rows / quoted newlines, a status map and forced category, a missing library, BOM + category column + stray and unknown columns, and a tab-separated file. `TestImporterMatchesNode` requires identical results, key for key, including spreadsheet row numbers in `importRef`, warning order and the CSV export bytes.
2. **Go exports read by Node.** `INTEROP_OUT=/tmp/x go test -run TestWriteInteropExports ./internal/importexport`, then `npx tsx scripts/check-go-exports.ts /tmp/x`: the Node importer (ExcelJS) reads the Go-written XLSX and CSV for both libraries, gets identical entries, and the workbook has the banner, frozen header, dropdowns and tab color. Result: `Node read the Go-written XLSX and CSV exports cleanly.` (This check found a real bug: excelize writes `docProps/core.xml` in a non-standard form that Excel accepts but ExcelJS rejects. The exporter now rewrites it in the standard form; a test guards it.)
3. **Your real workbooks.** `TestPrivateFixturesLocalOnly` (skipped when `private-fixtures/` is absent, prints counts only) imports the two real workbooks with the Go importer: the documented counts (66 / 48 entries, categories, raw statuses, 36 vs 35 and 14 vs 14 tags) match, and re-importing is idempotent.
4. **Over real HTTP.** With the built `cmd/server` and `curl`: upload `Reading_Tracker.xlsx`, preview (66 creates; Romance 32, Action 33, Fantasy 1), commit (5 categories created), download the XLSX export (42 KB, correct headers), dashboard totals show 66 entries, 9 completed.

## Tests

`internal/importexport`: 31 tests plus 15 scenario sub-tests, all passing (2 more run only on request: perf and the interop writer). Ported from the 17 tests of `import.test.ts` (synthetic workbooks, raw progress text and links, scores across label sets, idempotent re-import, strategies, undo including persistence, preview does not write, XLSX round trip, XLSX structure, CSV BOM/quotes/semicolon/Windows-1252, stray and unknown columns, missing library, the import API, private fixtures), plus: PapaParse edge cases (line endings, quotes, ragged rows, delimiter guess), CSV quoting, header matching, options validation, sheet-name sanitizing, date cells, error cells, file-name sanitizing, API error codes (no file, wrong type, not multipart, bad ids), CSV export across pages of 1,000, and the JSON export. `go test -race ./...` passes for the whole module.

The import tests need the generated workbooks (never committed): `npm run fixtures:generate`. Locally a missing workbook skips the test with that hint; in CI the Go job installs Node, generates them and sets `REQUIRE_FIXTURES=1` so a missing file fails.

## Timing vs Node (same machine, warm caches)

Test files (note the Node file includes its private-fixtures test, 1.9 s, because this machine has the real workbooks; the Go suite does too, 1.6 s):

| Measure           | Node (`import.test.ts`) | Go (`importexport`)          |
| ----------------- | ----------------------- | ---------------------------- |
| Tests             | 17                      | 31 (+ 15 scenario sub-tests) |
| Wall time, 3 runs | 8.34 s, 7.70 s, 7.82 s  | 6.45 s, 6.28 s, 5.80 s       |

Operations (`npx tsx scripts/perf-import.ts` vs `PERF=1 go test -run TestPerfImport -v ./internal/importexport`), p50 of 2 runs each:

| Operation                                      | Node         | Go           |
| ---------------------------------------------- | ------------ | ------------ |
| Read the reading workbook (66 rows, 6 sheets)  | 6.5-7.5 ms   | 3.6-4.5 ms   |
| Plan the reading import                        | 38.7-39.8 ms | 12.9-13.1 ms |
| Commit the reading import (stage, plan, write) | 203-204 ms   | 134-139 ms   |
| Export the reading XLSX (5 sheets)             | 39.5-43.8 ms | 14.4-14.9 ms |
| Export the reading CSV                         | 0.3-0.4 ms   | 0.1 ms       |
| Read a 2,000-row workbook                      | 30-31 ms     | 36-37 ms     |
| Import a 2,000-row workbook                    | 6.5 s        | 1.5-1.6 s    |
| Read a 10,000-row workbook                     | 123-127 ms   | 176-184 ms   |
| Import a 10,000-row workbook                   | 13.8-14.2 s  | 3.5 s        |

Go is 2.5-4x faster for planning, exporting and whole imports. Reading very large sheets is slightly _slower_ than ExcelJS (excelize's row iterator); at 10,000 rows that is under 0.2 s and not the bottleneck. Both importers are quadratic on the near-duplicate scan (capped at 3,000 existing titles per library); the Go one normalizes each title once.

## Differences from the Node importer, on purpose

- **Dates** in cells: read through the cell's number format and written as `YYYY-MM-DD` (Node got JavaScript `Date` objects and wrote the same). Spreadsheet error cells (`#N/A`) read as empty.
- **Sparse cells**: on XLSX sheets an empty cell never claims a field, so a second column with the same meaning can still fill it (ExcelJS left empty cells undefined; CSV cells are always present). Reproduced explicitly with a `Sparse` flag.
- **XLSX files are not byte-identical** to ExcelJS output, but equivalent: same layout, banner in the category color with readable text, frozen header, alternating shading, column widths, status and score dropdowns, tab colors. The inline dropdown list is skipped if it would exceed Excel's 255-character limit (Node would have written a file Excel repairs).
- **Upload errors**: not multipart is `406 BAD_REQUEST`, over 50 MB is `413`, as Fastify's multipart plugin answered.
- Report and manifest files (`import-report.json`, `manifest.json`) are written with sorted keys like every other file; `MergeSuggestion` fields were reordered for that.
- The HTTP router now matches on the _escaped_ path, so `%2F` inside an id stays inside the id (the Node route had this behaviour; a test for `/imports/..%2F..%2Fx/report` caught the difference).

## Known gaps / next

- AniList routes (and `BeginUndoable`, already ported for the AniList list import) are phase 5, so that screen still errors against the Go server.
- The new CI steps (Node + fixtures inside the Go job) have not run on GitHub yet.
- The real workbooks test only runs on this machine.

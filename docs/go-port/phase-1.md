# Phase 1: scaffold + shared package

Status: **done** (awaiting approval for phase 2).

## What was done

- `go/` module `github.com/reeper316099/Lorekeeper/go` (Go 1.27). Only dependency: `golang.org/x/text` (Unicode NFKC/NFKD/NFD, which Go's stdlib lacks).
- `go/internal/shared`, a port of `packages/shared`:
  - `constants.go`: libraries, statuses, media types, score label sets, seed categories, palette.
  - `schemas.go`: Entry, Category, Tag, Scales, Settings, Meta, ActivityRecord, Progress, Link as structs. Decoding applies Zod's defaults and validates (`*ValidationError` with the field path); `ParseEntryFile` / `ParseCategoriesFile` / `ParseTagsFile` load whole data files.
  - `scales.go`, `color.go`, `slug.go`, `text.go` (Levenshtein, Damerau).
  - Parsers: progress, links, tags (+ merge suggestions), titles (normalize, similarity, near-duplicates), status, score, sources.
- `jsutil.go`: JavaScript-compatible `\s`, `trim()` and `String()` helpers. Go's `\s` and `TrimSpace` differ from JS (no `\v`, NBSP and BOM handling), and spreadsheet cells often contain NBSPs.
- CI: new `go` job (gofmt on Linux, `go vet`, `go test`) on Ubuntu, Windows and macOS.

## How parity with Node is proven

`scripts/gen-go-parity.ts` runs the **TypeScript** implementation over a corpus of invented inputs and writes the results to `go/internal/shared/testdata/`:

- `parity.json`: 1,401 input/output pairs across 18 functions (progress, links, tags, sources, status, score, titles, slugs, colours, edit distances, merge suggestions). Includes empty cells, NBSP/BOM/full-width characters, accents, numbers as numbers, and odd progress strings.
- `schemas.json`: 37 documents (14 valid, 23 invalid) with Zod's verdict and parsed output for every schema.

The Go tests must give the same JSON, and must reject the same documents. I checked the corpus has teeth by temporarily changing a threshold in the tag-merge code: the parity test failed (and the first corpus did not catch it, so cases were added). Re-run `npx tsx scripts/gen-go-parity.ts` after changing a TS parser, then `go test ./...`.

## Tests

| Go test                                                                                                       | What it covers                                          |
| ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| `TestParityWithTypeScript` (18 sub-tests)                                                                     | all parser outputs equal TypeScript's                   |
| `TestSchemasMatchZod`                                                                                         | accept/reject and parsed output equal Zod's             |
| `TestParseProgress`, `TestParseLink`, `TestTags`, `TestTitleNormalizer`, `TestStatusScoreSources`, `TestMisc` | the 15 cases of `parse.test.ts`, ported                 |
| `TestValidationErrorNamesField`, `TestFileHelpers`, `TestDefaultsAreFresh`                                    | error paths, whole-file loading, no shared default maps |

33 tests including sub-tests, 33 passing.

## Timing vs Node (same machine, warm caches)

| Measure                             | Node (Vitest, `packages/shared`)        | Go (`go test`)                         |
| ----------------------------------- | --------------------------------------- | -------------------------------------- |
| Tests                               | 15                                      | 33 (+ ~1,900 parity/schema cases)      |
| Test run itself                     | 0.04s (`parse.test.ts` in the full run) | 0.26-0.33s (reported by `go test`)     |
| Wall clock, command start to finish | 0.41-0.44s (first run 2.2s)             | 1.4-1.5s (first run, cold build, 1.7s) |

Read this fairly: the Go run is not faster here. Node's parser tests are tiny, and Go's number includes linking a fresh test binary on every run and a much bigger test set (parity + schemas). The speed difference will matter in phases 2 to 4 (storage, import), where the Node suite spends 5s and 22s.

## Differences from the TypeScript on purpose

- **Lengths count runes, not UTF-16 units**, in edit distance and title similarity. Only differs for characters outside the BMP (emoji); Go's answer is the more correct one. Kept `jsLen` where a tie-break depends on JS `length`.
- **`null` vs missing** for non-pointer fields: `"tags": null` decodes to an empty list (Zod rejects it); absent required strings decode as `""` (Zod rejects them). Real data files never contain these.
- **Optional + nullable link fields** (`channel`, `date`) are always written, as `null` when empty. Zod keeps them absent. Reading either form works.
- `SuggestTagMerges` takes a slice (`[]TagUsage`) instead of a map so results are deterministic.
- Optional string fields (`icon`, `startedAt`, ...) that are present but `""` are kept as `""`.

## Known gaps / next

- JSON output ordering (the "stable stringify" with sorted keys) is a storage concern and lands in phase 2 with a test that compares file bytes to what Node writes.
- `go.sum` is committed; run `go mod tidy` after adding dependencies.

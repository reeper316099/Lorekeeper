# Cutover: the Go app is the only build

After phase 7 the Go build was used next to the Electron one on the same data folder. It turned up three problems, all fixed
before this step (full screen unavailable on macOS, cover images not displaying, and AniList searches with no country
filter finding nothing: AniList treats `countryOfOrigin: null` as a filter, so the argument is now left out; the Node client had
the same bug). With those fixed and confirmed by hand, the Electron/Node code was removed.

## Removed

| Path                                                      | Was                                                        |
| --------------------------------------------------------- | ---------------------------------------------------------- |
| `apps/desktop`                                            | Electron main, preload, bundling, electron-builder config  |
| `packages/server`                                         | Fastify API, storage, search, importers, AniList (Node)    |
| `.github/workflows/release.yml`                           | the Electron release workflow (`v*` tags)                  |
| `scripts/gen-go-*.ts`, `scripts/check-go-*.ts`            | recorded the Node outputs / checked Node against Go data   |
| `scripts/perf*.ts`                                        | the Node timing runs (the Go equivalents are `TestPerf*`)  |
| `e2e/smoke.spec.ts` (Electron)                            | replaced by the Go journey spec, now named `smoke.spec.ts` |
| CI jobs `e2e` (Electron) and the Electron steps of `test` | `test` became `web` (lint, types, UI unit tests)           |

## Kept, and what changed

- `apps/web` and `packages/shared`: the React UI and its shared parsers.
- `scripts/generate-fixtures.ts` (its `exceljs`/`iconv-lite` dependencies moved to the root `package.json`): the Go import tests read the
  generated sample workbooks.
- `go/internal/*/testdata`: the golden files recorded from the Node implementation. They can no longer be regenerated and are
  now fixed expectations; they prove the Go code still behaves like the original.
- Playwright (journey, WCAG 2 AA, links) now has one backend: the Go API server in Chromium.
- The version comes from the root `package.json`; release tags are `go-v<version>`.
- npm scripts: `dev` runs the Go API with the web UI, `build:desktop` builds the Wails app, `go:test` runs the Go tests,
  `verify` runs everything.

## Checked after the removal

- `npm run lint`, `npm run typecheck`, `npm test` (18 tests), `prettier --check`: clean.
- `go vet ./...` and `go test ./...` with the generated fixtures required: all packages pass.
- `npm run e2e`: 5 passed (accessibility in both themes, keyboard, links, journey), screenshots spec skipped as usual.
- The built desktop app's self-test inside the real WebKit webview: all 8 checks pass.

The old Electron releases (`v0.1.x`/`v1.x` tags and installers) are history only; the previous code is still in git history.

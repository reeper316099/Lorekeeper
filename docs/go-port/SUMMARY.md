# Go + Wails port: summary and cutover decision

The Go port is functionally complete and verified against the Node implementation. Nothing from the Node build has been removed.

## What exists

| Area                    | Go                                   | Proven against Node by                                                                                          |
| ----------------------- | ------------------------------------ | --------------------------------------------------------------------------------------------------------------- |
| Schemas and parsers     | `internal/shared`                    | 1,401 recorded input/output pairs and 37 schema documents from the TypeScript                                   |
| Storage engine          | `internal/store`                     | Node's data folder opens and re-saves byte for byte; Go's folder (with zip backup) opens and restores in Node   |
| Search, stats, HTTP API | `internal/search`, `stats`, `api`    | MiniSearch rankings for 77 queries incl. after edits; ported API tests; the real React UI against the Go server |
| Import / export         | `internal/importexport`              | 13 import scenarios incl. ExcelJS workbooks and CSV quirks; Node reads Go's XLSX/CSV; your two real workbooks   |
| AniList                 | `internal/anilist`                   | 18 recorded service steps incl. cache file names; Node reads Go's queue and cache offline                       |
| Desktop shell           | `internal/desktop`, `cmd/lorekeeper` | self-test inside the real WebKit webview; 18 tests                                                              |
| Packaging, CI, e2e      | `go/*.sh`, workflows, `e2e/`         | Playwright journey, WCAG 2 AA audit and links on the Go server                                                  |

Every phase has its own page with the numbers: [phase-1](phase-1.md), [2](phase-2.md), [3](phase-3.md), [4](phase-4.md), [5](phase-5.md), [6](phase-6.md), [7](phase-7.md).

## Gains (macOS arm64, same machine)

- Installer 130 MB to 8 MB; installed app 296 MB to 18 MB; memory about 500 MB to about 240 MB; API ready in 0.22 s instead of 0.38 s.
- Heavy operations are 2-4x faster (storage open, writes, imports, stats, index build); warm search is on par.
- Your existing data folder and `window-state.json` are used as they are; the two builds refuse to run on the same folder at the same time.

## Costs and risks

- **Nobody has run it on Windows or Linux yet.** CI is written (`go-desktop`, `e2e-go`, `go-release.yml`) but has not run on GitHub.
- The window and its menus were not looked at by eye, only exercised by the self-test and by tests (phase 6 lists what to try by hand).
- WebKit (macOS, Linux) or WebView2 (Windows) renders the UI instead of one pinned Chromium, so small CSS differences are possible. The UI was checked in WebKit (self-test) and Chromium (Playwright) but not by a person.
- Building needs CGO and, on Linux, the GTK3/WebKit2GTK packages; no AppImage.
- The AniList integration has never talked to the real AniList from Go (the Node code has).
- Two behaviours are intentionally different: the macOS menu keeps Settings in the File menu and there are no zoom items; the window position is saved on close, not while moving.

## Suggested way to finish

1. Push the branch and let the three-OS CI run; fix whatever Windows/Linux turn up.
2. Use the Go build for a week next to the Electron one (same data folder, never at the same time), including one real AniList match run.
3. Then cut over: merge `go-port`, make `go-release.yml` the release path, and delete `apps/desktop`, `packages/server`, the Node scripts and the Electron CI jobs in a separate commit. Until then both stay.

The decision is yours; I have not removed any Node code.

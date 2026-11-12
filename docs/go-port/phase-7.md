# Phase 7: packaging, CI, end-to-end tests, comparison

Status: **done** (this is the last phase; the cutover decision is yours, see [SUMMARY.md](SUMMARY.md)).

## What was done

### Packaging (Wails CLI v2.17.1)

- `go/cmd/lorekeeper/wails.json` and `build/` (icon from the Electron build, macOS `Info.plist` with the same bundle id `app.lorekeeper.desktop` and minimum macOS 13, the Windows NSIS installer template, Linux `.desktop` file and `nfpm.yaml`).
- `go/package-desktop.sh`: macOS produces `Lorekeeper.app` and a dmg; Linux produces a tar.gz and, with `nfpm` installed, `.deb` and `.rpm`; Windows runs `wails build -nsis` in the workflow.
- `go/build-desktop.sh` (plain `go build`, used by CI and for quick local runs) and `go/prepare-frontend.sh`.
- New workflow `.github/workflows/go-release.yml` (`go-v*` tags or manual): macOS dmg, Windows installer plus portable exe, Linux tar.gz/deb/rpm. Like the Electron release workflow it only attaches files to a release on a tag push, and manual runs only upload artifacts. Nothing is signed.

### CI (`.github/workflows/ci.yml`)

- `go-desktop` (Linux, Windows, macOS): installs the webview toolchain (GTK3 + WebKit2GTK 4.1 + xvfb on Linux), builds the desktop binary, and runs the **self-test inside the real webview** (see phase 6).
- `e2e-go` (3 OS): the Playwright suite against the Go server in Chromium.
- The earlier `go` job (vet, gofmt, tests with generated fixtures) is unchanged.
- **None of these have run on GitHub yet.** I could only run them on this Mac, so the Windows and Linux jobs are written carefully but unproven.

### End-to-end and accessibility tests

The existing Playwright specs now run against either backend (`E2E_BACKEND=go`), through `e2e/helpers.ts`:

- The Go backend starts `go/cmd/server` (the same API the desktop shell embeds) serving the built web UI, drives it in Chromium, and provides the same `window.lorekeeper` bridge the shell does.
- `e2e/go-smoke.spec.ts` is the Go version of the user journey (empty library, import a real workbook through the wizard, search, filter by tag, edit progress, export, **restart the server and find the data**). The Electron-only parts of `smoke.spec.ts` (app identity, renderer isolation, single instance) stay Electron-only.
- `e2e/a11y.spec.ts` (WCAG 2 A/AA with axe on every screen in dark and light, keyboard focus trapping, skip link, reduced motion) and `e2e/links.spec.ts` run unchanged on both backends.
- A flake found on the way: one run reported a contrast failure on the AniList heading, measured while the entrance animation was still fading. The audit now waits for running animations to finish; 3 further full runs and the Electron run were clean.

### Results (macOS)

| Suite                                           | Electron backend | Go backend                         |
| ----------------------------------------------- | ---------------- | ---------------------------------- |
| All Playwright specs (5 run, 2 skipped)         | 5 passed, 32.1 s | 5 passed, 23.5 s                   |
| a11y spec alone (3 tests, 3 runs on Go)         | 1 run: passed    | 19.4 s, 24.5 s, 19.0 s, all passed |
| Go-only journey (import, search, edit, restart) | not applicable   | passed (5.2 s)                     |

## Size, memory and startup

From [BASELINE.md](BASELINE.md), macOS arm64, same machine, unsigned:

| Metric                       | Electron   | Go + Wails       | Ratio         |
| ---------------------------- | ---------- | ---------------- | ------------- |
| Installer (dmg)              | 130.3 MB   | 8.1 MB           | 16x smaller   |
| Installed app                | 296 MB     | 17.9 MB          | 16x smaller   |
| Memory, all processes        | 470-520 MB | about 235-245 MB | about 2x less |
| Launch until the API answers | 0.38 s     | 0.22 s           | 1.7x faster   |

The memory estimate for the Go app includes the system WebKit processes by measuring how much all `com.apple.WebKit.*` processes grew while it ran, which can include unrelated activity; the main process alone (113 MB resident, 36 MB footprint) is exact.

## Whole-suite timing

| Measure                  | Node                          | Go                                                                          |
| ------------------------ | ----------------------------- | --------------------------------------------------------------------------- |
| Unit + integration suite | 89 tests: 9.6-10.8 s (3 runs) | 153 tests + 40 sub-tests (7 on-request tests skipped): 11.3-11.6 s (3 runs) |

The Go suite is **not faster** as a whole: it has almost twice the tests, starts many real servers and writes to disk with fsync. The speed-ups are in the operations (see each phase page): storage open and writes 2-3x, import 2.5-4x, stats and index builds 2-4x.

## Not done / limits

- **AppImage** (Electron produced one) is not ported: bundling WebKitGTK into an AppImage is its own project. The Linux build is tar.gz, deb and rpm, which depend on the distro's GTK3 and WebKit2GTK 4.1 packages.
- No code signing or notarization (same as the Electron builds today).
- Windows and Linux were never run by me: CI is the first test. The Windows jobs rely on the WebView2 runtime that GitHub's Windows runners have; the installer uses the Wails bootstrapper for machines without it.
- Wails v3 reached 3.0.0 during this work; this port uses v2.17.1 as agreed.

# Phase 6: Wails desktop shell

Status: **done** (awaiting approval for phase 7).

## What was done

The desktop app now exists in Go + Wails v2.17.1 (the plan's choice; Wails v3 has since reached 3.0.0 and a `wails3` CLI is installed on this machine, but I kept v2 as agreed and did not touch it). It runs the same React UI and the same local HTTP API as before.

```
go/internal/desktop/     everything that is not drawing a window (tested on every OS)
  paths.go               data-folder resolution, default folder per OS, bootstrap.json
  lock.go                one process per data folder (.lorekeeper.lock, same pid format as Electron's)
  logger.go              rotating log (logs/app.log, 1 MB, 3 old files)
  windowstate.go         remembered size/position/maximized (window-state.json, same keys)
  copydata.go            "choose a new data folder, copy my data"
  core.go                Start/Shutdown: lock, store, API on 127.0.0.1:<random port> with a per-launch token
  bridge.go              window.lorekeeper (page side) and the asset middleware that injects it
  menu.go                the application menu as data (same items and shortcuts as Electron)
go/cmd/lorekeeper/       the Wails glue (build tag "desktop"): window, menu, bound methods, self-test
go/build-desktop.sh      builds the web UI, embeds it, builds the binary
```

### How it is put together

- The built web app (`apps/web/dist`, 3.0 MB) is **embedded in the binary** and served by Wails to the webview.
- The API keeps running as a normal HTTP server on `127.0.0.1` with a random port and a random 256-bit token, exactly like the Electron build. The page talks to it with `fetch` and `EventSource`. (I considered serving the API through Wails' in-process asset handler, which would need no port, but custom-scheme handlers cannot stream, so the live-update event stream would break.) CORS allows exactly the four origins the webview can have (`wails://wails`, `wails://wails.localhost`, `http(s)://wails.localhost`).
- The bridge: an asset middleware serves `/__lorekeeper.js`, a same-origin script (so the page's strict `script-src 'self'` still holds), and inserts its tag into `index.html`. The script defines `window.lorekeeper` with the same members the Electron preload had (`baseUrl`, `token`, `platform`, `openDataDir`, `chooseDataDir`, `setTheme`, `onMenu`), so **the web app needed no changes**. It also sends clicks on external links to the system browser.
- The data folder is the same as before (`~/Library/Application Support/Lorekeeper`, `%APPDATA%\Lorekeeper`, `$XDG_CONFIG_HOME|~/.config/Lorekeeper`), with the same override order (`LOREKEEPER_DATA_DIR`, then the folder chosen in Settings, then the default). So **your existing data opens as it is**, and the Electron and Go builds refuse to run on the same folder at the same time.

### Electron main process, feature by feature

| Electron                                  | Go + Wails                                                                                                                 |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `requestSingleInstanceLock`, focus window | Wails `SingleInstanceLock`, restores and shows the window                                                                  |
| `.lorekeeper.lock` pid file               | `LockDataDir` (live-process check on Unix and Windows, stale locks taken over)                                             |
| rotating logger                           | `Logger`; also logs invalid-file warnings found _while opening_ (Electron only listened after opening, so it missed those) |
| per-launch token, API on a random port    | `Core.Start`                                                                                                               |
| preload bridge, IPC handlers              | bridge script plus bound methods `OpenDataDir`, `ChooseDataDir`, `SetTheme`                                                |
| application menu                          | `desktop.BuildMenu` turned into a Wails menu; actions go to the page as the same `lorekeeper:menu` events                  |
| window size/position/maximized memory     | `LoadWindowState` / `SaveWindowState`                                                                                      |
| choose data folder, copy, relaunch        | folder dialog, `CopyDataFolder`, `bootstrap.json`, restart through a helper that waits for this process to exit            |
| theme to native chrome                    | `WindowSetDarkTheme` / `LightTheme` / `SystemDefaultTheme`                                                                 |
| clean shutdown (close API, store, lock)   | `OnShutdown` calls `Core.Shutdown` (ends event streams first so it never hangs)                                            |

## How it was verified

1. **A self-test inside the real webview.** `LOREKEEPER_SELFTEST=1 ./release-go/lorekeeper` starts the app with a hidden window; once the page loads, a script inside the webview checks seven things and reports back through a bound method, then the app exits with 0 or 1. On macOS (WebKit) all seven pass: the bridge is present (page origin `wails://wails`), the UI rendered (sidebar with the fixture library: categories and counts), the API answers from the page across origins, requests without the token get 401, the event stream connects, the native bindings exist. The log shows the clean start and `stopped`.
2. **Window state really round-trips**: after a normal run and a clean `SIGTERM`, `window-state.json` held the window's geometry and the log ended with `stopped`.
3. **`go test -race ./internal/desktop`**: 18 tests, all passing (see below).

**Not verified**, so please try these by hand: the window and its content _by eye_ (I did not capture your screen after one mistake, see below), clicking menu items and shortcuts, the "choose data folder" dialog and relaunch, dark/light switching of the title bar, second-launch focusing the existing window, and everything on Windows and Linux (phase 7 builds those in CI).

## Tests

`internal/desktop`: 18 tests, passing (also under `-race`). Ported the 3 of `paths.test.ts` (precedence, relative/blank overrides, default location per OS) and added: the real default folder equals the Electron one on this OS, bootstrap read/write/corruption, the lock (exclusive across processes using the parent process as a live holder, stale lock from an exited process taken over, garbage lock file, release), logger (format, rotation, never panics), window state (defaults, minimum sizes, off-screen position dropped, maximize keeps restored bounds, same JSON keys), copying only config/data/activity/meta, the bridge script (values JSON-escaped so a token can never break out of the script), the asset middleware (script served only when ready, tag injected, other files and 404s untouched), the menu on each OS (same actions and shortcuts, no duplicates, roles), and the running core (loopback + token, 401s, CORS for exactly the webview origins, fresh token and port per launch, second process refused and lock released on shutdown, event streams end on shutdown, startup/invalid-file/shutdown logged, folders resolved from environment and `bootstrap.json`).

## Timing and size

| Measure                                            | Electron build                        | Go + Wails                                                                              |
| -------------------------------------------------- | ------------------------------------- | --------------------------------------------------------------------------------------- |
| Shell tests (`paths.test.ts` / `desktop`)          | 3 tests, under 10 ms                  | 18 tests, 2.1 s (3.0 s with `-race`; they start and stop real servers)                  |
| Binary (macOS arm64, UI embedded)                  | measured in phase 7 (installer sizes) | 18.7 MB (web UI inside is 3.0 MB)                                                       |
| Launch to "UI rendered and checked", hidden window | not measured yet                      | 0.49-0.62 s, whole self-test run                                                        |
| Main process memory                                | measured in phase 7                   | 116 MB resident, 36 MB footprint (3 runs; the WebKit helper processes are not included) |

The Electron comparison needs packaged builds of both, so installer size, installed size and total memory (all processes) are filled in at phase 7.

## Differences from the Electron shell, on purpose

- **Menu**: no zoom items (the webview has no zoom API in Wails v2) and no Reload/DevTools entries; on macOS "Settings…" is in the File menu because the standard app menu cannot be extended. All shortcuts are otherwise the same.
- **Window state** is saved when the window closes (Electron saved while resizing and moving). Wails reports screen sizes but not their positions, so a saved position is only restored if it fits a row of screens starting at the primary one; otherwise the OS places the window.
- **Permissions and navigation**: Electron explicitly denied all permission requests and blocked navigation away from the app; here the page has no external navigation (links open in the system browser through the bridge) and the webview's defaults apply to permission prompts. The strict CSP comes from the built page's `<meta>` as before; Electron also added it as a header for the page it served.
- **`relaunch`** after changing the data folder starts a helper that waits a second and then starts a new copy, so the new process cannot collide with the old one's locks.
- Linux builds need the GTK3 and WebKit2GTK development packages (I use the `webkit2_41` tag for current distros); macOS builds need `-framework UniformTypeIdentifiers`. `build-desktop.sh` sets both; packaging into `.app`, installers and AppImage is phase 7.

## Building and running

```bash
go/build-desktop.sh            # web UI + Go binary -> release-go/lorekeeper
./release-go/lorekeeper        # the app
LOREKEEPER_SELFTEST=1 ./release-go/lorekeeper   # hidden-window self-test, exit code 0 = all checks passed
```

Without the `desktop` tag (CI, `go test ./...`) `cmd/lorekeeper` is a stub, so the rest of the module builds and tests without any webview toolchain.

## Known gaps / next

- Phase 7: `wails build` or equivalent packaging per OS, installers, CI on macOS/Windows/Linux (including building the desktop binary and running the self-test, under `xvfb` on Linux), the Playwright/axe suite against the Go server in a browser, the size/memory table, and the cutover decision.

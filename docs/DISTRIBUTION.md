# Building and distributing

```bash
npm run dev                  # API + web in a browser (http://127.0.0.1:5173)
npm run dev:desktop          # same UI inside Electron, with hot reload
npm run build:desktop        # installers for the OS you are on -> apps/desktop/release/
npm run e2e                  # Playwright smoke test against the built Electron app
```

`build:desktop` builds the web app, bundles the Electron main/preload (esbuild) and runs electron-builder:

| OS      | Artifacts                                   |
| ------- | ------------------------------------------- |
| Windows | NSIS installer (`.exe`) and portable `.exe` |
| macOS   | universal `.dmg`                            |
| Linux   | `.AppImage`, `.deb`, `.rpm` (needs `rpm`)   |

Push a tag like `v0.1.0` and `.github/workflows/release.yml` builds all three and attaches the files to a GitHub release.

## Unsigned builds

Builds are unsigned for now, so the OS will warn on first launch:

- **Windows SmartScreen**: choose "More info" → "Run anyway".
- **macOS Gatekeeper**: right-click the app → Open (or `xattr -dr com.apple.quarantine /Applications/Lorekeeper.app`).
- **Linux AppImage**: `chmod +x Lorekeeper-*.AppImage` then run it.

### Adding signing later

Everything lives in `apps/desktop/electron-builder.config.cjs`.

- macOS: remove `identity: null`, set `CSC_LINK` / `CSC_KEY_PASSWORD` and add notarization (`APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`) as repository secrets, then expose them to the release job.
- Windows: set `CSC_LINK` / `CSC_KEY_PASSWORD` (or Azure Trusted Signing settings under `win`).
- Linux packages are not signed by electron-builder.

## Where the data lives

Electron's `userData` folder, named after `APP_NAME`:

- Windows `%APPDATA%\Lorekeeper`
- macOS `~/Library/Application Support/Lorekeeper`
- Linux `$XDG_CONFIG_HOME/Lorekeeper` or `~/.config/Lorekeeper`

Override with `LOREKEEPER_DATA_DIR`, or pick a folder in Settings ▸ Data (stored in `bootstrap.json` beside Electron's default folder). The e2e test checks the OS convention on all three platforms in CI.

## Security model

The renderer runs with `contextIsolation`, `sandbox` and no Node. The API binds `127.0.0.1` on a random port and every `/api` call needs a per-launch random token that only the preload bridge hands to our own page. Navigation away from the app and new windows are blocked (external links open in the browser), permission requests are denied, and the built page ships a strict CSP.

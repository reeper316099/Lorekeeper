# Building and distributing

```bash
npm run dev                  # Go API + web in a browser (http://127.0.0.1:5173)
npm run build:desktop        # the desktop app for this machine -> release-go/lorekeeper
go/package-desktop.sh        # installers/packages for the OS you are on -> release-go/
npm run e2e                  # Playwright against the Go API server
```

Needs Go, Node 22+, a C compiler, and on Linux `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`. Packaging uses the Wails CLI
(`go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1`), plus `nfpm` for `.deb`/`.rpm` and NSIS for the Windows installer.

| OS      | Artifacts                                      |
| ------- | ---------------------------------------------- |
| Windows | NSIS installer (`-setup.exe`) and portable exe |
| macOS   | `.dmg` (Apple silicon build)                   |
| Linux   | `.tar.gz`, `.deb`, `.rpm`                      |

The version comes from `version` in the root `package.json` (and `productVersion` in `go/cmd/lorekeeper/wails.json`).
Push a tag like `go-v0.1.3` that matches it and `.github/workflows/go-release.yml` builds all three OSes and attaches the
files to a GitHub release. Running the workflow by hand only uploads build artifacts.

## Unsigned builds

Builds are unsigned for now (the macOS app is ad-hoc signed), so the OS will warn on first launch:

- **Windows SmartScreen**: choose "More info" → "Run anyway".
- **macOS Gatekeeper**: right-click the app → Open (or `xattr -dr com.apple.quarantine /Applications/Lorekeeper.app`).

### Adding signing later

Not set up yet. Wails supports Developer ID signing and notarization on macOS (`wails build` plus `codesign`/`notarytool`,
credentials as repository secrets used in `go-release.yml`) and `signtool` for the Windows installer.

## Where the data lives

A folder named after the app inside the OS config directory:

- Windows `%APPDATA%\Lorekeeper`
- macOS `~/Library/Application Support/Lorekeeper`
- Linux `$XDG_CONFIG_HOME/Lorekeeper` or `~/.config/Lorekeeper`

Override with `LOREKEEPER_DATA_DIR`, or pick a folder in Settings ▸ Data (remembered in `bootstrap.json` in the default
folder). Data written by the earlier Electron builds is used as it is.

## Security model

The UI runs in the system webview with no Node. The API binds `127.0.0.1` on a random port and every `/api` call needs a
per-launch random token that only our own page receives (through the injected bridge script). Cross-origin access is limited
to the app's own origins, external links open in the system browser, and images load through the app's own origin.

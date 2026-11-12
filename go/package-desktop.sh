#!/usr/bin/env sh
# Packages the Go + Wails desktop app with the Wails CLI (go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1).
#   macOS:   go/cmd/lorekeeper/build/bin/Lorekeeper.app  and  release-go/lorekeeper-<version>-mac-<arch>.dmg
#   Linux:   release-go/lorekeeper-<version>-linux-<arch>.tar.gz  (+ .deb and .rpm when nfpm is installed)
#   Windows: use the CI workflow (wails build -nsis needs NSIS), or run wails build by hand.
# Builds are unsigned (macOS ad-hoc), like the Electron builds were: see docs/DISTRIBUTION.md.
set -eu
cd "$(dirname "$0")/.."
root="$(pwd)"
version="${VERSION:-$(node -p "require('./apps/desktop/package.json').version")}"
sh go/prepare-frontend.sh
mkdir -p release-go
cd go/cmd/lorekeeper
wails="${WAILS:-wails}"
case "$(uname -s)" in
  Darwin)
    arch="$(uname -m | sed 's/x86_64/amd64/')"
    export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=13.0"
    export CGO_LDFLAGS="${CGO_LDFLAGS:-} -framework UniformTypeIdentifiers -mmacosx-version-min=13.0"
    # The CLI's own ad-hoc signing step fails on a binary the Go linker already signed; the bundle is
    # signed again below, so only a missing .app is an error here.
    "$wails" build -clean -platform "darwin/$arch" -skipbindings -s -ldflags "-s -w -X main.version=$version" || [ -d build/bin/Lorekeeper.app ]
    xattr -cr build/bin/Lorekeeper.app
    codesign --force --deep --sign - build/bin/Lorekeeper.app
    stage="$(mktemp -d)"
    cp -R build/bin/Lorekeeper.app "$stage/"
    ln -s /Applications "$stage/Applications"
    hdiutil create -volname Lorekeeper -srcfolder "$stage" -ov -format UDZO "$root/release-go/lorekeeper-$version-mac-$arch.dmg" >/dev/null
    rm -rf "$stage"
    ;;
  Linux)
    arch="$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')"
    "$wails" build -clean -tags webkit2_41 -skipbindings -s -ldflags "-s -w -X main.version=$version"
    stage="$(mktemp -d)/lorekeeper-$version"
    mkdir -p "$stage"
    cp build/bin/lorekeeper "$stage/"
    tar -C "$(dirname "$stage")" -czf "$root/release-go/lorekeeper-$version-linux-$arch.tar.gz" "lorekeeper-$version"
    if command -v nfpm >/dev/null 2>&1; then
      VERSION="$version" ARCH="$arch" BIN="$PWD/build/bin/lorekeeper" ICON="$PWD/build/appicon.png" DESKTOP="$PWD/build/linux/lorekeeper.desktop" \
        nfpm package -f build/linux/nfpm.yaml -p deb -t "$root/release-go/"
      VERSION="$version" ARCH="$arch" BIN="$PWD/build/bin/lorekeeper" ICON="$PWD/build/appicon.png" DESKTOP="$PWD/build/linux/lorekeeper.desktop" \
        nfpm package -f build/linux/nfpm.yaml -p rpm -t "$root/release-go/"
    fi
    ;;
  *) echo "Run the Wails CLI by hand on this OS (see docs/go-port/phase-7.md)"; exit 1 ;;
esac
ls -la "$root/release-go"

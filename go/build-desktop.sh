#!/usr/bin/env sh
# Builds the Go + Wails desktop app: the web UI first (apps/web), embedded into the binary.
#   go/build-desktop.sh [output-path]      (default: release-go/lorekeeper)
# Needs Node (for the web build), Go, a C compiler, and on Linux the GTK3 and WebKit2GTK dev packages.
set -eu
cd "$(dirname "$0")/.."
out="${1:-release-go/lorekeeper}"
version="${VERSION:-$(node -p "require('./apps/desktop/package.json').version")}"
sh go/prepare-frontend.sh
mkdir -p "$(dirname "$out")"
tags="desktop,production"
ldflags="-s -w -X main.version=$version"
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) # Windows (Git Bash): a GUI program, so no console window
    ldflags="$ldflags -H windowsgui"
    case "$out" in *.exe) ;; *) out="$out.exe" ;; esac ;;
  Linux) tags="$tags,webkit2_41" ;;
  Darwin) # what the Wails CLI sets: the file dialogs use UTType
    export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=13.0"
    export CGO_LDFLAGS="${CGO_LDFLAGS:-} -framework UniformTypeIdentifiers -mmacosx-version-min=13.0" ;;
esac
(cd go && CGO_ENABLED=1 go build -tags "$tags" -ldflags "$ldflags" -o "$(cd .. && pwd)/$out" ./cmd/lorekeeper)
echo "built $out ($version)"

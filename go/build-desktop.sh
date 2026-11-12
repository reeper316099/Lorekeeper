#!/usr/bin/env sh
# Builds the Go + Wails desktop app: the web UI first (apps/web), embedded into the binary.
#   go/build-desktop.sh [output-path]      (default: release-go/lorekeeper)
# Needs Node (for the web build), Go, a C compiler, and on Linux the GTK3 and WebKit2GTK dev packages.
set -eu
cd "$(dirname "$0")/.."
out="${1:-release-go/lorekeeper}"
version="${VERSION:-$(node -p "require('./apps/desktop/package.json').version")}"
npm run build -w @lorekeeper/web
rm -rf go/cmd/lorekeeper/frontend
mkdir -p go/cmd/lorekeeper/frontend "$(dirname "$out")"
cp -R apps/web/dist/. go/cmd/lorekeeper/frontend/
touch go/cmd/lorekeeper/frontend/.gitkeep
tags="desktop,production"
case "$(uname -s)" in
  Linux) tags="$tags,webkit2_41" ;;
  Darwin) # what the Wails CLI sets: the file dialogs use UTType
    export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=13.0"
    export CGO_LDFLAGS="${CGO_LDFLAGS:-} -framework UniformTypeIdentifiers -mmacosx-version-min=13.0" ;;
esac
(cd go && CGO_ENABLED=1 go build -tags "$tags" -ldflags "-s -w -X main.version=$version" -o "$(cd .. && pwd)/$out" ./cmd/lorekeeper)
echo "built $out ($version)"

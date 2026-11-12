#!/usr/bin/env sh
# Builds the web UI (apps/web) and copies it where the Go desktop app embeds it.
set -eu
cd "$(dirname "$0")/.."
npm run build -w @lorekeeper/web
rm -rf go/cmd/lorekeeper/frontend
mkdir -p go/cmd/lorekeeper/frontend
cp -R apps/web/dist/. go/cmd/lorekeeper/frontend/
touch go/cmd/lorekeeper/frontend/.gitkeep

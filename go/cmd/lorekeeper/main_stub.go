//go:build !desktop

// Command lorekeeper is the desktop app. The window needs the Wails webview toolchain, so it is built
// with the "desktop" tag (see docs/go-port/phase-6.md); without it this stub explains how.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "This build has no window. Build the desktop app with:\n  go build -tags desktop ./cmd/lorekeeper\n(see docs/go-port/phase-6.md), or run the headless API with: go run ./cmd/server")
	os.Exit(2)
}

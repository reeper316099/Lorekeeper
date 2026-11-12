// Package desktop is the part of the desktop app that does not need a window: where data lives,
// the one-process-per-folder lock, the rotating log, remembered window geometry, the local API
// server with its per-launch token, the page-to-app bridge, and the menu model. The Wails glue in
// cmd/lorekeeper only turns these into a window. Keeping it window-free means it is tested on every OS.
package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Bootstrap is stored in the default user-data folder (not the data folder) and remembers a data
// folder chosen in Settings.
type Bootstrap struct {
	DataDir string `json:"dataDir,omitempty"`
}

// UserDataDir is the app's default folder: %APPDATA%\<App> on Windows, ~/Library/Application
// Support/<App> on macOS, $XDG_CONFIG_HOME/<App> or ~/.config/<App> on Linux (os.UserConfigDir),
// the same place the Electron build used. LOREKEEPER_USER_DATA overrides it (tests, e2e).
func UserDataDir(appName string) (string, error) {
	if v := os.Getenv("LOREKEEPER_USER_DATA"); strings.TrimSpace(v) != "" {
		return filepath.Abs(v)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appName), nil
}

// ResolveDataDir picks the data folder: LOREKEEPER_DATA_DIR (dev/tests), then the folder chosen in
// Settings, then the default user-data folder. Blank and relative overrides are ignored.
func ResolveDataDir(env, bootstrapDir, userData string) string {
	for _, p := range []string{env, bootstrapDir} {
		if strings.TrimSpace(p) != "" && filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
	}
	abs, err := filepath.Abs(userData)
	if err != nil {
		return filepath.Clean(userData)
	}
	return abs
}

func ReadBootstrap(userData string) Bootstrap {
	b, err := os.ReadFile(filepath.Join(userData, "bootstrap.json"))
	if err != nil {
		return Bootstrap{}
	}
	var out Bootstrap
	if json.Unmarshal(b, &out) != nil {
		return Bootstrap{}
	}
	return out
}

func WriteBootstrap(userData, dataDir string) error {
	if err := os.MkdirAll(userData, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(Bootstrap{DataDir: dataDir})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(userData, "bootstrap.json"), b, 0o644)
}

// ExpectedUserData documents where the default folder is on each OS (used by the path test).
func ExpectedUserData(goos, appName string, env map[string]string) string {
	switch goos {
	case "windows":
		return env["APPDATA"] + `\` + appName
	case "darwin":
		return env["HOME"] + "/Library/Application Support/" + appName
	}
	if x := env["XDG_CONFIG_HOME"]; x != "" {
		return x + "/" + appName
	}
	return env["HOME"] + "/.config/" + appName
}

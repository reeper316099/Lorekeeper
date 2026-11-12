//go:build desktop

// Command lorekeeper is the Lorekeeper desktop app: a Wails window around the same React UI and the
// same local HTTP API as the headless server. Everything that is not drawing a window lives in
// internal/desktop, which is tested on every OS.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/reeper316099/Lorekeeper/go/internal/desktop"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

// frontend holds the built web app (apps/web/dist), copied here by go/build-desktop.sh.
//
//go:embed all:frontend
var frontend embed.FS

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

// App is bound to the page: its exported methods are what window.lorekeeper calls.
type App struct {
	ctx       context.Context
	mu        sync.Mutex
	core      *desktop.Core
	exitCode  int
	relaunch  bool
	maximised bool
}

func (a *App) config() (string, string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.core == nil {
		return "", "", false
	}
	return a.core.BaseURL, a.core.Token, true
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	core, err := desktop.Start(desktop.Options{AppVersion: version})
	if err != nil {
		wr.MessageDialog(ctx, wr.MessageDialogOptions{Type: wr.ErrorDialog, Title: shared.AppName, Message: err.Error()})
		a.exitCode = 1
		wr.Quit(ctx)
		return
	}
	a.mu.Lock()
	a.core = core
	a.mu.Unlock()
	st := desktop.LoadWindowState(core.UserData, a.visible)
	if st.HasPosition {
		wr.WindowSetPosition(ctx, st.X, st.Y)
	}
}

// visible reports whether a window at (x, y) would still be on a screen. Wails gives screen sizes,
// not positions, so this assumes screens side by side from the primary one; a saved position that
// does not fit is dropped and the OS places the window.
func (a *App) visible(x, y, w, h int) bool {
	screens, err := wr.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return false
	}
	totalW, maxH := 0, 0
	for _, s := range screens {
		totalW += s.Size.Width
		maxH = max(maxH, s.Size.Height)
	}
	return x < totalW-80 && x+w > 80 && y >= -10 && y < maxH-80
}

func (a *App) beforeClose(ctx context.Context) bool {
	if a.core == nil {
		return false
	}
	st := desktop.WindowState{Maximized: wr.WindowIsMaximised(ctx), HasPosition: true}
	st.Width, st.Height = wr.WindowGetSize(ctx)
	st.X, st.Y = wr.WindowGetPosition(ctx)
	_ = desktop.SaveWindowState(a.core.UserData, st)
	return false
}

func (a *App) shutdown(ctx context.Context) {
	if a.core != nil {
		a.core.Shutdown()
	}
	if a.relaunch {
		relaunchSoon()
	}
}

// relaunchSoon starts a fresh copy of this program a moment after this one has exited, so the new
// process does not collide with the data-folder and single-instance locks.
func relaunchSoon() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "ping 127.0.0.1 -n 2 >nul & start \"\" \""+exe+"\"")
	} else {
		cmd = exec.Command("sh", "-c", "sleep 1; exec \"$0\"", exe)
	}
	_ = cmd.Start()
}

// ---------------------------------------------------------------- methods called from the page

// OpenDataDir shows the data folder in the system file manager.
func (a *App) OpenDataDir() error {
	if a.core == nil {
		return nil
	}
	return openPath(a.core.DataDir)
}

func openPath(dir string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", dir).Start()
	case "windows":
		return exec.Command("explorer", dir).Start()
	}
	return exec.Command("xdg-open", dir).Start()
}

// ChooseDataDir lets the user pick a new data folder (optionally copying the current data into it),
// remembers it and restarts the app.
func (a *App) ChooseDataDir(copyExisting bool) map[string]bool {
	if a.core == nil {
		return map[string]bool{"ok": false}
	}
	dir, err := wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: "Choose data folder", CanCreateDirectories: true})
	if err != nil || dir == "" {
		return map[string]bool{"ok": false}
	}
	if abs, _ := filepath.Abs(dir); abs == a.core.DataDir {
		return map[string]bool{"ok": false}
	}
	if copyExisting {
		if err := desktop.CopyDataFolder(a.core.DataDir, dir); err != nil {
			a.core.Log.Log("error", "copying data failed: "+err.Error())
			return map[string]bool{"ok": false}
		}
	}
	if err := desktop.WriteBootstrap(a.core.UserData, dir); err != nil {
		return map[string]bool{"ok": false}
	}
	a.core.Log.Log("info", "data folder changed; relaunching")
	a.relaunch = true
	go wr.Quit(a.ctx)
	return map[string]bool{"ok": true}
}

// SetTheme follows the app's theme setting for native chrome (title bar, menus).
func (a *App) SetTheme(theme string) {
	switch theme {
	case "dark":
		wr.WindowSetDarkTheme(a.ctx)
	case "light":
		wr.WindowSetLightTheme(a.ctx)
	case "system":
		wr.WindowSetSystemDefaultTheme(a.ctx)
	}
}

// ---------------------------------------------------------------- menu

func accelerator(s string) *keys.Accelerator {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "+")
	key := parts[len(parts)-1]
	var mods []keys.Modifier
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "shift":
			mods = append(mods, keys.ShiftKey)
		default: // CmdOrCtrl, Cmd, Ctrl (the model is already per OS)
			mods = append(mods, keys.CmdOrCtrlKey)
		}
	}
	switch len(mods) {
	case 0:
		return keys.Key(strings.ToLower(key))
	case 1:
		return keys.Combo(strings.ToLower(key), mods[0], mods[0])
	}
	return keys.Combo(strings.ToLower(key), mods[0], mods[1])
}

func (a *App) buildMenu() *menu.Menu {
	root := menu.NewMenu()
	var fill func(m *menu.Menu, items []desktop.MenuItem)
	fill = func(m *menu.Menu, items []desktop.MenuItem) {
		for _, it := range items {
			it := it
			switch {
			case it.Separator:
				m.AddSeparator()
			case it.Role == "app":
				root.Append(menu.AppMenu())
			case it.Role == "edit":
				root.Append(menu.EditMenu())
			case it.Role == "window":
				root.Append(menu.WindowMenu())
			case it.Role == "quit":
				m.AddText(it.Label, accelerator(it.Accelerator), func(*menu.CallbackData) { wr.Quit(a.ctx) })
			case it.Role == "fullscreen":
				m.AddText(it.Label, nil, func(*menu.CallbackData) {
					if wr.WindowIsFullscreen(a.ctx) {
						wr.WindowUnfullscreen(a.ctx)
					} else {
						wr.WindowFullscreen(a.ctx)
					}
				})
			case it.Submenu != nil:
				fill(root.AddSubmenu(it.Label), it.Submenu)
			case it.Action == desktop.ActionOpenData:
				m.AddText(it.Label, accelerator(it.Accelerator), func(*menu.CallbackData) { _ = a.OpenDataDir() })
			default:
				m.AddText(it.Label, accelerator(it.Accelerator), func(*menu.CallbackData) {
					wr.EventsEmit(a.ctx, desktop.MenuEvent, it.Action)
				})
			}
		}
	}
	// Top-level entries with a submenu become menus of the root; roles are appended to it.
	for _, top := range desktop.BuildMenu(runtime.GOOS) {
		switch {
		case top.Role != "":
			fill(root, []desktop.MenuItem{top})
		default:
			fill(root.AddSubmenu(top.Label), top.Submenu)
		}
	}
	return root
}

// ---------------------------------------------------------------- self test

// selfTestScript runs in the page: it waits for the UI to render, checks the bridge, the API and the
// event stream through the real webview, and reports back through the bound SelfTestDone.
const selfTestScript = `(async function () {
  var out = { origin: location.origin, ok: false, steps: [] };
  var step = function (name, ok, detail) { out.steps.push({ name: name, ok: !!ok, detail: detail }); };
  try {
    var lk = window.lorekeeper;
    step('bridge present', lk && lk.baseUrl && lk.token, lk && { baseUrl: lk.baseUrl, platform: lk.platform });
    for (var i = 0; i < 100 && !document.querySelector('nav, aside, main'); i++) await new Promise(function (r) { setTimeout(r, 100); });
    step('UI rendered', document.querySelector('nav, aside, main'), document.title);
    var meta = await fetch(lk.baseUrl + '/api/v1/meta', { headers: { Authorization: 'Bearer ' + lk.token } });
    step('API reachable from the page (CORS)', meta.ok, meta.status);
    var anon = await fetch(lk.baseUrl + '/api/v1/meta');
    step('API refuses requests without the token', anon.status === 401, anon.status);
    var sse = await new Promise(function (resolve) {
      var es = new EventSource(lk.baseUrl + '/api/v1/events?token=' + lk.token);
      es.addEventListener('hello', function () { es.close(); resolve(true); });
      es.onerror = function () { es.close(); resolve(false); };
      setTimeout(function () { es.close(); resolve(false); }, 5000);
    });
    step('event stream connects', sse);
    step('native bindings present', window.go && window.go.main && window.go.main.App && window.runtime, null);
    out.text = (document.body.innerText || '').slice(0, 160);
    out.ok = out.steps.every(function (s) { return s.ok; });
  } catch (e) { step('exception', false, String(e)); }
  window.go.main.App.SelfTestDone(JSON.stringify(out));
})();`

// SelfTestDone receives the self-test report from the page, prints it and quits.
func (a *App) SelfTestDone(report string) {
	var r struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal([]byte(report), &r)
	fmt.Println(report)
	// A GUI-subsystem build on Windows has no console, so the report can also go to a file.
	if out := os.Getenv("LOREKEEPER_SELFTEST_OUT"); out != "" {
		_ = os.WriteFile(out, []byte(report), 0o644)
	}
	if !r.OK {
		a.exitCode = 1
	}
	go wr.Quit(a.ctx)
}

func main() {
	app := &App{}
	selfTest := os.Getenv("LOREKEEPER_SELFTEST") == "1"
	assets, err := fs.Sub(frontend, "frontend")
	if err == nil {
		_, err = fs.Stat(assets, "index.html")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "The web app is not embedded yet: run go/build-desktop.sh (it builds apps/web and copies it to cmd/lorekeeper/frontend).")
		os.Exit(2)
	}

	userData, _ := desktop.UserDataDir(shared.AppName)
	ws := desktop.LoadWindowState(userData, nil)
	startState := options.Normal
	if ws.Maximized {
		startState = options.Maximised
	}
	err = wails.Run(&options.App{
		Title:            shared.AppName,
		Width:            ws.Width,
		Height:           ws.Height,
		MinWidth:         desktop.MinWidth,
		MinHeight:        desktop.MinHeight,
		StartHidden:      selfTest || os.Getenv("LOREKEEPER_HEADLESS") == "1", // like the Electron build: no visible window (e2e)
		WindowStartState: startState,
		BackgroundColour: &options.RGBA{R: 13, G: 14, B: 19, A: 255},
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: assetserver.Middleware(func(next http.Handler) http.Handler { return desktop.AssetsMiddleware(app.config)(next) }),
		},
		Menu:               app.buildMenu(),
		LogLevel:           logger.ERROR,
		LogLevelProduction: logger.ERROR,
		OnStartup:          app.startup,
		OnDomReady: func(ctx context.Context) {
			if selfTest && app.core != nil {
				wr.WindowExecJS(ctx, selfTestScript)
			}
		},
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Bind:          []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "lorekeeper-8f0d1c52-3b8e-4a43-9a43-6d2b4e7c1a90",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if app.ctx != nil {
					wr.WindowUnminimise(app.ctx)
					wr.Show(app.ctx)
				}
			},
		},
		EnableDefaultContextMenu: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	os.Exit(app.exitCode)
}

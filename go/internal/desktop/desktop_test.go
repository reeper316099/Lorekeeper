package desktop

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Ported from apps/desktop/src/paths.test.ts, plus coverage for everything else in this package.

func abs(p string) string { a, _ := filepath.Abs(p); return a }

func TestDataDirPrecedence(t *testing.T) {
	// filepath.IsAbs("/tmp/a") is false on Windows, so expectations use the platform's own absolute paths.
	a, b, u := abs("/tmp/a"), abs("/tmp/b"), abs("/u")
	if got := ResolveDataDir(a, b, u); got != filepath.Clean(a) {
		t.Errorf("env first: %s", got)
	}
	if got := ResolveDataDir("", b, u); got != filepath.Clean(b) {
		t.Errorf("Settings choice second: %s", got)
	}
	if got := ResolveDataDir("", "", u); got != u {
		t.Errorf("userData last: %s", got)
	}
	if got := ResolveDataDir("relative/dir", "  ", u); got != u {
		t.Errorf("relative and blank overrides are ignored: %s", got)
	}
}

func TestDefaultLocationsPerOS(t *testing.T) {
	cases := []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"windows", map[string]string{"APPDATA": `C:\Users\me\AppData\Roaming`}, `C:\Users\me\AppData\Roaming\Lorekeeper`},
		{"darwin", map[string]string{"HOME": "/Users/me"}, "/Users/me/Library/Application Support/Lorekeeper"},
		{"linux", map[string]string{"HOME": "/home/me"}, "/home/me/.config/Lorekeeper"},
		{"linux", map[string]string{"HOME": "/home/me", "XDG_CONFIG_HOME": "/x"}, "/x/Lorekeeper"},
	}
	for _, c := range cases {
		if got := ExpectedUserData(c.goos, "Lorekeeper", c.env); got != c.want {
			t.Errorf("%s: %s, want %s", c.goos, got, c.want)
		}
	}
}

func TestUserDataDirMatchesThePlatformConvention(t *testing.T) {
	t.Setenv("LOREKEEPER_USER_DATA", "")
	got, err := UserDataDir("Lorekeeper")
	if err != nil {
		t.Skip("no user config dir in this environment")
	}
	var want string
	switch runtime.GOOS {
	case "windows":
		want = ExpectedUserData("windows", "Lorekeeper", map[string]string{"APPDATA": os.Getenv("APPDATA")})
	case "darwin":
		want = ExpectedUserData("darwin", "Lorekeeper", map[string]string{"HOME": os.Getenv("HOME")})
	default:
		want = ExpectedUserData("linux", "Lorekeeper", map[string]string{"HOME": os.Getenv("HOME"), "XDG_CONFIG_HOME": os.Getenv("XDG_CONFIG_HOME")})
	}
	if filepath.ToSlash(got) != filepath.ToSlash(want) {
		t.Errorf("UserDataDir = %s, want %s (the folder the Electron build used)", got, want)
	}
	dir := t.TempDir()
	t.Setenv("LOREKEEPER_USER_DATA", dir)
	if got, _ := UserDataDir("Lorekeeper"); got != abs(dir) {
		t.Errorf("override: %s", got)
	}
}

func TestBootstrapRoundTripAndCorruption(t *testing.T) {
	u := t.TempDir()
	if ReadBootstrap(u).DataDir != "" {
		t.Error("no file means no choice")
	}
	if err := WriteBootstrap(u, "/some/dir"); err != nil {
		t.Fatal(err)
	}
	if ReadBootstrap(u).DataDir != "/some/dir" {
		t.Error("round trip")
	}
	os.WriteFile(filepath.Join(u, "bootstrap.json"), []byte("{ nope"), 0o644)
	if ReadBootstrap(u).DataDir != "" {
		t.Error("corrupt file must be ignored")
	}
	os.WriteFile(filepath.Join(u, "bootstrap.json"), []byte(`{"dataDir":5}`), 0o644)
	if ReadBootstrap(u).DataDir != "" {
		t.Error("wrong type must be ignored")
	}
}

// ---------- lock ----------

func TestLockIsExclusiveAcrossProcessesAndTakesOverStaleLocks(t *testing.T) {
	dir := t.TempDir()
	release, err := LockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".lorekeeper.lock")); err != nil {
		t.Fatal("lock file missing")
	}
	// the same process asking again is fine (the Electron build behaved the same)
	if r2, err := LockDataDir(dir); err != nil {
		t.Fatalf("same pid: %v", err)
	} else {
		r2()
	}
	release()
	if _, err := os.Stat(filepath.Join(dir, ".lorekeeper.lock")); !os.IsNotExist(err) {
		t.Error("release must remove the lock")
	}

	// a lock held by a live process (our parent) refuses
	os.WriteFile(filepath.Join(dir, ".lorekeeper.lock"), []byte(itoa(os.Getppid())), 0o644)
	if _, err := LockDataDir(dir); err != ErrLocked {
		t.Errorf("live holder: got %v", err)
	}

	// a lock left by a process that has exited is taken over
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot spawn a helper process: %v", err)
	}
	os.WriteFile(filepath.Join(dir, ".lorekeeper.lock"), []byte(itoa(cmd.Process.Pid)), 0o644)
	r3, err := LockDataDir(dir)
	if err != nil {
		t.Fatalf("stale lock: %v", err)
	}
	r3()

	// garbage in the lock file counts as stale
	os.WriteFile(filepath.Join(dir, ".lorekeeper.lock"), []byte("not a pid"), 0o644)
	if r, err := LockDataDir(dir); err != nil {
		t.Errorf("garbage: %v", err)
	} else {
		r()
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// ---------- logger ----------

func TestLoggerWritesRotatesAndNeverFails(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir)
	l.Log("info", "hello")
	l.Log("warn", "careful")
	b, _ := os.ReadFile(filepath.Join(dir, "logs", "app.log"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], " INFO hello") || !strings.HasSuffix(lines[1], " WARN careful") || !strings.Contains(lines[0], "T") {
		t.Errorf("lines %q", lines)
	}
	big := strings.Repeat("x", 100_000)
	for i := 0; i < 60; i++ {
		l.Log("info", big)
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "logs"))
	names := map[string]bool{}
	for _, e := range ents {
		names[e.Name()] = true
		if info, _ := e.Info(); info.Size() > 1<<20+200_000 {
			t.Errorf("%s is %d bytes", e.Name(), info.Size())
		}
	}
	if !names["app.log"] || !names["app.log.1"] || names["app.log.4"] {
		t.Errorf("files %v (1 MB, 3 old files)", names)
	}
	var nilLogger *Logger
	nilLogger.Log("info", "no crash")
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o644)
	NewLogger(file).Log("error", "the log folder cannot be created; this must not panic")
}

// ---------- window state ----------

func TestWindowStateDefaultsLimitsAndOffScreenPositions(t *testing.T) {
	u := t.TempDir()
	if st := LoadWindowState(u, nil); st.Width != DefaultWidth || st.Height != DefaultHeight || st.HasPosition || st.Maximized {
		t.Errorf("defaults %+v", st)
	}
	os.WriteFile(filepath.Join(u, "window-state.json"), []byte(`{"x":50,"y":60,"width":300,"height":200,"maximized":true}`), 0o644)
	st := LoadWindowState(u, func(x, y, w, h int) bool { return true })
	if st.Width != MinWidth || st.Height != MinHeight || !st.HasPosition || st.X != 50 || st.Y != 60 || !st.Maximized {
		t.Errorf("limits %+v", st)
	}
	if st := LoadWindowState(u, func(x, y, w, h int) bool { return false }); st.HasPosition {
		t.Error("a position that is off every screen must be dropped")
	}
	var got [4]int
	LoadWindowState(u, func(x, y, w, h int) bool { got = [4]int{x, y, w, h}; return true })
	if got != [4]int{50, 60, MinWidth, MinHeight} {
		t.Errorf("visible() was given %v", got)
	}
	os.WriteFile(filepath.Join(u, "window-state.json"), []byte(`garbage`), 0o644)
	if st := LoadWindowState(u, nil); st.Width != DefaultWidth {
		t.Error("corrupt file falls back to defaults")
	}
}

func TestWindowStateSaveKeepsRestoredBoundsWhileMaximized(t *testing.T) {
	u := t.TempDir()
	if err := SaveWindowState(u, WindowState{X: 100, Y: 120, HasPosition: true, Width: 1400, Height: 900}); err != nil {
		t.Fatal(err)
	}
	if err := SaveWindowState(u, WindowState{X: 0, Y: 0, HasPosition: true, Width: 3000, Height: 2000, Maximized: true}); err != nil {
		t.Fatal(err)
	}
	st := LoadWindowState(u, func(int, int, int, int) bool { return true })
	if !st.Maximized || st.Width != 1400 || st.Height != 900 || st.X != 100 || st.Y != 120 {
		t.Errorf("%+v (maximizing must not overwrite the restored size)", st)
	}
	var raw map[string]any
	b, _ := os.ReadFile(filepath.Join(u, "window-state.json"))
	json.Unmarshal(b, &raw)
	for _, k := range []string{"x", "y", "width", "height", "maximized"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("window-state.json lacks %q (same keys as the Electron build)", k)
		}
	}
}

// ---------- copying data ----------

func TestCopyDataFolderCopiesOnlyTheDataParts(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(from, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	write("meta.json", "{}")
	write("config/settings.json", "s")
	write("data/reading/a.json", "[]")
	write("activity/2026-01.jsonl", "x")
	write("backups/backup.zip", "b")
	write("anilist/cache/x.json", "c")
	write("logs/app.log", "l")
	if err := CopyDataFolder(from, to); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"meta.json", "config/settings.json", "data/reading/a.json", "activity/2026-01.jsonl"} {
		if _, err := os.Stat(filepath.Join(to, rel)); err != nil {
			t.Errorf("%s was not copied", rel)
		}
	}
	for _, rel := range []string{"backups", "anilist", "logs"} {
		if _, err := os.Stat(filepath.Join(to, rel)); err == nil {
			t.Errorf("%s must stay behind", rel)
		}
	}
	if err := CopyDataFolder(t.TempDir(), t.TempDir()); err != nil {
		t.Errorf("an empty source is fine: %v", err)
	}
}

// ---------- bridge and assets ----------

func TestBridgeScriptEmbedsValuesSafely(t *testing.T) {
	js := BridgeScript(`http://127.0.0.1:1234`, `tok"</script><script>alert(1)`)
	if strings.Contains(js, `tok"</script>`) || !strings.Contains(js, `"http://127.0.0.1:1234"`) {
		t.Errorf("token must be JSON-escaped:\n%s", js)
	}
	for _, want := range []string{"window.lorekeeper", "baseUrl", "token", "platform", "openDataDir", "chooseDataDir", "setTheme", "onMenu", "lorekeeper:menu", "BrowserOpenURL"} {
		if !strings.Contains(js, want) {
			t.Errorf("bridge misses %q", want)
		}
	}
	if runtime.GOOS == "windows" && !strings.Contains(js, `"win32"`) {
		t.Error("the web app expects Node's platform names")
	}
}

func TestAssetsMiddleware(t *testing.T) {
	ready := false
	mw := AssetsMiddleware(func() (string, string, bool) { return "http://127.0.0.1:9", "T0KEN", ready })
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<html><head><meta charset="utf-8"></head><body>hi</body></html>`)
		case "/missing":
			http.NotFound(w, r)
		default:
			io.WriteString(w, "asset:"+r.URL.Path)
		}
	}))
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}
	if get(BridgePath).Code != 503 {
		t.Error("the bridge script must not be served before the API is up")
	}
	ready = true
	rec := get(BridgePath)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "T0KEN") || rec.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Errorf("script: %d %v", rec.Code, rec.Header())
	}
	for _, p := range []string{"/", "/index.html"} {
		page := get(p)
		body := page.Body.String()
		if !strings.Contains(body, `<script src="/__lorekeeper.js"></script></head>`) || page.Header().Get("Content-Length") != itoa(len(body)) {
			t.Errorf("%s: %q (%s)", p, body, page.Header().Get("Content-Length"))
		}
	}
	if got := get("/assets/app.js").Body.String(); got != "asset:/assets/app.js" {
		t.Errorf("other files pass through: %q", got)
	}
	if get("/missing").Code != 404 {
		t.Error("404s pass through")
	}
}

// ---------- menu ----------

func flatten(items []MenuItem) []MenuItem {
	var out []MenuItem
	for _, it := range items {
		out = append(out, it)
		out = append(out, flatten(it.Submenu)...)
	}
	return out
}

func TestMenuHasTheSameActionsAndShortcutsOnEveryOS(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		items := flatten(BuildMenu(goos))
		actions := map[string]string{}
		accels := map[string]bool{}
		for _, it := range items {
			if it.Action != "" {
				actions[it.Action] = it.Accelerator
			}
			if it.Accelerator != "" {
				if accels[it.Accelerator] {
					t.Errorf("%s: duplicate shortcut %s", goos, it.Accelerator)
				}
				accels[it.Accelerator] = true
			}
		}
		for action, accel := range map[string]string{
			ActionNew: "CmdOrCtrl+N", ActionImport: "CmdOrCtrl+Shift+I", ActionDashboard: "CmdOrCtrl+1",
			ActionLibrary: "CmdOrCtrl+2", ActionPalette: "CmdOrCtrl+K", ActionHelp: "", ActionOpenData: "",
		} {
			if got, ok := actions[action]; !ok || got != accel {
				t.Errorf("%s: %s has shortcut %q (present=%v), want %q", goos, action, got, ok, accel)
			}
		}
		want := "Ctrl+,"
		if goos == "darwin" {
			want = "Cmd+,"
		}
		if actions[ActionSettings] != want {
			t.Errorf("%s: settings shortcut %q", goos, actions[ActionSettings])
		}
		hasRole := func(r string) bool {
			for _, it := range items {
				if it.Role == r {
					return true
				}
			}
			return false
		}
		if hasRole("app") != (goos == "darwin") || hasRole("window") != (goos == "darwin") || hasRole("quit") != (goos != "darwin") || !hasRole("edit") {
			t.Errorf("%s: roles app=%v window=%v quit=%v edit=%v", goos, hasRole("app"), hasRole("window"), hasRole("quit"), hasRole("edit"))
		}
	}
}

// ---------- the running app (no window) ----------

func startCore(t *testing.T, mod func(*Options)) *Core {
	t.Helper()
	o := Options{AppVersion: "9.9.9", UserData: t.TempDir(), DataDir: t.TempDir()}
	if mod != nil {
		mod(&o)
	}
	c, err := Start(o)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func call(t *testing.T, c *Core, path, token, origin string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", c.BaseURL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestCoreServesTheAPIOnLoopbackWithAPerLaunchToken(t *testing.T) {
	c := startCore(t, nil)
	defer c.Shutdown()
	if !strings.HasPrefix(c.BaseURL, "http://127.0.0.1:") || len(c.Token) != 64 {
		t.Fatalf("base %s token %q", c.BaseURL, c.Token)
	}
	if res := call(t, c, "/api/v1/meta", "", ""); res.StatusCode != 401 {
		t.Errorf("no token: %d", res.StatusCode)
	}
	if res := call(t, c, "/api/v1/meta", "wrong", ""); res.StatusCode != 401 {
		t.Errorf("wrong token: %d", res.StatusCode)
	}
	res := call(t, c, "/api/v1/meta", c.Token, "")
	var meta map[string]any
	json.NewDecoder(res.Body).Decode(&meta)
	if res.StatusCode != 200 || meta["appVersion"] != "9.9.9" || meta["dataDir"] != c.DataDir {
		t.Errorf("meta %d %v", res.StatusCode, meta)
	}
	for _, origin := range WebviewOrigins {
		if got := call(t, c, "/api/v1/meta", c.Token, origin).Header.Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("CORS for %s: %q", origin, got)
		}
	}
	if got := call(t, c, "/api/v1/meta", c.Token, "http://evil.example").Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("a foreign origin must get no CORS header, got %q", got)
	}
	if res := call(t, c, "/api/v1/anilist/status", c.Token, ""); res.StatusCode != 200 {
		t.Errorf("AniList routes must be registered: %d", res.StatusCode)
	}
	if res := call(t, c, "/api/v1/export/csv-template?library=reading", c.Token, ""); res.StatusCode != 200 {
		t.Errorf("import/export routes must be registered: %d", res.StatusCode)
	}
}

func TestCoreTokensAndPortsAreFreshEachLaunch(t *testing.T) {
	a := startCore(t, nil)
	b := startCore(t, nil)
	defer a.Shutdown()
	defer b.Shutdown()
	if a.Token == b.Token || a.BaseURL == b.BaseURL {
		t.Error("token and port must differ between launches")
	}
}

func TestCoreRefusesASecondProcessOnTheSameDataFolderAndReleasesOnShutdown(t *testing.T) {
	dir, user := t.TempDir(), t.TempDir()
	c := startCore(t, func(o *Options) { o.DataDir, o.UserData = dir, user })
	// simulate "another process": the lock names our parent, which is alive
	os.WriteFile(filepath.Join(dir, ".lorekeeper.lock"), []byte(itoa(os.Getppid())), 0o644)
	if _, err := Start(Options{UserData: user, DataDir: dir}); err == nil || !strings.Contains(err.Error(), "already using this data folder") || !strings.Contains(err.Error(), dir) {
		t.Errorf("second start: %v", err)
	}
	os.WriteFile(filepath.Join(dir, ".lorekeeper.lock"), []byte(itoa(os.Getpid())), 0o644)
	c.Shutdown()
	if _, err := os.Stat(filepath.Join(dir, ".lorekeeper.lock")); !os.IsNotExist(err) {
		t.Error("shutdown must release the lock")
	}
	c2, err := Start(Options{UserData: user, DataDir: dir})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	c2.Shutdown()
}

func TestCoreShutdownEndsEventStreamsAndStopsServing(t *testing.T) {
	c := startCore(t, nil)
	res := call(t, c, "/api/v1/events", c.Token, WebviewOrigins[0])
	if res.StatusCode != 200 || res.Header.Get("Access-Control-Allow-Origin") != WebviewOrigins[0] {
		t.Fatalf("events: %d %v", res.StatusCode, res.Header)
	}
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, res.Body); close(done) }()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	c.Shutdown()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the event stream did not end")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("shutdown took %v", time.Since(start))
	}
	if _, err := http.Get(c.BaseURL + "/api/v1/meta"); err == nil {
		t.Error("the server must be down after Shutdown")
	}
}

func TestCoreLogsStartupInvalidFilesAndShutdownWithoutEntryContent(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "config"), 0o755)
	os.WriteFile(filepath.Join(dir, "config", "settings.json"), []byte("{ broken"), 0o644)
	c := startCore(t, func(o *Options) { o.DataDir = dir })
	c.Shutdown()
	b, err := os.ReadFile(filepath.Join(dir, "logs", "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(b)
	for _, want := range []string{"INFO starting Lorekeeper 9.9.9", "INFO api listening on 127.0.0.1:", "WARN invalid file config/settings.json", "INFO stopped"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestCoreResolvesFoldersFromTheEnvironmentWhenNotGiven(t *testing.T) {
	user, data := t.TempDir(), t.TempDir()
	t.Setenv("LOREKEEPER_USER_DATA", user)
	t.Setenv("LOREKEEPER_DATA_DIR", data)
	c, err := Start(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()
	if c.UserData != abs(user) || c.DataDir != filepath.Clean(data) {
		t.Errorf("user %s data %s", c.UserData, c.DataDir)
	}
	// without the env override, the folder chosen in Settings is used
	t.Setenv("LOREKEEPER_DATA_DIR", "")
	chosen := t.TempDir()
	WriteBootstrap(user, chosen)
	c.Shutdown()
	c2, err := Start(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Shutdown()
	if c2.DataDir != filepath.Clean(chosen) {
		t.Errorf("bootstrap choice ignored: %s", c2.DataDir)
	}
}

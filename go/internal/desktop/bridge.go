package desktop

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"runtime"
	"strconv"
	"strings"
)

// BridgeScript is the page-side half of the native bridge (window.lorekeeper): where the API lives,
// the per-launch token, and a few native actions. It mirrors what the Electron preload exposed, so
// the web app is unchanged. It also sends clicks on external links to the system browser.
func BridgeScript(baseURL, token string) string {
	platform := runtime.GOOS
	if platform == "windows" {
		platform = "win32" // the web app uses Node's names
	}
	js := func(v string) string { b, _ := json.Marshal(v); return string(b) }
	return `(function () {
  var app = function () { return window.go && window.go.main && window.go.main.App; };
  window.lorekeeper = {
    baseUrl: ` + js(baseURL) + `,
    token: ` + js(token) + `,
    // Images load from the page's own origin: WebKit will not show an http:// <img> inside a wails:// page.
    imageBase: location.origin,
    platform: ` + js(platform) + `,
    openDataDir: function () { return app().OpenDataDir(); },
    chooseDataDir: function (copyExisting) { return app().ChooseDataDir(copyExisting === true); },
    setTheme: function (theme) { app().SetTheme(theme); },
    onMenu: function (cb) {
      var off = window.runtime.EventsOn('lorekeeper:menu', cb);
      return function () { if (off) off(); };
    }
  };
  function external(url) {
    try { return /^https?:$/.test(new URL(url, location.href).protocol) && new URL(url, location.href).origin !== location.origin; }
    catch (e) { return false; }
  }
  document.addEventListener('click', function (e) {
    var a = e.target && e.target.closest && e.target.closest('a[href]');
    if (a && external(a.href)) { e.preventDefault(); window.runtime.BrowserOpenURL(a.href); }
  }, true);
  var open = window.open;
  window.open = function (url) {
    if (url && external(String(url))) { window.runtime.BrowserOpenURL(String(url)); return null; }
    return open.apply(window, arguments);
  };
})();
`
}

// BridgePath is where the page loads the bridge script from (a same-origin script, so the strict
// script-src 'self' policy of the built page still holds).
const BridgePath = "/__lorekeeper.js"

// ImagePaths are the API routes <img> tags use. The page asks for them on its own origin (WebKit
// blocks http:// images in a wails:// page) and the middleware forwards them to the local API.
func isImagePath(p string) bool {
	return strings.HasPrefix(p, "/api/v1/anilist/cover/") || p == "/api/v1/anilist/image"
}

// AssetsMiddleware serves the bridge script and inserts its <script> tag into index.html. config
// is read on every request because the API only starts after the window machinery does.
func AssetsMiddleware(config func() (baseURL, token string, ok bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == BridgePath:
				baseURL, token, ok := config()
				if !ok {
					http.Error(w, "starting", http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				w.Write([]byte(BridgeScript(baseURL, token)))
			case r.Method == http.MethodGet && isImagePath(r.URL.Path):
				baseURL, _, ok := config()
				target, err := url.Parse(baseURL)
				if !ok || err != nil {
					http.Error(w, "starting", http.StatusServiceUnavailable)
					return
				}
				httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
			case r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/index.html"):
				rec := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
				next.ServeHTTP(rec, r)
				body := rec.body.Bytes()
				if rec.status == http.StatusOK && bytes.Contains(body, []byte("</head>")) {
					tag := `<script src="` + BridgePath + `"></script>`
					body = []byte(strings.Replace(string(body), "</head>", tag+"</head>", 1))
				}
				for k, v := range rec.header {
					w.Header()[k] = v
				}
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				w.WriteHeader(rec.status)
				w.Write(body)
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header         { return b.header }
func (b *bufferedResponse) WriteHeader(code int)        { b.status = code }
func (b *bufferedResponse) Write(p []byte) (int, error) { return b.body.Write(p) }

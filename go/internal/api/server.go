package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/reeper316099/Lorekeeper/go/internal/search"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

type Options struct {
	Store *store.Store
	Token string
	// AllowedOrigins are the exact origins CORS accepts (the packaged app origin, the Vite dev server).
	AllowedOrigins []string
	// StaticDir is the built web app served at / (public; the API stays behind the token).
	StaticDir string
	// Extend registers more route modules (importers/exporters, AniList) under /api/v1.
	Extend func(rt *Router, s *Server)
}

// Server is the HTTP app. Use Handler() with net/http, and Close() before shutting the listener
// down so open event streams end.
type Server struct {
	Store   *store.Store
	Index   *search.Index
	Querier *search.Querier
	opts    Options
	router  *Router
	token   []byte
	allowed []string

	closing   chan struct{}
	closeOnce sync.Once
}

const apiPrefix = "/api/v1"

func New(opts Options) *Server {
	idx := search.NewIndex(opts.Store)
	s := &Server{
		Store: opts.Store, Index: idx, Querier: search.NewQuerier(opts.Store, idx),
		opts: opts, router: NewRouter(apiPrefix), token: []byte(opts.Token), allowed: opts.AllowedOrigins,
		closing: make(chan struct{}),
	}
	s.registerRoutes()
	if opts.Extend != nil {
		opts.Extend(s.router, s)
	}
	return s
}

// Close ends every open event stream.
func (s *Server) Close() { s.closeOnce.Do(func() { close(s.closing) }) }

func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }

var reBearer = regexp.MustCompile(`(?i)^Bearer\s+`)

// queryTokenOK: EventSource and <img> cannot set headers, so a few read-only routes accept ?token=.
func queryTokenOK(p string) bool {
	return strings.HasSuffix(p, "/events") || strings.Contains(p, "/anilist/cover/") || strings.HasSuffix(p, "/anilist/image")
}

func (s *Server) authorized(r *http.Request) bool {
	given := ""
	if h := r.Header.Get("Authorization"); h != "" {
		given = reBearer.ReplaceAllString(h, "")
	} else if queryTokenOK(r.URL.Path) {
		given = r.URL.Query().Get("token")
	}
	g := []byte(given)
	return len(g) == len(s.token) && subtle.ConstantTimeCompare(g, s.token) == 1
}

func (s *Server) originAllowed(origin string) bool {
	return origin != "" && slices.Contains(s.allowed, origin)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Request-Id", uuid.NewString())
	if origin := r.Header.Get("Origin"); s.originAllowed(origin) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	} else if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/") && !s.authorized(r) {
		writeError(w, &store.HTTPError{Status: 401, Code: "UNAUTHORIZED", Message: "Missing or invalid token"})
		return
	}
	if rt, params := s.router.match(r.Method, r.URL.EscapedPath()); rt != nil {
		if rt.raw != nil {
			rt.raw(w, r, params)
			return
		}
		c := &Ctx{W: w, R: r, Params: params, maxBody: rt.maxBody}
		val, err := rt.handler(c)
		if err != nil {
			writeError(w, err)
			return
		}
		if d, ok := val.(*Download); ok {
			w.Header().Set("Content-Type", d.ContentType)
			w.Header().Set("Content-Disposition", `attachment; filename="`+d.Name+`"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(d.Body)))
			w.WriteHeader(http.StatusOK)
			w.Write(d.Body)
			return
		}
		status := c.Status
		if status == 0 {
			status = http.StatusOK
		}
		writeJSON(w, status, val)
		return
	}
	if s.opts.StaticDir != "" && !strings.HasPrefix(r.URL.Path, "/api/") && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		if s.serveStatic(w, r) {
			return
		}
	}
	writeError(w, &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "Route not found"})
}

// serveStatic serves files from StaticDir ("/" is index.html); false when there is no such file.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) bool {
	p := path.Clean("/" + r.URL.Path)
	if p == "/" {
		p = "/index.html"
	}
	file := filepath.Join(s.opts.StaticDir, filepath.FromSlash(p))
	if st, err := os.Stat(file); err != nil || st.IsDir() {
		return false
	}
	http.ServeFile(w, r, file)
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(bytes.TrimRight(buf.Bytes(), "\n"))
}

func writeError(w http.ResponseWriter, err error) {
	status, body := classify(err)
	writeJSON(w, status, map[string]any{"error": body})
}

// Download is a handler result that is sent as a file attachment instead of JSON.
type Download struct {
	Name, ContentType string
	Body              []byte
}

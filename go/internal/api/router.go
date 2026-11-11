// Package api is the HTTP layer: the same /api/v1 routes, bearer-token auth, CORS lock, error shape
// and server-sent events as the Fastify app in packages/server/src/api/app.ts.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Ctx is one request. Handlers return a value (written as JSON, status 200 unless Status is set)
// or an error (mapped to the uniform error shape).
type Ctx struct {
	W      http.ResponseWriter
	R      *http.Request
	Params map[string]string
	Status int

	maxBody int64
}

func (c *Ctx) Query() url.Values { return c.R.URL.Query() }

// bodyBytes reads the request body, bounded by the route's limit.
func (c *Ctx) bodyBytes() ([]byte, error) {
	if c.R.Body == nil {
		return nil, nil
	}
	return io.ReadAll(http.MaxBytesReader(c.W, c.R.Body, c.maxBody))
}

// Object reads the body as a JSON object (error if it is anything else, or empty).
func (c *Ctx) Object() (map[string]any, error) {
	b, err := c.bodyBytes()
	if err != nil {
		return nil, err
	}
	return parseObject(b)
}

func parseObject(b []byte) (map[string]any, error) {
	t := bytes.TrimSpace(b)
	if len(t) == 0 {
		return nil, invalidf("body", "Required")
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(t))
	if err := dec.Decode(&v); err != nil {
		return nil, &badRequest{msg: jsonErrorMessage(err)}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, invalidf("body", "Expected object")
	}
	return m, nil
}

func jsonErrorMessage(err error) string {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return "Invalid JSON: " + se.Error()
	}
	return "Invalid JSON body"
}

type HandlerFunc func(*Ctx) (any, error)

type route struct {
	method  string
	segs    []string
	handler HandlerFunc
	raw     func(w http.ResponseWriter, r *http.Request, params map[string]string)
	maxBody int64
	literal int
}

// Router matches method + path like Fastify: static segments beat :params, no trailing-slash
// tolerance, and anything unmatched is a JSON 404 (never a 405).
type Router struct {
	prefix string
	routes []*route
}

const defaultMaxBody = 1 << 20 // Fastify's default body limit

func NewRouter(prefix string) *Router { return &Router{prefix: prefix} }

func (rt *Router) add(method, pattern string, r *route) {
	r.method = method
	r.segs = strings.Split(strings.TrimPrefix(rt.prefix+pattern, "/"), "/")
	for _, s := range r.segs {
		if !strings.HasPrefix(s, ":") {
			r.literal++
		}
	}
	if r.maxBody == 0 {
		r.maxBody = defaultMaxBody
	}
	rt.routes = append(rt.routes, r)
}

func (rt *Router) Handle(method, pattern string, h HandlerFunc) {
	rt.add(method, pattern, &route{handler: h})
}

// HandleLimit is Handle with a larger body limit (uploads).
func (rt *Router) HandleLimit(method, pattern string, maxBody int64, h HandlerFunc) {
	rt.add(method, pattern, &route{handler: h, maxBody: maxBody})
}

func (rt *Router) GET(p string, h HandlerFunc)    { rt.Handle(http.MethodGet, p, h) }
func (rt *Router) POST(p string, h HandlerFunc)   { rt.Handle(http.MethodPost, p, h) }
func (rt *Router) PUT(p string, h HandlerFunc)    { rt.Handle(http.MethodPut, p, h) }
func (rt *Router) PATCH(p string, h HandlerFunc)  { rt.Handle(http.MethodPatch, p, h) }
func (rt *Router) DELETE(p string, h HandlerFunc) { rt.Handle(http.MethodDelete, p, h) }

// Raw registers a handler that writes its own response (streams, files).
func (rt *Router) Raw(method, pattern string, h func(w http.ResponseWriter, r *http.Request, params map[string]string)) {
	rt.add(method, pattern, &route{raw: h})
}

func (rt *Router) match(method, path string) (*route, map[string]string) {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var best *route
	var bestParams map[string]string
	for _, r := range rt.routes {
		if r.method != method || len(r.segs) != len(segs) {
			continue
		}
		if best != nil && r.literal <= best.literal {
			continue
		}
		params := map[string]string{}
		ok := true
		for i, s := range r.segs {
			if strings.HasPrefix(s, ":") {
				v, err := url.PathUnescape(segs[i])
				if err != nil || v == "" {
					ok = false
					break
				}
				params[s[1:]] = v
			} else if s != segs[i] {
				ok = false
				break
			}
		}
		if ok {
			best, bestParams = r, params
		}
	}
	return best, bestParams
}

package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// roundTripper turns a function into an http.RoundTripper.
type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func httpDoer(f roundTripper) Doer { return &http.Client{Transport: f} }

func response(status int, body string, headers map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func okJSON(data string, headers map[string]string) *http.Response {
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Type"] = "application/json"
	return response(200, `{"data":`+data+`}`, headers)
}

// fakeClock makes the client run on virtual time: sleeping just advances it.
type fakeClock struct {
	mu     sync.Mutex
	now    int64
	sleeps []int64
}

func (c *fakeClock) Now() int64 { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) Sleep(ctx context.Context, ms int64) error {
	if ctx.Err() != nil {
		return CancelledError{}
	}
	c.mu.Lock()
	c.sleeps = append(c.sleeps, ms)
	c.now += ms
	c.mu.Unlock()
	return nil
}

func (c *fakeClock) maxSleep() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var m int64
	for _, s := range c.sleeps {
		m = max(m, s)
	}
	return m
}

func tmpClient(t *testing.T, doer Doer, mod func(*ClientOptions)) (*Client, *fakeClock, string) {
	t.Helper()
	dir := t.TempDir()
	clock := &fakeClock{now: 1_000_000}
	o := ClientOptions{DataDir: dir, HTTP: doer, Now: clock.Now, Sleep: clock.Sleep}
	if mod != nil {
		mod(&o)
	}
	return NewClient(o), clock, dir
}

// ---- a tiny fake AniList that understands the three queries the app sends ----

type world struct {
	Catalog []Media                           `json:"catalog"`
	Lists   map[string]map[string][]ListEntry `json:"lists"`

	mu       sync.Mutex
	Offline  bool
	Delay    time.Duration
	Searches []string
}

func loadWorld(t testing.TB) *world {
	t.Helper()
	b, err := os.ReadFile("testdata/anilist-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		World world `json:"world"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return &doc.World
}

func (w *world) searches() int { w.mu.Lock(); defer w.mu.Unlock(); return len(w.Searches) }

func (w *world) setOffline(v bool)        { w.mu.Lock(); w.Offline = v; w.mu.Unlock() }
func (w *world) setDelay(d time.Duration) { w.mu.Lock(); w.Delay = d; w.mu.Unlock() }

func (w *world) transport(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	offline, delay := w.Offline, w.Delay
	w.mu.Unlock()
	if offline {
		return nil, io.ErrUnexpectedEOF
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	if strings.HasPrefix(r.URL.String(), "https://s4.anilist.co/") {
		resp := response(200, string([]byte{9, 9}), map[string]string{"Content-Type": "image/jpeg"})
		return resp, nil
	}
	var req struct {
		Query     string `json:"query"`
		Variables struct {
			UserName string  `json:"userName"`
			Type     string  `json:"type"`
			ID       int     `json:"id"`
			Search   string  `json:"search"`
			Country  *string `json:"country"`
		} `json:"variables"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		return response(400, "{}", nil), nil
	}
	fail := func(msg string, status int) (*http.Response, error) {
		b, _ := json.Marshal(map[string]any{"errors": []map[string]any{{"message": msg, "status": status}}})
		return response(status, string(b), nil), nil
	}
	out := func(v any) (*http.Response, error) {
		b, _ := json.Marshal(v)
		return okJSON(string(b), nil), nil
	}
	v := req.Variables
	switch {
	case strings.Contains(req.Query, "MediaListCollection"):
		l, ok := w.Lists[v.UserName]
		if !ok {
			return fail("User not found", 404)
		}
		entries := l[v.Type]
		if entries == nil {
			entries = []ListEntry{}
		}
		custom := entries[:min(1, len(entries))]
		return out(map[string]any{"MediaListCollection": map[string]any{"hasNextChunk": false, "lists": []map[string]any{
			{"name": "All", "isCustomList": false, "entries": entries},
			{"name": "Faves", "isCustomList": true, "entries": custom},
		}}})
	case strings.Contains(req.Query, "Media(id"):
		for _, m := range w.Catalog {
			if m.ID == v.ID {
				return out(map[string]any{"Media": m})
			}
		}
		return fail("Not Found.", 404)
	}
	w.mu.Lock()
	w.Searches = append(w.Searches, v.Search)
	w.mu.Unlock()
	q := strings.ToLower(v.Search)
	hits := []Media{}
	for _, m := range w.Catalog {
		if m.Type != v.Type || (v.Country != nil && *v.Country != "" && deref(m.CountryOfOrigin) != *v.Country) {
			continue
		}
		titles := append([]string{deref(m.Title.Romaji), deref(m.Title.English)}, m.Synonyms...)
		for _, t := range titles {
			if t != "" && (strings.Contains(strings.ToLower(t), q) || strings.Contains(q, strings.ToLower(t))) {
				hits = append(hits, m)
				break
			}
		}
	}
	return out(map[string]any{"Page": map[string]any{"media": hits}})
}

// worldClient is a client on the fake world with virtual time and no real sleeping.
func worldClient(t testing.TB, w *world, dir string) *Client {
	return NewClient(ClientOptions{
		DataDir: dir, HTTP: httpDoer(w.transport), MaxRetries: 1,
		Sleep: func(ctx context.Context, ms int64) error {
			if ctx.Err() != nil {
				return CancelledError{}
			}
			return nil
		},
	})
}

func newStore(t testing.TB) *store.Store {
	t.Helper()
	s := store.New(t.TempDir(), store.Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func mediaByID(w *world, id int) Media {
	for _, m := range w.Catalog {
		if m.ID == id {
			return m
		}
	}
	panic("no such media")
}

var _ = bytes.NewReader

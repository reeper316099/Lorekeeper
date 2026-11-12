package anilist

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Ported from packages/server/test/anilist-client.test.ts (client part).

func TestRateLimitNeverExceedsTheBudget(t *testing.T) {
	var clock *fakeClock
	var seen []int64
	doer := httpDoer(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, clock.Now())
		return okJSON(`{"n":1}`, nil), nil
	})
	c, clk, _ := tmpClient(t, doer, func(o *ClientOptions) { o.MaxPerMinute = 3 })
	clock = clk
	start := clock.Now()
	for i := 0; i < 7; i++ {
		if _, err := c.Request(context.Background(), "q", nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 3; i < len(seen); i++ {
		if seen[i]-seen[i-3] < 60_000 {
			t.Errorf("request %d was sent %d ms after request %d (budget is 3 per minute)", i, seen[i]-seen[i-3], i-3)
		}
	}
	if clock.Now()-start < 120_000 {
		t.Errorf("7 requests at 3/min took only %d ms", clock.Now()-start)
	}
}

func TestConcurrentCallersShareOneBudget(t *testing.T) {
	var calls atomic.Int32
	c, clock, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return okJSON(`{}`, nil), nil
	}), func(o *ClientOptions) { o.MaxPerMinute = 3 })
	start := clock.Now()
	done := make(chan error, 9)
	for i := 0; i < 9; i++ {
		go func() { _, err := c.Request(context.Background(), "q", nil); done <- err }()
	}
	for i := 0; i < 9; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 9 || clock.Now()-start < 120_000 {
		t.Errorf("calls=%d elapsed=%d ms (9 requests at 3/min need two full waits)", calls.Load(), clock.Now()-start)
	}
}

func TestAdoptsTheLowerLimitFromHeaders(t *testing.T) {
	c, _, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		return okJSON(`{}`, map[string]string{"X-RateLimit-Limit": "30", "X-RateLimit-Remaining": "29"}), nil
	}), func(o *ClientOptions) { o.MaxPerMinute = 60 })
	if _, err := c.Request(context.Background(), "q", nil); err != nil {
		t.Fatal(err)
	}
	if c.CurrentLimit() != 30 {
		t.Errorf("limit %d", c.CurrentLimit())
	}
}

func TestWaitsOutA429UsingRetryAfter(t *testing.T) {
	calls := 0
	c, clock, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(429, "{}", map[string]string{"Retry-After": "7"}), nil
		}
		return okJSON(`{"done":true}`, nil), nil
	}), nil)
	raw, err := c.Request(context.Background(), "q", nil)
	if err != nil || string(raw) != `{"done":true}` || calls != 2 {
		t.Fatalf("%s %v calls=%d", raw, err, calls)
	}
	if clock.maxSleep() < 7000 {
		t.Errorf("waited only %d ms", clock.maxSleep())
	}
}

func TestGivesUpWithRateLimitErrorAfterRepeated429s(t *testing.T) {
	c, _, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		return response(429, "{}", map[string]string{"Retry-After": "1"}), nil
	}), nil)
	_, err := c.Request(context.Background(), "q", nil)
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfterSec != 1 {
		t.Fatalf("got %v", err)
	}
}

func TestRetries5xxWithExponentialBackoff(t *testing.T) {
	calls := 0
	c, clock, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return response(503, "x", nil), nil
		}
		return okJSON(`{"fine":1}`, nil), nil
	}), nil)
	if _, err := c.Request(context.Background(), "q", nil); err != nil {
		t.Fatal(err)
	}
	var backoffs []int64
	for _, s := range clock.sleeps {
		if s == 1000 || s == 2000 {
			backoffs = append(backoffs, s)
		}
	}
	if !slices.Equal(backoffs, []int64{1000, 2000}) {
		t.Errorf("backoffs %v", backoffs)
	}
}

func TestDoesNotRetryGraphQLOr4xxErrors(t *testing.T) {
	var calls atomic.Int32
	c, _, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(404, `{"errors":[{"message":"Not Found.","status":404}]}`, nil), nil
	}), nil)
	for i := 0; i < 2; i++ {
		_, err := c.Request(context.Background(), "q", nil)
		var ae *AniListError
		if !errors.As(err, &ae) || ae.Status != 404 || ae.Message != "Not Found." {
			t.Fatalf("got %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Errorf("calls %d (each request must be sent exactly once)", calls.Load())
	}
}

func TestReportsOfflineAfterRetriesAndCanBeCancelled(t *testing.T) {
	c, _, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) { return nil, errors.New("down") }), nil)
	_, err := c.Request(context.Background(), "q", nil)
	if !errors.As(err, &OfflineError{}) || !c.Offline() {
		t.Fatalf("got %v offline=%v", err, c.Offline())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Request(ctx, "q", nil); !errors.As(err, &CancelledError{}) {
		t.Errorf("got %v", err)
	}
}

func TestSendsTheDocumentedRequestShape(t *testing.T) {
	var method, ct, accept, body string
	c, _, _ := tmpClient(t, httpDoer(func(r *http.Request) (*http.Response, error) {
		method, ct, accept = r.Method, r.Header.Get("Content-Type"), r.Header.Get("Accept")
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		body = string(b[:n])
		return okJSON(`{}`, nil), nil
	}), nil)
	if _, err := c.Request(context.Background(), "query { x }", map[string]any{"id": 5}); err != nil {
		t.Fatal(err)
	}
	if method != "POST" || ct != "application/json" || accept != "application/json" || body != `{"query":"query { x }","variables":{"id":5}}` {
		t.Errorf("%s %s %s %s", method, ct, accept, body)
	}
}

func TestRealTimeoutBecomesOffline(t *testing.T) {
	c := NewClient(ClientOptions{
		DataDir: t.TempDir(), MaxRetries: 1, Timeout: 30 * time.Millisecond,
		HTTP: httpDoer(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}),
		Sleep: func(context.Context, int64) error { return nil },
	})
	if _, err := c.Request(context.Background(), "q", nil); !errors.As(err, &OfflineError{}) {
		t.Fatalf("got %v", err)
	}
}

func TestServesFromTheDiskCacheWithinTheTTLAndStaleDataWhenOffline(t *testing.T) {
	calls, offline := 0, false
	c, clock, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		calls++
		if offline {
			return nil, errors.New("down")
		}
		return okJSON(`{"v":`+string(rune('0'+calls))+`}`, nil), nil
	}), func(o *ClientOptions) { o.TTL = time.Second })
	type v struct{ V int }
	a, err1 := CachedRequest[v](context.Background(), c, "media-1", "q", nil)
	b, err2 := CachedRequest[v](context.Background(), c, "media-1", "q", nil)
	if err1 != nil || err2 != nil || a.Cached || !b.Cached || calls != 1 {
		t.Fatalf("a=%+v b=%+v calls=%d %v %v", a, b, calls, err1, err2)
	}
	clock.now += 5000
	offline = true
	stale, err := CachedRequest[v](context.Background(), c, "media-1", "q", nil)
	if err != nil || !stale.Cached || !stale.Stale || stale.Data.V != 1 {
		t.Errorf("stale: %+v %v", stale, err)
	}
	if _, err := CachedRequest[v](context.Background(), c, "media-2", "q", nil); !errors.As(err, &OfflineError{}) {
		t.Errorf("uncached while offline: %v", err)
	}
}

func TestOnlyFetchesImagesFromTheAllowListedHostAndCachesThem(t *testing.T) {
	calls := 0
	c, _, dir := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, string([]byte{1, 2, 3}), map[string]string{"Content-Type": "image/jpeg"}), nil
	}), nil)
	ctx := context.Background()
	for _, bad := range []string{"https://evil.example/x.jpg", "http://s4.anilist.co/x.jpg", "https://s4.anilist.co.evil.example/x.jpg", "not a url", "ftp://s4.anilist.co/x"} {
		if _, _, err := c.FetchImage(ctx, bad); err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%q must be refused, got %v", bad, err)
		}
	}
	name, err := c.SaveCover(ctx, 42, "https://s4.anilist.co/file/c/42.jpg")
	if err != nil || name != "42.jpg" {
		t.Fatalf("%q %v", name, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "covers", name)); !slices.Equal(b, []byte{1, 2, 3}) {
		t.Errorf("cover bytes %v", b)
	}
	if _, _, err := c.FetchImage(ctx, "https://s4.anilist.co/file/c/42.jpg"); err != nil || calls != 1 {
		t.Errorf("second fetch must come from the cache: calls=%d err=%v", calls, err)
	}
}

func TestImageFetchRejectsNonImagesAndOversizedFiles(t *testing.T) {
	c, _, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		return response(200, "<html>", map[string]string{"Content-Type": "text/html"}), nil
	}), nil)
	if _, _, err := c.FetchImage(context.Background(), "https://s4.anilist.co/a"); err == nil {
		t.Error("html must not be cached as an image")
	}
	c2, _, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) {
		return response(200, strings.Repeat("x", 100), map[string]string{"Content-Type": "image/png"}), nil
	}), nil)
	c2.imageMax = 50
	var ae *AniListError
	if _, _, err := c2.FetchImage(context.Background(), "https://s4.anilist.co/a"); !errors.As(err, &ae) || ae.Status != 413 {
		t.Errorf("oversized: %v", err)
	}
	c3, _, _ := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) { return response(404, "", nil), nil }), nil)
	if _, _, err := c3.FetchImage(context.Background(), "https://s4.anilist.co/a"); err == nil {
		t.Error("404 must fail")
	}
}

func TestCleanupHelpers(t *testing.T) {
	c, _, dir := tmpClient(t, httpDoer(func(*http.Request) (*http.Response, error) { return nil, errors.New("x") }), nil)
	os.MkdirAll(filepath.Join(dir, "covers"), 0o755)
	for _, f := range []string{"1.jpg", "2.png", "notes.txt"} {
		os.WriteFile(filepath.Join(dir, "covers", f), []byte("x"), 0o644)
	}
	if c.RemoveCover("../config/settings.json") || c.RemoveCover("notes.txt") || c.RemoveCover("1.exe") {
		t.Error("only plain cover names may be removed")
	}
	if n := c.PruneCovers(map[string]bool{"2.png": true}); n != 1 {
		t.Errorf("pruned %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "covers", "notes.txt")); err != nil {
		t.Error("prune must leave files that are not covers")
	}
	if c.CacheStats() != (CacheStats{}) {
		t.Error("empty cache")
	}
	c.PrimeCache("media-9", map[string]any{"Media": map[string]any{"id": 9}})
	if s := c.CacheStats(); s.Files != 1 || s.Bytes == 0 {
		t.Errorf("stats %+v", s)
	}
	if s := c.ClearCache(); s.Files != 1 || c.CacheStats().Files != 0 {
		t.Errorf("clear %+v", s)
	}
}

// A real HTTP round trip (no fake transport): the client talks to a local server through net/http,
// as LOREKEEPER_ANILIST_ENDPOINT lets development and tests do.
func TestWorksAgainstARealHTTPServer(t *testing.T) {
	w := loadWorld(t)
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		resp, _ := w.transport(r)
		for k, v := range resp.Header {
			rw.Header()[k] = v
		}
		rw.WriteHeader(resp.StatusCode)
		io.Copy(rw, resp.Body)
	}))
	defer ts.Close()
	t.Setenv("LOREKEEPER_ANILIST_ENDPOINT", ts.URL)
	c := NewClient(ClientOptions{DataDir: t.TempDir()})
	r, err := CachedRequest[mediaData](context.Background(), c, "media-1", ByIDQuery, map[string]any{"id": 1})
	if err != nil || r.Data.Media.ID != 1 || deref(r.Data.Media.Title.Romaji) != "Lantern of the Hollow Crown" {
		t.Fatalf("%+v %v", r, err)
	}
	var ae *AniListError
	if _, err := CachedRequest[mediaData](context.Background(), c, "media-404", ByIDQuery, map[string]any{"id": 404}); !errors.As(err, &ae) || ae.Status != 404 {
		t.Errorf("unknown id: %v", err)
	}
	if c.Offline() {
		t.Error("a 404 from a reachable server is not offline")
	}
}

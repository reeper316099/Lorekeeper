package anilist

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// OfflineError means AniList could not be reached.
type OfflineError struct{}

func (OfflineError) Error() string { return "AniList is unreachable (offline?)" }

// RateLimitError means AniList kept answering 429.
type RateLimitError struct{ RetryAfterSec int }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("AniList rate limit hit; retry in %ds", e.RetryAfterSec)
}

// AniListError is an error answer from AniList (GraphQL error, 4xx, repeated 5xx).
type AniListError struct {
	Message string
	Status  int
}

func (e *AniListError) Error() string { return e.Message }

// CancelledError means the caller cancelled the operation.
type CancelledError struct{}

func (CancelledError) Error() string { return "Cancelled" }

// Doer is the HTTP client seam (tests supply a fake transport).
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type ClientOptions struct {
	DataDir  string
	Endpoint string // default https://graphql.anilist.co, or LOREKEEPER_ANILIST_ENDPOINT
	HTTP     Doer
	// MaxPerMinute is the request budget. AniList documents 90/min but is currently degraded to 30/min;
	// the real limit from the response headers wins if it is lower.
	MaxPerMinute int
	TTL          time.Duration
	MaxRetries   int
	Timeout      time.Duration
	// Now (unix ms) and Sleep are seams so tests can run on a fake clock.
	Now   func() int64
	Sleep func(ctx context.Context, ms int64) error
}

const week = 7 * 24 * time.Hour

// ImageHosts are the only hosts artwork may be fetched from.
var ImageHosts = map[string]bool{"s4.anilist.co": true}

var extByType = [][2]string{{"image/jpeg", "jpg"}, {"image/png", "png"}, {"image/webp", "webp"}, {"image/gif", "gif"}}

func extFor(contentType string) string {
	for _, p := range extByType {
		if p[0] == contentType {
			return p[1]
		}
	}
	return ""
}

func defaultSleep(ctx context.Context, ms int64) error {
	t := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return CancelledError{}
	case <-t.C:
		return nil
	}
}

// Client is a rate-limited, cached, cancellable AniList GraphQL client. It never logs query
// variables (they can contain titles from the user's library).
type Client struct {
	o        ClientOptions
	gate     store.KeyedMutex
	mu       sync.Mutex
	limit    int
	stamps   []int64
	blocked  int64
	offline  atomic.Bool
	imageMax int64
}

func NewClient(o ClientOptions) *Client {
	if o.Endpoint == "" {
		o.Endpoint = os.Getenv("LOREKEEPER_ANILIST_ENDPOINT")
	}
	if o.Endpoint == "" {
		o.Endpoint = "https://graphql.anilist.co"
	}
	if o.HTTP == nil {
		o.HTTP = http.DefaultClient
	}
	if o.MaxPerMinute == 0 {
		o.MaxPerMinute = 30
	}
	if o.TTL == 0 {
		o.TTL = week
	}
	if o.MaxRetries == 0 {
		o.MaxRetries = 3
	}
	if o.Timeout == 0 {
		o.Timeout = 15 * time.Second
	}
	if o.Now == nil {
		o.Now = func() int64 { return time.Now().UnixMilli() }
	}
	if o.Sleep == nil {
		o.Sleep = defaultSleep
	}
	return &Client{o: o, limit: o.MaxPerMinute, imageMax: 20 << 20}
}

// Offline is set when the last network attempt failed because AniList could not be reached.
func (c *Client) Offline() bool { return c.offline.Load() }

func (c *Client) CurrentLimit() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limit
}

func (c *Client) acquire(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return CancelledError{}
		}
		c.mu.Lock()
		now := c.o.Now()
		kept := c.stamps[:0]
		for _, t := range c.stamps {
			if now-t < 60_000 {
				kept = append(kept, t)
			}
		}
		c.stamps = kept
		var wait int64
		switch {
		case c.blocked > now:
			wait = c.blocked - now
		case len(c.stamps) >= c.limit:
			wait = c.stamps[0] + 60_000 - now + 5
		default:
			c.stamps = append(c.stamps, now)
			c.mu.Unlock()
			return nil
		}
		c.mu.Unlock()
		if err := c.o.Sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// schedule is a FIFO queue so concurrent callers share the rate budget in order.
func (c *Client) schedule(ctx context.Context) error {
	return c.gate.Enter("gate").Do(func() error { return c.acquire(ctx) })
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"errors"`
}

// Request sends one GraphQL request and returns the raw "data" object. It retries network errors
// and 5xx with 1s, 2s, 4s backoff, waits out 429s using Retry-After, and never retries GraphQL or
// other 4xx errors.
func (c *Client) Request(ctx context.Context, query string, variables any) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt <= c.o.MaxRetries; attempt++ {
		if err := c.schedule(ctx); err != nil {
			return nil, err
		}
		rctx, cancel := context.WithTimeout(ctx, c.o.Timeout)
		req, err := http.NewRequestWithContext(rctx, http.MethodPost, c.o.Endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		res, err := c.o.HTTP.Do(req)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return nil, CancelledError{}
			}
			lastErr = err
			if attempt == c.o.MaxRetries {
				c.offline.Store(true)
				return nil, OfflineError{}
			}
			if err := c.o.Sleep(ctx, 1000*(1<<attempt)); err != nil {
				return nil, err
			}
			continue
		}
		c.offline.Store(false)

		c.mu.Lock()
		if lim, _ := strconv.Atoi(res.Header.Get("X-RateLimit-Limit")); lim > 0 {
			c.limit = min(c.o.MaxPerMinute, lim)
		}
		if res.Header.Get("X-RateLimit-Remaining") == "0" {
			if reset, _ := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64); reset > 0 {
				c.blocked = max(c.blocked, reset*1000)
			}
		}
		c.mu.Unlock()

		switch {
		case res.StatusCode == 429:
			res.Body.Close()
			cancel()
			retryAfter, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			if retryAfter == 0 {
				retryAfter = 60
			}
			c.mu.Lock()
			c.blocked = c.o.Now() + int64(retryAfter)*1000
			c.mu.Unlock()
			lastErr = &RateLimitError{retryAfter}
			if attempt == c.o.MaxRetries {
				return nil, lastErr
			}
			continue
		case res.StatusCode >= 500:
			res.Body.Close()
			cancel()
			lastErr = &AniListError{fmt.Sprintf("AniList server error (%d)", res.StatusCode), res.StatusCode}
			if attempt == c.o.MaxRetries {
				return nil, lastErr
			}
			if err := c.o.Sleep(ctx, 1000*(1<<attempt)); err != nil {
				return nil, err
			}
			continue
		}
		raw, rerr := io.ReadAll(io.LimitReader(res.Body, 32<<20))
		res.Body.Close()
		cancel()
		var out graphQLResponse
		parsed := rerr == nil && json.Unmarshal(raw, &out) == nil
		ok := res.StatusCode >= 200 && res.StatusCode < 300
		if !ok || (parsed && len(out.Errors) > 0) {
			msg, status := fmt.Sprintf("AniList request failed (%d)", res.StatusCode), res.StatusCode
			if parsed && len(out.Errors) > 0 {
				if out.Errors[0].Message != "" {
					msg = out.Errors[0].Message
				}
				if out.Errors[0].Status != 0 {
					status = out.Errors[0].Status
				}
			}
			return nil, &AniListError{msg, status}
		}
		if !parsed {
			return nil, &AniListError{"AniList returned an unreadable response", res.StatusCode}
		}
		return out.Data, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, OfflineError{}
}

// ---------- disk cache ----------

var reUnsafeKey = regexp.MustCompile(`[^\w.-]+`)

func (c *Client) cacheDir() string { return filepath.Join(c.o.DataDir, "anilist", "cache") }

func (c *Client) cachePath(key string) string {
	return filepath.Join(c.cacheDir(), reUnsafeKey.ReplaceAllString(key, "_")+".json")
}

// HashKey names a cache file the same way the Node server did, so both read each other's cache:
// prefix + first 20 hex digits of the SHA-1 of the JSON of the query parameters.
func HashKey(prefix string, parts any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(parts)
	sum := sha1.Sum(bytes.TrimRight(buf.Bytes(), "\n"))
	return prefix + "-" + hex.EncodeToString(sum[:])[:20]
}

type cacheFile struct {
	Data      json.RawMessage `json:"data"`
	FetchedAt int64           `json:"fetchedAt"`
}

func (c *Client) writeCache(key string, data any) error {
	b, err := store.StableMarshalGeneric(map[string]any{"fetchedAt": c.o.Now(), "data": data})
	if err != nil {
		return err
	}
	return store.AtomicWrite(c.cachePath(key), b)
}

// PrimeCache stores a response we already have (e.g. media from a search) so later lookups hit the cache.
func (c *Client) PrimeCache(key string, data any) error { return c.writeCache(key, data) }

// Cached is a response together with where it came from.
type Cached[T any] struct {
	Data   T
	Cached bool
	Stale  bool
}

// CachedRequest fetches with a TTL disk cache. When offline (or rate limited), an expired entry is
// served as stale rather than failing.
func CachedRequest[T any](ctx context.Context, c *Client, key, query string, variables any) (Cached[T], error) {
	var zero Cached[T]
	var hit *cacheFile
	if b, err := os.ReadFile(c.cachePath(key)); err == nil {
		var f cacheFile
		if json.Unmarshal(b, &f) == nil && len(f.Data) > 0 {
			hit = &f
		}
	}
	decode := func(raw json.RawMessage) (T, error) {
		var v T
		return v, json.Unmarshal(raw, &v)
	}
	if hit != nil && c.o.Now()-hit.FetchedAt < c.o.TTL.Milliseconds() {
		if v, err := decode(hit.Data); err == nil {
			return Cached[T]{v, true, false}, nil
		}
	}
	raw, err := c.Request(ctx, query, variables)
	if err != nil {
		var rl *RateLimitError
		if hit != nil && (errors.As(err, &OfflineError{}) || errors.As(err, &rl)) {
			if v, derr := decode(hit.Data); derr == nil {
				return Cached[T]{v, true, true}, nil
			}
		}
		return zero, err
	}
	v, err := decode(raw)
	if err != nil {
		return zero, err
	}
	var generic any
	_ = json.Unmarshal(raw, &generic)
	if err := c.writeCache(key, generic); err != nil {
		return zero, err
	}
	return Cached[T]{v, false, false}, nil
}

// ---------- images (allow-listed hosts only, cached on disk) ----------

// AllowedImage returns the parsed URL when it is https on an allow-listed host.
func AllowedImage(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !ImageHosts[u.Hostname()] {
		return nil
	}
	return u
}

func (c *Client) imageBase(href string) string {
	sum := sha1.Sum([]byte(href))
	return filepath.Join(c.cacheDir(), "img-"+hex.EncodeToString(sum[:])[:20])
}

// FetchImage returns artwork from the disk cache, or downloads and caches it.
func (c *Client) FetchImage(ctx context.Context, raw string) (data []byte, contentType string, err error) {
	u := AllowedImage(raw)
	if u == nil {
		return nil, "", &AniListError{"Image host not allowed", 400}
	}
	base := c.imageBase(u.String())
	for _, p := range extByType {
		if b, err := os.ReadFile(base + "." + p[1]); err == nil {
			return b, p[0], nil
		}
	}
	rctx, cancel := context.WithTimeout(ctx, c.o.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	res, err := c.o.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", CancelledError{}
		}
		return nil, "", OfflineError{}
	}
	defer res.Body.Close()
	contentType = strings.TrimSpace(strings.SplitN(res.Header.Get("Content-Type"), ";", 2)[0])
	ext := extFor(contentType)
	if res.StatusCode < 200 || res.StatusCode > 299 || ext == "" {
		return nil, "", &AniListError{fmt.Sprintf("Image fetch failed (%d)", res.StatusCode), res.StatusCode}
	}
	data, err = io.ReadAll(io.LimitReader(res.Body, c.imageMax+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", CancelledError{}
		}
		return nil, "", OfflineError{}
	}
	if int64(len(data)) > c.imageMax {
		return nil, "", &AniListError{"Image is too large", 413}
	}
	if err := store.AtomicWrite(base+"."+ext, data); err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

// SaveCover caches a cover for a confirmed match as covers/<anilistId>.<ext>; returns the file name.
func (c *Client) SaveCover(ctx context.Context, anilistID int, imageURL string) (string, error) {
	data, contentType, err := c.FetchImage(ctx, imageURL)
	if err != nil {
		return "", err
	}
	ext := extFor(contentType)
	if ext == "" {
		ext = "jpg"
	}
	name := fmt.Sprintf("%d.%s", anilistID, ext)
	return name, store.AtomicWrite(filepath.Join(c.o.DataDir, "covers", name), data)
}

// ---------- cleanup ----------

var reCoverName = regexp.MustCompile(`^\d+\.(jpg|png|webp|gif)$`)

// RemoveCover deletes a cached cover (covers/<id>.<ext>). Only plain cover file names are accepted.
func (c *Client) RemoveCover(file string) bool {
	if !reCoverName.MatchString(file) {
		return false
	}
	return os.Remove(filepath.Join(c.o.DataDir, "covers", file)) == nil
}

// PurgeMedia forgets everything cached about one title: its metadata and the artwork downloaded for it.
func (c *Client) PurgeMedia(id int) {
	file := c.cachePath(fmt.Sprintf("media-%d", id))
	if b, err := os.ReadFile(file); err == nil {
		var cached struct {
			Data struct {
				Media *Media `json:"Media"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &cached) == nil && cached.Data.Media != nil {
			m := cached.Data.Media
			var urls []*string
			if m.CoverImage != nil {
				urls = append(urls, m.CoverImage.Large, m.CoverImage.ExtraLarge)
			}
			urls = append(urls, m.BannerImage)
			for _, raw := range urls {
				if raw == nil {
					continue
				}
				if u := AllowedImage(*raw); u != nil {
					for _, p := range extByType {
						os.Remove(c.imageBase(u.String()) + "." + p[1])
					}
				}
			}
		}
	}
	os.Remove(file)
}

type CacheStats struct {
	Bytes int64 `json:"bytes"`
	Files int   `json:"files"`
}

func (c *Client) CacheStats() CacheStats {
	var s CacheStats
	ents, _ := os.ReadDir(c.cacheDir())
	for _, e := range ents {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			s.Files++
			s.Bytes += info.Size()
		}
	}
	return s
}

// ClearCache removes every cached response and image. Covers of matched titles live in covers/ and are untouched.
func (c *Client) ClearCache() CacheStats {
	stats := c.CacheStats()
	ents, _ := os.ReadDir(c.cacheDir())
	for _, e := range ents {
		os.Remove(filepath.Join(c.cacheDir(), e.Name()))
	}
	return stats
}

// PruneCovers deletes cover files that no entry references any more.
func (c *Client) PruneCovers(inUse map[string]bool) int {
	removed := 0
	ents, _ := os.ReadDir(filepath.Join(c.o.DataDir, "covers"))
	for _, e := range ents {
		if !inUse[e.Name()] && c.RemoveCover(e.Name()) {
			removed++
		}
	}
	return removed
}

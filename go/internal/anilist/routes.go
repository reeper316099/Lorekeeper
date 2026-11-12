package anilist

import (
	"bytes"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/reeper316099/Lorekeeper/go/internal/api"
	"github.com/reeper316099/Lorekeeper/go/internal/importexport"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

var contentTypes = map[string]string{"jpg": "image/jpeg", "png": "image/png", "webp": "image/webp", "gif": "image/gif"}

// RouteOptions customises Register (tests inject a client with a fake transport).
type RouteOptions struct {
	// NewClient, when set, builds the client instead of the default one.
	NewClient     func(s *store.Store) *Client
	ClientOptions ClientOptions
}

func invalid(path, msg string) error { return &shared.ValidationError{Path: path, Msg: msg} }

// friendly maps network-ish failures to HTTP errors the UI can show as "offline".
func friendly(err error) error {
	var rl *RateLimitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &OfflineError{}):
		return &store.HTTPError{Status: 503, Code: "OFFLINE", Message: "AniList is unreachable. Check your connection."}
	case errors.As(err, &rl):
		return &store.HTTPError{Status: 429, Code: "RATE_LIMITED", Message: rl.Error(), Details: map[string]int{"retryAfterSec": rl.RetryAfterSec}}
	case errors.As(err, &CancelledError{}):
		return &store.HTTPError{Status: 499, Code: "CANCELLED", Message: "Cancelled"}
	}
	return err
}

var reUsername = regexp.MustCompile(`^[\w-]+$`)

// optionalObject reads a JSON object body that may be empty.
func optionalObject(c *api.Ctx) (map[string]any, error) {
	b, err := c.RawBody()
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]any{}, nil
	}
	return c.ParseObject(b)
}

// Register returns the route module for AniList (use as part of api.Options.Extend).
func Register(opts RouteOptions) func(rt *api.Router, srv *api.Server) {
	return func(rt *api.Router, srv *api.Server) {
		s := srv.Store
		var client *Client
		if opts.NewClient != nil {
			client = opts.NewClient(s)
		} else {
			co := opts.ClientOptions
			co.DataDir = s.Dir
			client = NewClient(co)
		}
		jobs := NewJobRegistry(func(j Job) { s.Emit(store.EventJob, j) })
		svc := NewService(s, client, jobs)

		rt.GET("/anilist/status", func(c *api.Ctx) (any, error) {
			counts := svc.Counts()
			return map[string]any{
				"matched": counts.Matched, "queue": counts.Queue, "total": counts.Total,
				"offline": client.Offline(), "requestsPerMinute": client.CurrentLimit(),
				"autoConfirmThreshold": s.Settings().AutoConfirmThreshold,
				"matchJob":             jobs.Running("match"), "importJob": jobs.Running("import"),
			}, nil
		})

		rt.POST("/anilist/match/run", func(c *api.Ctx) (any, error) {
			body, err := optionalObject(c)
			if err != nil {
				return nil, err
			}
			var ids []string
			if v, ok := body["entryIds"]; ok && v != nil {
				arr, ok := v.([]any)
				if !ok {
					return nil, invalid("entryIds", "Expected array")
				}
				ids = []string{}
				for _, x := range arr {
					str, ok := x.(string)
					if !ok {
						return nil, invalid("entryIds", "Expected string")
					}
					ids = append(ids, str)
				}
			}
			if running := jobs.Running("match"); running != nil {
				return running, nil
			}
			return svc.StartMatching(ids), nil
		})
		rt.GET("/anilist/jobs/:id", func(c *api.Ctx) (any, error) {
			j, ok := jobs.Get(c.Params["id"])
			if !ok {
				return nil, &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "Job not found"}
			}
			return j, nil
		})
		rt.POST("/anilist/jobs/:id/cancel", func(c *api.Ctx) (any, error) {
			return map[string]bool{"cancelled": jobs.Cancel(c.Params["id"])}, nil
		})

		rt.GET("/anilist/queue", func(c *api.Ctx) (any, error) {
			status := QPending
			if q := c.Query(); q.Has("status") {
				switch v := QueueStatus(q.Get("status")); v {
				case QPending, QConfirmed, QRejected, QNever, QUnlinkable:
					status = v
				default:
					return nil, invalid("status", "Invalid enum value")
				}
			}
			return svc.QueueItems(status), nil
		})
		rt.GET("/anilist/search", func(c *api.Ctx) (any, error) {
			q := c.Query()
			if !q.Has("entryId") {
				return nil, invalid("entryId", "Required")
			}
			out, err := svc.CandidatesFor(c.R.Context(), q.Get("entryId"), q.Get("q"))
			return out, friendly(err)
		})

		rt.POST("/anilist/match/:id/confirm", func(c *api.Ctx) (any, error) {
			body, err := c.Object()
			if err != nil {
				return nil, err
			}
			n, ok := body["mediaId"].(float64)
			if !ok || n != float64(int(n)) || n <= 0 {
				return nil, invalid("mediaId", "Expected a positive integer")
			}
			e, err := svc.Confirm(c.R.Context(), c.Params["id"], int(n))
			return e, friendly(err)
		})
		rt.POST("/anilist/match/:id/set", func(c *api.Ctx) (any, error) {
			body, err := c.Object()
			if err != nil {
				return nil, err
			}
			ref, ok := body["ref"].(string)
			if !ok || ref == "" {
				return nil, invalid("ref", "Required")
			}
			e, err := svc.SetByRef(c.R.Context(), c.Params["id"], ref)
			return e, friendly(err)
		})
		rt.POST("/anilist/match/:id/reject", func(c *api.Ctx) (any, error) {
			if err := svc.Reject(c.Params["id"]); err != nil {
				return nil, err
			}
			return map[string]bool{"ok": true}, nil
		})
		rt.POST("/anilist/match/:id/never", func(c *api.Ctx) (any, error) {
			if err := svc.Never(c.Params["id"]); err != nil {
				return nil, err
			}
			return map[string]bool{"ok": true}, nil
		})
		rt.POST("/anilist/match/:id/retry", func(c *api.Ctx) (any, error) {
			id := c.Params["id"]
			cands, err := svc.CandidatesFor(c.R.Context(), id, "")
			if err != nil {
				return nil, friendly(err)
			}
			return cands, svc.SetPending(id, cands)
		})
		rt.GET("/anilist/cache", func(c *api.Ctx) (any, error) { return client.CacheStats(), nil })
		rt.POST("/anilist/cache/clear", func(c *api.Ctx) (any, error) { return svc.ClearCaches(), nil })
		rt.POST("/anilist/entries/:id/unmatch", func(c *api.Ctx) (any, error) { return svc.Unmatch(c.Params["id"]) })
		rt.POST("/anilist/entries/:id/suggested-tags", func(c *api.Ctx) (any, error) {
			e, err := svc.AddSuggestedTags(c.R.Context(), c.Params["id"])
			return e, friendly(err)
		})

		rt.POST("/anilist/import/preview", func(c *api.Ctx) (any, error) {
			body, err := c.Object()
			if err != nil {
				return nil, err
			}
			opts, err := parseListBody(body)
			if err != nil {
				return nil, err
			}
			out, err := svc.PreviewList(c.R.Context(), opts)
			return out, friendly(err)
		})
		rt.POST("/anilist/import/commit", func(c *api.Ctx) (any, error) {
			body, err := c.Object()
			if err != nil {
				return nil, err
			}
			id, ok := body["previewId"].(string)
			if !ok {
				return nil, invalid("previewId", "Required")
			}
			return svc.StartListImport(id)
		})

		// Images: <img> cannot send an Authorization header, so these accept ?token= (see api/server.go).
		rt.GET("/anilist/cover/:file", func(c *api.Ctx) (any, error) {
			file := c.Params["file"]
			m := reCoverName.FindStringSubmatch(file)
			if m == nil {
				return nil, &store.HTTPError{Status: 400, Code: "VALIDATION", Message: "Invalid cover name"}
			}
			b, err := os.ReadFile(filepath.Join(s.Dir, "covers", file))
			if err != nil {
				return nil, &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "No such cover"}
			}
			return &api.Download{Name: file, ContentType: contentTypes[m[1]], Body: b, Inline: true, CacheControl: "private, max-age=86400"}, nil
		})
		rt.GET("/anilist/image", func(c *api.Ctx) (any, error) {
			q := c.Query()
			raw := q.Get("url")
			if u, err := url.Parse(raw); !q.Has("url") || err != nil || u.Scheme == "" || u.Host == "" {
				return nil, invalid("url", "Invalid url")
			}
			if AllowedImage(raw) == nil {
				return nil, &store.HTTPError{Status: 400, Code: "VALIDATION", Message: "Image host is not allowed"}
			}
			b, ct, err := client.FetchImage(c.R.Context(), raw)
			if err != nil {
				return nil, friendly(err)
			}
			return &api.Download{Name: "image", ContentType: ct, Body: b, Inline: true, CacheControl: "private, max-age=86400"}, nil
		})
	}
}

func parseListBody(m map[string]any) (ListImportOptions, error) {
	var o ListImportOptions
	user, ok := m["username"].(string)
	if !ok {
		return o, invalid("username", "Required")
	}
	user = shared.JSTrim(user)
	switch {
	case user == "":
		return o, invalid("username", "String must contain at least 1 character(s)")
	case len(user) > 40:
		return o, invalid("username", "String must contain at most 40 character(s)")
	case !reUsername.MatchString(user):
		return o, invalid("username", "Usernames use letters, numbers, _ and -")
	}
	o.Username = user
	o.Types = []string{"ANIME", "MANGA"}
	if v, ok := m["types"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok || len(arr) == 0 {
			return o, invalid("types", "Expected a non-empty array")
		}
		o.Types = nil
		for _, x := range arr {
			t, _ := x.(string)
			if t != "ANIME" && t != "MANGA" {
				return o, invalid("types", "Invalid enum value")
			}
			o.Types = append(o.Types, t)
		}
	}
	if v, ok := m["strategy"]; ok && v != nil {
		s, _ := v.(string)
		switch importexport.Strategy(s) {
		case importexport.StrategySkip, importexport.StrategyFill, importexport.StrategyOverwrite, importexport.StrategyKeepBoth:
			o.Strategy = importexport.Strategy(s)
		default:
			return o, invalid("strategy", "Invalid enum value")
		}
	}
	for _, f := range []struct {
		key string
		dst *string
	}{{"animeCategoryId", &o.AnimeCategoryID}, {"mangaCategoryId", &o.MangaCategoryID}} {
		if v, ok := m[f.key]; ok && v != nil {
			s, ok := v.(string)
			if !ok {
				return o, invalid(f.key, "Expected string")
			}
			*f.dst = s
		}
	}
	return o, nil
}

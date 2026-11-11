package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/reeper316099/Lorekeeper/go/internal/search"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/stats"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

func okTrue() any { return map[string]bool{"ok": true} }

func (s *Server) entryNotFound() error {
	return &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "Entry not found"}
}

func (s *Server) registerRoutes() {
	rt, st := s.router, s.Store

	rt.GET("/health", func(c *Ctx) (any, error) { return okTrue(), nil })
	rt.GET("/meta", func(c *Ctx) (any, error) {
		return struct {
			shared.Meta
			Entries int    `json:"entries"`
			DataDir string `json:"dataDir"`
		}{st.Meta(), len(st.All()), st.Dir}, nil
	})

	// ----- entries -----
	rt.GET("/entries", func(c *Ctx) (any, error) {
		q, err := search.ParseEntryQuery(c.Query())
		if err != nil {
			return nil, err
		}
		return s.Querier.Query(q), nil
	})
	rt.POST("/entries", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		f.str("title", true, 1)
		f.str("categoryId", true, 1)
		if err := f.err(); err != nil {
			return nil, err
		}
		for _, k := range []string{"id", "createdAt", "updatedAt", "library"} {
			delete(body, k)
		}
		e, err := st.CreateEntry(body)
		c.Status = http.StatusCreated
		return e, err
	})
	rt.POST("/entries/restore", func(c *Ctx) (any, error) {
		b, err := c.bodyBytes()
		if err != nil {
			return nil, err
		}
		if _, err := parseObject(b); err != nil {
			return nil, err
		}
		var e shared.Entry
		if err := json.Unmarshal(b, &e); err != nil {
			return nil, err
		}
		out, err := st.RestoreEntry(e)
		c.Status = http.StatusCreated
		return out, err
	})
	rt.GET("/entries/:id/activity", func(c *Ctx) (any, error) {
		recs, err := st.ReadActivity("")
		if err != nil {
			return nil, err
		}
		out := []shared.ActivityRecord{}
		for i := len(recs) - 1; i >= 0; i-- {
			if recs[i].EntryID == c.Params["id"] {
				out = append(out, recs[i])
			}
		}
		return out, nil
	})
	rt.POST("/entries/bulk", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		ids := f.strings("ids", true, 1)
		action := f.oneOf("action", true, "set_status", "add_tag", "remove_tag", "move", "delete")
		value := f.str("value", false, 0)
		if err := f.err(); err != nil {
			return nil, err
		}
		if action == "set_status" && !slices.Contains(statusNames(), value) {
			return nil, invalidf("value", "Invalid enum value")
		}
		if action != "delete" && value == "" {
			return nil, &store.HTTPError{Status: 400, Code: "VALIDATION", Message: "value required"}
		}
		n, err := st.Bulk(store.BulkOp{IDs: ids, Action: action, Value: value})
		return map[string]int{"affected": n}, err
	})
	rt.GET("/entries/:id", func(c *Ctx) (any, error) {
		e, ok := st.GetEntry(c.Params["id"])
		if !ok {
			return nil, s.entryNotFound()
		}
		return e, nil
	})
	rt.PATCH("/entries/:id", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		return st.UpdateEntry(c.Params["id"], body)
	})
	rt.DELETE("/entries/:id", func(c *Ctx) (any, error) { return st.DeleteEntry(c.Params["id"]) })
	rt.POST("/entries/:id/progress", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		op := store.ProgressOp{}
		if d, ok := f.integer("delta"); ok {
			op.Delta = float64(d)
		}
		if f.has("set") {
			switch v := body["set"].(type) {
			case string, float64:
				op.Set, op.HasSet = v, true
			default:
				f.fail("set", "Expected string or number")
			}
		}
		if f.err() == nil && !f.has("delta") && !f.has("set") {
			f.fail("", "delta or set required")
		}
		if err := f.err(); err != nil {
			return nil, err
		}
		return st.SetProgress(c.Params["id"], op)
	})
	rt.POST("/entries/:id/move", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		to := f.str("categoryId", true, 0)
		if err := f.err(); err != nil {
			return nil, err
		}
		return st.MoveEntry(c.Params["id"], to)
	})
	rt.GET("/entries/:id/similar", func(c *Ctx) (any, error) {
		e, ok := st.GetEntry(c.Params["id"])
		if !ok {
			return nil, s.entryNotFound()
		}
		type sim struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		out := []sim{}
		for _, o := range st.All() {
			if o.ID != e.ID && o.Library == e.Library && shared.TitleSimilarity(o.Title, e.Title) >= 0.8 {
				out = append(out, sim{o.ID, o.Title})
			}
		}
		return out, nil
	})

	// ----- search -----
	rt.GET("/search", func(c *Ctx) (any, error) {
		q := c.Query()
		limit := 20
		if v := q.Get("limit"); q.Has("limit") {
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if v == "" {
				n, err = 0, nil
			}
			if err != nil || n != float64(int(n)) || n < 1 || n > 100 {
				return nil, invalidf("limit", "Expected an integer between 1 and 100")
			}
			limit = int(n)
		}
		text := q.Get("q")
		hits := s.Index.Search(text, limit)
		byID := map[string]shared.Entry{}
		for _, e := range st.All() {
			byID[e.ID] = e
		}
		type hit struct {
			Entry shared.Entry `json:"entry"`
			Score float64      `json:"score"`
			Terms []string     `json:"terms"`
		}
		res := struct {
			Entries    []hit             `json:"entries"`
			Tags       []shared.Tag      `json:"tags"`
			Categories []shared.Category `json:"categories"`
		}{[]hit{}, []shared.Tag{}, []shared.Category{}}
		for _, h := range hits {
			res.Entries = append(res.Entries, hit{byID[h.ID], h.Score, h.Terms})
		}
		if lc := strings.ToLower(strings.TrimSpace(text)); lc != "" {
			// like the Node route, the raw lowercase text (not the trimmed one) is matched
			lc = strings.ToLower(text)
			for _, t := range st.Tags() {
				if strings.Contains(strings.ToLower(t.Name), lc) && len(res.Tags) < 8 {
					res.Tags = append(res.Tags, t)
				}
			}
			for _, cat := range st.Categories() {
				if strings.Contains(strings.ToLower(cat.Name), lc) {
					res.Categories = append(res.Categories, cat)
				}
			}
		}
		return res, nil
	})

	// ----- categories -----
	rt.GET("/categories", func(c *Ctx) (any, error) {
		type withCount struct {
			shared.Category
			Count int `json:"count"`
		}
		cats := st.Categories()
		slices.SortStableFunc(cats, func(a, b shared.Category) int {
			if d := search.LocaleCompare(string(a.Library), string(b.Library)); d != 0 {
				return d
			}
			return a.Order - b.Order
		})
		out := make([]withCount, len(cats))
		for i, cat := range cats {
			out[i] = withCount{cat, len(st.EntriesOf(cat.ID))}
		}
		return out, nil
	})
	rt.POST("/categories", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		lib := f.oneOf("library", true, "reading", "watching")
		name := f.str("name", true, 1)
		color := f.color("color")
		if err := f.err(); err != nil {
			return nil, err
		}
		col := ""
		if color != nil {
			col = *color
		}
		cat, err := st.CreateCategory(shared.Library(lib), name, col)
		c.Status = http.StatusCreated
		return cat, err
	})
	rt.POST("/categories/reorder", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		lib := f.oneOf("library", true, "reading", "watching")
		ids := f.strings("ids", true, 0)
		if err := f.err(); err != nil {
			return nil, err
		}
		if err := st.ReorderCategories(shared.Library(lib), ids); err != nil {
			return nil, err
		}
		return okTrue(), nil
	})
	rt.PATCH("/categories/:id", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		patch := store.CategoryPatch{Name: f.strPtr("name", 1), Color: f.color("color")}
		if n, ok := f.integer("order"); ok {
			patch.Order = &n
		}
		if err := f.err(); err != nil {
			return nil, err
		}
		return st.UpdateCategory(c.Params["id"], patch)
	})
	rt.DELETE("/categories/:id", func(c *Ctx) (any, error) {
		if err := st.DeleteCategory(c.Params["id"], c.Query().Get("moveTo")); err != nil {
			return nil, err
		}
		return okTrue(), nil
	})

	// ----- tags -----
	rt.GET("/tags", func(c *Ctx) (any, error) {
		usage := map[string]int{}
		for _, u := range st.TagUsage() {
			usage[u.Name] = u.Count
		}
		type withCount struct {
			shared.Tag
			Count int `json:"count"`
		}
		tags := st.Tags()
		out := make([]withCount, len(tags))
		for i, t := range tags {
			out[i] = withCount{t, usage[t.Name]}
		}
		slices.SortStableFunc(out, func(a, b withCount) int {
			if a.Count != b.Count {
				return b.Count - a.Count
			}
			return search.LocaleCompare(a.Name, b.Name)
		})
		return out, nil
	})
	rt.GET("/tags/suggestions", func(c *Ctx) (any, error) {
		return shared.SuggestTagMerges(st.TagUsage()), nil
	})
	rt.POST("/tags/merge", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		from := f.strings("from", true, 1)
		to := f.str("to", true, 1)
		if err := f.err(); err != nil {
			return nil, err
		}
		n, err := st.MergeTags(from, to)
		return map[string]int{"affected": n}, err
	})
	rt.PATCH("/tags/:name", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		patch := store.TagPatch{Name: f.strPtr("name", 0), Color: f.strPtr("color", 0), Hidden: f.boolPtr("hidden")}
		if err := f.err(); err != nil {
			return nil, err
		}
		name := c.Params["name"]
		if d, err := url.PathUnescape(name); err == nil { // the Node route decoded the (already decoded) param again
			name = d
		}
		return st.UpdateTag(name, patch)
	})

	// ----- stats -----
	rt.GET("/stats/overview", func(c *Ctx) (any, error) {
		lib := ""
		if q := c.Query(); q.Has("library") {
			lib = q.Get("library")
			if lib != "reading" && lib != "watching" {
				return nil, invalidf("library", "Invalid enum value")
			}
		}
		return stats.Compute(st, lib)
	})

	// ----- settings & scales -----
	rt.GET("/settings", func(c *Ctx) (any, error) { return st.Settings(), nil })
	rt.PUT("/settings", func(c *Ctx) (any, error) {
		b, err := c.bodyBytes()
		if err != nil {
			return nil, err
		}
		if _, err := parseObject(b); err != nil {
			return nil, err
		}
		var next shared.Settings
		if err := json.Unmarshal(b, &next); err != nil {
			return nil, err
		}
		return st.SaveSettings(next)
	})
	rt.GET("/scales", func(c *Ctx) (any, error) { return st.Scales(), nil })
	rt.PUT("/scales", func(c *Ctx) (any, error) {
		b, err := c.bodyBytes()
		if err != nil {
			return nil, err
		}
		if _, err := parseObject(b); err != nil {
			return nil, err
		}
		var next shared.Scales
		if err := json.Unmarshal(b, &next); err != nil {
			return nil, err
		}
		return st.SaveScales(next)
	})

	// ----- backups -----
	rt.GET("/backups", func(c *Ctx) (any, error) { return st.ListBackups() })
	rt.POST("/backups", func(c *Ctx) (any, error) {
		b, err := st.Backup("manual")
		c.Status = http.StatusCreated
		return b, err
	})
	rt.POST("/backup/restore", func(c *Ctx) (any, error) {
		body, err := c.Object()
		if err != nil {
			return nil, err
		}
		f := &fields{m: body}
		name := f.str("name", true, 0)
		if err := f.err(); err != nil {
			return nil, err
		}
		if !store.IsValidBackupName(name) {
			return nil, &store.HTTPError{Status: 400, Code: "VALIDATION", Message: "Invalid backup name"}
		}
		if err := st.RestoreBackup(name); err != nil {
			return nil, err
		}
		return okTrue(), nil
	})

	// ----- realtime -----
	rt.Raw(http.MethodGet, "/events", s.events)
}

func statusNames() []string {
	out := make([]string, len(shared.Statuses))
	for i, s := range shared.Statuses {
		out[i] = string(s)
	}
	return out
}

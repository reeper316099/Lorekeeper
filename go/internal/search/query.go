package search

import (
	"bytes"
	"fmt"
	"math"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// EntryQuery is the validated query string of GET /entries (port of EntryQuerySchema).
type EntryQuery struct {
	Q           string
	Library     string
	Category    []string // a nil slice means "not given"
	Status      []string
	Tags        []string
	TagMode     string
	ExcludeTags []string
	MediaType   []string
	MinScore    *float64
	MaxScore    *float64
	HasNotes    *bool
	Behind      *bool
	Favorite    *bool
	Source      string
	Sort        string
	Order       string
	Page        int
	Limit       int
}

// QueryError is a validation failure of the query string.
type QueryError struct {
	Path string
	Msg  string
}

func (e *QueryError) Error() string { return e.Path + ": " + e.Msg }

func arrayParam(v url.Values, key string) []string {
	vals := append(append([]string(nil), v[key]...), v[key+"[]"]...)
	switch len(vals) {
	case 0:
		return nil
	case 1:
		return strings.Split(vals[0], ",")
	}
	return vals
}

func scalar(v url.Values, key string) (string, bool, error) {
	vals := append(append([]string(nil), v[key]...), v[key+"[]"]...)
	switch len(vals) {
	case 0:
		return "", false, nil
	case 1:
		return vals[0], true, nil
	}
	return "", true, &QueryError{key, "expected a single value"}
}

func parseBool(key, s string) (*bool, error) {
	switch s {
	case "true", "1":
		t := true
		return &t, nil
	case "false", "0":
		f := false
		return &f, nil
	}
	return nil, &QueryError{key, "expected boolean"}
}

// jsNumber mirrors Number(string): empty is 0, anything non-numeric is an error.
func jsNumber(key, s string) (float64, error) {
	s = shared.JSTrim(s)
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) {
		return 0, &QueryError{key, "expected number"}
	}
	return f, nil
}

func oneOf(key, v string, allowed ...string) error {
	if slices.Contains(allowed, v) {
		return nil
	}
	return &QueryError{key, fmt.Sprintf("invalid value %q", v)}
}

// ParseEntryQuery validates a query string like the Zod schema does, applying defaults.
func ParseEntryQuery(v url.Values) (EntryQuery, error) {
	q := EntryQuery{TagMode: "all", Order: "asc", Page: 1, Limit: 100}
	var err error
	str := func(key string, dst *string, allowed ...string) {
		if err != nil {
			return
		}
		s, ok, e := scalar(v, key)
		if e != nil {
			err = e
			return
		}
		if !ok {
			return
		}
		if len(allowed) > 0 {
			if e := oneOf(key, s, allowed...); e != nil {
				err = e
				return
			}
		}
		*dst = s
	}
	flag := func(key string, dst **bool) {
		if err != nil {
			return
		}
		s, ok, e := scalar(v, key)
		if e != nil {
			err = e
		} else if ok {
			*dst, err = parseBool(key, s)
		}
	}
	number := func(key string, dst **float64) {
		if err != nil {
			return
		}
		s, ok, e := scalar(v, key)
		if e != nil {
			err = e
		} else if ok {
			f, e := jsNumber(key, s)
			if e != nil {
				err = e
				return
			}
			*dst = &f
		}
	}
	integer := func(key string, dst *int, lo, hi int) {
		if err != nil {
			return
		}
		s, ok, e := scalar(v, key)
		if e != nil {
			err = e
			return
		}
		if !ok {
			return
		}
		f, e := jsNumber(key, s)
		if e != nil || f != math.Trunc(f) || f < float64(lo) || f > float64(hi) {
			err = &QueryError{key, fmt.Sprintf("expected an integer between %d and %d", lo, hi)}
			return
		}
		*dst = int(f)
	}
	str("q", &q.Q)
	str("library", &q.Library, "reading", "watching")
	q.Category, q.Status, q.Tags = arrayParam(v, "category"), arrayParam(v, "status"), arrayParam(v, "tags")
	str("tagMode", &q.TagMode, "all", "any")
	q.ExcludeTags, q.MediaType = arrayParam(v, "excludeTags"), arrayParam(v, "mediaType")
	number("minScore", &q.MinScore)
	number("maxScore", &q.MaxScore)
	flag("hasNotes", &q.HasNotes)
	flag("behind", &q.Behind)
	flag("favorite", &q.Favorite)
	str("source", &q.Source)
	str("sort", &q.Sort, "title", "score", "updatedAt", "createdAt", "status", "category", "relevance")
	str("order", &q.Order, "asc", "desc")
	integer("page", &q.Page, 1, math.MaxInt32)
	integer("limit", &q.Limit, 1, 1000)
	return q, err
}

// Result is one page of entries.
type Result struct {
	Items []shared.Entry `json:"items"`
	Total int            `json:"total"`
	Page  int            `json:"page"`
	Limit int            `json:"limit"`
}

// Querier runs entry queries against a store and its search index.
type Querier struct {
	Store *store.Store
	Index *Index

	mu       sync.Mutex
	titleV   uint64
	titleSet bool
	titleOrd []*shared.Entry
	keys     map[string][]byte
}

func NewQuerier(s *store.Store, ix *Index) *Querier {
	return &Querier{Store: s, Index: ix, keys: map[string][]byte{}}
}

// sortedByTitle is every entry ordered by title, cached per store version (sorting 10k titles on
// every request is slow). Sort keys are cached per title across versions.
func (qr *Querier) sortedByTitle() []*shared.Entry {
	qr.mu.Lock()
	defer qr.mu.Unlock()
	v := qr.Store.Version()
	if qr.titleSet && qr.titleV == v {
		return qr.titleOrd
	}
	all := qr.Store.All()
	list := pointers(all)
	if len(qr.keys) > 2*len(all)+1000 {
		qr.keys = map[string][]byte{}
	}
	key := func(t string) []byte {
		k, ok := qr.keys[t]
		if !ok {
			k = BaseKey(t)
			qr.keys[t] = k
		}
		return k
	}
	sort.SliceStable(list, func(i, j int) bool { return bytes.Compare(key(list[i].Title), key(list[j].Title)) < 0 })
	qr.titleOrd, qr.titleV, qr.titleSet = list, v, true
	return list
}

// pointers lets sorts move 8-byte pointers instead of ~400-byte entries. The entries are immutable
// snapshots, so pointing into the store's slice is safe.
func pointers(es []shared.Entry) []*shared.Entry {
	out := make([]*shared.Entry, len(es))
	for i := range es {
		out[i] = &es[i]
	}
	return out
}

func lower(xs []string) []string {
	if xs == nil {
		return nil
	}
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = strings.ToLower(x)
	}
	return out
}

func set(xs []string) map[string]bool {
	if xs == nil {
		return nil
	}
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Query filters, sorts and paginates entries (port of queryEntries).
func (qr *Querier) Query(q EntryQuery) Result {
	text := strings.TrimSpace(q.Q) != ""
	sortKey := q.Sort
	if sortKey == "" {
		if text {
			sortKey = "relevance"
		} else {
			sortKey = "title"
		}
	}
	presorted := sortKey == "title" && !text
	var list []*shared.Entry
	if presorted {
		list = qr.sortedByTitle()
	} else {
		list = pointers(qr.Store.All())
	}
	rank := map[string]int{}
	if text {
		for i, h := range qr.Index.Search(q.Q, 5000) {
			rank[h.ID] = i
		}
	}
	tags, ex := lower(q.Tags), lower(q.ExcludeTags)
	cats, sts, mts := set(q.Category), set(q.Status), set(q.MediaType)
	src := strings.ToLower(q.Source)
	out := make([]*shared.Entry, 0, len(list))
	for _, e := range list {
		if text {
			if _, ok := rank[e.ID]; !ok {
				continue
			}
		}
		if q.Library != "" && string(e.Library) != q.Library {
			continue
		}
		if cats != nil && !cats[e.CategoryID] {
			continue
		}
		if sts != nil {
			st := "none"
			if e.Status != nil {
				st = string(*e.Status)
			}
			if !sts[st] {
				continue
			}
		}
		if mts != nil && !mts[string(e.MediaType)] {
			continue
		}
		score := 0
		if e.Score != nil {
			score = *e.Score
		}
		if q.MinScore != nil && float64(score) < *q.MinScore {
			continue
		}
		if q.MaxScore != nil && (e.Score == nil || float64(*e.Score) > *q.MaxScore) {
			continue
		}
		if q.HasNotes != nil {
			has := (e.Notes.General != nil && *e.Notes.General != "") || (e.Notes.Extra1 != nil && *e.Notes.Extra1 != "") || (e.Notes.Extra2 != nil && *e.Notes.Extra2 != "")
			if has != *q.HasNotes {
				continue
			}
		}
		if q.Favorite != nil && e.Favorite != *q.Favorite {
			continue
		}
		if q.Behind != nil && *q.Behind {
			if b := shared.BehindBy(e.Current, e.ConsumedUpTo); b == nil || *b <= 0 {
				continue
			}
		}
		if src != "" && !slices.ContainsFunc(e.Sources, func(s string) bool { return strings.ToLower(s) == src }) {
			continue
		}
		if len(tags) > 0 || len(ex) > 0 {
			et := lower(e.Tags)
			if len(tags) > 0 {
				if q.TagMode == "any" {
					if !slices.ContainsFunc(tags, func(t string) bool { return slices.Contains(et, t) }) {
						continue
					}
				} else if slices.ContainsFunc(tags, func(t string) bool { return !slices.Contains(et, t) }) {
					continue
				}
			}
			if slices.ContainsFunc(ex, func(t string) bool { return slices.Contains(et, t) }) {
				continue
			}
		}
		out = append(out, e)
	}

	dir := 1
	if q.Order != "asc" {
		dir = -1
	}
	catName := map[string]string{}
	for _, c := range qr.Store.Categories() {
		catName[c.ID] = c.Name
	}
	cmp := func(a, b *shared.Entry) int {
		switch sortKey {
		case "score":
			return (scoreOr(a, -1) - scoreOr(b, -1)) * dir
		case "updatedAt":
			return strings.Compare(a.UpdatedAt, b.UpdatedAt) * dir
		case "createdAt":
			return strings.Compare(a.CreatedAt, b.CreatedAt) * dir
		case "status":
			return strings.Compare(statusStr(a), statusStr(b)) * dir
		case "category":
			return LocaleCompare(catName[a.CategoryID], catName[b.CategoryID]) * dir
		case "relevance":
			if len(rank) == 0 {
				return 0
			}
			return (rank[a.ID] - rank[b.ID]) * dir
		}
		return CompareBase(a.Title, b.Title) * dir
	}
	switch {
	case presorted:
		if q.Order == "desc" {
			slices.Reverse(out)
		}
	case sortKey == "relevance":
		slices.SortStableFunc(out, func(a, b *shared.Entry) int { return rank[a.ID] - rank[b.ID] })
	default:
		slices.SortStableFunc(out, cmp)
	}
	total := len(out)
	start := min((q.Page-1)*q.Limit, total)
	end := min(start+q.Limit, total)
	items := make([]shared.Entry, 0, end-start)
	for _, e := range out[start:end] {
		items = append(items, *e)
	}
	return Result{Items: items, Total: total, Page: q.Page, Limit: q.Limit}
}

func scoreOr(e *shared.Entry, def int) int {
	if e.Score == nil {
		return def
	}
	return *e.Score
}

func statusStr(e *shared.Entry) string {
	if e.Status == nil {
		return ""
	}
	return string(*e.Status)
}

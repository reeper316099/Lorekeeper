package anilist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// anilist-parity.json holds what the Node AniList service did against the same fake AniList
// (scripts/gen-go-anilist-fixtures.ts): the Go service must end up with the same entries, queue,
// job results, cache file names and covers after every step.

type parityStep struct {
	Step    map[string]any `json:"step"`
	Out     any            `json:"out"`
	Entries []any          `json:"entries"`
	Cache   []string       `json:"cache"`
	Covers  []string       `json:"covers"`
}

type parityScenario struct {
	Name  string `json:"name"`
	Setup struct {
		Categories []struct{ Library, Name string } `json:"categories"`
		Entries    []struct {
			Title    string         `json:"title"`
			Category string         `json:"category"`
			Extra    map[string]any `json:"extra"`
		} `json:"entries"`
	} `json:"setup"`
	Results []parityStep `json:"results"`
}

var skipKeys = map[string]bool{"id": true, "createdAt": true, "updatedAt": true}

// normalize drops volatile keys, and the null link channel/date Go always writes (Node left them out).
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			if skipKeys[k] || ((k == "channel" || k == "date") && val == nil) {
				continue
			}
			out[k] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalize(val)
		}
		return out
	}
	return v
}

func generic(t testing.TB, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	json.Unmarshal(b, &out)
	return out
}

func firstDiff(a, b any, path string) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: %T vs %T", path, a, b)
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			xv, xok := x[k]
			yv, yok := y[k]
			if xok != yok {
				return fmt.Sprintf("%s.%s: only on one side (node %v / go %v)", path, k, xv, yv)
			}
			if d := firstDiff(xv, yv, path+"."+k); d != "" {
				return d
			}
		}
		return ""
	case []any:
		y, ok := b.([]any)
		if !ok {
			return fmt.Sprintf("%s: %T vs %T", path, a, b)
		}
		if len(x) != len(y) {
			return fmt.Sprintf("%s: length node %d / go %d", path, len(x), len(y))
		}
		for i := range x {
			if d := firstDiff(x[i], y[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
		return ""
	}
	if !reflect.DeepEqual(a, b) {
		return fmt.Sprintf("%s: node %v / go %v", path, a, b)
	}
	return ""
}

func fileNames(dir string) []string {
	ents, _ := os.ReadDir(dir)
	out := []string{}
	for _, e := range ents {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func waitJob(t testing.TB, jobs *JobRegistry, id string) Job {
	t.Helper()
	for i := 0; i < 1000; i++ {
		if j, _ := jobs.Get(id); j.State != JobRunning {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}

func errOut(err error) any {
	var he *store.HTTPError
	var ae *AniListError
	switch {
	case errors.As(err, &he):
		return map[string]any{"error": map[string]any{"status": float64(he.Status), "code": he.Code}}
	case errors.As(err, &ae):
		return map[string]any{"error": map[string]any{"status": float64(ae.Status), "code": "ANILIST_ERROR"}}
	}
	return map[string]any{"error": map[string]any{"status": nil, "code": nil}}
}

func TestServiceMatchesNode(t *testing.T) {
	raw, err := os.ReadFile("testdata/anilist-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Scenarios []parityScenario `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, sc := range doc.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			w := loadWorld(t)
			s := newStore(t)
			for _, c := range sc.Setup.Categories {
				must(s.CreateCategory(shared.Library(c.Library), c.Name, ""))
			}
			for _, e := range sc.Setup.Entries {
				in := map[string]any{"title": e.Title, "categoryId": e.Category}
				for k, v := range e.Extra {
					in[k] = v
				}
				must(s.CreateEntry(in))
			}
			jobs := NewJobRegistry(nil)
			svc := NewService(s, worldClient(t, w, s.Dir), jobs)
			byTitle := func(title string) shared.Entry {
				for _, e := range s.All() {
					if e.Title == title {
						return e
					}
				}
				t.Fatalf("no entry titled %q", title)
				return shared.Entry{}
			}
			ctx := context.Background()
			previewID := ""
			for i, want := range sc.Results {
				step := want.Step
				title, _ := step["title"].(string)
				var out any
				switch step["do"] {
				case "match":
					j := waitJob(t, jobs, svc.StartMatching(nil).ID)
					out = map[string]any{"state": j.State, "result": j.Result, "done": j.Done, "total": j.Total}
				case "queue":
					rows := []any{}
					for _, r := range svc.QueueItems(QueueStatus(step["status"].(string))) {
						m := generic(t, r.QueueItem).(map[string]any)
						delete(m, "updatedAt")
						m["title"] = r.Entry.Title
						rows = append(rows, m)
					}
					out = rows
				case "suggestedTags":
					e, err := svc.AddSuggestedTags(ctx, byTitle(title).ID)
					out = entryOrErr(e, err)
				case "confirm":
					e, err := svc.Confirm(ctx, byTitle(title).ID, int(step["mediaId"].(float64)))
					out = entryOrErr(e, err)
				case "set":
					e, err := svc.SetByRef(ctx, byTitle(title).ID, step["ref"].(string))
					out = entryOrErr(e, err)
				case "search":
					c, err := svc.CandidatesFor(ctx, byTitle(title).ID, step["q"].(string))
					out = c
					if err != nil {
						out = errOut(err)
					}
				case "never":
					if err := svc.Never(byTitle(title).ID); err != nil {
						t.Fatal(err)
					}
				case "unmatch":
					e, err := svc.Unmatch(byTitle(title).ID)
					out = entryOrErr(e, err)
				case "clear":
					r := svc.ClearCaches()
					out = map[string]any{"files": float64(r.Files), "bytes": float64(r.Bytes), "coversRemoved": float64(r.CoversRemoved)}
				case "preview":
					opts, err := parseListBody(step["options"].(map[string]any))
					if err != nil {
						t.Fatal(err)
					}
					pv, err := svc.PreviewList(ctx, opts)
					if err != nil {
						out = errOut(err)
						break
					}
					previewID = pv.PreviewID
					m := generic(t, pv).(map[string]any)
					delete(m, "previewId")
					out = m
				case "commit":
					job, err := svc.StartListImport(previewID)
					if err != nil {
						out = errOut(err)
						break
					}
					j := waitJob(t, jobs, job.ID)
					res := generic(t, j.Result)
					if m, ok := res.(map[string]any); ok {
						delete(m, "undoToken")
					} else {
						res = map[string]any{}
					}
					out = map[string]any{"state": j.State, "result": res}
				default:
					t.Fatalf("unknown step %v", step["do"])
				}
				label := fmt.Sprintf("step %d (%v)", i, step["do"])
				if d := firstDiff(normalize(want.Out), normalize(generic(t, out)), "out"); d != "" {
					t.Errorf("%s: %s", label, d)
				}
				entries := generic(t, s.All()).([]any)
				sort.SliceStable(entries, func(a, b int) bool {
					return entries[a].(map[string]any)["title"].(string) < entries[b].(map[string]any)["title"].(string)
				})
				if d := firstDiff(any(normalizeList(want.Entries)), normalize(entries), "entries"); d != "" {
					t.Errorf("%s: %s", label, d)
				}
				if got := fileNames(filepath.Join(s.Dir, "anilist", "cache")); !slices.Equal(got, nonNil(want.Cache)) {
					t.Errorf("%s: cache files node %v / go %v", label, want.Cache, got)
				}
				if got := fileNames(filepath.Join(s.Dir, "covers")); !slices.Equal(got, nonNil(want.Covers)) {
					t.Errorf("%s: covers node %v / go %v", label, want.Covers, got)
				}
			}
		})
	}
}

func normalizeList(es []any) []any {
	out := make([]any, len(es))
	for i, e := range es {
		out[i] = normalize(e)
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func entryOrErr(e shared.Entry, err error) any {
	if err != nil {
		return errOut(err)
	}
	return e
}

func TestMatcherMatchesNode(t *testing.T) {
	raw, _ := os.ReadFile("testdata/anilist-parity.json")
	var doc struct {
		Matcher struct {
			Ranked []Candidate `json:"ranked"`
			Refs   []struct {
				In  string         `json:"in"`
				Out map[string]any `json:"out"`
			} `json:"refs"`
			Keys []string `json:"keys"`
		} `json:"matcher"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	w := loadWorld(t)
	e := shared.NewEntry()
	e.Library, e.Title, e.MediaType = shared.LibraryReading, "Lantern of the Hollow Crown", "manhwa"
	m := func(id int, title string, mod func(*Media)) Media {
		x := Media{ID: id, Type: "MANGA", Format: str("MANGA"), CountryOfOrigin: str("KR"), Title: Title{Romaji: &title}, Synonyms: []string{}}
		if mod != nil {
			mod(&x)
		}
		return x
	}
	got := Rank(e.Title, e.AltTitles, []Media{
		mediaByID(w, 1), mediaByID(w, 2),
		m(30, "Lantern of the Hollow Crown", func(x *Media) { x.CountryOfOrigin = str("JP") }),
		m(31, "Lantern of the Hollow Crown", func(x *Media) { x.Format = str("NOVEL") }),
		m(32, "Lantern of the Hollow Crown", func(x *Media) { x.Format = str("MUSIC") }),
		m(33, "Lantern Hollow", func(x *Media) { x.Format = str("SPECIAL") }),
	}, PlanSearch(e), 8)
	if len(got) != len(doc.Matcher.Ranked) {
		t.Fatalf("ranked %d, node %d", len(got), len(doc.Matcher.Ranked))
	}
	for i, c := range doc.Matcher.Ranked {
		if got[i].ID != c.ID || got[i].Confidence != c.Confidence {
			t.Errorf("rank %d: go %d (%v), node %d (%v)", i, got[i].ID, got[i].Confidence, c.ID, c.Confidence)
		}
	}
	for _, r := range doc.Matcher.Refs {
		id, typ, ok := ParseRef(r.In)
		switch {
		case r.Out == nil && ok:
			t.Errorf("ParseRef(%q) = %d, node says null", r.In, id)
		case r.Out != nil && (!ok || float64(id) != r.Out["id"].(float64) || (r.Out["type"] != nil && r.Out["type"] != typ)):
			t.Errorf("ParseRef(%q) = %d %s, node %v", r.In, id, typ, r.Out)
		}
	}
	keys := []string{
		HashKey("search", searchVars{Search: "Lantern of the Hollow Crown", Type: "MANGA", Country: str("KR"), PerPage: 8}),
		HashKey("search", searchVars{Search: "Café <&> “quoted”", Type: "ANIME", PerPage: 8}),
	}
	if !slices.Equal(keys, doc.Matcher.Keys) {
		t.Errorf("cache keys go %v, node %v", keys, doc.Matcher.Keys)
	}
}

// TestWriteInteropFolder writes a data folder after a matching run for scripts/check-go-anilist.ts
// (Node) to open. Run: INTEROP_OUT=/tmp/x go test -run TestWriteInteropFolder ./internal/anilist
func TestWriteInteropFolder(t *testing.T) {
	out := os.Getenv("INTEROP_OUT")
	if out == "" {
		t.Skip("set INTEROP_OUT to write the interop folder")
	}
	os.RemoveAll(out)
	w := loadWorld(t)
	s := store.New(out, store.Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cat := must(s.CreateCategory(shared.LibraryReading, "Misc.", ""))
	tv := must(s.CreateCategory(shared.LibraryWatching, "TV Shows", ""))
	for _, title := range []string{"Lantern of the Hollow Crown", "Twin Stars of Ember", "A Show With No Match"} {
		must(s.CreateEntry(map[string]any{"title": title, "categoryId": cat.ID}))
	}
	must(s.CreateEntry(map[string]any{"title": "Some TV Show", "categoryId": tv.ID}))
	jobs := NewJobRegistry(nil)
	svc := NewService(s, worldClient(t, w, s.Dir), jobs)
	waitJob(t, jobs, svc.StartMatching(nil).ID)
}

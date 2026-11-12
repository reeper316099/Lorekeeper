package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// Ported from packages/server/test/api.test.ts, plus extra coverage for the Go router.

const token = "test-token-123"

type env struct {
	t   *testing.T
	st  *store.Store
	srv *Server
	h   http.Handler
}

func newEnv(t *testing.T, opts ...func(*Options)) *env {
	t.Helper()
	st := store.New(t.TempDir(), store.Options{})
	if err := st.Open(); err != nil {
		t.Fatal(err)
	}
	o := Options{Store: st, Token: token}
	for _, f := range opts {
		f(&o)
	}
	srv := New(o)
	t.Cleanup(func() { srv.Close(); st.Close() })
	return &env{t, st, srv, srv.Handler()}
}

type resp struct {
	Status int
	Body   any
	Header http.Header
	Raw    string
}

func (r resp) m() map[string]any { return r.Body.(map[string]any) }
func (r resp) list() []any       { return r.Body.([]any) }
func (r resp) errCode() string   { return r.m()["error"].(map[string]any)["code"].(string) }

func (e *env) do(method, url string, payload any, tok string) resp {
	e.t.Helper()
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, "/api/v1"+url, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	out := resp{Status: rec.Code, Header: rec.Header(), Raw: rec.Body.String()}
	if rec.Body.Len() > 0 {
		json.Unmarshal(rec.Body.Bytes(), &out.Body)
	}
	return out
}

func (e *env) call(method, url string, payload any) resp { return e.do(method, url, payload, token) }

func (e *env) twoCats() (rom, act string) {
	e.t.Helper()
	rom = e.call("POST", "/categories", map[string]any{"library": "reading", "name": "Romance"}).m()["id"].(string)
	act = e.call("POST", "/categories", map[string]any{"library": "reading", "name": "Action"}).m()["id"].(string)
	return
}

func titles(items []any) []string {
	out := []string{}
	for _, i := range items {
		out = append(out, i.(map[string]any)["title"].(string))
	}
	return out
}

func eq(t *testing.T, got, want any, what string) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("%s: got %s, want %s", what, g, w)
	}
}

func TestAuthAndErrors(t *testing.T) {
	e := newEnv(t)
	for _, r := range [][2]string{{"GET", "/health"}, {"GET", "/entries"}, {"POST", "/entries"}, {"GET", "/stats/overview"}, {"GET", "/backups"}} {
		if s := e.do(r[0], r[1], nil, "").Status; s != 401 {
			t.Errorf("%v without token: %d", r, s)
		}
		bad := e.do(r[0], r[1], nil, "nope")
		if bad.Status != 401 || bad.errCode() != "UNAUTHORIZED" {
			t.Errorf("%v bad token: %d %s", r, bad.Status, bad.Raw)
		}
	}
	eq(t, e.call("GET", "/health", nil).Body, map[string]bool{"ok": true}, "health")
}

func TestValidationAndErrorShape(t *testing.T) {
	e := newEnv(t)
	r := e.call("POST", "/entries", map[string]any{"title": ""})
	if r.Status != 400 || r.errCode() != "VALIDATION" || r.Header.Get("X-Request-Id") == "" {
		t.Fatalf("%d %s", r.Status, r.Raw)
	}
	if d := r.m()["error"].(map[string]any)["details"].([]any); len(d) == 0 {
		t.Error("validation errors must carry details")
	}
	if e.call("GET", "/entries/nope", nil).Status != 404 {
		t.Error("missing entry must be 404")
	}
	if c := e.call("GET", "/nowhere", nil).errCode(); c != "NOT_FOUND" {
		t.Errorf("unknown route code %s", c)
	}
	if e.call("POST", "/backup/restore", map[string]any{"name": "../../etc/passwd"}).Status != 400 {
		t.Error("path traversal in a backup name must be rejected")
	}
}

func TestRouterQuirks(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct{ method, url string }{
		{"PUT", "/health"}, {"DELETE", "/entries"}, {"GET", "/entries/"}, {"GET", "/entries/x/progress"}, {"POST", "/health"},
	} {
		if r := e.call(c.method, c.url, nil); r.Status != 404 || r.errCode() != "NOT_FOUND" {
			t.Errorf("%s %s: want JSON 404, got %d %s", c.method, c.url, r.Status, r.Raw)
		}
	}
	// static routes win over :id (POST /entries/bulk must not be treated as an entry id)
	if r := e.call("POST", "/entries/bulk", map[string]any{"ids": []string{}, "action": "delete"}); r.Status != 400 {
		t.Errorf("bulk with no ids: %d %s", r.Status, r.Raw)
	}
	req := httptest.NewRequest("POST", "/api/v1/entries", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var body map[string]map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 400 || body["error"]["code"] != "BAD_REQUEST" {
		t.Errorf("malformed JSON: %d %s", rec.Code, rec.Body.String())
	}
	big := httptest.NewRequest("POST", "/api/v1/entries", strings.NewReader(`{"title":"`+strings.Repeat("x", 2<<20)+`"}`))
	big.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, big)
	if rec.Code != 413 {
		t.Errorf("oversized body: %d", rec.Code)
	}
}

func TestTokenInQueryOnlyForStreamAndImages(t *testing.T) {
	e := newEnv(t)
	for _, u := range []string{"/api/v1/entries?token=" + token, "/api/v1/health?token=" + token} {
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
		if rec.Code != 401 {
			t.Errorf("%s must require the header, got %d", u, rec.Code)
		}
	}
}

func TestEntriesCRUDProgressMoveBulk(t *testing.T) {
	e := newEnv(t)
	rom, act := e.twoCats()
	created := e.call("POST", "/entries", map[string]any{"title": "Alpha", "categoryId": rom, "tags": []string{"Romance", "romance", "Sorcery"}, "status": "in_progress"})
	if created.Status != 201 {
		t.Fatalf("create: %d %s", created.Status, created.Raw)
	}
	id := created.m()["id"].(string)
	eq(t, created.m()["tags"], []string{"Romance", "Sorcery"}, "canonical tags")

	p := e.call("PATCH", "/entries/"+id, map[string]any{"score": 8, "notes": map[string]any{"extra1": "Left off at ch 5"}}).m()
	notes := p["notes"].(map[string]any)
	if p["score"] != 8.0 || notes["extra1"] != "Left off at ch 5" || notes["general"] != nil {
		t.Errorf("patch: %v", p)
	}
	if e.call("PATCH", "/entries/"+id, map[string]any{"score": 11}).Status != 400 {
		t.Error("score 11 must be rejected")
	}
	cur := func(body map[string]any) map[string]any { return body["current"].(map[string]any) }
	r := cur(e.call("POST", "/entries/"+id+"/progress", map[string]any{"set": "S2 E1"}).m())
	if r["season"] != 2.0 || r["number"] != 1.0 {
		t.Errorf("S2 E1: %v", r)
	}
	if k := cur(e.call("POST", "/entries/"+id+"/progress", map[string]any{"set": "End of S1"}).m())["kind"]; k != "end" {
		t.Errorf("kind %v", k)
	}
	if e.call("POST", "/entries/"+id+"/progress", map[string]any{"delta": 1}).Status != 422 {
		t.Error("delta on non-numeric progress must be 422")
	}
	if n := cur(e.call("POST", "/entries/"+id+"/progress", map[string]any{"set": 10}).m())["number"]; n != 10.0 {
		t.Errorf("set 10: %v", n)
	}
	if n := cur(e.call("POST", "/entries/"+id+"/progress", map[string]any{"delta": 2}).m())["number"]; n != 12.0 {
		t.Errorf("delta 2: %v", n)
	}
	for _, bad := range []map[string]any{{}, {"delta": 1.5}, {"set": true}, {"set": []int{1}}} {
		if e.call("POST", "/entries/"+id+"/progress", bad).Status != 400 {
			t.Errorf("progress body %v must be rejected", bad)
		}
	}
	if c := e.call("POST", "/entries/"+id+"/move", map[string]any{"categoryId": act}).m()["categoryId"]; c != act {
		t.Errorf("move: %v", c)
	}
	b := e.call("POST", "/entries/bulk", map[string]any{"ids": []string{id}, "action": "add_tag", "value": "Overlord Lead"})
	if b.m()["affected"] != 1.0 {
		t.Errorf("bulk: %s", b.Raw)
	}
	if !strings.Contains(e.call("GET", "/entries/"+id, nil).Raw, "Overlord Lead") {
		t.Error("tag not added")
	}
	for _, bad := range []map[string]any{
		{"ids": []string{id}, "action": "set_status", "value": "nope"},
		{"ids": []string{id}, "action": "add_tag"},
		{"ids": []string{id}, "action": "explode"},
	} {
		if e.call("POST", "/entries/bulk", bad).Status != 400 {
			t.Errorf("bulk %v must be rejected", bad)
		}
	}
	if e.call("DELETE", "/entries/"+id, nil).Status != 200 {
		t.Error("delete")
	}
	if e.call("GET", "/entries", nil).m()["total"] != 0.0 {
		t.Error("list should be empty")
	}
}

func TestListFiltersTagModesBehindSortPaginationAndSearch(t *testing.T) {
	e := newEnv(t)
	rom, _ := e.twoCats()
	mk := func(o map[string]any) {
		o["categoryId"] = rom
		if r := e.call("POST", "/entries", o); r.Status != 201 {
			t.Fatalf("%v: %s", o, r.Raw)
		}
	}
	mk(map[string]any{"title": "Marlow’s Plan", "tags": []string{"Romance", "Overlord Lead"}, "score": 9, "status": "completed"})
	mk(map[string]any{"title": "Sorcery Academy", "tags": []string{"Sorcery", "Romance"}, "score": 6, "status": "in_progress",
		"current":      map[string]any{"raw": "111", "number": 111, "season": nil, "kind": "number"},
		"consumedUpTo": map[string]any{"raw": "110", "number": 110, "season": nil, "kind": "number"}})
	mk(map[string]any{"title": "Third", "tags": []string{"Sorcery"}, "notes": map[string]any{"general": "has notes"}})
	q := func(qs string) map[string]any { return e.call("GET", "/entries?"+qs, nil).m() }
	total := func(qs string) any { return q(qs)["total"] }
	if total("tags=Romance,Sorcery&tagMode=all") != 1.0 {
		t.Error("tag mode all")
	}
	if total("tags[]=Romance&tags[]=Sorcery&tagMode=any") != 3.0 {
		t.Error("tag mode any with [] keys")
	}
	eq(t, titles(q("tags=Sorcery&excludeTags=Romance")["items"].([]any)), []string{"Third"}, "excludeTags")
	if titles(q("behind=true")["items"].([]any))[0] != "Sorcery Academy" {
		t.Error("behind")
	}
	if total("hasNotes=true") != 1.0 || total("minScore=7") != 1.0 || total("status=completed,in_progress") != 2.0 {
		t.Error("hasNotes/minScore/status filters")
	}
	if titles(q("sort=score&order=desc")["items"].([]any))[0] != "Marlow’s Plan" {
		t.Error("sort by score")
	}
	if len(q("limit=2&page=2")["items"].([]any)) != 1 {
		t.Error("pagination")
	}
	if titles(q("q=sorcerx")["items"].([]any))[0] != "Sorcery Academy" {
		t.Error("typo search")
	}
	if n := q("q=marlow")["total"].(float64) + q("q=marlows")["total"].(float64); n <= 0 {
		t.Error("marlow search")
	}
	s := e.call("GET", "/search?q=acad", nil).m()
	if s["entries"].([]any)[0].(map[string]any)["entry"].(map[string]any)["title"] != "Sorcery Academy" {
		t.Errorf("/search: %v", s)
	}
	// natural title order: collation is case-insensitive and numeric
	e.call("POST", "/entries", map[string]any{"title": "vol 10", "categoryId": rom})
	e.call("POST", "/entries", map[string]any{"title": "Vol 2", "categoryId": rom})
	all := titles(q("sort=title&limit=100")["items"].([]any))
	if i2, i10 := indexOf(all, "Vol 2"), indexOf(all, "vol 10"); i2 < 0 || i10 < 0 || i2 > i10 {
		t.Errorf("title order: %v", all)
	}
	for _, bad := range []string{"limit=0", "limit=1001", "page=0", "page=x", "behind=maybe", "minScore=abc", "sort=nope", "order=up", "library=books", "tagMode=some", "limit=1.5"} {
		if r := e.call("GET", "/entries?"+bad, nil); r.Status != 400 || r.errCode() != "VALIDATION" {
			t.Errorf("?%s: %d %s", bad, r.Status, r.Raw)
		}
	}
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func TestCategoryLifecycle(t *testing.T) {
	e := newEnv(t)
	rom, act := e.twoCats()
	e.call("POST", "/entries", map[string]any{"title": "E", "categoryId": rom})
	if e.call("DELETE", "/categories/"+rom, nil).Status != 409 {
		t.Error("deleting a non-empty category needs moveTo")
	}
	p := e.call("PATCH", "/categories/"+rom, map[string]any{"name": "Love", "color": "#112233"}).m()
	if p["name"] != "Love" || p["slug"] != "love" {
		t.Errorf("rename: %v", p)
	}
	if e.call("DELETE", "/categories/"+rom+"?moveTo="+act, nil).Status != 200 {
		t.Error("delete with moveTo")
	}
	if c := e.call("GET", "/entries", nil).m()["items"].([]any)[0].(map[string]any)["categoryId"]; c != act {
		t.Errorf("entry should have moved: %v", c)
	}
	if col := e.call("POST", "/categories", map[string]any{"library": "reading", "name": "Harem"}).m()["color"]; col != "#880E4F" {
		t.Errorf("palette color %v", col)
	}
	for _, bad := range []map[string]any{{"library": "reading", "name": ""}, {"library": "books", "name": "X"}, {"library": "reading", "name": "X", "color": "red"}} {
		if e.call("POST", "/categories", bad).Status != 400 {
			t.Errorf("%v must be rejected", bad)
		}
	}
	list := e.call("GET", "/categories", nil).list()
	if list[0].(map[string]any)["count"] == nil {
		t.Error("categories carry a count")
	}
	if e.call("POST", "/categories/reorder", map[string]any{"library": "reading", "ids": []string{act}}).Status != 200 {
		t.Error("reorder")
	}
}

func TestTagSuggestionsAndMerge(t *testing.T) {
	e := newEnv(t)
	rom, _ := e.twoCats()
	for _, x := range []struct {
		title string
		tags  []string
	}{{"a", []string{"Sorcery", "Fantasy"}}, {"b", []string{"Sorcery"}}, {"c", []string{"Sorcrey"}}} {
		e.call("POST", "/entries", map[string]any{"title": x.title, "categoryId": rom, "tags": x.tags})
	}
	sug := e.call("GET", "/tags/suggestions", nil).list()
	if len(sug) != 1 || sug[0].(map[string]any)["from"] != "Sorcrey" || sug[0].(map[string]any)["to"] != "Sorcery" {
		t.Fatalf("suggestions: %v", sug)
	}
	if n := e.call("POST", "/tags/merge", map[string]any{"from": []string{"Sorcrey"}, "to": "Sorcery"}).m()["affected"]; n != 1.0 {
		t.Errorf("merge affected %v", n)
	}
	for _, tg := range e.call("GET", "/tags", nil).list() {
		if m := tg.(map[string]any); m["name"] == "Sorcery" && m["count"] != 3.0 {
			t.Errorf("Sorcery count %v", m["count"])
		}
	}
	if r := e.call("PATCH", "/tags/Sorcery", map[string]any{"color": "#abcdef", "hidden": true}); r.Status != 200 || r.m()["hidden"] != true {
		t.Errorf("patch tag: %s", r.Raw)
	}
	if e.call("PATCH", "/tags/Nope", map[string]any{"color": "#fff"}).Status != 404 {
		t.Error("unknown tag must be 404")
	}
}

func TestStatsOverview(t *testing.T) {
	e := newEnv(t)
	rom, _ := e.twoCats()
	e.call("POST", "/entries", map[string]any{"title": "a", "categoryId": rom, "score": 8, "status": "completed", "tags": []string{"x", "y"}})
	e.call("POST", "/entries", map[string]any{"title": "b", "categoryId": rom, "score": 6, "status": "in_progress", "tags": []string{"x", "y"}})
	e.call("POST", "/entries", map[string]any{"title": "held", "categoryId": rom, "status": "on_hold"})
	e.call("POST", "/entries", map[string]any{"title": "gone", "categoryId": rom, "status": "dropped"})
	s := e.call("GET", "/stats/overview", nil).m()
	tot := s["totals"].(map[string]any)
	if tot["entries"] != 4.0 || tot["completed"] != 1.0 || tot["avgScore"] != 7.0 {
		t.Errorf("totals %v", tot)
	}
	eq(t, s["byStatus"], map[string]int{"completed": 1, "in_progress": 1, "on_hold": 1, "dropped": 1}, "byStatus")
	co := s["tagCooccurrence"].([]any)[0].(map[string]any)
	if co["a"] != "x" || co["b"] != "y" || co["count"] != 2.0 {
		t.Errorf("cooccurrence %v", co)
	}
	sum := 0.0
	for _, v := range s["heatmap"].(map[string]any) {
		sum += v.(float64)
	}
	if sum <= 0 {
		t.Error("heatmap should count today's activity")
	}
	// dropped titles are not "needs a decision" (regression: they used to be listed)
	nd := s["needsDecision"].([]any)
	if len(nd) != 1 || nd[0].(map[string]any)["title"] != "held" {
		t.Errorf("needsDecision: %v", nd)
	}
	for _, k := range []string{"inProgress", "behindList", "backlogList", "recentlyUpdated", "highestRated", "scoreDistribution", "topTags", "byCategory"} {
		if _, ok := s[k].([]any); !ok {
			t.Errorf("%s must be a JSON array, got %T", k, s[k])
		}
	}
	if e.call("GET", "/stats/overview?library=nope", nil).Status != 400 {
		t.Error("bad library")
	}
	if r := e.call("GET", "/stats/overview?library=watching", nil).m(); r["totals"].(map[string]any)["entries"] != 0.0 {
		t.Error("library filter")
	}
}

func TestSettingsScalesBackupsRoundTrip(t *testing.T) {
	e := newEnv(t)
	rom, _ := e.twoCats()
	st := e.call("GET", "/settings", nil).m()
	st["accent"] = "#00ff00"
	if e.call("PUT", "/settings", st).m()["accent"] != "#00ff00" {
		t.Error("settings update")
	}
	for _, bad := range []any{map[string]any{"theme": "neon"}, []int{1}, "str"} {
		if e.call("PUT", "/settings", bad).Status != 400 {
			t.Errorf("PUT /settings %v must be 400", bad)
		}
	}
	if e.call("PUT", "/settings", map[string]any{}).m()["theme"] != "dark" {
		t.Error("an empty settings body means defaults, as with Zod")
	}
	keep := e.call("POST", "/entries", map[string]any{"title": "keep", "categoryId": rom}).m()["id"].(string)
	b := e.call("POST", "/backups", nil)
	if b.Status != 201 {
		t.Fatalf("backup: %d %s", b.Status, b.Raw)
	}
	e.call("DELETE", "/entries/"+keep, nil)
	if e.call("POST", "/backup/restore", map[string]any{"name": b.m()["name"]}).Status != 200 {
		t.Error("restore")
	}
	if e.call("GET", "/entries", nil).m()["total"] != 1.0 {
		t.Error("restore should bring the entry back")
	}
	if len(e.call("GET", "/backups", nil).list()) < 2 {
		t.Error("expected the manual and pre-restore backups")
	}
	sc := e.call("GET", "/scales", nil).m()
	if sc["watching"].(map[string]any)["statusLabels"].(map[string]any)["completed"] != "Finished" {
		t.Error("scales")
	}
	if e.call("PUT", "/scales", sc).Status != 200 {
		t.Error("scales round trip")
	}
	sc["reading"].(map[string]any)["scoreLabels"] = []string{"x"}
	if e.call("PUT", "/scales", sc).Status != 400 {
		t.Error("scales with the wrong label count must be rejected")
	}
}

func TestEntryActivitySimilarRestoreAndMeta(t *testing.T) {
	e := newEnv(t)
	rom, _ := e.twoCats()
	a := e.call("POST", "/entries", map[string]any{"title": "Quiet Harbor Without Mr Pell", "categoryId": rom}).m()
	e.call("POST", "/entries", map[string]any{"title": "Quiet Harbor Without Mr Pelle", "categoryId": rom})
	e.call("POST", "/entries", map[string]any{"title": "Totally Different", "categoryId": rom})
	id := a["id"].(string)
	e.call("POST", "/entries/"+id+"/progress", map[string]any{"delta": 1})
	act := e.call("GET", "/entries/"+id+"/activity", nil).list()
	if len(act) < 2 || act[0].(map[string]any)["kind"] == "created" {
		t.Errorf("activity should be newest first: %v", act)
	}
	if sim := e.call("GET", "/entries/"+id+"/similar", nil).list(); len(sim) != 1 {
		t.Errorf("similar: %v", sim)
	}
	if e.call("GET", "/entries/nope/similar", nil).Status != 404 {
		t.Error("similar on a missing entry")
	}
	full := e.call("GET", "/entries/"+id, nil)
	e.call("DELETE", "/entries/"+id, nil)
	var restored any
	json.Unmarshal([]byte(full.Raw), &restored)
	if r := e.call("POST", "/entries/restore", restored); r.Status != 201 {
		t.Fatalf("restore: %d %s", r.Status, r.Raw)
	}
	if e.call("GET", "/entries/"+id, nil).Status != 200 {
		t.Error("restored entry missing")
	}
	if e.call("POST", "/entries/restore", map[string]any{"id": "x"}).Status != 400 {
		t.Error("restore of an invalid entry must be 400")
	}
	meta := e.call("GET", "/meta", nil).m()
	if meta["schemaVersion"] != 1.0 || meta["entries"] != 3.0 || meta["dataDir"] == "" {
		t.Errorf("meta: %v", meta)
	}
}

func TestCORSPreflightAndOrigins(t *testing.T) {
	e := newEnv(t, func(o *Options) { o.AllowedOrigins = []string{"http://localhost:5173"} })
	req := httptest.NewRequest("OPTIONS", "/api/v1/entries", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" || !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Errorf("preflight: %d %v", rec.Code, rec.Header())
	}
	req = httptest.NewRequest("GET", "/api/v1/health", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("foreign origin must get no CORS header: %v", rec.Header())
	}
}

func TestStreamsServerSentEventsWithCORSAndToken(t *testing.T) {
	e := newEnv(t, func(o *Options) { o.AllowedOrigins = []string{"http://localhost:5173"} })
	ts := httptest.NewServer(e.h)
	defer ts.Close()
	base := ts.URL + "/api/v1/events"
	if r, err := http.Get(base + "?token=nope"); err != nil || r.StatusCode != 401 {
		t.Fatalf("bad token: %v %v", r, err)
	}
	req, _ := http.NewRequest("GET", base+"?token="+token, nil)
	req.Header.Set("Origin", "http://localhost:5173")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("headers: %d %v", res.StatusCode, res.Header)
	}
	readUntil := func(want string) string {
		t.Helper()
		var got strings.Builder
		buf := make([]byte, 512)
		deadline := time.After(5 * time.Second)
		ch := make(chan string, 1)
		go func() {
			for !strings.Contains(got.String(), want) {
				n, err := res.Body.Read(buf)
				got.Write(buf[:n])
				if err != nil {
					break
				}
			}
			ch <- got.String()
		}()
		select {
		case s := <-ch:
			return s
		case <-deadline:
			t.Fatalf("timed out waiting for %q, got %q", want, got.String())
			return ""
		}
	}
	if first := readUntil("event: hello"); !strings.Contains(first, `"version"`) {
		t.Fatalf("hello: %q", first)
	}
	e.call("POST", "/categories", map[string]any{"library": "reading", "name": "Streamed"})
	if ev := readUntil("event: change"); !strings.Contains(ev, `data: {"version":`) {
		t.Fatalf("change event: %q", ev)
	}
	other, _ := http.NewRequest("GET", base+"?token="+token, nil)
	other.Header.Set("Origin", "http://evil.example")
	r2, err := http.DefaultClient.Do(other)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("foreign origin must not get CORS headers on the stream")
	}
	r2.Body.Close()
}

func TestCloseEndsEventStreams(t *testing.T) {
	e := newEnv(t)
	ts := httptest.NewServer(e.h)
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, res.Body); close(done) }()
	time.Sleep(100 * time.Millisecond)
	e.srv.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream did not end after Close()")
	}
}

func TestServesWebBuildWithoutTokenWhileAPIStaysProtected(t *testing.T) {
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html><title>x</title>"), 0o644)
	os.MkdirAll(filepath.Join(web, "assets"), 0o755)
	os.WriteFile(filepath.Join(web, "assets", "app.js"), []byte("console.log(1)"), 0o644)
	e := newEnv(t, func(o *Options) { o.StaticDir = web })
	get := func(path, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec
	}
	if r := get("/", ""); r.Code != 200 || !strings.Contains(r.Body.String(), "<title>x</title>") {
		t.Errorf("/: %d", r.Code)
	}
	if get("/assets/app.js", "").Code != 200 {
		t.Error("asset")
	}
	if get("/missing.js", "").Code != 404 || get("/../../etc/passwd", "").Code != 404 {
		t.Error("missing files and traversal must be 404")
	}
	if get("/api/v1/entries", "").Code != 401 {
		t.Error("API without a token")
	}
	if get("/api/v1/entries", "Bearer "+token).Code != 200 {
		t.Error("API with a token")
	}
}

func TestExtendHookRegistersRoutes(t *testing.T) {
	e := newEnv(t, func(o *Options) {
		o.Extend = func(rt *Router, s *Server) {
			rt.GET("/extra/:id", func(c *Ctx) (any, error) { return map[string]string{"id": c.Params["id"]}, nil })
		}
	})
	r := e.call("GET", "/extra/a%20b", nil)
	if r.Status != 200 || r.m()["id"] != "a b" {
		t.Errorf("%d %s", r.Status, r.Raw)
	}
}

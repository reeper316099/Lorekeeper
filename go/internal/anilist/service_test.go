package anilist

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/api"
	"github.com/reeper316099/Lorekeeper/go/internal/importexport"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// Ported from packages/server/test/anilist-service.test.ts: the same scenarios, over HTTP.

const token = "test-token-123"

// helperMedia mirrors the `media()` helper of the TypeScript tests (invented content).
func helperMedia(id int, title string, mod func(*Media)) Media {
	desc := "A <i>story</i><br>line"
	large := "https://s4.anilist.co/file/c/" + itoa(id) + "-large.jpg"
	xl := "https://s4.anilist.co/file/c/" + itoa(id) + "-xl.jpg"
	r80, r90, chapters, year, score := 80, 90, 120, 2020, 80
	m := Media{
		ID: id, Type: "MANGA", Format: str("MANGA"), Status: str("RELEASING"), Chapters: &chapters,
		CountryOfOrigin: str("KR"), AverageScore: &score, SiteURL: str("https://anilist.co/manga/" + itoa(id)),
		Title: Title{Romaji: &title}, Synonyms: []string{}, StartDate: &StartDate{Year: &year}, Description: &desc,
		CoverImage: &CoverImage{Large: &large, ExtraLarge: &xl}, Genres: []string{"Fantasy"},
		Tags: []MediaTag{{Name: "Time Loop", Rank: &r80}, {Name: "Twist", Rank: &r90, IsMediaSpoiler: true}},
	}
	if mod != nil {
		mod(&m)
	}
	return m
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

type env struct {
	t     *testing.T
	st    *store.Store
	w     *world
	h     http.Handler
	srv   *api.Server
	rd    shared.Category
	anime shared.Category
	tv    shared.Category
}

func newEnv(t *testing.T, w *world) *env {
	t.Helper()
	st := newStore(t)
	srv := api.New(api.Options{
		Store: st, Token: token,
		Extend: func(rt *api.Router, s *api.Server) {
			importexport.Register(rt, s)
			Register(RouteOptions{NewClient: func(s *store.Store) *Client { return worldClient(t, w, s.Dir) }})(rt, s)
		},
	})
	t.Cleanup(srv.Close)
	e := &env{t: t, st: st, w: w, h: srv.Handler(), srv: srv}
	e.rd = must(st.CreateCategory(shared.LibraryReading, "Misc.", ""))
	e.anime = must(st.CreateCategory(shared.LibraryWatching, "Anime", ""))
	e.tv = must(st.CreateCategory(shared.LibraryWatching, "TV Shows", ""))
	return e
}

type resp struct {
	Status int
	Body   any
	Header http.Header
	Raw    []byte
}

func (r resp) m() map[string]any { return r.Body.(map[string]any) }
func (r resp) list() []any       { return r.Body.([]any) }

func (e *env) do(method, path string, payload any, auth bool) resp {
	e.t.Helper()
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(body))
	if auth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	out := resp{Status: rec.Code, Header: rec.Header(), Raw: rec.Body.Bytes()}
	json.Unmarshal(rec.Body.Bytes(), &out.Body)
	return out
}

func (e *env) call(method, path string, payload any) resp { return e.do(method, path, payload, true) }

func (e *env) waitJob(id string) map[string]any {
	e.t.Helper()
	for i := 0; i < 400; i++ {
		j := e.call("GET", "/anilist/jobs/"+id, nil).m()
		if j["state"] != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatal("job did not finish")
	return nil
}

func (e *env) runMatch() map[string]any {
	return e.waitJob(e.call("POST", "/anilist/match/run", map[string]any{}).m()["id"].(string))
}

func (e *env) newEntry(title, cat string, extra map[string]any) shared.Entry {
	in := map[string]any{"title": title, "categoryId": cat}
	for k, v := range extra {
		in[k] = v
	}
	return must(e.st.CreateEntry(in))
}

func (e *env) exists(p string) bool { _, err := os.Stat(filepath.Join(e.st.Dir, p)); return err == nil }

func idsOf(rows []any) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.(map[string]any)["entry"].(map[string]any)["id"].(string))
	}
	slices.Sort(out)
	return out
}

func TestAutoConfirmsClearMatchesQueuesAmbiguousOnesSkipsUnlinkable(t *testing.T) {
	native := "空洞の王冠"
	english := "The Hollow Crown"
	w := &world{Catalog: []Media{
		helperMedia(1, "Lantern of the Hollow Crown", func(m *Media) {
			m.Synonyms = []string{"Hollow Crown Lantern"}
			m.Title.English, m.Title.Native = &english, &native
		}),
		helperMedia(2, "Twin Stars of Ember", func(m *Media) { c := 40; m.Chapters = &c }),
		helperMedia(3, "Twin Stars of Embers", func(m *Media) { c := 41; m.Chapters = &c }),
	}}
	e := newEnv(t, w)
	a := e.newEntry("Lantern of the Hollow Crown", e.rd.ID, map[string]any{"tags": []string{"mine"}})
	b := e.newEntry("Twin Stars of Ember", e.rd.ID, nil)
	c := e.newEntry("A Show With No Match", e.rd.ID, nil)
	tvShow := e.newEntry("Lantern of the Hollow Crown", e.tv.ID, nil)

	job := e.runMatch()
	res := job["result"].(map[string]any)
	if job["state"] != "done" || res["autoConfirmed"] != 1.0 || res["queued"] != 2.0 || res["unlinkable"] != 1.0 {
		t.Fatalf("job %v", job)
	}
	matched, _ := e.st.GetEntry(a.ID)
	ext := matched.External
	if ext["anilistId"] != 1.0 || ext["matchLocked"] != true || ext["countryOfOrigin"] != "KR" || ext["cover"] != "1.jpg" || ext["format"] != "MANGA" {
		t.Errorf("external %v", ext)
	}
	if ext["description"] != "A story\nline" {
		t.Errorf("HTML must be stripped: %q", ext["description"])
	}
	for _, want := range []string{"The Hollow Crown", "空洞の王冠", "Hollow Crown Lantern"} {
		if !slices.Contains(matched.AltTitles, want) {
			t.Errorf("alt titles %v miss %q", matched.AltTitles, want)
		}
	}
	if *matched.TotalUnits != 120 || matched.MediaType != "manhwa" || !slices.Equal(matched.Tags, []string{"mine"}) {
		t.Errorf("total=%v type=%s tags=%v (AniList tags are only suggestions)", *matched.TotalUnits, matched.MediaType, matched.Tags)
	}
	hasLink := false
	for _, l := range matched.Links {
		hasLink = hasLink || (l.Kind == shared.LinkAniList && l.URL != nil && *l.URL == "https://anilist.co/manga/1")
	}
	if !hasLink || !e.exists("covers/1.jpg") {
		t.Error("expected the AniList link and the cover file")
	}

	pending := e.call("GET", "/anilist/queue?status=pending", nil).list()
	if got, want := idsOf(pending), []string{b.ID, c.ID}; !(slices.Equal(got, want) || slices.Equal(got, []string{c.ID, b.ID})) {
		t.Errorf("pending %v", got)
	}
	for _, row := range pending {
		r := row.(map[string]any)
		if r["entry"].(map[string]any)["id"] == b.ID {
			var ids []float64
			for _, cand := range r["candidates"].([]any) {
				ids = append(ids, cand.(map[string]any)["id"].(float64))
			}
			slices.Sort(ids)
			if !slices.Equal(ids, []float64{2, 3}) {
				t.Errorf("candidates %v", ids)
			}
		}
	}
	if un := e.call("GET", "/anilist/queue?status=unlinkable", nil).list(); idsOf(un)[0] != tvShow.ID {
		t.Errorf("unlinkable %v", idsOf(un))
	}

	added := e.call("POST", "/anilist/entries/"+a.ID+"/suggested-tags", nil).m()["tags"].([]any)
	var tags []string
	for _, x := range added {
		tags = append(tags, x.(string))
	}
	for _, want := range []string{"mine", "Fantasy", "Time Loop"} {
		if !slices.Contains(tags, want) {
			t.Errorf("tags %v miss %s", tags, want)
		}
	}
	if slices.Contains(tags, "Twist") {
		t.Error("spoiler tags must not be suggested")
	}

	if id := e.call("POST", "/anilist/match/"+b.ID+"/confirm", map[string]any{"mediaId": 3}).m()["external"].(map[string]any)["anilistId"]; id != 3.0 {
		t.Errorf("confirm: %v", id)
	}
	if id := e.call("POST", "/anilist/match/"+c.ID+"/set", map[string]any{"ref": "https://anilist.co/manga/2/Twin"}).m()["external"].(map[string]any)["anilistId"]; id != 2.0 {
		t.Errorf("set: %v", id)
	}
	if e.call("POST", "/anilist/match/"+c.ID+"/set", map[string]any{"ref": "nonsense"}).Status != 400 {
		t.Error("nonsense ref")
	}
	if e.call("POST", "/anilist/match/"+c.ID+"/set", map[string]any{"ref": "999999"}).Status != 404 {
		t.Error("unknown id")
	}
	if st := e.call("GET", "/anilist/status", nil).m(); st["matched"] != 3.0 || st["total"] != 4.0 || st["requestsPerMinute"] != 30.0 {
		t.Errorf("status %v", st)
	}
	for _, bad := range []map[string]any{{}, {"mediaId": 0}, {"mediaId": 1.5}, {"mediaId": "3"}} {
		if e.call("POST", "/anilist/match/"+b.ID+"/confirm", bad).Status != 400 {
			t.Errorf("confirm body %v must be rejected", bad)
		}
	}
}

func TestRemembersRejectAndNeverAndRetryResearches(t *testing.T) {
	w := &world{Catalog: []Media{helperMedia(5, "Quiet Orchard Tale", nil), helperMedia(6, "Quiet Orchard Tales", nil)}}
	e := newEnv(t, w)
	ent := e.newEntry("Quiet Orchard Tale", e.rd.ID, nil)
	e.runMatch()
	if len(e.call("GET", "/anilist/queue", nil).list()) != 1 {
		t.Fatal("expected one pending item")
	}
	e.call("POST", "/anilist/match/"+ent.ID+"/never", nil)
	before := w.searches()
	if job := e.runMatch(); w.searches() != before || job["state"] != "done" {
		t.Errorf("never = never searched again: searches %d -> %d, %v", before, w.searches(), job["state"])
	}
	if len(e.call("GET", "/anilist/queue?status=never", nil).list()) != 1 {
		t.Error("never queue")
	}
	if retry := e.call("POST", "/anilist/match/"+ent.ID+"/retry", nil); len(retry.list()) == 0 {
		t.Error("retry returned no candidates")
	}
	if len(e.call("GET", "/anilist/queue", nil).list()) != 1 {
		t.Error("retry must re-queue")
	}
	e.call("POST", "/anilist/match/"+ent.ID+"/reject", nil)
	if len(e.call("GET", "/anilist/queue?status=rejected", nil).list()) != 1 {
		t.Error("rejected")
	}
	e.call("POST", "/anilist/match/"+ent.ID+"/confirm", map[string]any{"mediaId": 5})
	un := e.call("POST", "/anilist/entries/"+ent.ID+"/unmatch", nil).m()
	if _, has := un["external"].(map[string]any)["anilistId"]; has {
		t.Error("unmatch must clear the lock")
	}
	for _, l := range un["links"].([]any) {
		if l.(map[string]any)["kind"] == "anilist" {
			t.Error("unmatch must remove the AniList link")
		}
	}
}

func TestUnmatchDeletesTheCoverAndCachedDataButKeepsASharedCover(t *testing.T) {
	e := newEnv(t, &world{Catalog: []Media{helperMedia(7, "Shared Cover Tale", nil)}})
	a := e.newEntry("Shared Cover Tale", e.rd.ID, nil)
	b := e.newEntry("Shared Cover Tale Copy", e.rd.ID, nil)
	e.call("POST", "/anilist/match/"+a.ID+"/confirm", map[string]any{"mediaId": 7})
	e.call("POST", "/anilist/match/"+b.ID+"/confirm", map[string]any{"mediaId": 7})
	if !e.exists("covers/7.jpg") || !e.exists("anilist/cache/media-7.json") {
		t.Fatal("cover and cache should exist after matching")
	}
	e.call("POST", "/anilist/entries/"+a.ID+"/unmatch", nil)
	if !e.exists("covers/7.jpg") {
		t.Error("one title still points at the cover, so it must stay")
	}
	un := e.call("POST", "/anilist/entries/"+b.ID+"/unmatch", nil).m()
	if _, has := un["external"].(map[string]any)["cover"]; has || e.exists("covers/7.jpg") || e.exists("anilist/cache/media-7.json") {
		t.Error("unmatching the last title must delete the cover and the cached metadata")
	}
	for _, n := range fileNames(filepath.Join(e.st.Dir, "anilist", "cache")) {
		if strings.HasPrefix(n, "img-") {
			t.Errorf("leftover artwork %s", n)
		}
	}
}

func TestClearsTheAniListCacheAndOrphanedCoversKeepingCoversInUse(t *testing.T) {
	e := newEnv(t, &world{Catalog: []Media{helperMedia(1, "Kept Title", nil), helperMedia(2, "Dropped Title", nil)}})
	a := e.newEntry("Kept Title", e.rd.ID, nil)
	e.call("POST", "/anilist/match/"+a.ID+"/confirm", map[string]any{"mediaId": 1})
	e.call("GET", "/anilist/search?entryId="+a.ID+"&q=Dropped", nil)
	os.WriteFile(filepath.Join(e.st.Dir, "covers", "99.jpg"), []byte("orphan"), 0o644)
	if e.call("GET", "/anilist/cache", nil).m()["files"].(float64) == 0 {
		t.Fatal("cache should have files")
	}
	r := e.call("POST", "/anilist/cache/clear", nil).m()
	if r["files"].(float64) == 0 || r["coversRemoved"] != 1.0 {
		t.Errorf("clear %v", r)
	}
	if got := e.call("GET", "/anilist/cache", nil).m(); got["files"] != 0.0 || got["bytes"] != 0.0 {
		t.Errorf("cache after clear %v", got)
	}
	if !e.exists("covers/1.jpg") || e.exists("covers/99.jpg") {
		t.Error("covers in use stay, orphans go")
	}
	if got, _ := e.st.GetEntry(a.ID); got.External["cover"] != "1.jpg" {
		t.Error("entry must keep its cover")
	}
}

func TestHandlesOfflineAndCancellationGracefully(t *testing.T) {
	w := &world{Catalog: []Media{helperMedia(1, "Some Title", nil)}, Offline: true}
	e := newEnv(t, w)
	first := e.newEntry("Some Title", e.rd.ID, nil)
	if job := e.runMatch(); job["state"] != "offline" {
		t.Fatalf("job %v", job)
	}
	if e.call("GET", "/anilist/status", nil).m()["offline"] != true {
		t.Error("status must say offline")
	}
	r := e.call("GET", "/anilist/search?entryId="+first.ID+"&q=x", nil)
	if r.Status != 503 || r.m()["error"].(map[string]any)["code"] != "OFFLINE" {
		t.Errorf("search offline: %d %s", r.Status, r.Raw)
	}

	w.setOffline(false)
	w.setDelay(40 * time.Millisecond)
	for i := 0; i < 6; i++ {
		e.newEntry("Slow Title "+itoa(i), e.rd.ID, nil)
	}
	run := e.call("POST", "/anilist/match/run", map[string]any{}).m()
	time.Sleep(60 * time.Millisecond)
	if e.call("POST", "/anilist/jobs/"+run["id"].(string)+"/cancel", nil).m()["cancelled"] != true {
		t.Fatal("cancel")
	}
	done := e.waitJob(run["id"].(string))
	if done["state"] != "cancelled" || done["done"].(float64) >= done["total"].(float64) {
		t.Errorf("job %v", done)
	}
	if e.call("POST", "/anilist/jobs/"+run["id"].(string)+"/cancel", nil).m()["cancelled"] != false {
		t.Error("cancelling a finished job")
	}
	if e.call("GET", "/anilist/jobs/nope", nil).Status != 404 {
		t.Error("unknown job")
	}
}

func TestRequiresAuthAndServesCoversViaTokenOnlyForSafeFileNames(t *testing.T) {
	e := newEnv(t, &world{Catalog: []Media{helperMedia(1, "Exact Title Here", nil)}})
	e.newEntry("Exact Title Here", e.rd.ID, nil)
	e.runMatch()
	if e.do("GET", "/anilist/status", nil, false).Status != 401 || e.do("GET", "/anilist/cover/1.jpg", nil, false).Status != 401 {
		t.Error("no token must be 401")
	}
	ok := e.do("GET", "/anilist/cover/1.jpg?token="+token, nil, false)
	if ok.Status != 200 || ok.Header.Get("Content-Type") != "image/jpeg" || ok.Header.Get("Cache-Control") != "private, max-age=86400" || ok.Header.Get("Content-Disposition") != "" {
		t.Errorf("cover: %d %v", ok.Status, ok.Header)
	}
	for _, bad := range []string{"..%2Fconfig%2Fsettings.json", "1.exe", "x.jpg"} {
		if r := e.do("GET", "/anilist/cover/"+bad+"?token="+token, nil, false); r.Status != 400 {
			t.Errorf("cover %q: %d", bad, r.Status)
		}
	}
	if e.do("GET", "/anilist/cover/404.jpg?token="+token, nil, false).Status != 404 {
		t.Error("missing cover")
	}
	img := func(u string) int {
		return e.do("GET", "/anilist/image?url="+url.QueryEscape(u)+"&token="+token, nil, false).Status
	}
	if img("https://evil.example/a.jpg") != 400 || img("https://s4.anilist.co/x.jpg") != 200 || img("not a url") != 400 {
		t.Error("image proxy allow-list")
	}
}

func TestRateLimitedRequestsAre429WithRetryAfter(t *testing.T) {
	st := newStore(t)
	srv := api.New(api.Options{Store: st, Token: token, Extend: func(rt *api.Router, s *api.Server) {
		Register(RouteOptions{NewClient: func(s *store.Store) *Client {
			clock := &fakeClock{now: 1_000_000}
			return NewClient(ClientOptions{DataDir: s.Dir, MaxRetries: 1, Now: clock.Now, Sleep: clock.Sleep,
				HTTP: httpDoer(func(*http.Request) (*http.Response, error) {
					return response(429, "{}", map[string]string{"Retry-After": "9"}), nil
				})})
		}})(rt, s)
	}})
	t.Cleanup(srv.Close)
	cat := must(st.CreateCategory(shared.LibraryReading, "Misc.", ""))
	ent := must(st.CreateEntry(map[string]any{"title": "X", "categoryId": cat.ID}))
	req := httptest.NewRequest("GET", "/api/v1/anilist/search?entryId="+ent.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var body map[string]map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	details, _ := body["error"]["details"].(map[string]any)
	if rec.Code != 429 || body["error"]["code"] != "RATE_LIMITED" || details["retryAfterSec"] != 9.0 {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestJobProgressIsPublishedAsStoreEvents(t *testing.T) {
	e := newEnv(t, &world{Catalog: []Media{helperMedia(1, "Eventful Title", nil)}})
	var mu sync.Mutex
	var states []string
	e.st.On(func(ev store.Event) {
		if ev.Name == store.EventJob {
			mu.Lock()
			states = append(states, string(ev.Data.(Job).State))
			mu.Unlock()
		}
	})
	e.newEntry("Eventful Title", e.rd.ID, nil)
	e.runMatch()
	mu.Lock()
	defer mu.Unlock()
	if len(states) < 2 || states[0] != "running" || states[len(states)-1] != "done" {
		t.Errorf("job events %v", states)
	}
}

// ---------- account import ----------

func listWorld() *world {
	item := func(m Media, mod func(*ListEntry)) ListEntry {
		p, s := 12.0, 8.0
		le := ListEntry{Status: "CURRENT", Progress: &p, Notes: str("my note"), Score: &s, StartedAt: &Date{Year: ip(2024), Month: ip(3), Day: ip(9)}, CompletedAt: &Date{}, Media: m}
		mod(&le)
		return le
	}
	zero := 0.0
	return &world{
		Catalog: nil,
		Lists: map[string]map[string][]ListEntry{"reader_one": {
			"MANGA": {
				item(helperMedia(10, "Brand New Manhwa", nil), func(l *ListEntry) {
					l.Status = "COMPLETED"
					s := 9.0
					l.Score = &s
					l.CompletedAt = &Date{Year: ip(2025), Month: ip(1), Day: ip(2)}
				}),
				item(helperMedia(11, "Already Tracked Series", func(m *Media) { m.Synonyms = []string{"Alt Name"} }), func(l *ListEntry) {
					l.Status, l.Progress, l.Score, l.Notes = "PLANNING", &zero, &zero, nil
				}),
				item(helperMedia(12, "Chinese Comic", func(m *Media) { m.CountryOfOrigin = str("CN") }), func(l *ListEntry) { l.Status = "PAUSED" }),
			},
			"ANIME": {
				item(helperMedia(20, "Moonlit Show", func(m *Media) {
					m.Type, m.Format, m.CountryOfOrigin = "ANIME", str("TV"), str("CN")
					ep := 24
					m.Episodes, m.Chapters = &ep, nil
				}), func(l *ListEntry) { l.Status = "REPEATING" }),
			},
		}},
	}
}

func ip(n int) *int { return &n }

func TestAccountImportPreviewsWithoutWritingThenCommitsWithCoversAndUndoes(t *testing.T) {
	e := newEnv(t, listWorld())
	mine := e.newEntry("Already tracked series", e.rd.ID, map[string]any{"notes": map[string]any{"general": "keep me", "extra1": nil, "extra2": nil}})
	body := map[string]any{"username": "reader_one", "types": []string{"MANGA", "ANIME"}, "animeCategoryId": e.anime.ID, "mangaCategoryId": e.rd.ID}

	pv := e.call("POST", "/anilist/import/preview", body).m()
	counts := pv["counts"].(map[string]any)
	if counts["fetched"] != 4.0 || counts["create"] != 3.0 || counts["update"] != 1.0 { // the custom-list duplicate was ignored
		t.Fatalf("counts %v", counts)
	}
	if len(e.st.All()) != 1 {
		t.Fatal("preview must not write")
	}
	jobID := e.call("POST", "/anilist/import/commit", map[string]any{"previewId": pv["previewId"]}).m()["id"].(string)
	job := e.waitJob(jobID)
	result := job["result"].(map[string]any)
	if job["state"] != "done" || result["created"] != 3.0 || result["updated"] != 1.0 {
		t.Fatalf("job %v", job)
	}

	by := map[string]shared.Entry{}
	for _, x := range e.st.All() {
		by[x.Title] = x
	}
	fresh := by["Brand New Manhwa"]
	if fresh.Library != "reading" || *fresh.Status != shared.StatusCompleted || *fresh.Score != 9 || fresh.MediaType != "manhwa" ||
		*fresh.CompletedAt != "2025-01-02" || *fresh.StartedAt != "2024-03-09" {
		t.Errorf("fresh %+v", fresh)
	}
	if *fresh.Current.Raw != "12" || *fresh.Current.Number != 12 || *fresh.Notes.General != "my note" || fresh.External["anilistId"] != 10.0 || fresh.External["cover"] != "10.jpg" {
		t.Errorf("fresh details %+v", fresh)
	}
	if c := by["Chinese Comic"]; c.MediaType != "manhua" || *c.Status != shared.StatusOnHold {
		t.Errorf("Chinese Comic %+v", c)
	}
	if s := by["Moonlit Show"]; s.Library != "watching" || s.MediaType != "donghua" || *s.Status != shared.StatusRepeating || *s.TotalUnits != 24 {
		t.Errorf("Moonlit Show %+v", s)
	}
	merged, _ := e.st.GetEntry(mine.ID)
	if *merged.Notes.General != "keep me" || merged.External["anilistId"] != 11.0 || !slices.Contains(merged.AltTitles, "Alt Name") || len(by) != 4 {
		t.Errorf("merged %+v (never overwrites notes by default, but links to AniList)", merged)
	}

	again := e.call("POST", "/anilist/import/preview", body).m()["counts"].(map[string]any)
	if again["create"] != 0.0 || again["update"] != 0.0 {
		t.Errorf("second import must change nothing: %v", again)
	}
	if e.call("POST", "/import/undo/"+result["undoToken"].(string), nil).Status != 200 {
		t.Fatal("undo")
	}
	if all := e.st.All(); len(all) != 1 || all[0].ID != mine.ID || all[0].External["anilistId"] != nil {
		t.Errorf("undo should restore the original library: %d entries", len(all))
	}
}

func TestAccountImportReportsUnknownUsersAndValidatesUsernames(t *testing.T) {
	e := newEnv(t, &world{})
	r := e.call("POST", "/anilist/import/preview", map[string]any{"username": "nobody_here", "types": []string{"ANIME"}, "animeCategoryId": e.anime.ID})
	if r.Status != 404 || r.m()["error"].(map[string]any)["code"] != "USER_NOT_FOUND" {
		t.Errorf("%d %s", r.Status, r.Raw)
	}
	for _, bad := range []map[string]any{
		{"username": "bad name!", "types": []string{"ANIME"}}, {"username": ""}, {"username": strings.Repeat("a", 41)},
		{"username": "ok", "types": []string{}}, {"username": "ok", "types": []string{"GAMES"}}, {"username": "ok", "strategy": "merge"}, {},
	} {
		if e.call("POST", "/anilist/import/preview", bad).Status != 400 {
			t.Errorf("%v must be rejected", bad)
		}
	}
	if e.call("POST", "/anilist/import/commit", map[string]any{"previewId": "nope"}).Status != 404 {
		t.Error("unknown preview")
	}
	nocat := e.call("POST", "/anilist/import/preview", map[string]any{"username": "x", "types": []string{"MANGA"}, "mangaCategoryId": "nope"}).m()
	_ = nocat
}

func TestAccountImportOfflineIs503(t *testing.T) {
	e := newEnv(t, &world{Offline: true})
	r := e.call("POST", "/anilist/import/preview", map[string]any{"username": "someone", "types": []string{"ANIME"}, "animeCategoryId": e.anime.ID})
	if r.Status != 503 || r.m()["error"].(map[string]any)["code"] != "OFFLINE" {
		t.Errorf("%d %s", r.Status, r.Raw)
	}
}

func TestMatchRunAcceptsAnEmptyBodyAndSpecificEntries(t *testing.T) {
	e := newEnv(t, &world{Catalog: []Media{helperMedia(1, "Pick Me", nil), helperMedia(2, "Skip Me", nil)}})
	a := e.newEntry("Pick Me", e.rd.ID, nil)
	e.newEntry("Skip Me", e.rd.ID, nil)
	req := httptest.NewRequest("POST", "/api/v1/anilist/match/run", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("empty body: %d %s", rec.Code, rec.Body.String())
	}
	var j map[string]any
	json.Unmarshal(rec.Body.Bytes(), &j)
	e.waitJob(j["id"].(string))
	if e.call("POST", "/anilist/match/run", map[string]any{"entryIds": "x"}).Status != 400 {
		t.Error("entryIds must be an array")
	}
	// an explicit list re-searches entries the queue already decided on
	e.call("POST", "/anilist/match/"+a.ID+"/unmatch", nil)
	e.call("POST", "/anilist/entries/"+a.ID+"/unmatch", nil)
	job := e.waitJob(e.call("POST", "/anilist/match/run", map[string]any{"entryIds": []string{a.ID}}).m()["id"].(string))
	if job["total"] != 1.0 {
		t.Errorf("job %v", job)
	}
}

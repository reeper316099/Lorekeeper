package search

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

type expectedHit struct {
	ID    string   `json:"id"`
	Score float64  `json:"score"`
	Terms []string `json:"terms"`
}

type op struct {
	Op    string         `json:"op"`
	ID    string         `json:"id"`
	Patch map[string]any `json:"patch"`
	Entry *shared.Entry  `json:"entry"`
	To    string         `json:"to"`
}

type rankings struct {
	Queries []string                 `json:"queries"`
	Before  map[string][]expectedHit `json:"before"`
	Ops     []op                     `json:"ops"`
	After   map[string][]expectedHit `json:"after"`
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s := store.New(t.TempDir(), store.Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func corpusStore(t *testing.T) (*store.Store, rankings) {
	t.Helper()
	s := newStore(t)
	for _, c := range []struct {
		lib  shared.Library
		name string
	}{{shared.LibraryReading, "A"}, {shared.LibraryReading, "B"}, {shared.LibraryWatching, "C"}} {
		if _, err := s.CreateCategory(c.lib, c.name, ""); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Entries []shared.Entry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEntries(corpus.Entries); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("testdata/rankings.json")
	if err != nil {
		t.Fatal(err)
	}
	var r rankings
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return s, r
}

// sameRanking checks equal scores position by position, and equal id sets within each group of
// tied scores (the order inside a tie is not defined by MiniSearch either).
func sameRanking(want []expectedHit, got []Hit) string {
	if len(want) != len(got) {
		return fmt.Sprintf("%d results, want %d", len(got), len(want))
	}
	for i := range want {
		if math.Abs(want[i].Score-got[i].Score) > 1e-9*math.Max(1, math.Abs(want[i].Score)) {
			return fmt.Sprintf("rank %d: score %v (%s), want %v (%s)", i, got[i].Score, got[i].ID, want[i].Score, want[i].ID)
		}
		w, g := slices.Clone(want[i].Terms), slices.Clone(got[i].Terms)
		sort.Strings(w)
		sort.Strings(g)
		if !slices.Equal(w, g) && want[i].ID == got[i].ID {
			return fmt.Sprintf("rank %d (%s): terms %v, want %v", i, got[i].ID, g, w)
		}
	}
	for i := 0; i < len(want); {
		j := i
		for j < len(want) && math.Abs(want[j].Score-want[i].Score) <= 1e-9*math.Max(1, math.Abs(want[i].Score)) {
			j++
		}
		w, g := map[string]bool{}, map[string]bool{}
		for k := i; k < j; k++ {
			w[want[k].ID], g[got[k].ID] = true, true
		}
		for id := range w {
			if !g[id] {
				return fmt.Sprintf("tie group at rank %d..%d: missing %s", i, j-1, id)
			}
		}
		i = j
	}
	return ""
}

func TestRankingsMatchMiniSearch(t *testing.T) {
	s, r := corpusStore(t)
	ix := NewIndex(s)
	withHits := 0
	for _, q := range r.Queries {
		got := ix.Search(q, 5000)
		if len(r.Before[q]) > 0 {
			withHits++
		}
		if msg := sameRanking(r.Before[q], got); msg != "" {
			t.Errorf("query %q: %s", q, msg)
		}
	}
	if withHits < 60 {
		t.Fatalf("corpus too weak: only %d queries have hits", withHits)
	}
}

// After edits, deletes, inserts and moves the incrementally updated index must rank exactly like
// the Node one (which is itself updated incrementally).
func TestIncrementalUpdatesMatchMiniSearch(t *testing.T) {
	s, r := corpusStore(t)
	ix := NewIndex(s)
	ix.Search("crimson", 10) // build the index first so the edits below are incremental
	for _, o := range r.Ops {
		var err error
		switch o.Op {
		case "update":
			_, err = s.UpdateEntry(o.ID, o.Patch)
		case "delete":
			_, err = s.DeleteEntry(o.ID)
		case "insert":
			err = s.InsertEntries([]shared.Entry{*o.Entry})
		case "move":
			_, err = s.MoveEntry(o.ID, o.To)
		}
		if err != nil {
			t.Fatalf("%s %s: %v", o.Op, o.ID, err)
		}
		ix.Search("crimson", 10) // search between edits, like a user would
	}
	for _, q := range r.Queries {
		if msg := sameRanking(r.After[q], ix.Search(q, 5000)); msg != "" {
			t.Errorf("after edits, query %q: %s", q, msg)
		}
	}
	// ...and a freshly built index agrees with the incrementally updated one.
	fresh := NewIndex(s)
	for _, q := range r.Queries {
		a, b := ix.Search(q, 5000), fresh.Search(q, 5000)
		if len(a) != len(b) {
			t.Errorf("query %q: incremental has %d hits, fresh build %d", q, len(a), len(b))
		}
	}
}

func TestIndexSeesHandEditsWithUnchangedTimestamps(t *testing.T) {
	s := newStore(t)
	c, _ := s.CreateCategory(shared.LibraryReading, "A", "")
	e, _ := s.CreateEntry(map[string]any{"title": "Quartz Lantern", "categoryId": c.ID})
	ix := NewIndex(s)
	if len(ix.Search("quartz", 5)) != 1 {
		t.Fatal("expected a hit")
	}
	// ReplaceEntries keeps updatedAt: the index must notice by content, not by timestamp.
	e.Title = "Violet Orchard"
	if err := s.ReplaceEntries([]shared.Entry{e}); err != nil {
		t.Fatal(err)
	}
	if len(ix.Search("quartz", 5)) != 0 || len(ix.Search("violet", 5)) != 1 {
		t.Fatal("index did not follow the replaced entry")
	}
	// ported from storage.test.ts: one typo still finds it
	if hits := ix.Search("orchrd", 5); len(hits) != 1 || hits[0].ID != e.ID {
		t.Fatalf("typo search: %+v", hits)
	}
}

func TestSearchEdgeCases(t *testing.T) {
	s := newStore(t)
	ix := NewIndex(s)
	if got := ix.Search("anything", 10); len(got) != 0 {
		t.Fatalf("empty store: %v", got)
	}
	for _, q := range []string{"", "   ", "!!!", "..."} {
		if got := ix.Search(q, 10); len(got) != 0 {
			t.Fatalf("%q: %v", q, got)
		}
	}
	c, _ := s.CreateCategory(shared.LibraryReading, "A", "")
	a, _ := s.CreateEntry(map[string]any{"title": "Quartz Lantern", "categoryId": c.ID})
	b, _ := s.CreateEntry(map[string]any{"title": "Amber Harbor", "categoryId": c.ID})
	if got := ix.Search("quartz", 10); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("got %+v", got)
	}
	if _, err := s.DeleteEntry(b.ID); err != nil {
		t.Fatal(err)
	}
	if got := ix.Search("amber", 10); len(got) != 0 {
		t.Fatalf("deleted entry still found: %+v", got)
	}
	n, _ := s.CreateEntry(map[string]any{"title": "Amber Fresh", "categoryId": c.ID})
	if got := ix.Search("amber", 10); len(got) != 1 || got[0].ID != n.ID {
		t.Fatalf("got %+v", got)
	}
	if got := ix.Search("quartz lantern amber", 10); len(got) != 0 {
		t.Fatalf("all words must match: %+v", got)
	}
	if got := ix.Search("lantern", 0); len(got) != 0 {
		t.Fatalf("limit 0: %+v", got)
	}
}

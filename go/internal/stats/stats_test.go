package stats

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

func fixture(t *testing.T) *store.Store {
	t.Helper()
	s := store.New(t.TempDir(), store.Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r, _ := s.CreateCategory(shared.LibraryReading, "R", "")
	w, _ := s.CreateCategory(shared.LibraryWatching, "W", "")
	mk := func(cat, title, status string, score int, tags ...string) {
		in := map[string]any{"title": title, "categoryId": cat, "tags": tags}
		if status != "" {
			in["status"] = status
		}
		if score > 0 {
			in["score"] = score
		}
		if _, err := s.CreateEntry(in); err != nil {
			t.Fatal(err)
		}
	}
	mk(r.ID, "a", "completed", 9, "x", "y", "z")
	mk(r.ID, "b", "in_progress", 7, "x", "y")
	mk(r.ID, "c", "planned", 0, "z")
	mk(w.ID, "d", "on_hold", 5, "y")
	mk(w.ID, "e", "dropped", 3)
	mk(w.ID, "f", "", 0)
	return s
}

func TestOverviewCounts(t *testing.T) {
	o, err := Compute(fixture(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if o.Totals.Entries != 6 || o.Totals.Reading != 3 || o.Totals.Watching != 3 || o.Totals.Completed != 1 {
		t.Errorf("totals %+v", o.Totals)
	}
	if o.Totals.AvgScore == nil || *o.Totals.AvgScore != 6 {
		t.Errorf("avg %v", o.Totals.AvgScore)
	}
	if !reflect.DeepEqual(o.ByStatus, map[string]int{"completed": 1, "in_progress": 1, "planned": 1, "on_hold": 1, "dropped": 1, "none": 1}) {
		t.Errorf("byStatus %v", o.ByStatus)
	}
	if o.Backlog != 1 || len(o.NeedsDecision) != 1 || o.NeedsDecision[0].Title != "d" {
		t.Errorf("backlog %d needsDecision %+v", o.Backlog, o.NeedsDecision)
	}
	if o.ScoreDist[8].Count != 1 || o.ScoreDist[2].Count != 1 || len(o.ScoreDist) != 10 {
		t.Errorf("score distribution %+v", o.ScoreDist)
	}
	if o.HighestRated[0].Title != "a" || *o.HighestRated[0].Score != 9 || len(o.HighestRated) != 4 {
		t.Errorf("highestRated %+v", o.HighestRated)
	}
	if len(o.ByCategory) != 2 || o.ByCategory[0].Count != 3 {
		t.Errorf("byCategory %+v", o.ByCategory)
	}
}

func TestTagRankingsAndCooccurrence(t *testing.T) {
	o, _ := Compute(fixture(t), "")
	want := []NameCount{{"y", 3}, {"x", 2}, {"z", 2}}
	if !reflect.DeepEqual(o.TopTags, want) {
		t.Errorf("topTags %v, want %v (count desc, then name)", o.TopTags, want)
	}
	if o.TagCooccurrence[0] != (Cooccurrence{"x", "y", 2}) {
		t.Errorf("cooccurrence %v", o.TagCooccurrence)
	}
}

func TestLibraryFilterAndEmptyStoreProduceArraysNotNull(t *testing.T) {
	o, _ := Compute(fixture(t), "reading")
	if o.Totals.Entries != 3 || len(o.ByCategory) != 1 {
		t.Errorf("reading only: %+v", o.Totals)
	}
	empty := store.New(t.TempDir(), store.Options{})
	empty.Open()
	defer empty.Close()
	oe, _ := Compute(empty, "")
	b, _ := json.Marshal(oe)
	var m map[string]any
	json.Unmarshal(b, &m)
	for k, v := range m {
		if v == nil {
			t.Errorf("%s is null on an empty store", k)
		}
	}
	if m["totals"].(map[string]any)["avgScore"] != nil {
		t.Error("avgScore is null with no scores")
	}
}

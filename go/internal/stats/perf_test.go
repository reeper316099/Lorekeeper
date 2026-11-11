package stats

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/perfdata"
	"github.com/reeper316099/Lorekeeper/go/internal/search"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// TestPerfSearchAndStats mirrors the search, list and stats rows of scripts/perf.ts. Run:
//
//	PERF=1 go test -run TestPerfSearchAndStats -v ./internal/stats
func TestPerfSearchAndStats(t *testing.T) {
	if os.Getenv("PERF") == "" {
		t.Skip("set PERF=1 to run")
	}
	n := 10000
	if v, err := strconv.Atoi(os.Getenv("PERF_N")); err == nil {
		n = v
	}
	timeIt := func(label string, runs int, fn func()) {
		samples := make([]float64, runs)
		for i := range samples {
			start := time.Now()
			fn()
			samples[i] = float64(time.Since(start).Microseconds()) / 1000
		}
		sort.Float64s(samples)
		suffix := ""
		if runs > 1 {
			suffix = fmt.Sprintf("  (%d runs)", runs)
		}
		fmt.Printf("%-44s %8.1f ms p50 %8.1f ms p95%s\n", label, samples[len(samples)/2], samples[min(len(samples)-1, int(float64(len(samples))*0.95))], suffix)
	}
	dir := t.TempDir()
	s := store.New(dir, store.Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	c1, _ := s.CreateCategory(shared.LibraryReading, "Big", "")
	c2, _ := s.CreateCategory(shared.LibraryWatching, "Other", "")
	if err := s.InsertEntries(perfdata.Entries(n, c1.ID, c2.ID)); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = store.New(dir, store.Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ix := search.NewIndex(s)
	qr := search.NewQuerier(s, ix)
	q := func(raw string) search.Result {
		v, _ := url.ParseQuery(raw)
		eq, err := search.ParseEntryQuery(v)
		if err != nil {
			t.Fatal(err)
		}
		return qr.Query(eq)
	}
	fmt.Printf("Perf run with %d synthetic entries (Go)\n\n", n)
	timeIt("first search (builds the index)", 1, func() { q("q=crimson") })
	timeIt(`search "crimson tower" (warm)`, 30, func() { q("q=crimson+tower&limit=50") })
	var fuzzy int
	timeIt(`search fuzzy "cirmson" (warm)`, 30, func() { fuzzy = q("q=cirmson&limit=50").Total })
	fmt.Println("  fuzzy hits:", fuzzy)
	timeIt(`search prefix "wand" (warm)`, 30, func() { q("q=wand&limit=50") })
	timeIt("list, sort by title, page 1", 15, func() { q("sort=title&limit=100") })
	timeIt("list, 2 tags (any) + status filter", 15, func() {
		q("tags=" + url.QueryEscape("Tag 3,Tag 21") + "&tagMode=any&status=in_progress&limit=100")
	})
	timeIt("stats overview", 5, func() {
		if _, err := Compute(s, ""); err != nil {
			t.Fatal(err)
		}
	})
	target := s.All()[n>>1]
	timeIt("PATCH one entry (persist the big file)", 8, func() {
		if _, err := s.UpdateEntry(target.ID, map[string]any{"score": 9}); err != nil {
			t.Fatal(err)
		}
	})
	timeIt("search right after a write (incremental)", 8, func() { q("q=lantern&limit=50") })
}

package store

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/perfdata"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// TestPerfStore mirrors the storage rows of scripts/perf.ts (same synthetic data, same
// operations) so the Go and Node numbers can be compared. Run:
//
//	PERF=1 go test -run TestPerfStore -v ./internal/store      (PERF_N=25000 for a bigger set)
func TestPerfStore(t *testing.T) {
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
		p50 := samples[len(samples)/2]
		p95 := samples[min(len(samples)-1, int(float64(len(samples))*0.95))]
		suffix := ""
		if runs > 1 {
			suffix = fmt.Sprintf("  (%d runs)", runs)
		}
		fmt.Printf("%-44s %8.1f ms p50 %8.1f ms p95%s\n", label, p50, p95, suffix)
	}

	dir := t.TempDir()
	s := New(dir, Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	cats := []shared.Category{must(s.CreateCategory(shared.LibraryReading, "Big", "")), must(s.CreateCategory(shared.LibraryWatching, "Other", ""))}
	entries := perfdata.Entries(n, cats[0].ID, cats[1].ID)
	fmt.Printf("Perf run with %d synthetic entries (Go)\n\n", n)
	timeIt("bulk insert (1 write per category)", 1, func() {
		if err := s.InsertEntries(entries); err != nil {
			t.Fatal(err)
		}
	})
	s.Close()

	var re *Store
	timeIt("cold open (read + validate all JSON)", 1, func() {
		re = New(dir, Options{})
		if err := re.Open(); err != nil {
			t.Fatal(err)
		}
	})
	target := re.All()[n>>1]
	timeIt("PATCH one entry (persist the big file)", 8, func() {
		if _, err := re.UpdateEntry(target.ID, map[string]any{"score": 9}); err != nil {
			t.Fatal(err)
		}
	})
	all := re.All()
	timeIt("20 concurrent progress writes (coalesced)", 1, func() {
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := re.SetProgress(all[i].ID, ProgressOp{Delta: 1}); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
	})
	var maxRead time.Duration
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				start := time.Now()
				_ = re.All()
				if d := time.Since(start); d > maxRead {
					maxRead = d
				}
			}
		}
	}()
	timeIt("10 writes while a reader polls All()", 1, func() {
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				re.SetProgress(all[i].ID, ProgressOp{Delta: 1})
			}()
		}
		wg.Wait()
	})
	close(stop)
	<-done
	fmt.Printf("%-44s %8.2f ms\n", "slowest All() while writes were in flight", float64(maxRead.Microseconds())/1000)
	re.Close()
}

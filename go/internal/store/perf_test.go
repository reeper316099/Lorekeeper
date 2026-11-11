package store

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

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
	words := []string{"Silent", "Crimson", "Hollow", "Gilded", "Fallen", "Wandering", "Eternal", "Ashen", "Velvet", "Iron", "Lunar", "Broken", "Hidden", "Scarlet", "Verdant", "Frozen"}
	nouns := []string{"Crown", "Garden", "Oath", "Tower", "Contract", "Blade", "Archive", "Throne", "Lantern", "Covenant", "Harbor", "Saga"}
	tags := make([]string, 60)
	for i := range tags {
		tags[i] = fmt.Sprintf("Tag %d", i)
	}
	statuses := []shared.Status{"planned", "in_progress", "completed", "on_hold", "dropped"}

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
	now := ISO(time.Now())
	entries := make([]shared.Entry, n)
	for i := range entries {
		e := shared.NewEntry()
		ci := 1
		e.Library = shared.LibraryWatching
		if i%3 != 0 {
			ci, e.Library = 0, shared.LibraryReading
		}
		st := statuses[i%5]
		score := i%10 + 1
		num := float64(i % 300)
		raw := strconv.Itoa(i % 300)
		kind := shared.ProgressNumber
		gen := fmt.Sprintf("Notes about entry %d with a few words", i)
		var extra *string
		if i%4 == 0 {
			x := "Left off somewhere"
			extra = &x
		}
		e.ID, e.CategoryID = fmt.Sprintf("perf-%d", i), cats[ci].ID
		e.Title = fmt.Sprintf("%s %s %d", words[i%16], nouns[(i>>4)%12], i)
		e.Status, e.Score = &st, &score
		e.Tags = []string{tags[i%60], tags[(i*7)%60], tags[(i*13)%60]}
		e.Current = shared.Progress{Raw: &raw, Number: &num, Kind: &kind}
		e.Notes = shared.Notes{General: &gen, Extra1: extra}
		e.CreatedAt, e.UpdatedAt = now, now
		entries[i] = e
	}
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

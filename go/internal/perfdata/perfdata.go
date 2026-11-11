// Package perfdata builds the synthetic entry set used by the performance checks, identical to the
// one in scripts/perf.ts so Go and Node timings are comparable. All content is invented.
package perfdata

import (
	"fmt"
	"strconv"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

var (
	words = []string{"Silent", "Crimson", "Hollow", "Gilded", "Fallen", "Wandering", "Eternal", "Ashen", "Velvet", "Iron", "Lunar", "Broken", "Hidden", "Scarlet", "Verdant", "Frozen"}
	nouns = []string{"Crown", "Garden", "Oath", "Tower", "Contract", "Blade", "Archive", "Throne", "Lantern", "Covenant", "Harbor", "Saga"}
)

// Entries returns n entries: two thirds in readingCat, one third in watchingCat.
func Entries(n int, readingCat, watchingCat string) []shared.Entry {
	tags := make([]string, 60)
	for i := range tags {
		tags[i] = fmt.Sprintf("Tag %d", i)
	}
	statuses := []shared.Status{"planned", "in_progress", "completed", "on_hold", "dropped"}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	out := make([]shared.Entry, n)
	for i := range out {
		e := shared.NewEntry()
		e.Library, e.CategoryID = shared.LibraryWatching, watchingCat
		if i%3 != 0 {
			e.Library, e.CategoryID = shared.LibraryReading, readingCat
		}
		st, score := statuses[i%5], i%10+1
		num, raw, kind := float64(i%300), strconv.Itoa(i%300), shared.ProgressNumber
		gen := fmt.Sprintf("Notes about entry %d with a few words", i)
		var extra *string
		if i%4 == 0 {
			x := "Left off somewhere"
			extra = &x
		}
		e.ID = fmt.Sprintf("perf-%d", i)
		e.Title = fmt.Sprintf("%s %s %d", words[i%16], nouns[(i>>4)%12], i)
		e.Status, e.Score = &st, &score
		e.Tags = []string{tags[i%60], tags[(i*7)%60], tags[(i*13)%60]}
		e.Current = shared.Progress{Raw: &raw, Number: &num, Kind: &kind}
		e.Notes = shared.Notes{General: &gen, Extra1: extra}
		e.CreatedAt, e.UpdatedAt = now, now
		out[i] = e
	}
	return out
}

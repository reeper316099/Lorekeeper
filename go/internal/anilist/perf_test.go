package anilist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// TestPerfAniList mirrors scripts/perf-anilist.ts. Run:
//
//	PERF=1 go test -run TestPerfAniList -v ./internal/anilist
func TestPerfAniList(t *testing.T) {
	if os.Getenv("PERF") == "" {
		t.Skip("set PERF=1 to run")
	}
	n := 300
	if v, err := strconv.Atoi(os.Getenv("PERF_N")); err == nil {
		n = v
	}
	words := []string{"Silent", "Crimson", "Hollow", "Gilded", "Fallen", "Wandering", "Eternal", "Ashen"}
	nouns := []string{"Crown", "Garden", "Oath", "Tower", "Contract", "Blade", "Archive", "Throne"}
	title := func(i int) string { return fmt.Sprintf("%s %s %d", words[i%8], nouns[(i>>3)%8], i) }
	catalog := make([]Media, n)
	list := make([]ListEntry, n)
	for i := range catalog {
		tt, ch, year := title(i), 100, 2020
		desc, cover, score := "A <i>story</i>", fmt.Sprintf("https://s4.anilist.co/c/%d.jpg", i), 80
		catalog[i] = Media{
			ID: 1000 + i, Type: "MANGA", Format: str("MANGA"), Status: str("RELEASING"), Chapters: &ch, CountryOfOrigin: str("KR"),
			AverageScore: &score, SiteURL: str(fmt.Sprintf("https://anilist.co/manga/%d", 1000+i)), Title: Title{Romaji: &tt},
			Synonyms: []string{}, StartDate: &StartDate{Year: &year}, Description: &desc, CoverImage: &CoverImage{Large: &cover},
			Genres: []string{"Fantasy"}, Tags: []MediaTag{},
		}
		p, s := float64(i%50), float64(i%10+1)
		list[i] = ListEntry{Status: "CURRENT", Progress: &p, Score: &s, Media: catalog[i]}
	}
	doer := httpDoer(func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.String(), "https://s4.anilist.co/") {
			return response(200, string([]byte{9, 9}), map[string]string{"Content-Type": "image/jpeg"}), nil
		}
		var req struct {
			Query     string `json:"query"`
			Variables struct{ Search string }
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &req)
		var out any
		if strings.Contains(req.Query, "MediaListCollection") {
			out = map[string]any{"MediaListCollection": map[string]any{"hasNextChunk": false, "lists": []any{map[string]any{"name": "All", "isCustomList": false, "entries": list}}}}
		} else {
			hits := []Media{}
			for _, m := range catalog {
				if strings.EqualFold(deref(m.Title.Romaji), req.Variables.Search) {
					hits = append(hits, m)
				}
			}
			out = map[string]any{"Page": map[string]any{"media": hits}}
		}
		body, _ := json.Marshal(out)
		return okJSON(string(body), nil), nil
	})
	s := newStore(t)
	cat := must(s.CreateCategory(shared.LibraryReading, "Misc.", ""))
	var entries []shared.Entry
	for i := range catalog {
		entries = append(entries, must(s.BuildEntry(map[string]any{"title": title(i), "categoryId": cat.ID})))
	}
	if err := s.InsertEntries(entries); err != nil {
		t.Fatal(err)
	}
	jobs := NewJobRegistry(nil)
	client := NewClient(ClientOptions{DataDir: s.Dir, HTTP: doer, MaxPerMinute: 1_000_000, Sleep: func(context.Context, int64) error { return nil }})
	svc := NewService(s, client, jobs)

	fmt.Printf("Go AniList timings, %d titles\n\n", n)
	start := time.Now()
	job := svc.StartMatching(nil)
	for {
		if j, _ := jobs.Get(job.ID); j.State != JobRunning {
			fmt.Printf("%-46s %8.1f ms  %v\n", "matching job (search+rank+apply+cover)", float64(time.Since(start).Microseconds())/1000, j.Result)
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	start = time.Now()
	pv, err := svc.PreviewList(context.Background(), ListImportOptions{Username: "x", Types: []string{"MANGA"}, MangaCategoryID: cat.ID, Strategy: "fill"})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("%-46s %8.1f ms  %+v\n", "account import preview", float64(time.Since(start).Microseconds())/1000, pv.Counts)
}

// Package stats computes the dashboard overview (port of packages/server/src/stats.ts).
package stats

import (
	"sort"
	"strings"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/search"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

type Brief struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	CategoryID   string          `json:"categoryId"`
	Library      shared.Library  `json:"library"`
	Status       *shared.Status  `json:"status"`
	Score        *int            `json:"score"`
	Current      shared.Progress `json:"current"`
	ConsumedUpTo shared.Progress `json:"consumedUpTo"`
	TotalUnits   *float64        `json:"totalUnits"`
	LeftOff      *string         `json:"leftOff"`
	Cover        any             `json:"cover"`
	UpdatedAt    string          `json:"updatedAt"`
	BehindBy     *float64        `json:"behindBy"`
}

type Totals struct {
	Entries   int      `json:"entries"`
	Reading   int      `json:"reading"`
	Watching  int      `json:"watching"`
	Completed int      `json:"completed"`
	Behind    int      `json:"behind"`
	AvgScore  *float64 `json:"avgScore"`
}

type CategoryCount struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Color   string         `json:"color"`
	Library shared.Library `json:"library"`
	Count   int            `json:"count"`
}

type ScoreBucket struct {
	Score int `json:"score"`
	Count int `json:"count"`
}

type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type Cooccurrence struct {
	A     string `json:"a"`
	B     string `json:"b"`
	Count int    `json:"count"`
}

type Overview struct {
	Totals          Totals          `json:"totals"`
	ByStatus        map[string]int  `json:"byStatus"`
	ByLibrary       map[string]int  `json:"byLibrary"`
	ByCategory      []CategoryCount `json:"byCategory"`
	ScoreDist       []ScoreBucket   `json:"scoreDistribution"`
	TopTags         []NameCount     `json:"topTags"`
	TagCooccurrence []Cooccurrence  `json:"tagCooccurrence"`
	InProgress      []Brief         `json:"inProgress"`
	BehindList      []Brief         `json:"behindList"`
	Backlog         int             `json:"backlog"`
	BacklogList     []Brief         `json:"backlogList"`
	RecentlyUpdated []Brief         `json:"recentlyUpdated"`
	HighestRated    []Brief         `json:"highestRated"`
	NeedsDecision   []Brief         `json:"needsDecision"`
	Heatmap         map[string]int  `json:"heatmap"`
}

func brief(e *shared.Entry) Brief {
	var cover any
	if c, ok := e.External["cover"]; ok {
		cover = c
	}
	return Brief{
		ID: e.ID, Title: e.Title, CategoryID: e.CategoryID, Library: e.Library, Status: e.Status, Score: e.Score,
		Current: e.Current, ConsumedUpTo: e.ConsumedUpTo, TotalUnits: e.TotalUnits, LeftOff: e.Notes.Extra1,
		Cover: cover, UpdatedAt: e.UpdatedAt, BehindBy: shared.BehindBy(e.Current, e.ConsumedUpTo),
	}
}

func briefs(es []*shared.Entry, n int) []Brief {
	if n < 0 || n > len(es) {
		n = len(es)
	}
	out := make([]Brief, n)
	for i := 0; i < n; i++ {
		out[i] = brief(es[i])
	}
	return out
}

func behind(e *shared.Entry) bool {
	b := shared.BehindBy(e.Current, e.ConsumedUpTo)
	return b != nil && *b > 0
}

// Compute builds the overview for all entries or one library ("" = both).
func Compute(s *store.Store, library string) (Overview, error) {
	// Pointers into the store's immutable snapshot: sorting moves 8-byte pointers, not ~400-byte entries.
	var all []*shared.Entry
	snap := s.All()
	for i := range snap {
		if library == "" || string(snap[i].Library) == library {
			all = append(all, &snap[i])
		}
	}
	o := Overview{ByStatus: map[string]int{}, ByLibrary: map[string]int{}}
	var scored []*shared.Entry
	sum := 0
	for _, e := range all {
		st := "none"
		if e.Status != nil {
			st = string(*e.Status)
		}
		o.ByStatus[st]++
		o.ByLibrary[string(e.Library)]++
		if e.Score != nil {
			scored = append(scored, e)
			sum += *e.Score
		}
		if e.Library == shared.LibraryReading {
			o.Totals.Reading++
		} else {
			o.Totals.Watching++
		}
		if e.Status != nil && *e.Status == shared.StatusCompleted {
			o.Totals.Completed++
		}
		if behind(e) {
			o.Totals.Behind++
		}
	}
	o.Totals.Entries = len(all)
	if len(scored) > 0 {
		avg := float64(sum) / float64(len(scored))
		o.Totals.AvgScore = &avg
	}

	o.ScoreDist = make([]ScoreBucket, 10)
	for i := range o.ScoreDist {
		o.ScoreDist[i].Score = i + 1
	}
	for _, e := range scored {
		if *e.Score >= 1 && *e.Score <= 10 {
			o.ScoreDist[*e.Score-1].Count++
		}
	}

	tagIdx := map[string]int{}
	tagList := []NameCount{}
	type pairKey struct{ a, b string }
	pairIdx := map[pairKey]int{}
	pairList := []Cooccurrence{}
	for _, e := range all {
		for _, t := range e.Tags {
			if i, ok := tagIdx[t]; ok {
				tagList[i].Count++
			} else {
				tagIdx[t] = len(tagList)
				tagList = append(tagList, NameCount{t, 1})
			}
		}
		sorted := append([]string(nil), e.Tags...)
		sort.Strings(sorted)
		for i := 0; i < len(sorted); i++ {
			for j := i + 1; j < len(sorted); j++ {
				k := pairKey{sorted[i], sorted[j]}
				if p, ok := pairIdx[k]; ok {
					pairList[p].Count++
				} else {
					pairIdx[k] = len(pairList)
					pairList = append(pairList, Cooccurrence{k.a, k.b, 1})
				}
			}
		}
	}
	sort.SliceStable(tagList, func(i, j int) bool {
		if tagList[i].Count != tagList[j].Count {
			return tagList[i].Count > tagList[j].Count
		}
		return search.LocaleCompare(tagList[i].Name, tagList[j].Name) < 0
	})
	o.TopTags = tagList[:min(25, len(tagList))]
	sort.SliceStable(pairList, func(i, j int) bool { return pairList[i].Count > pairList[j].Count })
	o.TagCooccurrence = pairList[:min(40, len(pairList))]

	byUpdated := append([]*shared.Entry(nil), all...)
	sort.SliceStable(byUpdated, func(i, j int) bool { return strings.Compare(byUpdated[i].UpdatedAt, byUpdated[j].UpdatedAt) > 0 })
	var inProg, planned, onHold, behindList []*shared.Entry
	for _, e := range byUpdated {
		switch {
		case e.Status != nil && *e.Status == shared.StatusInProgress:
			inProg = append(inProg, e)
		case e.Status != nil && *e.Status == shared.StatusPlanned:
			planned = append(planned, e)
		}
	}
	for _, e := range all {
		if e.Status != nil && *e.Status == shared.StatusOnHold {
			onHold = append(onHold, e)
		}
		if behind(e) {
			behindList = append(behindList, e)
		}
	}
	best := append([]*shared.Entry(nil), scored...)
	sort.SliceStable(best, func(i, j int) bool { return *best[i].Score > *best[j].Score })

	o.InProgress = briefs(inProg, 30)
	o.BehindList = briefs(behindList, -1)
	o.Backlog = len(planned)
	o.BacklogList = briefs(planned, 10)
	o.RecentlyUpdated = briefs(byUpdated, 10)
	o.HighestRated = briefs(best, 10)
	o.NeedsDecision = briefs(onHold, -1)

	o.ByCategory = []CategoryCount{}
	for _, c := range s.Categories() {
		if library == "" || string(c.Library) == library {
			o.ByCategory = append(o.ByCategory, CategoryCount{c.ID, c.Name, c.Color, c.Library, len(s.EntriesOf(c.ID))})
		}
	}

	since := store.ISO(time.Now().Add(-365 * 24 * time.Hour))
	recs, err := s.ReadActivity(since)
	if err != nil {
		return o, err
	}
	o.Heatmap = map[string]int{}
	for _, r := range recs {
		if len(r.At) >= 10 {
			o.Heatmap[r.At[:10]]++
		}
	}
	return o, nil
}

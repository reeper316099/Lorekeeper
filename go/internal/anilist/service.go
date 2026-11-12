package anilist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/reeper316099/Lorekeeper/go/internal/importexport"
	"github.com/reeper316099/Lorekeeper/go/internal/search"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

type QueueStatus string

const (
	QPending    QueueStatus = "pending"
	QConfirmed  QueueStatus = "confirmed"
	QRejected   QueueStatus = "rejected"
	QNever      QueueStatus = "never"
	QUnlinkable QueueStatus = "unlinkable"
)

// QueueItem is one entry's place in the match review queue (keys sorted: it is written to
// anilist/match-queue.json).
type QueueItem struct {
	Auto          *bool       `json:"auto,omitempty"`
	Candidates    []Candidate `json:"candidates"`
	Reason        *string     `json:"reason,omitempty"`
	Status        QueueStatus `json:"status"`
	SuggestedTags []string    `json:"suggestedTags"`
	UpdatedAt     string      `json:"updatedAt"`
}

type queueFile struct {
	Items map[string]QueueItem `json:"items"`
}

// itemPatch is a partial QueueItem; only the fields that are set replace the stored ones.
type itemPatch struct {
	Status        QueueStatus
	Candidates    *[]Candidate
	SuggestedTags *[]string
	Auto          *bool
	Reason        *string
}

var statusMap = map[string]shared.Status{
	"CURRENT": shared.StatusInProgress, "PLANNING": shared.StatusPlanned, "COMPLETED": shared.StatusCompleted,
	"DROPPED": shared.StatusDropped, "PAUSED": shared.StatusOnHold, "REPEATING": shared.StatusRepeating,
}

func dateOf(d *Date) *string {
	if d == nil || d.Year == nil || *d.Year == 0 {
		return nil
	}
	month, day := 1, 1
	if d.Month != nil {
		month = *d.Month
	}
	if d.Day != nil {
		day = *d.Day
	}
	s := fmt.Sprintf("%d-%02d-%02d", *d.Year, month, day)
	return &s
}

var (
	reBr   = regexp.MustCompile(`(?i)<br\s*/?>`)
	reHTML = regexp.MustCompile(`<[^>]+>`)
)

// stripHTML turns an AniList synopsis into plain text ("" when nothing is left).
func stripHTML(s *string) string {
	if s == nil || *s == "" {
		return ""
	}
	t := reBr.ReplaceAllString(*s, "\n")
	t = reHTML.ReplaceAllString(t, "")
	t = strings.NewReplacer("&amp;", "&", "&quot;", `"`, "&#039;", "'").Replace(t)
	return shared.JSTrim(t)
}

// SuggestedTagsFor are genres plus well-ranked, non-spoiler tags. Offered as suggestions, never applied automatically.
func SuggestedTagsFor(m Media) []string {
	var names []string
	names = append(names, m.Genres...)
	for _, t := range m.Tags {
		if !t.IsMediaSpoiler && t.Rank != nil && *t.Rank >= 60 {
			names = append(names, t.Name)
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func anilistID(e shared.Entry) int {
	if f, ok := e.External["anilistId"].(float64); ok {
		return int(f)
	}
	return 0
}

func externalString(e shared.Entry, key string) string {
	s, _ := e.External[key].(string)
	return s
}

func yearOf(m Media) int {
	if m.SeasonYear != nil && *m.SeasonYear != 0 {
		return *m.SeasonYear
	}
	if m.StartDate != nil && m.StartDate.Year != nil {
		return *m.StartDate.Year
	}
	return 0
}

// mediaExternal is the `external` data AniList contributes to an entry.
func mediaExternal(m Media) map[string]any {
	ext := map[string]any{"anilistId": float64(m.ID)}
	if m.IDMal != nil && *m.IDMal != 0 {
		ext["malId"] = float64(*m.IDMal)
	}
	if m.BannerImage != nil && *m.BannerImage != "" {
		ext["bannerUrl"] = *m.BannerImage
	}
	if m.Format != nil && *m.Format != "" {
		ext["format"] = *m.Format
	}
	if m.CountryOfOrigin != nil && *m.CountryOfOrigin != "" {
		ext["countryOfOrigin"] = *m.CountryOfOrigin
	}
	if m.Status != nil && *m.Status != "" {
		ext["apiStatus"] = *m.Status
	}
	if y := yearOf(m); y != 0 {
		ext["year"] = float64(y)
	}
	if m.AverageScore != nil && *m.AverageScore != 0 {
		ext["averageScore"] = float64(*m.AverageScore)
	}
	if d := stripHTML(m.Description); d != "" {
		ext["description"] = d
	}
	return ext
}

func coverURL(m Media) string {
	if m.CoverImage == nil {
		return ""
	}
	if m.CoverImage.ExtraLarge != nil && *m.CoverImage.ExtraLarge != "" {
		return *m.CoverImage.ExtraLarge
	}
	return deref(m.CoverImage.Large)
}

type ListImportOptions struct {
	Username        string
	Types           []string
	Strategy        importexport.Strategy
	AnimeCategoryID string
	MangaCategoryID string
}

type prepared struct {
	creates   []shared.Entry
	updates   []shared.Entry
	unchanged int
	skipped   int
	covers    map[string]string // entry id -> remote cover url
	order     []string
	createdAt time.Time
}

// Service ties the client, the store and the review queue together.
type Service struct {
	Store  *store.Store
	Client *Client
	Jobs   *JobRegistry

	qmu   sync.Mutex
	queue *queueFile

	pmu      sync.Mutex
	previews map[string]*prepared
}

func NewService(s *store.Store, c *Client, jobs *JobRegistry) *Service {
	return &Service{Store: s, Client: c, Jobs: jobs, previews: map[string]*prepared{}}
}

// ---------------- queue persistence ----------------

func (s *Service) queueFilePath() string {
	return filepath.Join(s.Store.Dir, "anilist", "match-queue.json")
}

// loadQueueLocked requires qmu.
func (s *Service) loadQueueLocked() *queueFile {
	if s.queue != nil {
		return s.queue
	}
	q := &queueFile{Items: map[string]QueueItem{}}
	if b, err := os.ReadFile(s.queueFilePath()); err == nil {
		var raw queueFile
		if json.Unmarshal(b, &raw) == nil && raw.Items != nil {
			q.Items = raw.Items
		}
	}
	s.queue = q
	return q
}

func (s *Service) saveQueueLocked() error {
	if s.queue == nil {
		return nil
	}
	return s.Store.WriteJSON(s.queueFilePath(), s.queue)
}

func (s *Service) saveQueue() error {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	return s.saveQueueLocked()
}

func (s *Service) setItem(entryID string, p itemPatch) {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	q := s.loadQueueLocked()
	next, ok := q.Items[entryID]
	if !ok {
		next = QueueItem{Candidates: []Candidate{}, SuggestedTags: []string{}}
	}
	next.Status = p.Status
	if p.Candidates != nil {
		next.Candidates = *p.Candidates
	}
	if p.SuggestedTags != nil {
		next.SuggestedTags = *p.SuggestedTags
	}
	if p.Auto != nil {
		next.Auto = p.Auto
	}
	if p.Reason != nil {
		next.Reason = p.Reason
	}
	if next.Candidates == nil {
		next.Candidates = []Candidate{}
	}
	if next.SuggestedTags == nil {
		next.SuggestedTags = []string{}
	}
	next.UpdatedAt = store.ISO(time.Now())
	q.Items[entryID] = next
}

func (s *Service) item(entryID string) (QueueItem, bool) {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	it, ok := s.loadQueueLocked().Items[entryID]
	return it, ok
}

// Counts summarises the queue and how many titles are matched.
type Counts struct {
	Matched int                 `json:"matched"`
	Queue   map[QueueStatus]int `json:"queue"`
	Total   int                 `json:"total"`
}

func (s *Service) Counts() Counts {
	s.qmu.Lock()
	items := maps(s.loadQueueLocked().Items)
	s.qmu.Unlock()
	out := map[QueueStatus]int{QPending: 0, QConfirmed: 0, QRejected: 0, QNever: 0, QUnlinkable: 0}
	for id, it := range items {
		if _, ok := s.Store.GetEntry(id); ok {
			out[it.Status]++
		}
	}
	all := s.Store.All()
	matched := 0
	for _, e := range all {
		if anilistID(e) != 0 {
			matched++
		}
	}
	return Counts{Matched: matched, Queue: out, Total: len(all)}
}

func maps(m map[string]QueueItem) map[string]QueueItem {
	out := make(map[string]QueueItem, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---------------- searching ----------------

type searchVars struct {
	Search  string  `json:"search"`
	Type    string  `json:"type"`
	Country *string `json:"country,omitempty"`
	PerPage int     `json:"perPage"`
}

type searchData struct {
	Page struct {
		Media []Media `json:"media"`
	} `json:"Page"`
}

type mediaData struct {
	Media Media `json:"Media"`
}

type searchResult struct {
	Media []Media
	Stale bool
}

func (s *Service) searchMedia(ctx context.Context, text, typ, country string) (searchResult, error) {
	vars := searchVars{Search: text, Type: typ, PerPage: 8}
	query := SearchQueryAnyCountry
	if country != "" {
		vars.Country = &country
		query = SearchQuery
	}
	r, err := CachedRequest[searchData](ctx, s.Client, HashKey("search", vars), query, vars)
	if err != nil {
		return searchResult{}, err
	}
	for _, m := range r.Data.Page.Media {
		if err := s.Client.PrimeCache(fmt.Sprintf("media-%d", m.ID), map[string]any{"Media": m}); err != nil {
			return searchResult{}, err
		}
	}
	return searchResult{r.Data.Page.Media, r.Stale}, nil
}

func (s *Service) mediaByID(ctx context.Context, id int) (Media, error) {
	vars := map[string]any{"id": id}
	r, err := CachedRequest[mediaData](ctx, s.Client, fmt.Sprintf("media-%d", id), ByIDQuery, vars)
	return r.Data.Media, err
}

var reTitleSplit = regexp.MustCompile(`[(:\[]`)
var reParenBracket = regexp.MustCompile(`\(.*?\)|\[.*?\]`)

type searchOutcome struct {
	plan       SearchPlan
	media      []Media
	candidates []Candidate
}

func (s *Service) searchFor(ctx context.Context, e shared.Entry) (searchOutcome, error) {
	plan := PlanSearch(e)
	if !plan.Linkable {
		return searchOutcome{plan: plan, candidates: []Candidate{}}, nil
	}
	r, err := s.searchMedia(ctx, e.Title, plan.Type, plan.Country)
	if err != nil {
		return searchOutcome{}, err
	}
	media := r.Media
	if len(media) == 0 && reTitleSplit.MatchString(e.Title) {
		shorter := shared.JSTrim(strings.SplitN(reParenBracket.ReplaceAllString(e.Title, ""), ":", 2)[0])
		if shorter != "" && shorter != e.Title {
			if r, err = s.searchMedia(ctx, shorter, plan.Type, plan.Country); err != nil {
				return searchOutcome{}, err
			}
			media = r.Media
		}
	}
	return searchOutcome{plan, media, Rank(e.Title, e.AltTitles, media, plan, 5)}, nil
}

func (s *Service) requireEntry(id string) (shared.Entry, error) {
	e, ok := s.Store.GetEntry(id)
	if !ok {
		return e, &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "Entry not found"}
	}
	return e, nil
}

// CandidatesFor lists candidates for one entry (also used by "pick another" with a custom query).
func (s *Service) CandidatesFor(ctx context.Context, entryID, query string) ([]Candidate, error) {
	e, err := s.requireEntry(entryID)
	if err != nil {
		return nil, err
	}
	plan := PlanSearch(e)
	if !plan.Linkable {
		return []Candidate{}, nil
	}
	if q := shared.JSTrim(query); q != "" {
		r, err := s.searchMedia(ctx, q, plan.Type, "")
		if err != nil {
			return nil, err
		}
		media := r.Media
		if len(media) == 0 {
			// AniList does not forgive typos but does match word beginnings, so retry with the
			// last word cut short, then without it ("I will fall with the emporer" still finds it).
			for _, alt := range typoFallbacks(q) {
				fr, err := s.searchMedia(ctx, alt, plan.Type, "")
				if err != nil {
					return nil, err
				}
				if len(fr.Media) > 0 {
					media = fr.Media
					break
				}
			}
		}
		plan.Country = ""
		return Rank(e.Title, e.AltTitles, media, plan, 8), nil
	}
	o, err := s.searchFor(ctx, e)
	return o.candidates, err
}

// typoFallbacks lists looser versions of a multi-word query to try when it finds nothing.
func typoFallbacks(q string) []string {
	words := strings.Fields(q)
	if len(words) < 2 {
		return nil
	}
	last := []rune(words[len(words)-1])
	head := strings.Join(words[:len(words)-1], " ")
	var out []string
	if len(last) > 3 {
		out = append(out, head+" "+string(last[:3]))
	}
	return append(out, head)
}

// ---------------- applying a match ----------------

// ApplyMatch links an entry to an AniList title: external data, alternate titles, cover, link, media type.
func (s *Service) ApplyMatch(ctx context.Context, entryID string, m Media, confidence float64, auto bool) (shared.Entry, error) {
	e, err := s.requireEntry(entryID)
	if err != nil {
		return e, err
	}
	cover := externalString(e, "cover")
	if url := coverURL(m); url != "" {
		name, err := s.Client.SaveCover(ctx, m.ID, url)
		switch {
		case err == nil:
			cover = name
		case errors.As(err, &CancelledError{}):
			return e, err // otherwise keep going without a cover
		}
	}
	titles := MediaTitles(m)
	seen := map[string]bool{}
	alt := []string{}
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			alt = append(alt, t)
		}
	}
	for _, t := range e.AltTitles {
		add(t)
	}
	for _, t := range titles {
		if shared.NormalizeTitle(t) != shared.NormalizeTitle(e.Title) {
			add(t)
		}
	}
	country := deref(m.CountryOfOrigin)
	countryType := shared.MediaType("")
	switch country {
	case "KR":
		countryType = "manhwa"
	case "CN":
		countryType = "manhua"
	case "JP":
		countryType = "manga"
	}
	mediaType := e.MediaType
	if e.Library == shared.LibraryReading && mediaType == "unknown" {
		switch {
		case deref(m.Format) == "NOVEL":
			mediaType = "novel"
		case countryType != "":
			mediaType = countryType
		}
	}
	if e.Library == shared.LibraryWatching && mediaType == "unknown" {
		mediaType = "anime"
		if country == "CN" {
			mediaType = "donghua"
		}
	}
	links := e.Links
	hasLink := slices.ContainsFunc(e.Links, func(l shared.Link) bool {
		return l.Kind == shared.LinkAniList || (l.URL == nil) == (m.SiteURL == nil) && (l.URL == nil || *l.URL == *m.SiteURL)
	})
	if !hasLink && m.SiteURL != nil {
		links = append(slices.Clone(e.Links), shared.Link{Kind: shared.LinkAniList, URL: m.SiteURL, Label: str("AniList")})
	}
	total := m.Episodes
	if m.Type == "MANGA" {
		total = m.Chapters
	}
	ext := mediaExternal(m)
	ext["matchConfidence"] = confidence
	ext["matchLocked"] = true
	if cover != "" {
		ext["cover"] = cover
	}
	patch := map[string]any{"altTitles": alt, "mediaType": string(mediaType), "links": links, "external": ext}
	if e.TotalUnits == nil && total != nil && *total != 0 {
		patch["totalUnits"] = float64(*total)
	}
	updated, err := s.Store.UpdateEntry(entryID, patch)
	if err != nil {
		return updated, err
	}
	tags := SuggestedTagsFor(m)
	s.setItem(entryID, itemPatch{Status: QConfirmed, Auto: &auto, SuggestedTags: &tags, Candidates: &[]Candidate{}})
	return updated, s.saveQueue()
}

// Confirm accepts one of the queued candidates.
func (s *Service) Confirm(ctx context.Context, entryID string, mediaID int) (shared.Entry, error) {
	confidence := 1.0
	if it, ok := s.item(entryID); ok {
		for _, c := range it.Candidates {
			if c.ID == mediaID {
				confidence = c.Confidence
			}
		}
	}
	m, err := s.mediaByID(ctx, mediaID)
	if err != nil {
		return shared.Entry{}, err
	}
	return s.ApplyMatch(ctx, entryID, m, confidence, false)
}

// SetByRef links an entry to a pasted AniList URL or numeric id.
func (s *Service) SetByRef(ctx context.Context, entryID, ref string) (shared.Entry, error) {
	id, _, ok := ParseRef(ref)
	if !ok {
		return shared.Entry{}, &store.HTTPError{Status: 400, Code: "VALIDATION", Message: "Paste an AniList URL or numeric ID"}
	}
	m, err := s.mediaByID(ctx, id)
	if err == nil {
		return s.ApplyMatch(ctx, entryID, m, 1, false)
	}
	var ae *AniListError
	if errors.As(err, &ae) && ae.Status == 404 {
		return shared.Entry{}, &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "AniList has no title with that ID"}
	}
	return shared.Entry{}, err
}

func (s *Service) Reject(entryID string) error {
	s.setItem(entryID, itemPatch{Status: QRejected, Candidates: &[]Candidate{}})
	return s.saveQueue()
}

func (s *Service) Never(entryID string) error {
	s.setItem(entryID, itemPatch{Status: QNever, Candidates: &[]Candidate{}})
	return s.saveQueue()
}

// SetPending (re)queues an entry with fresh candidates ("retry").
func (s *Service) SetPending(entryID string, c []Candidate) error {
	s.qmu.Lock()
	q := s.loadQueueLocked()
	q.Items[entryID] = QueueItem{Status: QPending, Candidates: c, SuggestedTags: []string{}, UpdatedAt: store.ISO(time.Now())}
	err := s.saveQueueLocked()
	s.qmu.Unlock()
	return err
}

var linkedKeys = []string{"anilistId", "malId", "matchConfidence", "matchLocked", "cover", "bannerUrl", "format", "countryOfOrigin", "apiStatus", "year", "averageScore", "description"}

// Unmatch removes a link (and its lock) so the entry can be matched again, and frees what only
// this link needed: the cover file and the cached metadata and artwork.
func (s *Service) Unmatch(entryID string) (shared.Entry, error) {
	e, err := s.requireEntry(entryID)
	if err != nil {
		return e, err
	}
	external := make(map[string]any, len(e.External))
	for k, v := range e.External {
		external[k] = v
	}
	for _, k := range linkedKeys {
		delete(external, k)
	}
	s.qmu.Lock()
	delete(s.loadQueueLocked().Items, entryID)
	err = s.saveQueueLocked()
	s.qmu.Unlock()
	if err != nil {
		return e, err
	}
	kept := []shared.Link{}
	for _, l := range e.Links {
		if l.Kind != shared.LinkAniList {
			kept = append(kept, l)
		}
	}
	out, err := s.Store.UpdateEntry(entryID, map[string]any{"links": kept})
	if err != nil {
		return out, err
	}
	// `external` is merged shallowly by UpdateEntry, so replace it through the store directly.
	out.External = external
	if err := s.Store.ReplaceEntries([]shared.Entry{out}); err != nil {
		return out, err
	}
	cover, id := externalString(e, "cover"), anilistID(e)
	coverShared, idShared := false, false
	for _, o := range s.Store.All() {
		if o.ID == entryID {
			continue
		}
		coverShared = coverShared || (cover != "" && externalString(o, "cover") == cover)
		idShared = idShared || (id != 0 && anilistID(o) == id)
	}
	if cover != "" && !coverShared {
		s.Client.RemoveCover(cover)
	}
	if id != 0 && !idShared {
		s.Client.PurgeMedia(id)
	}
	got, _ := s.Store.GetEntry(entryID)
	return got, nil
}

// ClearResult is the response of "clear AniList data".
type ClearResult struct {
	Bytes         int64 `json:"bytes"`
	CoversRemoved int   `json:"coversRemoved"`
	Files         int   `json:"files"`
}

// ClearCaches clears cached AniList responses and images and drops covers nothing points at.
func (s *Service) ClearCaches() ClearResult {
	stats := s.Client.ClearCache()
	inUse := map[string]bool{}
	for _, e := range s.Store.All() {
		if c := externalString(e, "cover"); c != "" {
			inUse[c] = true
		}
	}
	return ClearResult{stats.Bytes, s.Client.PruneCovers(inUse), stats.Files}
}

// AddSuggestedTags adds AniList's suggested genres/tags to an entry on request.
func (s *Service) AddSuggestedTags(ctx context.Context, entryID string) (shared.Entry, error) {
	e, err := s.requireEntry(entryID)
	if err != nil {
		return e, err
	}
	var tags []string
	if it, ok := s.item(entryID); ok {
		tags = it.SuggestedTags
	}
	if len(tags) == 0 && anilistID(e) != 0 {
		m, err := s.mediaByID(ctx, anilistID(e))
		if err != nil {
			return e, err
		}
		tags = SuggestedTagsFor(m)
	}
	return s.Store.UpdateEntry(entryID, map[string]any{"tags": append(slices.Clone(e.Tags), tags...)})
}

// ---------------- matching job ----------------

// MatchResult is the result of a matching job.
type MatchResult struct {
	AutoConfirmed int `json:"autoConfirmed"`
	Queued        int `json:"queued"`
	Unlinkable    int `json:"unlinkable"`
}

// StartMatching starts a background job; entryIDs nil means every unmatched, unlocked entry.
func (s *Service) StartMatching(entryIDs []string) Job {
	var targets []string
	for _, e := range s.Store.All() {
		if entryIDs != nil {
			if slices.Contains(entryIDs, e.ID) {
				targets = append(targets, e.ID)
			}
		} else if anilistID(e) == 0 && e.External["matchLocked"] == nil {
			targets = append(targets, e.ID)
		}
	}
	id, ctx := s.Jobs.Create("match", len(targets))
	go s.runMatching(ctx, id, targets, entryIDs != nil)
	job, _ := s.Jobs.Get(id)
	return job
}

func (s *Service) runMatching(ctx context.Context, id string, ids []string, explicit bool) {
	threshold := s.Store.Settings().AutoConfirmThreshold
	var res MatchResult
	progress := func(patch func(*Job)) Job { return s.Jobs.Update(id, patch) }
	err := func() error {
		done := 0
		for _, eid := range ids {
			if ctx.Err() != nil {
				return CancelledError{}
			}
			e, ok := s.Store.GetEntry(eid)
			prev, hasPrev := s.item(eid)
			skip := !ok || anilistID(e) != 0 ||
				(!explicit && hasPrev && slices.Contains([]QueueStatus{QNever, QRejected, QConfirmed, QPending}, prev.Status))
			if skip {
				done++
				progress(func(j *Job) { j.Done = done })
				continue
			}
			plan := PlanSearch(e)
			if !plan.Linkable {
				s.setItem(eid, itemPatch{Status: QUnlinkable, Reason: &plan.Reason})
				res.Unlinkable++
			} else {
				o, err := s.searchFor(ctx, e)
				if err != nil {
					return err
				}
				if ShouldAutoConfirm(o.candidates, threshold, defaultMargin) {
					idx := slices.IndexFunc(o.media, func(m Media) bool { return m.ID == o.candidates[0].ID })
					if _, err := s.ApplyMatch(ctx, eid, o.media[idx], o.candidates[0].Confidence, true); err != nil {
						return err
					}
					res.AutoConfirmed++
				} else {
					s.setItem(eid, itemPatch{Status: QPending, Candidates: &o.candidates})
					res.Queued++
				}
			}
			done++
			title := e.Title
			progress(func(j *Job) { j.Done, j.Message = done, &title })
			if done%10 == 0 {
				if err := s.saveQueue(); err != nil {
					return err
				}
			}
		}
		return s.saveQueue()
	}()
	if err != nil {
		_ = s.saveQueue()
	}
	s.finishJob(id, err, res, "")
}

// finishJob records how a job ended, with the same states and messages the UI expects.
func (s *Service) finishJob(id string, err error, result any, cancelMsg string) {
	var rl *RateLimitError
	s.Jobs.Update(id, func(j *Job) {
		switch {
		case err == nil:
			j.State, j.Message, j.Result = JobDone, nil, result
		case errors.As(err, &CancelledError{}):
			msg := "Cancelled"
			if cancelMsg != "" {
				msg = cancelMsg
			}
			j.State, j.Message = JobCancelled, &msg
			if result != nil {
				j.Result = result
			}
		case errors.As(err, &OfflineError{}):
			msg := "AniList is unreachable. Check your connection and run matching again."
			if cancelMsg != "" {
				msg = "AniList is unreachable."
			}
			j.State, j.Message = JobOffline, &msg
			if result != nil {
				j.Result = result
			}
		case errors.As(err, &rl):
			msg := fmt.Sprintf("Rate limited by AniList. Try again in %ds.", rl.RetryAfterSec)
			if cancelMsg != "" {
				msg = err.Error()
			}
			j.State, j.Message = JobError, &msg
			if result != nil {
				j.Result = result
			}
		default:
			msg := err.Error()
			j.State, j.Message = JobError, &msg
			if result != nil {
				j.Result = result
			}
		}
	})
}

// QueueRow is one review-queue item with its entry.
type QueueRow struct {
	Entry shared.Entry `json:"entry"`
	QueueItem
}

// QueueItems lists the queue for a status, ordered by title.
func (s *Service) QueueItems(status QueueStatus) []QueueRow {
	s.qmu.Lock()
	items := maps(s.loadQueueLocked().Items)
	s.qmu.Unlock()
	out := []QueueRow{}
	for id, it := range items {
		if it.Status != status {
			continue
		}
		if e, ok := s.Store.GetEntry(id); ok {
			out = append(out, QueueRow{e, it})
		}
	}
	slices.SortStableFunc(out, func(a, b QueueRow) int { return search.LocaleCompare(a.Entry.Title, b.Entry.Title) })
	return out
}

// ---------------- import from an AniList account ----------------

func (s *Service) fetchList(ctx context.Context, username, typ string) ([]ListEntry, error) {
	seen := map[int]bool{}
	var out []ListEntry
	for chunk := 1; chunk < 50; chunk++ {
		raw, err := s.Client.Request(ctx, ListQuery, map[string]any{"userName": username, "type": typ, "chunk": chunk})
		if err != nil {
			return nil, err
		}
		var data listCollection
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, err
		}
		col := data.MediaListCollection
		for _, list := range col.Lists {
			if list.IsCustomList {
				continue // custom lists repeat entries already in the status lists
			}
			for _, e := range list.Entries {
				if !seen[e.Media.ID] {
					seen[e.Media.ID] = true
					out = append(out, e)
				}
			}
		}
		if !col.HasNextChunk {
			break
		}
	}
	return out, nil
}

func jsRound(f float64) float64 { return math.Floor(f + 0.5) }

func formatProgress(f float64) string {
	if f == math.Trunc(f) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func (s *Service) toEntry(e ListEntry, categoryID string, library shared.Library, now string) (shared.Entry, error) {
	m := e.Media
	title := fmt.Sprintf("AniList #%d", m.ID)
	if t := firstNonNil(m.Title.Romaji, m.Title.English, m.Title.Native); t != nil {
		title = *t
	}
	seen := map[string]bool{}
	alt := []string{}
	for _, t := range append([]string{deref(m.Title.English), deref(m.Title.Native)}, m.Synonyms...) {
		if t != "" && shared.NormalizeTitle(t) != shared.NormalizeTitle(title) && !seen[t] {
			seen[t] = true
			alt = append(alt, t)
		}
	}
	var mediaType shared.MediaType
	if library == shared.LibraryReading {
		switch {
		case deref(m.Format) == "NOVEL":
			mediaType = "novel"
		case deref(m.CountryOfOrigin) == "KR":
			mediaType = "manhwa"
		case deref(m.CountryOfOrigin) == "CN":
			mediaType = "manhua"
		default:
			mediaType = "manga"
		}
	} else if deref(m.CountryOfOrigin) == "CN" {
		mediaType = "donghua"
	} else {
		mediaType = "anime"
	}
	id, err := uuid.NewV7()
	if err != nil {
		return shared.Entry{}, err
	}
	st := statusMap[e.Status]
	entry := shared.NewEntry()
	entry.ID, entry.Library, entry.CategoryID, entry.Title = id.String(), library, categoryID, title
	entry.AltTitles, entry.MediaType, entry.Status = alt, mediaType, &st
	if e.Score != nil && *e.Score > 0 {
		score := int(math.Min(10, jsRound(*e.Score)))
		entry.Score = &score
	}
	if e.Progress != nil && *e.Progress > 0 {
		raw, n, kind := formatProgress(*e.Progress), *e.Progress, shared.ProgressNumber
		entry.Current = shared.Progress{Raw: &raw, Number: &n, Kind: &kind}
	}
	total := m.Episodes
	if library == shared.LibraryReading {
		total = m.Chapters
	}
	if total != nil {
		t := float64(*total)
		entry.TotalUnits = &t
	}
	if m.SiteURL != nil {
		entry.Links = []shared.Link{{Kind: shared.LinkAniList, URL: m.SiteURL, Label: str("AniList")}}
	}
	if e.Notes != nil {
		entry.Notes.General = shared.CleanText(*e.Notes)
	}
	ext := mediaExternal(m)
	ext["matchConfidence"] = float64(1)
	ext["matchLocked"] = true
	entry.External = ext
	entry.StartedAt, entry.CompletedAt = dateOf(e.StartedAt), dateOf(e.CompletedAt)
	entry.CreatedAt, entry.UpdatedAt = now, now
	return entry, entry.Validate()
}

// ListPreview is the response of the account-import preview.
type ListPreview struct {
	Counts struct {
		Create    int `json:"create"`
		Fetched   int `json:"fetched"`
		Skipped   int `json:"skipped"`
		Unchanged int `json:"unchanged"`
		Update    int `json:"update"`
	} `json:"counts"`
	PreviewID string        `json:"previewId"`
	Sample    []SampleEntry `json:"sample"`
	Username  string        `json:"username"`
	Warnings  []string      `json:"warnings"`
}

type SampleEntry struct {
	Library  shared.Library `json:"library"`
	Progress *string        `json:"progress"`
	Score    *int           `json:"score"`
	Status   *shared.Status `json:"status"`
	Title    string         `json:"title"`
}

// PreviewList fetches a public AniList list and works out what an import would do, writing nothing.
func (s *Service) PreviewList(ctx context.Context, opts ListImportOptions) (*ListPreview, error) {
	strategy := opts.Strategy
	if strategy == "" {
		strategy = importexport.StrategyFill
	}
	now := store.ISO(time.Now())
	warnings := []string{}
	cats := s.Store.Categories()
	pick := func(id string, lib shared.Library, fallback string) *shared.Category {
		for i, c := range cats {
			if c.ID == id && c.Library == lib {
				return &cats[i]
			}
		}
		for i, c := range cats {
			if c.Library == lib && strings.ToLower(c.Name) == fallback {
				return &cats[i]
			}
		}
		for i, c := range cats {
			if c.Library == lib {
				return &cats[i]
			}
		}
		return nil
	}
	p := &prepared{creates: []shared.Entry{}, updates: []shared.Entry{}, covers: map[string]string{}, createdAt: time.Now()}
	existing := s.Store.All()
	byAni := map[string]shared.Entry{}
	byTitle := map[string]shared.Entry{}
	for _, e := range existing {
		if id := anilistID(e); id != 0 {
			byAni[fmt.Sprintf("%s:%d", e.Library, id)] = e
		}
		for _, t := range append([]string{e.Title}, e.AltTitles...) {
			k := string(e.Library) + ":" + shared.NormalizeTitle(t)
			if _, ok := byTitle[k]; !ok {
				byTitle[k] = e
			}
		}
	}
	touched := map[string]shared.Entry{}
	var touchedOrder []string
	total := 0

	for _, typ := range opts.Types {
		library, catID, fallback := shared.LibraryReading, opts.MangaCategoryID, "misc."
		if typ == "ANIME" {
			library, catID, fallback = shared.LibraryWatching, opts.AnimeCategoryID, "anime"
		}
		cat := pick(catID, library, fallback)
		if cat == nil {
			warnings = append(warnings, fmt.Sprintf("No %s category exists to import %s into. Create one first.", library, strings.ToLower(typ)))
			continue
		}
		list, err := s.fetchList(ctx, opts.Username, typ)
		if err != nil {
			var ae *AniListError
			if errors.As(err, &ae) && ae.Status == 404 {
				return nil, &store.HTTPError{Status: 404, Code: "USER_NOT_FOUND", Message: fmt.Sprintf("AniList has no public list for “%s”.", opts.Username)}
			}
			return nil, err
		}
		for _, item := range list {
			total++
			incoming, err := s.toEntry(item, cat.ID, library, now)
			if err != nil {
				return nil, err
			}
			hit, found := byAni[fmt.Sprintf("%s:%d", library, item.Media.ID)]
			if !found {
				for _, t := range append([]string{incoming.Title}, incoming.AltTitles...) {
					if h, ok := byTitle[string(library)+":"+shared.NormalizeTitle(t)]; ok {
						hit, found = h, true
						break
					}
				}
			}
			cover := coverURL(item.Media)
			if found && strategy != importexport.StrategyKeepBoth {
				cur := hit
				if t, ok := touched[hit.ID]; ok {
					cur = t
				}
				if strategy == importexport.StrategySkip {
					p.skipped++
					continue
				}
				merged, mergedChanged := importexport.MergeInto(cur, incoming, strategy)
				base := cur
				if mergedChanged {
					base = merged
				}
				enriched, enrichedChanged := enrich(base, incoming, strategy == importexport.StrategyOverwrite)
				if !mergedChanged && !enrichedChanged {
					p.unchanged++
				} else {
					if _, ok := touched[hit.ID]; !ok {
						touchedOrder = append(touchedOrder, hit.ID)
					}
					touched[hit.ID] = enriched
					if cover != "" && externalString(enriched, "cover") == "" {
						p.covers[hit.ID] = cover
						p.order = append(p.order, hit.ID)
					}
				}
				continue
			}
			p.creates = append(p.creates, incoming)
			if cover != "" {
				p.covers[incoming.ID] = cover
				p.order = append(p.order, incoming.ID)
			}
		}
	}
	for _, id := range touchedOrder {
		p.updates = append(p.updates, touched[id])
	}
	previewID := uuid.NewString()
	s.pmu.Lock()
	s.previews[previewID] = p
	for k, v := range s.previews {
		if time.Since(v.createdAt) > 30*time.Minute {
			delete(s.previews, k)
		}
	}
	s.pmu.Unlock()

	out := &ListPreview{PreviewID: previewID, Username: opts.Username, Warnings: warnings, Sample: []SampleEntry{}}
	out.Counts.Fetched, out.Counts.Create, out.Counts.Update = total, len(p.creates), len(p.updates)
	out.Counts.Unchanged, out.Counts.Skipped = p.unchanged, p.skipped
	for _, e := range p.creates[:min(12, len(p.creates))] {
		out.Sample = append(out.Sample, SampleEntry{Library: e.Library, Progress: e.Current.Raw, Score: e.Score, Status: e.Status, Title: e.Title})
	}
	return out, nil
}

// enrich makes sure the AniList identity is attached to an existing entry after merging fields.
func enrich(e, incoming shared.Entry, overwrite bool) (shared.Entry, bool) {
	external := map[string]any{}
	first, second := incoming.External, e.External
	if overwrite {
		first, second = e.External, incoming.External
	}
	for k, v := range first {
		external[k] = v
	}
	for k, v := range second {
		external[k] = v
	}
	seen := map[string]bool{}
	alt := []string{}
	for _, t := range append(slices.Clone(e.AltTitles), incoming.AltTitles...) {
		if !seen[t] && shared.NormalizeTitle(t) != shared.NormalizeTitle(e.Title) {
			seen[t] = true
			alt = append(alt, t)
		}
	}
	links := e.Links
	if !slices.ContainsFunc(e.Links, func(l shared.Link) bool { return l.Kind == shared.LinkAniList }) {
		links = slices.Clone(e.Links)
		for _, l := range incoming.Links {
			if l.Kind == shared.LinkAniList {
				links = append(links, l)
			}
		}
	}
	next := e
	next.External, next.AltTitles, next.Links = external, alt, links
	if e.TotalUnits == nil {
		next.TotalUnits = incoming.TotalUnits
	}
	key := func(x shared.Entry) string {
		b, _ := json.Marshal([]any{x.External, x.AltTitles, x.Links, x.TotalUnits})
		return string(b)
	}
	if key(next) == key(e) {
		return e, false
	}
	next.UpdatedAt = store.ISO(time.Now())
	return next, true
}

// ListImportResult is the result of the commit job.
type ListImportResult struct {
	Created   int    `json:"created"`
	Skipped   int    `json:"skipped"`
	Unchanged int    `json:"unchanged"`
	UndoToken string `json:"undoToken"`
	Updated   int    `json:"updated"`
}

// StartListImport commits a previewed import as a background job: covers first (cancellable, nothing is
// written if cancelled), then a snapshot, then the entries.
func (s *Service) StartListImport(previewID string) (Job, error) {
	s.pmu.Lock()
	p := s.previews[previewID]
	s.pmu.Unlock()
	if p == nil {
		return Job{}, &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "Preview expired. Run the preview again."}
	}
	steps := len(p.covers) + 1
	id, ctx := s.Jobs.Create("import", steps)
	go func() {
		err := func() error {
			files := map[string]string{}
			var fmu sync.Mutex
			var next int
			var nmu sync.Mutex
			var firstErr error
			var wg sync.WaitGroup
			for w := 0; w < 3; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						nmu.Lock()
						if next >= len(p.order) || firstErr != nil {
							nmu.Unlock()
							return
						}
						eid := p.order[next]
						next++
						nmu.Unlock()
						var aid int
						for _, e := range append(slices.Clone(p.creates), p.updates...) {
							if e.ID == eid {
								aid = anilistID(e)
							}
						}
						if aid != 0 {
							name, err := s.Client.SaveCover(ctx, aid, p.covers[eid])
							switch {
							case err == nil:
								fmu.Lock()
								files[eid] = name
								fmu.Unlock()
							case errors.As(err, &CancelledError{}):
								nmu.Lock()
								firstErr = err
								nmu.Unlock()
								return
							}
						}
						msg := "Downloading covers"
						s.Jobs.Update(id, func(j *Job) { j.Done++; j.Message = &msg })
					}
				}()
			}
			wg.Wait()
			if firstErr != nil {
				return firstErr
			}
			if ctx.Err() != nil {
				return CancelledError{}
			}
			withCover := func(e shared.Entry) shared.Entry {
				if name, ok := files[e.ID]; ok {
					ext := make(map[string]any, len(e.External)+1)
					for k, v := range e.External {
						ext[k] = v
					}
					ext["cover"] = name
					e.External = ext
				}
				return e
			}
			creates, updates := make([]shared.Entry, len(p.creates)), make([]shared.Entry, len(p.updates))
			catSeen := map[string]bool{}
			var cats []string
			for i, e := range p.creates {
				creates[i] = withCover(e)
			}
			for i, e := range p.updates {
				updates[i] = withCover(e)
			}
			for _, e := range append(slices.Clone(creates), updates...) {
				if !catSeen[e.CategoryID] {
					catSeen[e.CategoryID] = true
					cats = append(cats, e.CategoryID)
				}
			}
			undoID, _, err := importexport.BeginUndoable(s.Store, cats)
			if err != nil {
				return err
			}
			if len(creates) > 0 {
				if err := s.Store.InsertEntries(creates); err != nil {
					return err
				}
			}
			if len(updates) > 0 {
				if err := s.Store.ReplaceEntries(updates); err != nil {
					return err
				}
			}
			s.pmu.Lock()
			delete(s.previews, previewID)
			s.pmu.Unlock()
			s.Jobs.Update(id, func(j *Job) {
				j.State, j.Done, j.Message = JobDone, steps, nil
				j.Result = ListImportResult{len(creates), p.skipped, p.unchanged, undoID, len(updates)}
			})
			return nil
		}()
		if err != nil {
			s.finishJob(id, err, nil, "Cancelled. Nothing was imported.")
		}
	}()
	job, _ := s.Jobs.Get(id)
	return job, nil
}

package anilist

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// SearchPlan says which AniList search (if any) fits an entry.
type SearchPlan struct {
	Linkable bool
	Reason   string
	Type     string // ANIME | MANGA
	Country  string // KR | CN | JP, "" for any
}

// Candidate is one possible AniList match, stored in the review queue (keys sorted).
type Candidate struct {
	Chapters   *int     `json:"chapters"`
	Confidence float64  `json:"confidence"`
	Country    *string  `json:"country"`
	Cover      *string  `json:"cover"`
	Episodes   *int     `json:"episodes"`
	Format     *string  `json:"format"`
	ID         int      `json:"id"`
	SiteURL    *string  `json:"siteUrl"`
	Title      string   `json:"title"`
	Titles     []string `json:"titles"`
	Type       string   `json:"type"`
	Year       *int     `json:"year"`
}

var unlinkable = map[shared.MediaType]string{
	"tv":          "TV shows are not on AniList",
	"cartoon":     "Cartoons are not on AniList",
	"kdrama":      "K-dramas are not on AniList",
	"documentary": "Documentaries are not on AniList",
}

// PlanSearch decides which AniList search fits this entry.
func PlanSearch(e shared.Entry) SearchPlan {
	if e.Library == shared.LibraryReading {
		country := ""
		switch e.MediaType {
		case "manhwa":
			country = "KR"
		case "manhua":
			country = "CN"
		case "manga":
			country = "JP"
		}
		return SearchPlan{Linkable: true, Type: "MANGA", Country: country}
	}
	if reason, ok := unlinkable[e.MediaType]; ok {
		return SearchPlan{Linkable: false, Reason: reason}
	}
	if e.MediaType == "donghua" {
		return SearchPlan{Linkable: true, Type: "ANIME", Country: "CN"}
	}
	return SearchPlan{Linkable: true, Type: "ANIME"}
}

// MediaTitles are every title AniList knows for a work: romaji, English, native and synonyms.
func MediaTitles(m Media) []string {
	var out []string
	for _, t := range []*string{m.Title.Romaji, m.Title.English, m.Title.Native} {
		if t != nil && shared.JSTrim(*t) != "" {
			out = append(out, *t)
		}
	}
	for _, t := range m.Synonyms {
		if shared.JSTrim(t) != "" {
			out = append(out, t)
		}
	}
	return out
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ScoreCandidate is the 0..1 confidence that media is the title the user tracks.
func ScoreCandidate(title string, altTitles []string, m Media, plan SearchPlan) float64 {
	mine := append([]string{title}, altTitles...)
	best := 0.0
	theirs := MediaTitles(m)
	for _, a := range mine {
		na := shared.NormalizeTitle(a)
		for _, b := range theirs {
			s := shared.TitleSimilarity(a, b)
			if na == shared.NormalizeTitle(b) {
				s = 1
			} else if shared.IsNearDuplicateTitle(a, b) {
				s = math.Max(s, 0.8)
			}
			best = math.Max(best, s)
		}
	}
	score := best
	if plan.Country != "" {
		if deref(m.CountryOfOrigin) == plan.Country {
			score += 0.04
		} else {
			score -= 0.15
		}
	}
	format := deref(m.Format)
	if format == "MUSIC" {
		score -= 0.2
	}
	if format == "NOVEL" && plan.Type == "MANGA" {
		score -= 0.12
	}
	if format == "SPECIAL" || format == "TV_SHORT" {
		score -= 0.04
	}
	return math.Max(0, math.Min(1, score))
}

func firstNonNil(ss ...*string) *string {
	for _, s := range ss {
		if s != nil {
			return s
		}
	}
	return nil
}

// ToCandidate summarises a media for the review queue.
func ToCandidate(title string, altTitles []string, m Media, plan SearchPlan) Candidate {
	t := firstNonNil(m.Title.Romaji, m.Title.English, m.Title.Native)
	name := "#" + strconv.Itoa(m.ID)
	if t != nil {
		name = *t
	}
	var year *int
	switch {
	case m.SeasonYear != nil:
		year = m.SeasonYear
	case m.StartDate != nil:
		year = m.StartDate.Year
	}
	var cover *string
	if m.CoverImage != nil {
		cover = m.CoverImage.Large
	}
	titles := MediaTitles(m)
	if titles == nil {
		titles = []string{}
	}
	return Candidate{
		ID: m.ID, Title: name, Titles: titles, Year: year, Format: m.Format, Type: m.Type, Country: m.CountryOfOrigin,
		Chapters: m.Chapters, Episodes: m.Episodes, Cover: cover, SiteURL: m.SiteURL,
		Confidence: math.Round(ScoreCandidate(title, altTitles, m, plan)*1000) / 1000,
	}
}

// Rank returns the best candidates, most confident first.
func Rank(title string, altTitles []string, media []Media, plan SearchPlan, limit int) []Candidate {
	out := make([]Candidate, len(media))
	for i, m := range media {
		out[i] = ToCandidate(title, altTitles, m, plan)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ShouldAutoConfirm is true only when the best candidate is clearly ahead of the rest.
func ShouldAutoConfirm(c []Candidate, threshold, margin float64) bool {
	if len(c) == 0 || c[0].Confidence < threshold {
		return false
	}
	return len(c) < 2 || c[0].Confidence-c[1].Confidence >= margin
}

const defaultMargin = 0.08

var (
	reDigits = regexp.MustCompile(`^\d+$`)
	reRef    = regexp.MustCompile(`(?i)anilist\.co/(anime|manga)/(\d+)`)
)

// ParseRef accepts "12345", "anilist.co/anime/12345/Some-Title" or a full URL.
func ParseRef(input string) (id int, typ string, ok bool) {
	t := shared.JSTrim(input)
	if reDigits.MatchString(t) {
		n, err := strconv.Atoi(t)
		return n, "", err == nil
	}
	if m := reRef.FindStringSubmatch(t); m != nil {
		n, err := strconv.Atoi(m[2])
		typ = "MANGA"
		if strings.EqualFold(m[1], "anime") {
			typ = "ANIME"
		}
		return n, typ, err == nil
	}
	return 0, "", false
}

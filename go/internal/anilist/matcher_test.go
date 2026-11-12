package anilist

import (
	"testing"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// Ported from the matcher tests in packages/server/test/anilist-client.test.ts.

func entryOf(title, mediaType string, library shared.Library) shared.Entry {
	e := shared.NewEntry()
	e.Title, e.MediaType, e.Library = title, shared.MediaType(mediaType), library
	return e
}

func testMedia(id int, title string, mod func(*Media)) Media {
	m := Media{
		ID: id, Type: "MANGA", Format: str("MANGA"), CountryOfOrigin: str("KR"), Title: Title{Romaji: &title},
		Synonyms: []string{}, Genres: []string{}, Tags: []MediaTag{},
	}
	if mod != nil {
		mod(&m)
	}
	return m
}

func TestDecidesWhatIsLinkable(t *testing.T) {
	check := func(e shared.Entry, linkable bool, typ, country string) {
		t.Helper()
		p := PlanSearch(e)
		if p.Linkable != linkable || p.Type != typ || p.Country != country {
			t.Errorf("%s/%s: %+v, want linkable=%v %s %s", e.Library, e.MediaType, p, linkable, typ, country)
		}
	}
	check(entryOf("x", "manhwa", "reading"), true, "MANGA", "KR")
	check(entryOf("x", "manhua", "reading"), true, "MANGA", "CN")
	check(entryOf("x", "manga", "reading"), true, "MANGA", "JP")
	check(entryOf("x", "unknown", "reading"), true, "MANGA", "")
	check(entryOf("x", "donghua", "watching"), true, "ANIME", "CN")
	check(entryOf("x", "anime", "watching"), true, "ANIME", "")
	for _, typ := range []string{"tv", "kdrama", "cartoon", "documentary"} {
		p := PlanSearch(entryOf("x", typ, "watching"))
		if p.Linkable || p.Reason == "" {
			t.Errorf("%s must not be linkable: %+v", typ, p)
		}
	}
}

func TestRanksByTitleCountryAndFormat(t *testing.T) {
	e := entryOf("Lantern of the Hollow Crown", "manhwa", "reading")
	ranked := Rank(e.Title, nil, []Media{
		testMedia(1, "Lantern of the Hollow Crown", func(m *Media) { m.CountryOfOrigin = str("JP") }),
		testMedia(2, "Lantern of the Hollow Crown", nil),
		testMedia(3, "Lantern of the Hollow Crown", func(m *Media) { m.Format = str("NOVEL") }),
		testMedia(4, "Something else entirely", nil),
	}, PlanSearch(e), 5)
	var ids []int
	for _, c := range ranked {
		ids = append(ids, c.ID)
	}
	if len(ids) != 4 || ids[0] != 2 || ids[1] != 3 || ids[2] != 1 || ids[3] != 4 {
		t.Errorf("order %v", ids)
	}
	if ranked[0].Confidence != 1 {
		t.Errorf("best confidence %v", ranked[0].Confidence)
	}
	if got := Rank(e.Title, nil, nil, PlanSearch(e), 5); len(got) != 0 {
		t.Errorf("no media: %v", got)
	}
}

func TestMatchesAlternateTitles(t *testing.T) {
	e := entryOf("Crown of Ash", "unknown", "reading")
	e.AltTitles = []string{"Kuroi Ou"}
	native := "Kuroi Ou"
	c := Rank(e.Title, e.AltTitles, []Media{testMedia(9, "Different Name", func(m *Media) { m.Title.Native = &native })}, PlanSearch(e), 5)
	if c[0].Confidence <= 0.95 {
		t.Errorf("confidence %v", c[0].Confidence)
	}
}

func TestAutoConfirmsOnlyWithAClearMarginAboveTheThreshold(t *testing.T) {
	c := func(x ...float64) []Candidate {
		var out []Candidate
		for _, v := range x {
			out = append(out, Candidate{Confidence: v})
		}
		return out
	}
	cases := []struct {
		c    []Candidate
		want bool
	}{{c(0.99, 0.5), true}, {c(0.99, 0.97), false}, {c(0.9), false}, {c(), false}, {c(0.95), true}, {c(0.92, 0.84), true}, {c(0.92, 0.85), false}}
	for i, tc := range cases {
		if got := ShouldAutoConfirm(tc.c, 0.92, defaultMargin); got != tc.want {
			t.Errorf("case %d: got %v", i, got)
		}
	}
}

func TestParsesAniListReferences(t *testing.T) {
	cases := []struct {
		in   string
		id   int
		typ  string
		fail bool
	}{
		{"12345", 12345, "", false}, {"https://anilist.co/manga/987/Some-Title/", 987, "MANGA", false},
		{"anilist.co/anime/55", 55, "ANIME", false}, {"  7  ", 7, "", false}, {"not a ref", 0, "", true}, {"", 0, "", true},
		{"https://anilist.co/user/5", 0, "", true},
	}
	for _, c := range cases {
		id, typ, ok := ParseRef(c.in)
		if ok == c.fail || id != c.id || typ != c.typ {
			t.Errorf("ParseRef(%q) = %d %q %v", c.in, id, typ, ok)
		}
	}
}

func TestSuggestedTagsAreGenresPlusRankedNonSpoilerTags(t *testing.T) {
	m := testMedia(1, "x", func(m *Media) {
		m.Genres = []string{"Fantasy", "Drama"}
		r80, r90, r20 := 80, 90, 20
		m.Tags = []MediaTag{{Name: "Time Loop", Rank: &r80}, {Name: "Twist", IsMediaSpoiler: true, Rank: &r90}, {Name: "Low", Rank: &r20}, {Name: "Fantasy", Rank: &r80}, {Name: "Unranked"}}
	})
	got := SuggestedTagsFor(m)
	want := []string{"Fantasy", "Drama", "Time Loop"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := SuggestedTagsFor(testMedia(2, "y", func(m *Media) {
		for i := 0; i < 40; i++ {
			m.Genres = append(m.Genres, string(rune('A'+i%26))+string(rune('a'+i/26)))
		}
	})); len(got) != 20 {
		t.Errorf("at most 20 suggestions, got %d", len(got))
	}
}

func TestStripHTML(t *testing.T) {
	cases := map[string]string{
		"A <i>story</i><br>line":  "A story\nline",
		"Tom &amp; Jerry":         "Tom & Jerry",
		"&quot;q&quot; it&#039;s": `"q" it's`,
		"<p></p>":                 "",
		"  padded  ":              "padded",
		"line<BR/>two<br />three": "line\ntwo\nthree",
	}
	for in, want := range cases {
		if got := stripHTML(&in); got != want {
			t.Errorf("stripHTML(%q) = %q, want %q", in, got, want)
		}
	}
	if stripHTML(nil) != "" {
		t.Error("nil")
	}
}

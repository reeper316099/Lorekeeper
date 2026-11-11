package shared

import (
	"reflect"
	"strings"
	"testing"
)

// Ported from packages/shared/src/parse/parse.test.ts.

func p(s string) *string              { return &s }
func f(n float64) *float64            { return &n }
func pk(k ProgressKind) *ProgressKind { return &k }

func TestParseProgress(t *testing.T) {
	t.Run("numbers", func(t *testing.T) {
		got := ParseProgress(46.0)
		want := Progress{Raw: p("46"), Number: f(46), Kind: pk(ProgressNumber)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v", got)
		}
		g := ParseProgress(" 111 ")
		if *g.Number != 111 || *g.Kind != ProgressNumber {
			t.Fatalf("got %+v", g)
		}
	})
	t.Run("raw text kept", func(t *testing.T) {
		want := Progress{Raw: p("End of S1"), Season: f(1), Kind: pk(ProgressEnd)}
		if got := ParseProgress("End of S1"); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v", got)
		}
		if g := ParseProgress("Beginning of S2"); *g.Season != 2 || *g.Kind != ProgressStart {
			t.Fatalf("got %+v", g)
		}
		want = Progress{Raw: p("S2 E1"), Number: f(1), Season: f(2), Kind: pk(ProgressNumber)}
		if got := ParseProgress("S2 E1"); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v", got)
		}
		want = Progress{Raw: p("49?"), Number: f(49), Kind: pk(ProgressText)}
		if got := ParseProgress("49?"); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v", got)
		}
		for _, w := range []string{"Done", "Completed", "Finished", "Finished?"} {
			if k := ParseProgress(w).Kind; k == nil || *k != ProgressComplete {
				t.Errorf("%s: kind %v", w, k)
			}
		}
		if k := ParseProgress("End").Kind; *k != ProgressEnd {
			t.Errorf("End kind %v", *k)
		}
		odd := ParseProgress("A few chapters after Season 3?")
		if *odd.Raw != "A few chapters after Season 3?" || *odd.Kind != ProgressText || odd.Number != nil {
			t.Errorf("odd: %+v", odd)
		}
		if k := ParseProgress("N/A").Kind; *k != ProgressText {
			t.Errorf("N/A kind %v", *k)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if ParseProgress(nil).Raw != nil || ParseProgress("  ").Kind != nil {
			t.Fatal("expected null progress")
		}
	})
	t.Run("behindBy and cell", func(t *testing.T) {
		if d := BehindBy(ParseProgress(111.0), ParseProgress(110.0)); d == nil || *d != 1 {
			t.Errorf("behindBy %v", d)
		}
		if BehindBy(ParseProgress(111.0), ParseProgress("End")) != nil {
			t.Error("expected nil")
		}
		if c := ProgressCell(ParseProgress(46.0)); c != float64(46) {
			t.Errorf("cell %v", c)
		}
		if c := ProgressCell(ParseProgress("S2 E1")); c != "S2 E1" {
			t.Errorf("cell %v", c)
		}
	})
}

func TestParseLink(t *testing.T) {
	l := ParseLink("youtu.be/nQ5pQCTXeB0?si=abc", LinkYouTube)
	if *l.URL != "https://youtu.be/nQ5pQCTXeB0?si=abc" || l.Kind != LinkYouTube || *l.Raw != "youtu.be/nQ5pQCTXeB0?si=abc" {
		t.Errorf("bare youtube: %+v", l)
	}
	if u := ParseLink(" tapas.io/episode/2", LinkOther).URL; *u != "https://tapas.io/episode/2" {
		t.Errorf("tapas: %v", *u)
	}
	d := ParseLink("(S1 #full) Some Title (3/3/26) By: PanelPilot", LinkYouTube)
	if d.URL != nil || !strings.Contains(*d.Label, "Some Title") || *d.Date != "3/3/26" || *d.Channel != "PanelPilot" {
		t.Errorf("descriptive: %+v", d)
	}
	if u := ParseLink("see https://example.com/a.", LinkOther).URL; *u != "https://example.com/a" {
		t.Errorf("embedded: %v", *u)
	}
	if ParseLink("  ", LinkOther) != nil {
		t.Error("empty cell must be nil")
	}
}

func TestTags(t *testing.T) {
	cases := map[string][]string{
		"Romance,Slapstick":      {"Romance", "Slapstick"},
		"Action, Isekai, ":       {"Action", "Isekai"},
		"Quest-like, Quest-Like": {"Quest-like"},
	}
	for in, want := range cases {
		if got := SplitTags(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v", in, got)
		}
	}
	usage := []TagUsage{
		{"Sorcery", 10}, {"Sorcrey", 1}, {"Fantasy", 8}, {"Fantsy", 1}, {"Rank Board", 5},
		{"Rankk Board", 1}, {"Sword Arts", 3}, {"Sword Artss", 1}, {"Quest-like", 4}, {"Quest-Like", 1},
		{"Sunny Days", 5}, {"Sunny Days(loud)", 1}, {"Romance", 9},
	}
	got := map[string]bool{}
	for _, s := range SuggestTagMerges(usage) {
		got[s.From+">"+s.To] = true
		if s.From == "Romance" || s.To == "Romance" {
			t.Errorf("Romance should not be suggested: %+v", s)
		}
	}
	for _, want := range []string{
		"Sorcrey>Sorcery", "Fantsy>Fantasy", "Rankk Board>Rank Board", "Sword Artss>Sword Arts",
		"Quest-Like>Quest-like", "Sunny Days(loud)>Sunny Days",
	} {
		if !got[want] {
			t.Errorf("missing suggestion %s", want)
		}
	}
}

func TestTitleNormalizer(t *testing.T) {
	if NormalizeTitle("The “Marlow’s” Plan!") != NormalizeTitle("marlows plan") {
		t.Error("quotes/articles")
	}
	if NormalizeTitle("Mr. Marlow") != NormalizeTitle("Mr Marlow") {
		t.Error("punctuation")
	}
	if NormalizeTitle("ＡＢＣ") != "abc" {
		t.Error("fullwidth")
	}
	a, b := "Quiet Harbor Without Mr. Pell", "Can You Do Quiet Harbor Without Mr Pelle?"
	if NormalizeTitle(a) == NormalizeTitle(b) {
		t.Error("near dups must not be equal")
	}
	if TitleSimilarity(a, b) <= 0.6 || !IsNearDuplicateTitle(a, b) {
		t.Error("near dup not detected")
	}
	if IsNearDuplicateTitle("Silent Crown of Ash", "Frozen Garden of Seven Moons") {
		t.Error("unrelated titles flagged")
	}
}

func TestStatusScoreSources(t *testing.T) {
	if s := ParseStatus("Finished"); s.Status != StatusCompleted {
		t.Errorf("finished: %+v", s)
	}
	if s := ParseStatus("plan to read"); s.Status != StatusPlanned || s.Blank {
		t.Errorf("plan to read: %+v", s)
	}
	if s := ParseStatus("Re-Reading"); s.Status != StatusRepeating {
		t.Errorf("re-reading: %+v", s)
	}
	if s := ParseStatus(nil); s.Status != StatusPlanned || !s.Blank {
		t.Errorf("blank: %+v", s)
	}
	if s := ParseStatus("???"); s.Ok || s.Raw != "???" {
		t.Errorf("unknown: %+v", s)
	}
	for in, want := range map[any]int{"7 - Good": 7, " 9 - Amazing": 9, "10 - Masterpiece": 10, 15.0: 10} {
		if g := ParseScore(in); g == nil || *g != want {
			t.Errorf("score %v: got %v", in, g)
		}
	}
	if ParseScore("Great") != nil {
		t.Error("Great should be nil")
	}
	if got := ParseSources("Youtube, Inkreader"); !reflect.DeepEqual(got, []string{"YouTube", "Inkreader"}) {
		t.Errorf("sources %v", got)
	}
	if got := ParseSources("ClipHub"); !reflect.DeepEqual(got, []string{"ClipHub"}) {
		t.Errorf("sources %v", got)
	}
}

func TestMisc(t *testing.T) {
	if ReadableTextColor("#B71C1C") != "#ffffff" || ReadableTextColor("#FFEB3B") != "#000000" {
		t.Error("readable text color")
	}
	for in, want := range map[string]string{"Misc.": "misc", "Sci-Fi": "sci-fi", "Slice of Life": "slice-of-life", "---": "category"} {
		if got := Slugify(in); got != want {
			t.Errorf("slugify %q = %q", in, got)
		}
	}
	scales := DefaultScales()
	st := StatusCompleted
	if StatusLabel(scales, LibraryWatching, &st) != "Finished" || StatusLabel(scales, LibraryReading, &st) != "Completed" {
		t.Error("status labels")
	}
	seven := 7
	if ScoreLabel(scales, LibraryReading, &seven) != "7 - Good" {
		t.Errorf("score label %q", ScoreLabel(scales, LibraryReading, &seven))
	}
}

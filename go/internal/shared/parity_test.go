package shared

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// parity.json is produced by scripts/gen-go-parity.ts from the TypeScript implementation. Every
// case must produce the same JSON in Go.
type parityCase struct {
	In  any `json:"in"`
	Out any `json:"out"`
}

func asPair(in any) (any, any) {
	p := in.([]any)
	return p[0], p[1]
}

func str(in any) string { s, _ := in.(string); return s }

func asUsage(in any) []TagUsage {
	out := []TagUsage{}
	for _, it := range in.([]any) {
		p := it.([]any)
		out = append(out, TagUsage{Name: p[0].(string), Count: int(p[1].(float64))})
	}
	return out
}

var parityFuncs = map[string]func(any) any{
	"parseProgress":     func(in any) any { return ParseProgress(in) },
	"progressCell":      func(in any) any { return ProgressCell(ParseProgress(in)) },
	"parseLink":         func(in any) any { return ParseLink(in, LinkOther) },
	"parseLinkYouTube":  func(in any) any { return ParseLink(in, LinkYouTube) },
	"splitTags":         func(in any) any { return SplitTags(in) },
	"parseSources":      func(in any) any { return ParseSources(in) },
	"parseStatus":       func(in any) any { return ParseStatus(in) },
	"parseScore":        func(in any) any { return ParseScore(in) },
	"cleanText":         func(in any) any { return CleanText(in) },
	"normalizeTitle":    func(in any) any { return NormalizeTitle(str(in)) },
	"slugify":           func(in any) any { return Slugify(str(in)) },
	"readableTextColor": func(in any) any { return ReadableTextColor(str(in)) },
	"behindBy": func(in any) any {
		a, b := asPair(in)
		return BehindBy(ParseProgress(a), ParseProgress(b))
	},
	"titleSimilarity":      func(in any) any { a, b := asPair(in); return TitleSimilarity(str(a), str(b)) },
	"isNearDuplicateTitle": func(in any) any { a, b := asPair(in); return IsNearDuplicateTitle(str(a), str(b)) },
	"levenshtein":          func(in any) any { a, b := asPair(in); return Levenshtein(str(a), str(b)) },
	"damerau":              func(in any) any { a, b := asPair(in); return Damerau(str(a), str(b)) },
	"suggestTagMerges":     func(in any) any { return SuggestTagMerges(asUsage(in)) },
}

// roundTrip turns a Go value into what JSON.stringify + JSON.parse would give.
func roundTrip(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParityWithTypeScript(t *testing.T) {
	raw, err := os.ReadFile("testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus map[string][]parityCase
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for name := range parityFuncs {
		if len(corpus[name]) == 0 {
			t.Errorf("%s: no cases in parity.json", name)
		}
	}
	for name, cases := range corpus {
		fn, ok := parityFuncs[name]
		if !ok {
			t.Errorf("%s: in parity.json but not covered by a Go function", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			for i, c := range cases {
				got := roundTrip(t, fn(c.In))
				if !reflect.DeepEqual(got, c.Out) {
					t.Errorf("case %d input %#v\n  ts: %#v\n  go: %#v", i, c.In, c.Out, got)
				}
			}
		})
	}
}

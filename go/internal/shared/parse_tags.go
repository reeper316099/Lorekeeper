package shared

import (
	"regexp"
	"strings"
)

var (
	reTagSplit    = regexp.MustCompile(`[,;\n]`)
	reSrcSplit    = regexp.MustCompile(`[,;/\n]`)
	reNonAlnumRun = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	reParens      = regexp.MustCompile(`\(.*?\)`)
)

func splitDedupe(input any, split *regexp.Regexp, canon map[string]string) []string {
	out := []string{}
	if input == nil {
		return out
	}
	seen := map[string]bool{}
	for _, part := range split.Split(jsString(input), -1) {
		t := jsTrim(jsCollapse(part))
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		if c, ok := canon[key]; ok {
			t = c
		}
		out = append(out, t)
	}
	return out
}

// SplitTags splits a free-text tag cell: trim, drop empties, case-insensitive dedupe (first casing wins).
func SplitTags(input any) []string { return splitDedupe(input, reTagSplit, nil) }

var canonSources = map[string]string{"youtube": "YouTube", "inkreader": "Inkreader", "yt": "YouTube"}

// ParseSources: "Youtube, Inkreader" -> ["YouTube", "Inkreader"]; case-insensitive dedupe.
func ParseSources(input any) []string { return splitDedupe(input, reSrcSplit, canonSources) }

type MergeReason string

const (
	MergeCase        MergeReason = "case"
	MergePunctuation MergeReason = "punctuation"
	MergeVariant     MergeReason = "variant"
	MergeTypo        MergeReason = "typo"
)

// Fields are in sorted JSON-key order (see TestJSONFieldsAreSorted): suggestions are written into
// import reports, which the store format keeps key-sorted.
type MergeSuggestion struct {
	Distance int         `json:"distance"`
	From     string      `json:"from"`
	Reason   MergeReason `json:"reason"`
	To       string      `json:"to"`
}

// TagUsage is one tag and how many entries use it. A slice (not a map) so the result order is
// deterministic and matches the TypeScript output for the same input order.
type TagUsage struct {
	Name  string
	Count int
}

func alnum(s string) string { return reNonAlnumRun.ReplaceAllString(strings.ToLower(s), "") }

func baseNoParens(s string) string { return alnum(reParens.ReplaceAllString(s, "")) }

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return -1
}

// SuggestTagMerges suggests merges for near-duplicate tags. The less-used tag is suggested to merge
// into the more-used one. Nothing is ever applied automatically.
func SuggestTagMerges(usage []TagUsage) []MergeSuggestion {
	out := []MergeSuggestion{}
	for i := 0; i < len(usage); i++ {
		for j := i + 1; j < len(usage); j++ {
			a, ca := usage[i].Name, usage[i].Count
			b, cb := usage[j].Name, usage[j].Count
			var reason MergeReason
			distance := 0
			la := strings.ToLower(jsTrim(a))
			lb := strings.ToLower(jsTrim(b))
			switch {
			case la == lb:
				reason = MergeCase
			case alnum(a) == alnum(b):
				reason = MergePunctuation
			case strings.Contains(a+b, "(") && baseNoParens(a) == baseNoParens(b):
				reason = MergeVariant
			default:
				x, y := alnum(a), alnum(b)
				xl, yl := len([]rune(x)), len([]rune(y))
				limit := 1
				if min(xl, yl) >= 6 {
					limit = 2
				}
				if xl >= 4 && yl >= 4 && firstRune(x) == firstRune(y) {
					distance = Damerau(x, y)
					if distance <= limit {
						reason = MergeTypo
					}
				}
			}
			if reason == "" {
				continue
			}
			keepA := ca > cb || (ca == cb && jsLen(a) >= jsLen(b) && a <= b)
			if keepA {
				out = append(out, MergeSuggestion{From: b, To: a, Reason: reason, Distance: distance})
			} else {
				out = append(out, MergeSuggestion{From: a, To: b, Reason: reason, Distance: distance})
			}
		}
	}
	return out
}

// jsLen is JavaScript's String.length (UTF-16 code units).
func jsLen(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

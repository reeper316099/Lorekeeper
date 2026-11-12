package shared

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var (
	rePunctSym = regexp.MustCompile(`[\p{P}\p{S}]+`)
	reArticle  = regexp.MustCompile(`^(the|a|an) `)
)

// NormalizeTitle is the dedupe key: NFKC, lowercase, no diacritics/punctuation/leading article/extra spaces.
func NormalizeTitle(title string) string {
	base := jsTrim(strings.ToLower(norm.NFKC.String(title)))
	s := norm.NFD.String(base)
	s = reMarks.ReplaceAllString(s, "")
	s = rePunctSym.ReplaceAllString(s, "")
	s = jsTrim(jsCollapse(s))
	s = reArticle.ReplaceAllString(s, "")
	if s == "" {
		return base
	}
	return s
}

// TitleSimilarity is a 0..1 similarity of two titles after normalization (1 = identical).
func TitleSimilarity(a, b string) float64 {
	return titleSimilarityNorm(NormalizeTitle(a), NormalizeTitle(b))
}

func titleSimilarityNorm(x, y string) float64 {
	xl, yl := len([]rune(x)), len([]rune(y))
	if xl == 0 && yl == 0 {
		return 1
	}
	return 1 - float64(Levenshtein(x, y))/float64(max(xl, yl))
}

func fields(s string) []string {
	out := []string{}
	for _, w := range strings.Split(s, " ") {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// IsNearDuplicateTitle is the fuzzy "might be the same series" test: high similarity, or most words of
// the shorter title appear in the other.
func IsNearDuplicateTitle(a, b string) bool {
	return IsNearDuplicateNorm(NormalizeTitle(a), NormalizeTitle(b))
}

// IsNearDuplicateNorm is IsNearDuplicateTitle for titles that are already normalized, so callers
// comparing one title against thousands can normalize each only once.
func IsNearDuplicateNorm(x, y string) bool {
	if titleSimilarityNorm(x, y) >= 0.85 {
		return true
	}
	ta, tb := fields(x), fields(y)
	small, big := ta, tb
	if len(ta) > len(tb) {
		small, big = tb, ta
	}
	if len(small) < 3 {
		return false
	}
	hit := 0
	for _, w := range small {
		for _, x := range big {
			wl, xl := len([]rune(w)), len([]rune(x))
			if x == w || (wl >= 5 && xl >= 5 && Damerau(w, x) <= 1) {
				hit++
				break
			}
		}
	}
	return float64(hit)/float64(len(small)) >= 0.8
}

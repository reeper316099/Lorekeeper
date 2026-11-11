package shared

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var (
	reMarks    = regexp.MustCompile(`\p{M}+`)
	reNonAlnum = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// Slugify builds a filename-safe slug; falls back to "category".
func Slugify(name string) string {
	s := norm.NFKD.String(name)
	s = reMarks.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = reNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "category"
	}
	return s
}

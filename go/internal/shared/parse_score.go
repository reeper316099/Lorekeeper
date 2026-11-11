package shared

import (
	"regexp"
	"strconv"
)

var reScore = regexp.MustCompile(`^[` + jsWS + `]*(\d+)`)

// ParseScore parses "7 - Good", " 9 - Amazing", 8, "8/10" into an integer 1..10, else nil.
// Three or more leading digits are not a score (JS: /^\s*(\d{1,2})(?!\d)/).
func ParseScore(input any) *int {
	if input == nil {
		return nil
	}
	m := reScore.FindStringSubmatch(jsString(input))
	if m == nil || len(m[1]) > 2 {
		return nil
	}
	n, _ := strconv.Atoi(m[1])
	if n < 1 {
		return nil
	}
	n = min(10, n)
	return &n
}

package shared

import (
	"math"
	"regexp"
	"strconv"
)

var reHex6 = regexp.MustCompile(`(?i)^#?([0-9a-f]{6})$`)

// ReadableTextColor returns #000000 or #ffffff for a #RRGGBB background (WCAG luminance).
func ReadableTextColor(hex string) string {
	m := reHex6.FindStringSubmatch(hex)
	if m == nil {
		return "#ffffff"
	}
	n, _ := strconv.ParseInt(m[1], 16, 64)
	lin := func(c int64) float64 {
		s := float64(c) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin((n>>16)&255) + 0.7152*lin((n>>8)&255) + 0.0722*lin(n&255)
	if l > 0.4 {
		return "#000000"
	}
	return "#ffffff"
}

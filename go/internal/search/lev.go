package search

// editDistance is the Levenshtein distance between a and b, or any value > max once it is known to
// exceed max (so most candidates are rejected after a few cells). Strings are compared by rune.
func editDistance(a, b string, max int) int {
	if a == b {
		return 0
	}
	var ra, rb []rune
	if isASCII(a) && isASCII(b) {
		return editBytes(a, b, max)
	}
	ra, rb = []rune(a), []rune(b)
	return editRunes(ra, rb, max)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func editBytes(a, b string, max int) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	if len(a)-len(b) > max {
		return max + 1
	}
	var stack [2][65]int
	prev, cur := stack[0][:0], stack[1][:0]
	if len(b)+1 > 65 {
		prev, cur = make([]int, 0, len(b)+1), make([]int, 0, len(b)+1)
	}
	for j := 0; j <= len(b); j++ {
		prev = append(prev, j)
	}
	for i := 1; i <= len(a); i++ {
		cur = append(cur[:0], i)
		rowMin := i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			v := min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			cur = append(cur, v)
			rowMin = min(rowMin, v)
		}
		if rowMin > max {
			return max + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func editRunes(a, b []rune, max int) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	if len(a)-len(b) > max {
		return max + 1
	}
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		rowMin := i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			rowMin = min(rowMin, cur[j])
		}
		if rowMin > max {
			return max + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

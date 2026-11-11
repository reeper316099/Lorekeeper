package shared

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// JavaScript's \s and String.prototype.trim() cover more characters than Go's \s / TrimSpace
// (\v, NBSP, U+2028/9, BOM, ...). These helpers keep the parsers byte-for-byte compatible with the
// TypeScript originals on spreadsheet text, which often contains stray NBSPs.
const jsWS = "\\t\\n\\v\\f\\r \\x{a0}\\x{1680}\\x{2000}-\\x{200a}\\x{2028}\\x{2029}\\x{202f}\\x{205f}\\x{3000}\\x{feff}"
const jsWSChars = "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

var (
	reWSRun = regexp.MustCompile("[" + jsWS + "]+")
	reHasWS = regexp.MustCompile("[" + jsWS + "]")
	reNotWS = "[^" + jsWS + "]"
	reNotNL = "[^\\n\\r\\x{2028}\\x{2029}]"
)

func jsTrim(s string) string { return strings.Trim(s, jsWSChars) }

// jsCollapse is s.replace(/\s+/g, " ").
func jsCollapse(s string) string { return reWSRun.ReplaceAllString(s, " ") }

// jsString mimics String(v) for the value shapes that come out of spreadsheet cells and JSON.
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return jsNumber(x)
	case float32:
		return jsNumber(float64(x))
	}
	return ""
}

func jsNumber(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// JSTrim is JavaScript's String.prototype.trim().
func JSTrim(s string) string { return jsTrim(s) }

// JSCollapse is JavaScript's s.replace(/\s+/g, " ").
func JSCollapse(s string) string { return jsCollapse(s) }

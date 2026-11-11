// Package search holds the search index (a port of MiniSearch's scoring), the entry list query
// (filters, sorting, pagination) and text collation shared by the API and stats.
package search

import (
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Collators are not safe for concurrent use, so every call takes this lock.
var coll struct {
	sync.Mutex
	base *collate.Collator // Intl.Collator({ sensitivity: 'base', numeric: true })
	full *collate.Collator // String.prototype.localeCompare
	buf  collate.Buffer
}

func init() {
	coll.base = collate.New(language.Und, collate.Numeric, collate.Loose)
	coll.full = collate.New(language.Und)
}

// CompareBase compares like Intl.Collator(undefined, { sensitivity: 'base', numeric: true }):
// case and accents are ignored, digit runs compare as numbers ("Vol 2" < "Vol 10").
func CompareBase(a, b string) int {
	coll.Lock()
	defer coll.Unlock()
	return coll.base.CompareString(a, b)
}

// LocaleCompare compares like String.prototype.localeCompare.
func LocaleCompare(a, b string) int {
	coll.Lock()
	defer coll.Unlock()
	return coll.full.CompareString(a, b)
}

// BaseKey returns a sort key for CompareBase (bytes.Compare on keys gives the same order).
func BaseKey(s string) []byte {
	coll.Lock()
	defer coll.Unlock()
	return append([]byte(nil), coll.base.KeyFromString(&coll.buf, s)...)
}

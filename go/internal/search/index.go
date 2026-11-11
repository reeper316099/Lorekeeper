package search

import (
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// The index is a port of the parts of MiniSearch 7 the app uses, with the same options the Node
// server passes (fields title/alt/tags/notes boosted 6/3/2/1, prefix matching, fuzzy matching that
// depends on term length, all terms must match) and the same BM25+ scoring, so rankings agree with
// the Node implementation (checked by TestRankingsMatchMiniSearch).

const (
	fTitle = iota
	fAlt
	fTags
	fNotes
	nFields
)

var fieldBoost = [nFields]float64{6, 3, 2, 1}

const (
	bm25K        = 1.2
	bm25B        = 0.7
	bm25D        = 0.5
	prefixWeight = 0.375
	fuzzyWeight  = 0.45
	maxFuzzy     = 6
)

type doc struct {
	id     string
	fields [nFields]string
}

func docOf(e shared.Entry) doc {
	notes := []string{}
	for _, n := range []*string{e.Notes.General, e.Notes.Extra1, e.Notes.Extra2} {
		if n != nil && *n != "" {
			notes = append(notes, *n)
		}
	}
	return doc{e.ID, [nFields]string{e.Title, strings.Join(e.AltTitles, " "), strings.Join(e.Tags, " "), strings.Join(notes, " ")}}
}

// fingerprint identifies the searchable content of an entry without allocating.
func fingerprint(e shared.Entry) uint64 {
	h := fnv.New64a()
	w := func(s string) { h.Write([]byte(s)); h.Write([]byte{0}) }
	w(e.Title)
	for _, s := range e.AltTitles {
		w(s)
	}
	w("|")
	for _, s := range e.Tags {
		w(s)
	}
	w("|")
	for _, n := range []*string{e.Notes.General, e.Notes.Extra1, e.Notes.Extra2} {
		if n != nil {
			w(*n)
		} else {
			w("\x01")
		}
	}
	return h.Sum64()
}

type posting map[int32]int32 // doc -> term frequency

type termData struct{ fields [nFields]posting }

func (t *termData) empty() bool {
	for _, p := range t.fields {
		if p != nil {
			return false
		}
	}
	return true
}

type docInfo struct {
	id     string
	length [nFields]int
	terms  [nFields][]string // unique processed terms per field, to remove the doc
}

// Hit is one ranked result.
type Hit struct {
	ID    string
	Score float64
	// Terms are the index terms that matched (for highlighting).
	Terms []string
}

// Index is a full-text index over a store's entries, kept in sync incrementally.
type Index struct {
	store *store.Store

	mu       sync.RWMutex
	builtAt  uint64
	built    bool
	terms    map[string]*termData
	sorted   []string         // all terms, sorted (prefix search); valid when !dirty
	byLen    map[int][]string // terms by UTF-16 length (fuzzy search); valid when !dirty
	dirty    bool
	docs     map[int32]*docInfo
	shortID  map[string]int32
	nextID   int32
	sumLen   [nFields]int
	lists    map[string][]shared.Entry // category snapshot last indexed
	fp       map[string]uint64         // entry id -> fingerprint
	location map[string]string         // entry id -> category id
	catIDs   map[string][]string       // category id -> entry ids as indexed
}

func NewIndex(s *store.Store) *Index {
	return &Index{store: s}
}

func (ix *Index) reset() {
	ix.terms = map[string]*termData{}
	ix.docs = map[int32]*docInfo{}
	ix.shortID = map[string]int32{}
	ix.nextID = 0
	ix.sumLen = [nFields]int{}
	ix.lists = map[string][]shared.Entry{}
	ix.fp = map[string]uint64{}
	ix.location = map[string]string{}
	ix.catIDs = map[string][]string{}
	ix.dirty = true
}

func sameList(a, b []shared.Entry) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// ensure brings the index up to date with the store. Only categories whose list was replaced are
// looked at, and within them only entries whose searchable content changed.
func (ix *Index) ensure() {
	ix.mu.RLock()
	if ix.built && ix.builtAt == ix.store.Version() {
		ix.mu.RUnlock()
		return
	}
	ix.mu.RUnlock()
	ix.mu.Lock()
	defer ix.mu.Unlock()
	version, snap := ix.store.Snapshot()
	if ix.built && ix.builtAt == version {
		return
	}
	if !ix.built {
		ix.reset()
	}
	seenCats := map[string]bool{}
	var removed []string
	var removedFrom []string
	for _, c := range snap {
		seenCats[c.ID] = true
		if prev, ok := ix.lists[c.ID]; ok && sameList(prev, c.Entries) {
			continue
		}
		newIDs := make(map[string]bool, len(c.Entries))
		for _, e := range c.Entries {
			newIDs[e.ID] = true
			f := fingerprint(e)
			old, had := ix.fp[e.ID]
			switch {
			case !had:
				ix.add(docOf(e))
			case old != f:
				ix.remove(e.ID)
				ix.add(docOf(e))
			}
			ix.fp[e.ID] = f
			ix.location[e.ID] = c.ID
		}
		for _, id := range ix.catIDs[c.ID] {
			if !newIDs[id] {
				removed = append(removed, id)
				removedFrom = append(removedFrom, c.ID)
			}
		}
		ids := make([]string, len(c.Entries))
		for i, e := range c.Entries {
			ids[i] = e.ID
		}
		ix.catIDs[c.ID] = ids
		ix.lists[c.ID] = c.Entries
	}
	for catID := range ix.lists {
		if !seenCats[catID] { // category deleted
			for _, id := range ix.catIDs[catID] {
				removed = append(removed, id)
				removedFrom = append(removedFrom, catID)
			}
			delete(ix.lists, catID)
			delete(ix.catIDs, catID)
		}
	}
	for i, id := range removed {
		if ix.location[id] == removedFrom[i] { // not moved to another category
			ix.remove(id)
			delete(ix.fp, id)
			delete(ix.location, id)
		}
	}
	ix.builtAt, ix.built = version, true
}

func isSeparator(r rune) bool {
	return r == '\n' || r == '\r' || unicode.In(r, unicode.Z, unicode.P)
}

// tokenize splits like MiniSearch's default tokenizer, /[\n\r\p{Z}\p{P}]+/u: runs of spaces and
// punctuation separate tokens, and (as with String.split) leading and trailing separators leave
// empty tokens at the ends.
func tokenize(s string) []string {
	out := make([]string, 0, 8)
	start := 0
	inSep := false
	for i, r := range s {
		sep := isSeparator(r)
		switch {
		case sep && !inSep:
			inSep = true
			out = append(out, s[start:i])
		case !sep && inSep:
			inSep, start = false, i
		}
	}
	if inSep {
		out = append(out, "")
	} else {
		out = append(out, s[start:])
	}
	return out
}

func (ix *Index) add(d doc) {
	id := ix.nextID
	ix.nextID++
	ix.shortID[d.id] = id
	info := &docInfo{id: d.id}
	for f := 0; f < nFields; f++ {
		tokens := tokenize(d.fields[f])
		info.length[f] = countUnique(tokens) // MiniSearch counts unique raw tokens
		ix.sumLen[f] += info.length[f]
		for _, t := range tokens {
			term := strings.ToLower(t)
			if term == "" {
				continue
			}
			td := ix.terms[term]
			if td == nil {
				td = &termData{}
				ix.terms[term] = td
				ix.dirty = true
			}
			if td.fields[f] == nil {
				td.fields[f] = posting{}
			}
			td.fields[f][id]++
			if td.fields[f][id] == 1 {
				info.terms[f] = append(info.terms[f], term)
			}
		}
	}
	ix.docs[id] = info
}

func (ix *Index) remove(entryID string) {
	id, ok := ix.shortID[entryID]
	if !ok {
		return
	}
	info := ix.docs[id]
	for f := 0; f < nFields; f++ {
		ix.sumLen[f] -= info.length[f]
		for _, term := range info.terms[f] {
			td := ix.terms[term]
			if td == nil || td.fields[f] == nil {
				continue
			}
			delete(td.fields[f], id)
			if len(td.fields[f]) == 0 {
				td.fields[f] = nil
			}
			if td.empty() {
				delete(ix.terms, term)
				ix.dirty = true
			}
		}
	}
	delete(ix.docs, id)
	delete(ix.shortID, entryID)
}

func (ix *Index) rebuildTermLists() {
	ix.sorted = ix.sorted[:0]
	ix.byLen = map[int][]string{}
	for t := range ix.terms {
		ix.sorted = append(ix.sorted, t)
		n := jsLen(t)
		ix.byLen[n] = append(ix.byLen[n], t)
	}
	sort.Strings(ix.sorted)
	ix.dirty = false
}

// jsLen is JavaScript's String.length (UTF-16 code units).
func jsLen(s string) int {
	n := len(s)
	for i := 0; i < len(s); {
		if s[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r >= 0x10000 {
			n -= 2 // 4 bytes in UTF-8, 2 units in UTF-16
		} else {
			n -= size - 1
		}
		i += size
	}
	return n
}

func countUnique(tokens []string) int {
	if len(tokens) <= 12 {
		n := 0
		for i, t := range tokens {
			dup := false
			for _, u := range tokens[:i] {
				if u == t {
					dup = true
					break
				}
			}
			if !dup {
				n++
			}
		}
		return n
	}
	m := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		m[t] = struct{}{}
	}
	return len(m)
}

// fuzzyFor mirrors the Node server's option: roughly one typo for mid-length words, two for long ones.
func fuzzyFor(term string) float64 {
	switch n := jsLen(term); {
	case n >= 7:
		return 0.3
	case n > 4:
		return 0.2
	}
	return 0
}

type result struct {
	score float64
	terms []string // distinct query terms matched
	match map[string]struct{}
	order int
}

func (ix *Index) termResults(sourceTerm string, derived string, weight float64, td *termData, results map[int32]*result, order *int) {
	n := float64(len(ix.docs))
	for f := 0; f < nFields; f++ {
		p := td.fields[f]
		if p == nil {
			continue
		}
		matching := float64(len(p))
		avg := float64(ix.sumLen[f]) / n
		for id, tf := range p {
			info := ix.docs[id]
			if info == nil {
				continue
			}
			inv := math.Log(1 + (n-matching+0.5)/(matching+0.5))
			raw := inv * (bm25D + float64(tf)*(bm25K+1)/(float64(tf)+bm25K*(1-bm25B+bm25B*float64(info.length[f])/avg)))
			w := weight * fieldBoost[f] * raw
			if r := results[id]; r != nil {
				r.score += w
				if !contains(r.terms, sourceTerm) {
					r.terms = append(r.terms, sourceTerm)
				}
				r.match[derived] = struct{}{}
			} else {
				*order++
				results[id] = &result{score: w, terms: []string{sourceTerm}, match: map[string]struct{}{derived: {}}, order: *order}
			}
		}
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func (ix *Index) querySpec(term string) map[int32]*result {
	results := map[int32]*result{}
	order := 0
	if td := ix.terms[term]; td != nil {
		ix.termResults(term, term, 1, td, results, &order)
	}
	// prefix matches (every term that starts with the query term)
	prefixed := map[string]bool{}
	i := sort.SearchStrings(ix.sorted, term)
	for ; i < len(ix.sorted) && strings.HasPrefix(ix.sorted[i], term); i++ {
		t := ix.sorted[i]
		distance := jsLen(t) - jsLen(term)
		if distance == 0 {
			continue
		}
		prefixed[t] = true
		w := prefixWeight * float64(jsLen(t)) / (float64(jsLen(t)) + 0.3*float64(distance))
		ix.termResults(term, t, w, ix.terms[t], results, &order)
	}
	// fuzzy matches (edit distance within a limit), except terms already scored as prefix matches
	if fz := fuzzyFor(term); fz > 0 {
		maxDist := min(maxFuzzy, int(math.Round(float64(jsLen(term))*fz)))
		if maxDist > 0 {
			ql := jsLen(term)
			for l := max(1, ql-maxDist); l <= ql+maxDist; l++ {
				for _, t := range ix.byLen[l] {
					if t == term || prefixed[t] {
						continue
					}
					if d := editDistance(term, t, maxDist); d <= maxDist {
						w := fuzzyWeight * float64(jsLen(t)) / (float64(jsLen(t)) + float64(d))
						ix.termResults(term, t, w, ix.terms[t], results, &order)
					}
				}
			}
		}
	}
	return results
}

// Search returns ranked entry ids; every query word must match (as a word, prefix or near match).
func (ix *Index) Search(q string, limit int) []Hit {
	if strings.TrimSpace(q) == "" {
		return []Hit{}
	}
	ix.ensure()
	ix.mu.Lock() // term lists are rebuilt lazily, which is a write
	if ix.dirty {
		ix.rebuildTermLists()
	}
	ix.mu.Unlock()
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	var queryTerms []string
	for _, t := range tokenize(q) {
		if lt := strings.ToLower(t); lt != "" {
			queryTerms = append(queryTerms, lt)
		}
	}
	if len(queryTerms) == 0 {
		return []Hit{}
	}
	var combined map[int32]*result
	for i, term := range queryTerms {
		r := ix.querySpec(term)
		if i == 0 {
			combined = r
			continue
		}
		next := map[int32]*result{}
		for id, b := range r {
			a := combined[id]
			if a == nil {
				continue
			}
			for _, t := range a.terms {
				if !contains(b.terms, t) {
					b.terms = append(b.terms, t)
				}
			}
			for m := range a.match {
				b.match[m] = struct{}{}
			}
			b.score += a.score
			b.order = a.order
			next[id] = b
		}
		combined = next
	}
	hits := make([]*result, 0, len(combined))
	ids := make(map[*result]int32, len(combined))
	for id, r := range combined {
		r.score *= float64(max(1, len(r.terms)))
		hits = append(hits, r)
		ids[r] = id
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return ids[hits[i]] < ids[hits[j]]
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Hit, len(hits))
	for i, r := range hits {
		terms := make([]string, 0, len(r.match))
		for m := range r.match {
			terms = append(terms, m)
		}
		sort.Strings(terms)
		out[i] = Hit{ID: ix.docs[ids[r]].id, Score: r.score, Terms: terms}
	}
	return out
}

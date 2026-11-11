package store

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// EntryInput is a partial entry as sent by the API: at least title and categoryId.
type EntryInput = map[string]any

// ---------- reads ----------

// All returns every entry, category by category. The slice is shared; do not modify it.
func (s *Store) All() []shared.Entry {
	s.mu.RLock()
	if s.allCache.ok {
		l := s.allCache.list
		s.mu.RUnlock()
		return l
	}
	s.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.allCache.ok {
		list := []shared.Entry{}
		for _, c := range s.categories {
			list = append(list, s.byCat[c.ID]...)
		}
		s.allCache.list, s.allCache.ok = list, true
	}
	return s.allCache.list
}

func (s *Store) GetEntry(id string) (shared.Entry, bool) {
	for _, e := range s.All() {
		if e.ID == id {
			return e, true
		}
	}
	return shared.Entry{}, false
}

// EntriesOf returns one category's entries (shared slice; do not modify).
func (s *Store) EntriesOf(categoryID string) []shared.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byCat[categoryID]
}

func (s *Store) requireCategoryLocked(id string) (shared.Category, error) {
	for _, c := range s.categories {
		if c.ID == id {
			return c, nil
		}
	}
	return shared.Category{}, notFound("Category not found: %s", id)
}

func (s *Store) requireEntryLocked(id string) (e shared.Entry, cat shared.Category, idx int, err error) {
	for _, c := range s.categories {
		for i, x := range s.byCat[c.ID] {
			if x.ID == id {
				return x, c, i, nil
			}
		}
	}
	return e, cat, 0, notFound("Entry not found: %s", id)
}

func inferMediaType(c shared.Category) shared.MediaType {
	if c.Library == shared.LibraryWatching {
		if m, ok := shared.WatchingMediaTypeByCategory[strings.ToLower(c.Name)]; ok {
			return m
		}
	}
	return "unknown"
}

func stringsOf(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, i := range x {
			if str, ok := i.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// BuildEntry builds a full, validated entry from partial input (no persistence).
func (s *Store) BuildEntry(input EntryInput) (shared.Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buildEntryLocked(input, ISO(time.Now()))
}

func (s *Store) buildEntryLocked(input EntryInput, now string) (shared.Entry, error) {
	catID, _ := input["categoryId"].(string)
	cat, err := s.requireCategoryLocked(catID)
	if err != nil {
		return shared.Entry{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return shared.Entry{}, err
	}
	m := map[string]any{"mediaType": inferMediaType(cat)}
	for k, v := range input {
		m[k] = v
	}
	m["tags"] = s.canonicalTagsLocked(stringsOf(input["tags"]))
	m["id"], m["library"], m["createdAt"], m["updatedAt"] = id.String(), cat.Library, now, now
	return entryFromMap(m)
}

func entryFromMap(m map[string]any) (shared.Entry, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return shared.Entry{}, err
	}
	var e shared.Entry
	return e, json.Unmarshal(b, &e)
}

func entryToMap(e shared.Entry) (map[string]any, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	return m, json.Unmarshal(b, &m)
}

// ---------- writes ----------

func (s *Store) CreateEntry(input EntryInput) (shared.Entry, error) {
	s.mu.Lock()
	entry, err := s.buildEntryLocked(input, ISO(time.Now()))
	if err != nil {
		s.mu.Unlock()
		return entry, err
	}
	s.byCat[entry.CategoryID] = append(s.byCat[entry.CategoryID], entry)
	s.mu.Unlock()
	if err := s.registerTags(entry.Tags); err != nil {
		return entry, err
	}
	if err := s.commit([]string{entry.CategoryID}, EventChange); err != nil {
		return entry, err
	}
	return entry, s.LogActivity(shared.ActivityRecord{At: entry.CreatedAt, EntryID: entry.ID, Kind: "created"})
}

// InsertEntries inserts many pre-built entries in one write per category (importer).
func (s *Store) InsertEntries(entries []shared.Entry) error {
	s.mu.Lock()
	touched := make([]string, 0, len(entries))
	var tags []string
	for _, e := range entries {
		s.byCat[e.CategoryID] = append(s.byCat[e.CategoryID], e)
		touched = append(touched, e.CategoryID)
		tags = append(tags, e.Tags...)
	}
	s.mu.Unlock()
	if err := s.registerTags(tags); err != nil {
		return err
	}
	return s.commit(touched, EventChange)
}

// ReplaceEntries swaps existing entries (matched by id) for new versions.
func (s *Store) ReplaceEntries(entries []shared.Entry) error {
	s.mu.Lock()
	touched := make([]string, 0, len(entries))
	var tags []string
	for _, e := range entries {
		_, cat, idx, err := s.requireEntryLocked(e.ID)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		list := slices.Clone(s.byCat[cat.ID])
		list[idx] = e
		s.byCat[cat.ID] = list
		touched = append(touched, e.CategoryID)
		tags = append(tags, e.Tags...)
	}
	s.mu.Unlock()
	if err := s.registerTags(tags); err != nil {
		return err
	}
	return s.commit(touched, EventChange)
}

// applyUpdateLocked applies a patch to one entry in memory. Object values (notes, current,
// external, ...) are merged one level deep; id, library, categoryId and createdAt cannot be patched.
func (s *Store) applyUpdateLocked(id string, patch map[string]any) (old, parsed shared.Entry, err error) {
	entry, cat, idx, err := s.requireEntryLocked(id)
	if err != nil {
		return entry, entry, err
	}
	next, err := entryToMap(entry)
	if err != nil {
		return entry, entry, err
	}
	for k, v := range patch {
		switch k {
		case "id", "library", "categoryId", "createdAt":
			continue
		}
		pv, pok := v.(map[string]any)
		cv, cok := next[k].(map[string]any)
		if pok && cok {
			merged := make(map[string]any, len(cv)+len(pv))
			for ck, cvv := range cv {
				merged[ck] = cvv
			}
			for pk, pvv := range pv {
				merged[pk] = pvv
			}
			next[k] = merged
		} else {
			next[k] = v
		}
	}
	if t, ok := patch["tags"]; ok && t != nil {
		next["tags"] = s.canonicalTagsLocked(stringsOf(t))
	}
	next["updatedAt"] = ISO(time.Now())
	parsed, err = entryFromMap(next)
	if err != nil {
		return entry, entry, err
	}
	list := slices.Clone(s.byCat[cat.ID])
	list[idx] = parsed
	s.byCat[cat.ID] = list
	return entry, parsed, nil
}

// finishUpdate persists a change made by applyUpdateLocked.
func (s *Store) finishUpdate(old, parsed shared.Entry) error {
	if err := s.registerTags(parsed.Tags); err != nil {
		return err
	}
	if err := s.commit([]string{parsed.CategoryID}, EventChange); err != nil {
		return err
	}
	if !sameStatus(parsed.Status, old.Status) {
		return s.LogActivity(shared.ActivityRecord{
			At: parsed.UpdatedAt, EntryID: parsed.ID, Kind: "status", From: shared.RawJSON(old.Status), To: shared.RawJSON(parsed.Status),
		})
	}
	return nil
}

// UpdateEntry applies a patch (see applyUpdateLocked) and persists it.
func (s *Store) UpdateEntry(id string, patch map[string]any) (shared.Entry, error) {
	s.mu.Lock()
	old, parsed, err := s.applyUpdateLocked(id, patch)
	s.mu.Unlock()
	if err != nil {
		return old, err
	}
	return parsed, s.finishUpdate(old, parsed)
}

func sameStatus(a, b *shared.Status) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ProgressOp is either an absolute Set (a cell value: string or number) or a Delta.
type ProgressOp struct {
	Delta  float64
	Set    any
	HasSet bool
}

func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// SetProgress updates "current". A delta is read-modify-write under the store lock, so concurrent
// +1 requests never lose an update.
func (s *Store) SetProgress(id string, op ProgressOp) (shared.Entry, error) {
	s.mu.Lock()
	entry, _, _, err := s.requireEntryLocked(id)
	if err != nil {
		s.mu.Unlock()
		return entry, err
	}
	var current shared.Progress
	if op.HasSet {
		current = shared.ParseProgress(op.Set)
	} else {
		var base float64
		if entry.Current.Number != nil {
			base = *entry.Current.Number
		}
		if entry.Current.Raw != nil && entry.Current.Number == nil && entry.Current.Kind != nil {
			s.mu.Unlock()
			return entry, &HTTPError{422, "UNPARSEABLE_PROGRESS", "Current progress is not numeric; use set."}
		}
		n := math.Max(0, base+op.Delta)
		raw := formatNumber(n)
		kind := shared.ProgressNumber
		current = shared.Progress{Raw: &raw, Number: &n, Season: entry.Current.Season, Kind: &kind}
	}
	cm := map[string]any{}
	b, _ := json.Marshal(current)
	_ = json.Unmarshal(b, &cm)
	patch := map[string]any{"current": cm}
	if entry.Status == nil || *entry.Status == shared.StatusPlanned {
		patch["status"] = string(shared.StatusInProgress)
	}
	old, parsed, err := s.applyUpdateLocked(id, patch)
	s.mu.Unlock()
	if err != nil {
		return old, err
	}
	if err := s.finishUpdate(old, parsed); err != nil {
		return parsed, err
	}
	return parsed, s.LogActivity(shared.ActivityRecord{
		At: parsed.UpdatedAt, EntryID: id, Kind: "progress", From: shared.RawJSON(old.Current.Raw), To: shared.RawJSON(current.Raw),
	})
}

// RestoreEntry re-inserts an exact entry (undo of a delete). No-op if the id already exists.
func (s *Store) RestoreEntry(entry shared.Entry) (shared.Entry, error) {
	if err := entry.Validate(); err != nil {
		return entry, err
	}
	if existing, ok := s.GetEntry(entry.ID); ok {
		return existing, nil
	}
	s.mu.Lock()
	if _, ok := s.byCat[entry.CategoryID]; !ok {
		s.mu.Unlock()
		return entry, notFound("Category not found: %s", entry.CategoryID)
	}
	s.byCat[entry.CategoryID] = append(s.byCat[entry.CategoryID], entry)
	s.mu.Unlock()
	if err := s.registerTags(entry.Tags); err != nil {
		return entry, err
	}
	return entry, s.commit([]string{entry.CategoryID}, EventChange)
}

func (s *Store) DeleteEntry(id string) (shared.Entry, error) {
	s.mu.Lock()
	entry, cat, idx, err := s.requireEntryLocked(id)
	if err != nil {
		s.mu.Unlock()
		return entry, err
	}
	s.byCat[cat.ID] = slices.Delete(slices.Clone(s.byCat[cat.ID]), idx, idx+1)
	s.mu.Unlock()
	return entry, s.commit([]string{entry.CategoryID}, EventChange)
}

// DeleteEntries removes many entries after taking a backup; returns how many were removed.
func (s *Store) DeleteEntries(ids []string) (int, error) {
	if _, err := s.Backup("pre-bulk-delete"); err != nil {
		return 0, err
	}
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	s.mu.Lock()
	n := 0
	var touched []string
	for _, c := range s.categories {
		list := s.byCat[c.ID]
		kept := make([]shared.Entry, 0, len(list))
		for _, e := range list {
			if !set[e.ID] {
				kept = append(kept, e)
			}
		}
		if len(kept) != len(list) {
			n += len(list) - len(kept)
			s.byCat[c.ID] = kept
			touched = append(touched, c.ID)
		}
	}
	s.mu.Unlock()
	return n, s.commit(touched, EventChange)
}

// MoveEntry is an atomic two-file move with in-memory and disk rollback if the second write fails.
// The destination is written first: a crash duplicates an entry, never loses one.
func (s *Store) MoveEntry(id, toCategoryID string) (shared.Entry, error) {
	s.mu.Lock()
	entry, from, idx, err := s.requireEntryLocked(id)
	if err != nil {
		s.mu.Unlock()
		return entry, err
	}
	to, err := s.requireCategoryLocked(toCategoryID)
	if err != nil {
		s.mu.Unlock()
		return entry, err
	}
	if from.ID == to.ID {
		s.mu.Unlock()
		return entry, nil
	}
	if from.Library != to.Library {
		s.mu.Unlock()
		return entry, &HTTPError{422, "LIBRARY_MISMATCH", "Cannot move between libraries."}
	}
	moved := entry
	moved.CategoryID, moved.UpdatedAt = to.ID, ISO(time.Now())
	dest := append(slices.Clone(s.byCat[to.ID]), moved)
	src := slices.Delete(slices.Clone(s.byCat[from.ID]), idx, idx+1)
	s.byCat[to.ID], s.byCat[from.ID] = dest, src
	s.bumpLocked()
	s.mu.Unlock()

	err = s.writeJSON(s.catFile(to), dest)
	if err == nil {
		err = s.writeJSON(s.catFile(from), src)
	}
	if err != nil {
		s.mu.Lock()
		s.byCat[to.ID] = slices.DeleteFunc(slices.Clone(s.byCat[to.ID]), func(e shared.Entry) bool { return e.ID == id })
		back := slices.Clone(s.byCat[from.ID])
		s.byCat[from.ID] = slices.Insert(back, min(idx, len(back)), entry)
		s.bumpLocked()
		restored := s.byCat[to.ID]
		s.mu.Unlock()
		_ = s.writeJSON(s.catFile(to), restored)
		return entry, err
	}
	s.emit(EventChange, nil)
	return moved, nil
}

type BulkOp struct {
	IDs    []string
	Action string // set_status | add_tag | remove_tag | move | delete
	Value  string
}

func (s *Store) Bulk(op BulkOp) (affected int, err error) {
	if op.Action == "delete" {
		return s.DeleteEntries(op.IDs)
	}
	for _, id := range op.IDs {
		e, ok := s.GetEntry(id)
		if !ok {
			continue
		}
		switch op.Action {
		case "set_status":
			_, err = s.UpdateEntry(id, map[string]any{"status": op.Value})
		case "add_tag":
			_, err = s.UpdateEntry(id, map[string]any{"tags": append(slices.Clone(e.Tags), op.Value)})
		case "remove_tag":
			kept := []string{}
			for _, t := range e.Tags {
				if !strings.EqualFold(t, op.Value) {
					kept = append(kept, t)
				}
			}
			_, err = s.UpdateEntry(id, map[string]any{"tags": kept})
		case "move":
			_, err = s.MoveEntry(id, op.Value)
		}
		if err != nil {
			return affected, err
		}
		affected++
	}
	return affected, nil
}

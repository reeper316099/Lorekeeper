package store

import (
	"slices"
	"strings"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

func (s *Store) CanonicalTags(names []string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canonicalTagsLocked(names)
}

func (s *Store) canonicalTagsLocked(names []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range names {
		t := shared.JSTrim(shared.JSCollapse(raw))
		if t == "" {
			continue
		}
		lc := strings.ToLower(t)
		name := t
		for _, x := range s.tags {
			if strings.ToLower(x.Name) == lc || slices.ContainsFunc(x.Aliases, func(a string) bool { return strings.ToLower(a) == lc }) {
				name = x.Name
				break
			}
		}
		if seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
	}
	return out
}

func (s *Store) writeTags() error {
	s.mu.RLock()
	snapshot := slices.Clone(s.tags)
	s.mu.RUnlock()
	if snapshot == nil {
		snapshot = []shared.Tag{}
	}
	return s.writeJSON(s.p("config", "tags.json"), snapshot)
}

func (s *Store) registerTags(names []string) error {
	s.mu.Lock()
	changed := false
	for _, n := range names {
		if !slices.ContainsFunc(s.tags, func(t shared.Tag) bool { return strings.EqualFold(t.Name, n) }) {
			s.tags = append(s.tags, shared.Tag{ID: shared.Slugify(n), Name: n, Aliases: []string{}})
			changed = true
		}
	}
	s.mu.Unlock()
	if !changed {
		return nil
	}
	return s.writeTags()
}

// TagUsage counts entries per tag, in the order tags are first seen.
func (s *Store) TagUsage() []shared.TagUsage {
	idx := map[string]int{}
	out := []shared.TagUsage{}
	for _, e := range s.All() {
		for _, t := range e.Tags {
			if i, ok := idx[t]; ok {
				out[i].Count++
			} else {
				idx[t] = len(out)
				out = append(out, shared.TagUsage{Name: t, Count: 1})
			}
		}
	}
	return out
}

type TagPatch struct {
	Name   *string
	Color  *string
	Hidden *bool
}

func (s *Store) UpdateTag(name string, patch TagPatch) (shared.Tag, error) {
	s.mu.Lock()
	idx := slices.IndexFunc(s.tags, func(t shared.Tag) bool { return strings.EqualFold(t.Name, name) })
	if idx < 0 {
		s.mu.Unlock()
		return shared.Tag{}, notFound("Tag not found: %s", name)
	}
	tag := s.tags[idx]
	oldName := tag.Name
	if patch.Color != nil {
		tag.Color = patch.Color
	}
	if patch.Hidden != nil {
		tag.Hidden = patch.Hidden
	}
	rename := ""
	if patch.Name != nil {
		if n := shared.JSTrim(*patch.Name); n != "" && n != oldName {
			if slices.ContainsFunc(s.tags, func(t shared.Tag) bool { return t.ID != tag.ID && strings.EqualFold(t.Name, n) }) {
				s.mu.Unlock()
				return tag, &HTTPError{Status: 409, Code: "TAG_EXISTS", Message: "A tag with that name exists; merge instead."}
			}
			tag.Name, tag.ID, rename = n, shared.Slugify(n), n
		}
	}
	s.tags[idx] = tag
	s.mu.Unlock()
	if rename != "" {
		if _, err := s.rewriteTags(func(tags []string) []string {
			out := make([]string, len(tags))
			for i, t := range tags {
				if t == oldName {
					t = rename
				}
				out[i] = t
			}
			return out
		}); err != nil {
			return tag, err
		}
	}
	if err := s.writeTags(); err != nil {
		return tag, err
	}
	s.mu.Lock()
	s.bumpLocked()
	s.mu.Unlock()
	s.emit(EventChange, nil)
	return tag, nil
}

// MergeTags merges `from` tags into `to`, remembering the old names as aliases so imports auto-resolve.
func (s *Store) MergeTags(from []string, to string) (affected int, err error) {
	s.mu.Lock()
	ti := slices.IndexFunc(s.tags, func(t shared.Tag) bool { return strings.EqualFold(t.Name, to) })
	if ti < 0 {
		s.tags = append(s.tags, shared.Tag{ID: shared.Slugify(to), Name: shared.JSTrim(to), Aliases: []string{}})
		ti = len(s.tags) - 1
	}
	targetName := s.tags[ti].Name
	fromLc := map[string]bool{}
	for _, f := range from {
		if lf := strings.ToLower(f); lf != strings.ToLower(targetName) {
			fromLc[lf] = true
		}
	}
	s.mu.Unlock()

	affected, err = s.rewriteTags(func(tags []string) []string {
		out := make([]string, len(tags))
		for i, t := range tags {
			if fromLc[strings.ToLower(t)] {
				t = targetName
			}
			out[i] = t
		}
		return out
	})
	if err != nil {
		return affected, err
	}

	s.mu.Lock()
	target := s.tags[slices.IndexFunc(s.tags, func(t shared.Tag) bool { return t.Name == targetName })]
	ti = slices.IndexFunc(s.tags, func(t shared.Tag) bool { return t.Name == targetName })
	for _, f := range from {
		if !strings.EqualFold(f, target.Name) && !slices.ContainsFunc(target.Aliases, func(a string) bool { return strings.EqualFold(a, f) }) {
			target.Aliases = append(target.Aliases, f)
		}
	}
	for _, t := range s.tags {
		if fromLc[strings.ToLower(t.Name)] {
			for _, a := range t.Aliases {
				if !slices.Contains(target.Aliases, a) {
					target.Aliases = append(target.Aliases, a)
				}
			}
		}
	}
	s.tags[ti] = target
	s.tags = slices.DeleteFunc(slices.Clone(s.tags), func(t shared.Tag) bool { return fromLc[strings.ToLower(t.Name)] })
	s.mu.Unlock()
	if err := s.writeTags(); err != nil {
		return affected, err
	}
	s.mu.Lock()
	s.bumpLocked()
	s.mu.Unlock()
	s.emit(EventChange, nil)
	return affected, nil
}

func dedupeTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		lc := strings.ToLower(t)
		if !seen[lc] {
			seen[lc] = true
			out = append(out, t)
		}
	}
	return out
}

func (s *Store) rewriteTags(fn func([]string) []string) (int, error) {
	s.mu.Lock()
	n := 0
	var touched []string
	for _, c := range s.categories {
		list := s.byCat[c.ID]
		var next []shared.Entry
		for i, e := range list {
			tags := dedupeTags(fn(e.Tags))
			if strings.Join(tags, "\x00") != strings.Join(e.Tags, "\x00") {
				if next == nil {
					next = slices.Clone(list)
				}
				e.Tags, e.UpdatedAt = tags, ISO(time.Now())
				next[i] = e
				n++
			}
		}
		if next != nil {
			s.byCat[c.ID] = next
			touched = append(touched, c.ID)
		}
	}
	s.mu.Unlock()
	return n, s.commit(touched, EventChange)
}

// ---------- settings ----------

func (s *Store) SaveSettings(next shared.Settings) (shared.Settings, error) {
	if err := next.Validate(); err != nil {
		return next, err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	if err := s.writeJSON(s.p("config", "settings.json"), next); err != nil {
		return next, err
	}
	s.emit(EventChange, nil)
	return next, nil
}

func (s *Store) SaveScales(next shared.Scales) (shared.Scales, error) {
	b, err := StableMarshal(next)
	if err != nil {
		return next, err
	}
	var checked shared.Scales
	if err := jsonUnmarshal(b, &checked); err != nil {
		return next, err
	}
	s.mu.Lock()
	s.scales = checked
	s.mu.Unlock()
	if err := s.writeFile(s.p("config", "scales.json"), b); err != nil {
		return checked, err
	}
	s.emit(EventChange, nil)
	return checked, nil
}

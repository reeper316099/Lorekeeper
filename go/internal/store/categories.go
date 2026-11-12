package store

import (
	"fmt"
	"slices"
	"strings"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

func (s *Store) saveCategories() error {
	s.mu.Lock()
	s.bumpLocked()
	snapshot := slices.Clone(s.categories)
	s.mu.Unlock()
	if err := s.writeJSON(s.p("config", "categories.json"), snapshot); err != nil {
		return err
	}
	s.emit(EventChange, nil)
	return nil
}

func (s *Store) uniqueSlugLocked(lib shared.Library, name string, self string) string {
	slug := shared.Slugify(name)
	base := slug
	for i := 2; ; i++ {
		taken := false
		for _, c := range s.categories {
			if c.ID != self && c.Library == lib && c.Slug == slug {
				taken = true
			}
		}
		if !taken {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

func (s *Store) CreateCategory(library shared.Library, name, color string) (shared.Category, error) {
	name = shared.JSTrim(name)
	s.mu.Lock()
	slug := s.uniqueSlugLocked(library, name, "")
	if color == "" {
		if c, ok := shared.NewCategoryPalette[strings.ToLower(name)]; ok {
			color = c
		} else {
			color = shared.DefaultCategoryColor
		}
	}
	order := 0
	for _, c := range s.categories {
		if c.Library == library {
			order++
		}
	}
	cat := shared.Category{ID: fmt.Sprintf("%s-%s", library, slug), Library: library, Name: name, Slug: slug, Color: color, Order: order}
	if err := cat.Validate(); err != nil {
		s.mu.Unlock()
		return cat, err
	}
	s.categories = append(s.categories, cat)
	s.byCat[cat.ID] = []shared.Entry{}
	s.mu.Unlock()
	if err := s.writeJSON(s.catFile(cat), []shared.Entry{}); err != nil {
		return cat, err
	}
	return cat, s.saveCategories()
}

type CategoryPatch struct {
	Name  *string
	Color *string
	Order *int
}

func (s *Store) UpdateCategory(id string, patch CategoryPatch) (shared.Category, error) {
	s.mu.Lock()
	idx := slices.IndexFunc(s.categories, func(c shared.Category) bool { return c.ID == id })
	if idx < 0 {
		s.mu.Unlock()
		return shared.Category{}, notFound("Category not found: %s", id)
	}
	cat := s.categories[idx]
	oldFile := s.catFile(cat)
	if patch.Color != nil && *patch.Color != "" {
		cat.Color = *patch.Color
	}
	if patch.Order != nil {
		cat.Order = *patch.Order
	}
	moveFile := false
	if patch.Name != nil && shared.JSTrim(*patch.Name) != "" && shared.JSTrim(*patch.Name) != cat.Name {
		cat.Name = shared.JSTrim(*patch.Name)
		if slug := s.uniqueSlugLocked(cat.Library, cat.Name, cat.ID); slug != cat.Slug {
			cat.Slug = slug
			moveFile = true
		}
	}
	if err := cat.Validate(); err != nil {
		s.mu.Unlock()
		return cat, err
	}
	s.categories[idx] = cat
	entries := s.byCat[id]
	s.mu.Unlock()
	if moveFile {
		if err := s.writeJSON(s.catFile(cat), entries); err != nil {
			return cat, err
		}
		_ = removeIfExists(oldFile)
	}
	return cat, s.saveCategories()
}

func (s *Store) ReorderCategories(library shared.Library, ids []string) error {
	s.mu.Lock()
	for i, id := range ids {
		for j := range s.categories {
			if s.categories[j].ID == id && s.categories[j].Library == library {
				s.categories[j].Order = i
			}
		}
	}
	s.mu.Unlock()
	return s.saveCategories()
}

// DeleteCategory removes a category. A non-empty one needs moveTo, which receives its entries.
func (s *Store) DeleteCategory(id, moveTo string) error {
	s.mu.RLock()
	cat, err := s.requireCategoryLocked(id)
	entries := slices.Clone(s.byCat[id])
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		if moveTo == "" {
			return &HTTPError{Status: 409, Code: "CATEGORY_NOT_EMPTY", Message: "Category has entries; pass moveTo."}
		}
		for _, e := range entries {
			if _, err := s.MoveEntry(e.ID, moveTo); err != nil {
				return err
			}
		}
	}
	if _, err := s.Backup("pre-category-delete"); err != nil {
		return err
	}
	s.mu.Lock()
	s.categories = slices.DeleteFunc(slices.Clone(s.categories), func(c shared.Category) bool { return c.ID == id })
	delete(s.byCat, id)
	s.mu.Unlock()
	_ = removeIfExists(s.catFile(cat))
	return s.saveCategories()
}

func (s *Store) EnsureCategory(library shared.Library, name, color string) (shared.Category, error) {
	s.mu.RLock()
	for _, c := range s.categories {
		if c.Library == library && strings.EqualFold(c.Name, shared.JSTrim(name)) {
			s.mu.RUnlock()
			return c, nil
		}
	}
	s.mu.RUnlock()
	return s.CreateCategory(library, name, color)
}

package importexport

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

type Strategy string

const (
	StrategySkip      Strategy = "skip"
	StrategyFill      Strategy = "fill"
	StrategyOverwrite Strategy = "overwrite"
	StrategyKeepBoth  Strategy = "keep_both"
)

type SheetOptions struct {
	Include  *bool
	Library  shared.Library
	Category string
	Mapping  []Field // nil = use the detected mapping
}

type ImportOptions struct {
	Strategy Strategy
	// Library forces a library for sheets whose headers do not reveal it (CSV).
	Library shared.Library
	// Category forces a category name (CSV without a Category column).
	Category string
	Sheets   map[string]SheetOptions
	// StatusMap maps raw unknown status text to a status (the "let me pick" step).
	StatusMap map[string]shared.Status
}

type WarningCode string

const (
	WarnBlankStatus     WarningCode = "BLANK_STATUS"
	WarnUnknownStatus   WarningCode = "UNKNOWN_STATUS"
	WarnBadScore        WarningCode = "BAD_SCORE"
	WarnStrayColumn     WarningCode = "STRAY_COLUMN"
	WarnUnknownColumn   WarningCode = "UNKNOWN_COLUMN"
	WarnNearDuplicate   WarningCode = "NEAR_DUPLICATE"
	WarnDuplicateInFile WarningCode = "DUPLICATE_IN_FILE"
	WarnSheetSkipped    WarningCode = "SHEET_SKIPPED"
	WarnNoLibrary       WarningCode = "NO_LIBRARY"
	WarnCategoryDiffers WarningCode = "CATEGORY_DIFFERS"
)

// ImportWarning fields are in sorted JSON-key order (they are written into import reports).
type ImportWarning struct {
	Code    WarningCode `json:"code"`
	Message string      `json:"message"`
	Row     int         `json:"row,omitempty"`
	Sheet   string      `json:"sheet"`
	Title   string      `json:"title,omitempty"`
}

type MappingEntry struct {
	Col    int    `json:"col"`
	Field  *Field `json:"field"`
	Header string `json:"header"`
}

type SheetPlan struct {
	Name            string          `json:"name"`
	Skipped         string          `json:"skipped,omitempty"`
	Library         *shared.Library `json:"library"`
	LibraryDetected *shared.Library `json:"libraryDetected"`
	Color           *string         `json:"color"`
	HeaderRow       int             `json:"headerRow"`
	Mapping         []MappingEntry  `json:"mapping"`
	Rows            int             `json:"rows"`
	CategoryExists  bool            `json:"categoryExists"`
	CategoryName    string          `json:"categoryName"`
}

type NewCategory struct {
	Library shared.Library `json:"library"`
	Name    string         `json:"name"`
	Color   string         `json:"color"`
}

type Summary struct {
	ByCategory             map[string]int `json:"byCategory"`
	ByRawStatus            map[string]int `json:"byRawStatus"`
	ByStatus               map[string]int `json:"byStatus"`
	DistinctTagsNormalized int            `json:"distinctTagsNormalized"`
	DistinctTagsRaw        int            `json:"distinctTagsRaw"`
	Entries                int            `json:"entries"`
}

type ImportPlan struct {
	Sheets         []SheetPlan
	NewCategories  []NewCategory
	Creates        []shared.Entry
	Updates        []shared.Entry
	Unchanged      int
	Skipped        int
	Warnings       []ImportWarning
	Summary        Summary
	TagSuggestions []shared.MergeSuggestion
}

type pendingCat struct {
	library     shared.Library
	name, color string
	slug, id    string
}

// categoryResolver resolves existing or planned categories without touching the store.
type categoryResolver struct {
	existing []shared.Category
	pending  []pendingCat
}

func (r *categoryResolver) resolve(library shared.Library, name, color string) (id string) {
	name = shared.JSTrim(name)
	lc := strings.ToLower(name)
	for _, c := range r.existing {
		if c.Library == library && strings.ToLower(c.Name) == lc {
			return c.ID
		}
	}
	for _, c := range r.pending {
		if c.library == library && strings.ToLower(c.name) == lc {
			return c.id
		}
	}
	slug := shared.Slugify(name)
	base := slug
	taken := func(s string) bool {
		for _, c := range r.existing {
			if c.Library == library && c.Slug == s {
				return true
			}
		}
		for _, c := range r.pending {
			if c.library == library && c.slug == s {
				return true
			}
		}
		return false
	}
	for i := 2; taken(slug); i++ {
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	if color == "" {
		color = SeedColor(library, name)
	}
	if color == "" {
		color = shared.NewCategoryPalette[lc]
	}
	if color == "" {
		color = shared.DefaultCategoryColor
	}
	p := pendingCat{library: library, name: name, color: color, slug: slug, id: fmt.Sprintf("%s-%s", library, slug)}
	r.pending = append(r.pending, p)
	return p.id
}

// cellsOf maps fields to the text of their first column. On sparse (xlsx) sheets an empty cell
// does not claim the field.
func cellsOf(mapping []Field, cells []string, sparse bool) map[Field]string {
	out := map[Field]string{}
	seen := map[Field]bool{}
	for i, f := range mapping {
		if f == "" || seen[f] {
			continue
		}
		v := ""
		if i < len(cells) {
			v = cells[i]
		}
		if sparse && v == "" {
			continue
		}
		seen[f] = true
		out[f] = v
	}
	return out
}

func orNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// mergeInto merges an incoming entry into an existing one. Returns false if nothing changed.
func mergeInto(existing, incoming shared.Entry, mode Strategy) (shared.Entry, bool) {
	overwrite := mode == StrategyOverwrite
	take := func(empty, hasIncoming bool) bool { return hasIncoming && (overwrite || empty) }
	next := existing
	if take(existing.Status == nil, incoming.Status != nil) {
		next.Status = incoming.Status
	}
	if take(existing.Score == nil, incoming.Score != nil) {
		next.Score = incoming.Score
	}
	if take(len(existing.Tags) == 0, len(incoming.Tags) > 0) {
		next.Tags = incoming.Tags
	}
	if take(existing.Season == nil, incoming.Season != nil) {
		next.Season = incoming.Season
	}
	if take(existing.Current.Raw == nil, incoming.Current.Raw != nil) {
		next.Current = incoming.Current
	}
	if take(existing.ConsumedUpTo.Raw == nil, incoming.ConsumedUpTo.Raw != nil) {
		next.ConsumedUpTo = incoming.ConsumedUpTo
	}
	if take(len(existing.Sources) == 0, len(incoming.Sources) > 0) {
		next.Sources = incoming.Sources
	}
	if take(len(existing.Links) == 0, len(incoming.Links) > 0) {
		next.Links = incoming.Links
	}
	if take(strVal(existing.Notes.General) == "", strVal(incoming.Notes.General) != "") {
		next.Notes.General = incoming.Notes.General
	}
	if take(strVal(existing.Notes.Extra1) == "", strVal(incoming.Notes.Extra1) != "") {
		next.Notes.Extra1 = incoming.Notes.Extra1
	}
	if take(strVal(existing.Notes.Extra2) == "", strVal(incoming.Notes.Extra2) != "") {
		next.Notes.Extra2 = incoming.Notes.Extra2
	}
	if existing.ImportRef == nil && incoming.ImportRef != nil {
		next.ImportRef = incoming.ImportRef
	}
	a, b := next, existing
	a.UpdatedAt, b.UpdatedAt, a.ImportRef, b.ImportRef = "", "", nil, nil
	sa, _ := store.StableMarshal(a)
	sb, _ := store.StableMarshal(b)
	if bytes.Equal(sa, sb) {
		return existing, false
	}
	next.UpdatedAt = store.ISO(time.Now())
	return next, true
}

type planned struct {
	e         shared.Entry
	createIdx int // index in creates, or -1 for an existing entry
	norm      string
}

type poolItem struct{ title, norm string }

// BuildPlan works out what an import would do without touching the store.
func BuildPlan(s *store.Store, fileName string, sheets []ParsedSheet, opts ImportOptions) (*ImportPlan, error) {
	strategy := opts.Strategy
	if strategy == "" {
		strategy = StrategyFill
	}
	now := store.ISO(time.Now())
	cats := s.Categories()
	resolver := &categoryResolver{existing: cats}
	plan := &ImportPlan{Sheets: []SheetPlan{}, Creates: []shared.Entry{}, Updates: []shared.Entry{}, Warnings: []ImportWarning{}}
	updates := map[string]shared.Entry{}
	var updateOrder []string
	warn := func(code WarningCode, sheet string, row int, title, msg string) {
		plan.Warnings = append(plan.Warnings, ImportWarning{Code: code, Sheet: sheet, Row: row, Title: title, Message: msg})
	}

	all := s.All()
	byKey := map[string]*planned{}
	pools := map[shared.Library][]poolItem{}
	for i := range all {
		n := shared.NormalizeTitle(all[i].Title)
		byKey[string(all[i].Library)+"\x00"+n] = &planned{e: all[i], createIdx: -1, norm: n}
		pools[all[i].Library] = append(pools[all[i].Library], poolItem{all[i].Title, n})
	}

	rawTags, normTags := map[string]bool{}, map[string]bool{}
	sum := Summary{ByCategory: map[string]int{}, ByRawStatus: map[string]int{}, ByStatus: map[string]int{}}

	for _, sheet := range sheets {
		so, hasSO := opts.Sheets[sheet.Name]
		library := firstLib(so.Library, sheet.Library, opts.Library)
		mapping := sheet.Mapping
		if hasSO && so.Mapping != nil {
			mapping = so.Mapping
		}
		catName := firstStr(so.Category, opts.Category, sheet.Name)
		sp := SheetPlan{
			Name: sheet.Name, Library: libPtr(library), LibraryDetected: libPtr(sheet.Library), Color: orNil(sheet.Color),
			HeaderRow: sheet.HeaderRow, Rows: len(sheet.Rows), CategoryName: catName, Mapping: []MappingEntry{},
		}
		for col, header := range sheet.Headers {
			var f *Field
			if col < len(mapping) && mapping[col] != "" {
				x := mapping[col]
				f = &x
			}
			sp.Mapping = append(sp.Mapping, MappingEntry{Col: col, Header: header, Field: f})
		}
		skip := func(reason string, code WarningCode, msg string) {
			sp.Skipped = reason
			plan.Sheets = append(plan.Sheets, sp)
			warn(code, sheet.Name, 0, "", msg)
		}
		if sheet.Skipped != "" || (hasSO && so.Include != nil && !*so.Include) {
			reason := sheet.Skipped
			if reason == "" {
				reason = "Excluded"
			}
			skip(reason, WarnSheetSkipped, "Skipped: "+reason)
			continue
		}
		if library == "" {
			skip("Library could not be detected", WarnNoLibrary, "Choose reading or watching for this sheet.")
			continue
		}
		if !slices.Contains(mapping, FTitle) {
			skip("No Title column", WarnSheetSkipped, "No Title column mapped.")
			continue
		}
		for _, c := range cats {
			if c.Library == library && strings.EqualFold(c.Name, catName) {
				sp.CategoryExists = true
			}
		}
		plan.Sheets = append(plan.Sheets, sp)
		// Always create the sheet's category, even when empty, to preserve structure.
		if !slices.Contains(mapping, FCategory) {
			resolver.resolve(library, catName, sheet.Color)
		}

		for i, h := range sheet.Headers {
			if i < len(mapping) && mapping[i] != "" {
				continue
			}
			hasData := false
			for _, r := range sheet.Rows {
				if i < len(r.Cells) && !isBlank(r.Cells[i]) {
					hasData = true
					break
				}
			}
			if h == "" && hasData {
				warn(WarnStrayColumn, sheet.Name, 0, "", fmt.Sprintf("Unlabeled column %d contains data and was ignored.", i+1))
			} else if h != "" {
				warn(WarnUnknownColumn, sheet.Name, 0, "", fmt.Sprintf("Column %q is not recognised and was ignored.", h))
			}
		}

		for _, r := range sheet.Rows {
			c := cellsOf(mapping, r.Cells, sheet.Sparse)
			title := strVal(shared.CleanText(c[FTitle]))
			if title == "" {
				continue
			}
			sum.Entries++

			rowCat := strVal(shared.CleanText(c[FCategory]))
			rowCatName, rowColor := catName, sheet.Color
			if rowCat != "" {
				rowCatName, rowColor = rowCat, ""
			}
			catID := resolver.resolve(library, rowCatName, rowColor)
			sum.ByCategory[rowCatName]++

			rawStatus := strVal(shared.CleanText(c[FStatus]))
			rs := rawStatus
			if rs == "" {
				rs = "(blank)"
			}
			sum.ByRawStatus[rs]++
			var status shared.Status
			ps := shared.ParseStatus(orNilAny(rawStatus))
			if !ps.Ok {
				if m, ok := opts.StatusMap[ps.Raw]; ok {
					status = m
				} else {
					status = shared.StatusPlanned
					warn(WarnUnknownStatus, sheet.Name, r.Row, title, fmt.Sprintf("Unknown status %q → Planned. Pick a mapping to override.", ps.Raw))
				}
			} else {
				status = ps.Status
				if ps.Blank {
					warn(WarnBlankStatus, sheet.Name, r.Row, title, "Blank status imported as Planned.")
				}
			}
			sum.ByStatus[string(status)]++

			rawScore := strVal(shared.CleanText(c[FScore]))
			score := shared.ParseScore(orNilAny(rawScore))
			if rawScore != "" && score == nil {
				warn(WarnBadScore, sheet.Name, r.Row, title, fmt.Sprintf("Could not read score %q.", rawScore))
			}

			for _, t := range reTagSplit.Split(c[FTags], -1) {
				if tt := shared.JSTrim(shared.JSCollapse(t)); tt != "" {
					rawTags[tt] = true
					normTags[strings.ToLower(tt)] = true
				}
			}

			links := []shared.Link{}
			if l := shared.ParseLink(orNilAny(c[FYoutubeLink]), shared.LinkYouTube); l != nil {
				l.Slot = shared.SlotPrimary
				links = append(links, *l)
			}
			if l := shared.ParseLink(orNilAny(c[FLink]), shared.LinkOther); l != nil {
				l.Slot = shared.SlotPrimary
				links = append(links, *l)
			}
			if l := shared.ParseLink(orNilAny(c[FOtherLinks]), shared.LinkOther); l != nil {
				l.Slot = shared.SlotOther
				links = append(links, *l)
			}

			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			in := shared.NewEntry()
			in.ID, in.Library, in.CategoryID, in.Title = id.String(), library, catID, title
			if library == shared.LibraryWatching {
				if m, ok := shared.WatchingMediaTypeByCategory[strings.ToLower(rowCatName)]; ok {
					in.MediaType = m
				}
			}
			in.Status, in.Score = &status, score
			in.Tags = s.CanonicalTags(shared.SplitTags(orNilAny(c[FTags])))
			in.Season = shared.CleanText(orNilAny(c[FSeason]))
			in.Current = shared.ParseProgress(orNilAny(c[FCurrent]))
			in.ConsumedUpTo = shared.ParseProgress(orNilAny(c[FConsumed]))
			in.Sources = shared.ParseSources(orNilAny(c[FSources]))
			in.Links = links
			in.Notes = shared.Notes{General: shared.CleanText(orNilAny(c[FGeneral])), Extra1: shared.CleanText(orNilAny(c[FExtra1])), Extra2: shared.CleanText(orNilAny(c[FExtra2]))}
			in.CreatedAt, in.UpdatedAt = now, now
			in.ImportRef = &shared.ImportRef{File: fileName, Sheet: sheet.Name, Row: float64(r.Row), ImportedAt: now}
			if err := in.Validate(); err != nil {
				return nil, err
			}

			norm := shared.NormalizeTitle(title)
			key := string(library) + "\x00" + norm
			if ex := byKey[key]; ex != nil && strategy != StrategyKeepBoth {
				if ex.createIdx >= 0 {
					warn(WarnDuplicateInFile, sheet.Name, r.Row, title, "Same title appears more than once in this import.")
				}
				if strategy == StrategySkip {
					plan.Skipped++
					continue
				}
				if ex.e.CategoryID != in.CategoryID {
					warn(WarnCategoryDiffers, sheet.Name, r.Row, title, "Existing entry is in another category; it was not moved.")
				}
				merged, changed := mergeInto(ex.e, in, strategy)
				switch {
				case !changed:
					plan.Unchanged++
				case ex.createIdx >= 0:
					plan.Creates[ex.createIdx] = merged
					ex.e = merged
				default:
					if _, ok := updates[merged.ID]; !ok {
						updateOrder = append(updateOrder, merged.ID)
					}
					updates[merged.ID] = merged
					ex.e = merged
				}
				continue
			}

			// A new entry: warn about fuzzy near-duplicates in the same library. The scan is quadratic,
			// so it is skipped for very large libraries.
			if pool := pools[library]; len(pool) <= 3000 {
				for _, o := range pool {
					if o.norm == norm {
						continue
					}
					if shared.IsNearDuplicateNorm(o.norm, norm) {
						warn(WarnNearDuplicate, sheet.Name, r.Row, title, fmt.Sprintf("Similar to existing title %q.", o.title))
						break
					}
				}
			}
			plan.Creates = append(plan.Creates, in)
			byKey[key] = &planned{e: in, createIdx: len(plan.Creates) - 1, norm: norm}
			pools[library] = append(pools[library], poolItem{title, norm})
		}
	}

	var usage []shared.TagUsage
	idx := map[string]int{}
	for _, e := range append(append([]shared.Entry(nil), all...), plan.Creates...) {
		for _, t := range e.Tags {
			if i, ok := idx[t]; ok {
				usage[i].Count++
			} else {
				idx[t] = len(usage)
				usage = append(usage, shared.TagUsage{Name: t, Count: 1})
			}
		}
	}
	for _, id := range updateOrder {
		plan.Updates = append(plan.Updates, updates[id])
	}
	for _, p := range resolver.pending {
		plan.NewCategories = append(plan.NewCategories, NewCategory{p.library, p.name, p.color})
	}
	if plan.NewCategories == nil {
		plan.NewCategories = []NewCategory{}
	}
	sum.DistinctTagsRaw, sum.DistinctTagsNormalized = len(rawTags), len(normTags)
	plan.Summary = sum
	plan.TagSuggestions = shared.SuggestTagMerges(usage)
	return plan, nil
}

var reTagSplit = regexp.MustCompile(`[,;\n]`)

// orNilAny turns "" into nil so the shared parsers treat an empty cell as no value.
func orNilAny(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func firstLib(libs ...shared.Library) shared.Library {
	for _, l := range libs {
		if l != "" {
			return l
		}
	}
	return ""
}

func firstStr(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func libPtr(l shared.Library) *shared.Library {
	if l == "" {
		return nil
	}
	return &l
}

// ---------------- persistence: preview / commit / undo ----------------

type Counts struct {
	Create    int `json:"create"`
	Update    int `json:"update"`
	Unchanged int `json:"unchanged"`
	Skipped   int `json:"skipped"`
}

// PublicPlan is the preview sent to the UI (no entries, only counts).
type PublicPlan struct {
	Sheets         []SheetPlan              `json:"sheets"`
	NewCategories  []NewCategory            `json:"newCategories"`
	Counts         Counts                   `json:"counts"`
	Warnings       []ImportWarning          `json:"warnings"`
	Summary        Summary                  `json:"summary"`
	TagSuggestions []shared.MergeSuggestion `json:"tagSuggestions"`
}

func (p *ImportPlan) Public() PublicPlan {
	return PublicPlan{p.Sheets, p.NewCategories, Counts{len(p.Creates), len(p.Updates), p.Unchanged, p.Skipped}, p.Warnings, p.Summary, p.TagSuggestions}
}

var idRe = regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{6}$`)

func importDir(s *store.Store, id string) (string, error) {
	if !idRe.MatchString(id) {
		return "", &store.HTTPError{Status: 400, Code: "VALIDATION", Message: "Invalid import id"}
	}
	return filepath.Join(s.ImportsDir(), id), nil
}

// ValidImportID reports whether id looks like an id NewID produced.
func ValidImportID(id string) bool { return idRe.MatchString(id) }

// NewID makes an import id: local date, time and 3 random bytes.
func NewID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

var reUnsafeName = regexp.MustCompile(`[^\w.\- ]+`)

// StageUpload saves an uploaded file under imports/<id>/ for the preview and commit steps.
func StageUpload(s *store.Store, fileName string, data []byte) (id, safeName string, err error) {
	id = NewID()
	dir, err := importDir(s, id)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	safeName = reUnsafeName.ReplaceAllString(filepath.Base(strings.ReplaceAll(fileName, `\`, "/")), "_")
	if safeName == "" || safeName == "." {
		safeName = "upload"
	}
	if err := store.AtomicWrite(filepath.Join(dir, "source"+strings.ToLower(filepath.Ext(safeName))), data); err != nil {
		return "", "", err
	}
	meta, err := store.StableMarshal(map[string]string{"fileName": safeName})
	if err != nil {
		return "", "", err
	}
	return id, safeName, store.AtomicWrite(filepath.Join(dir, "meta.json"), meta)
}

type staged struct {
	dir, fileName string
	sheets        []ParsedSheet
}

func loadStaged(s *store.Store, id string) (*staged, error) {
	dir, err := importDir(s, id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	var meta struct {
		FileName string `json:"fileName"`
	}
	if err != nil || json.Unmarshal(raw, &meta) != nil {
		return nil, &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "Unknown import id"}
	}
	ents, _ := os.ReadDir(dir)
	var src string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "source") {
			src = e.Name()
			break
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, src))
	if err != nil {
		return nil, &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "Unknown import id"}
	}
	sheets, err := ReadSpreadsheet(data, meta.FileName)
	if err != nil {
		return nil, err
	}
	return &staged{dir, meta.FileName, sheets}, nil
}

// PreviewResult is the response of POST /import/preview.
type PreviewResult struct {
	ImportID string `json:"importId"`
	FileName string `json:"fileName"`
	PublicPlan
}

func PreviewImport(s *store.Store, id string, opts ImportOptions) (*PreviewResult, error) {
	st, err := loadStaged(s, id)
	if err != nil {
		return nil, err
	}
	plan, err := BuildPlan(s, st.fileName, st.sheets, opts)
	if err != nil {
		return nil, err
	}
	return &PreviewResult{id, st.fileName, plan.Public()}, nil
}

// manifest records what an import touched so undo can restore it exactly.
type manifest struct {
	Created []string `json:"created"`
	Existed []string `json:"existed"`
	Undone  bool     `json:"undone,omitempty"`
}

type ReportCounts struct {
	CategoriesCreated int `json:"categoriesCreated"`
	Created           int `json:"created"`
	Skipped           int `json:"skipped"`
	Unchanged         int `json:"unchanged"`
	Updated           int `json:"updated"`
}

// ImportReport is written to imports/<id>/import-report.json (keys sorted).
type ImportReport struct {
	CommittedAt    string                   `json:"committedAt"`
	Counts         ReportCounts             `json:"counts"`
	CreatedIDs     []string                 `json:"createdIds"`
	File           string                   `json:"file"`
	ImportID       string                   `json:"importId"`
	Strategy       Strategy                 `json:"strategy"`
	Summary        Summary                  `json:"summary"`
	TagSuggestions []shared.MergeSuggestion `json:"tagSuggestions"`
	UpdatedIDs     []string                 `json:"updatedIds"`
	Warnings       []ImportWarning          `json:"warnings"`
}

// CommitResult is the response of POST /import/commit: the report plus the token that undoes it.
type CommitResult struct {
	ImportReport
	UndoToken string `json:"undoToken"`
}

// snapshotFiles copies every file an operation may touch into snapDir, returning the manifest.
func snapshotFiles(s *store.Store, snapDir string, targetCats map[string]bool) (manifest, error) {
	rels := []string{"config/categories.json", "config/tags.json"}
	for _, c := range s.Categories() {
		if targetCats[c.ID] {
			rels = append(rels, fmt.Sprintf("data/%s/%s.json", c.Library, c.Slug))
		}
	}
	m := manifest{Created: []string{}, Existed: []string{}}
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(s.Dir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		dst := filepath.Join(snapDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return m, err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return m, err
		}
		m.Existed = append(m.Existed, rel)
	}
	return m, nil
}

func writeJSONFile(file string, v any) error {
	b, err := store.StableMarshal(v)
	if err != nil {
		return err
	}
	return store.AtomicWrite(file, b)
}

func ids(es []shared.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

func CommitImport(s *store.Store, id string, opts ImportOptions) (*CommitResult, error) {
	st, err := loadStaged(s, id)
	if err != nil {
		return nil, err
	}
	preview, err := BuildPlan(s, st.fileName, st.sheets, opts)
	if err != nil {
		return nil, err
	}

	// Snapshot every file the import can touch (exactly restorable by undo).
	targets := map[string]bool{}
	for _, e := range append(append([]shared.Entry(nil), preview.Creates...), preview.Updates...) {
		targets[e.CategoryID] = true
	}
	man, err := snapshotFiles(s, filepath.Join(st.dir, "snapshot"), targets)
	if err != nil {
		return nil, err
	}
	if _, err := s.Backup("pre-import"); err != nil {
		return nil, err
	}

	// Create categories, then re-plan against the real ones so ids line up.
	for _, nc := range preview.NewCategories {
		cat, err := s.EnsureCategory(nc.Library, nc.Name, nc.Color)
		if err != nil {
			return nil, err
		}
		man.Created = append(man.Created, fmt.Sprintf("data/%s/%s.json", cat.Library, cat.Slug))
	}
	if err := writeJSONFile(filepath.Join(st.dir, "manifest.json"), man); err != nil {
		return nil, err
	}
	plan, err := BuildPlan(s, st.fileName, st.sheets, opts)
	if err != nil {
		return nil, err
	}
	if len(plan.NewCategories) > 0 {
		return nil, &store.HTTPError{Status: 500, Code: "IMPORT_INCONSISTENT", Message: "Categories did not resolve after creation"}
	}
	if err := s.InsertEntries(plan.Creates); err != nil {
		return nil, err
	}
	if len(plan.Updates) > 0 {
		if err := s.ReplaceEntries(plan.Updates); err != nil {
			return nil, err
		}
	}
	strategy := opts.Strategy
	if strategy == "" {
		strategy = StrategyFill
	}
	report := ImportReport{
		CommittedAt: store.ISO(time.Now()), File: st.fileName, ImportID: id, Strategy: strategy,
		Counts:  ReportCounts{CategoriesCreated: len(preview.NewCategories), Created: len(plan.Creates), Skipped: plan.Skipped, Unchanged: plan.Unchanged, Updated: len(plan.Updates)},
		Summary: plan.Summary, Warnings: plan.Warnings, TagSuggestions: plan.TagSuggestions,
		CreatedIDs: ids(plan.Creates), UpdatedIDs: ids(plan.Updates),
	}
	if report.TagSuggestions == nil {
		report.TagSuggestions = []shared.MergeSuggestion{}
	}
	if err := writeJSONFile(filepath.Join(st.dir, "import-report.json"), report); err != nil {
		return nil, err
	}
	return &CommitResult{report, id}, nil
}

// UndoImport restores the files an import touched and removes the category files it created.
func UndoImport(s *store.Store, id string) error {
	dir, err := importDir(s, id)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var man manifest
	if err != nil || json.Unmarshal(raw, &man) != nil {
		return &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "Nothing to undo for this import"}
	}
	if man.Undone {
		return &store.HTTPError{Status: 409, Code: "ALREADY_UNDONE", Message: "Import was already undone"}
	}
	for _, rel := range man.Existed {
		b, err := os.ReadFile(filepath.Join(dir, "snapshot", filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if err := store.AtomicWrite(filepath.Join(s.Dir, filepath.FromSlash(rel)), b); err != nil {
			return err
		}
	}
	for _, rel := range man.Created {
		if err := os.Remove(filepath.Join(s.Dir, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	man.Undone = true
	if err := writeJSONFile(filepath.Join(dir, "manifest.json"), man); err != nil {
		return err
	}
	if err := s.LoadAll(); err != nil {
		return err
	}
	s.Emit(store.EventChange, nil)
	return nil
}

// BeginUndoable snapshots every file an operation may touch so UndoImport can restore it exactly.
// Used by importers other than the spreadsheet one (the AniList list import).
func BeginUndoable(s *store.Store, targetCategoryIDs []string) (id, dir string, err error) {
	id = NewID()
	dir = filepath.Join(s.ImportsDir(), id)
	targets := map[string]bool{}
	for _, c := range targetCategoryIDs {
		targets[c] = true
	}
	man, err := snapshotFiles(s, filepath.Join(dir, "snapshot"), targets)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	if err := writeJSONFile(filepath.Join(dir, "manifest.json"), man); err != nil {
		return "", "", err
	}
	if _, err := s.Backup("pre-anilist-import"); err != nil {
		return "", "", err
	}
	return id, dir, nil
}

// ReadReport returns imports/<id>/import-report.json.
func ReadReport(s *store.Store, id string) (json.RawMessage, error) {
	dir, err := importDir(s, id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "import-report.json"))
	if err != nil {
		return nil, &store.HTTPError{Status: 404, Code: "NOT_FOUND", Message: "No report"}
	}
	return b, nil
}

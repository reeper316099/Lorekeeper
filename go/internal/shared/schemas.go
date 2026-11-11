package shared

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

// Schemas mirror packages/shared/src/schemas.ts (Zod). Decoding fills defaults and validates like
// Zod's parse(): unknown keys are dropped (except Entry.External, which is passthrough), and an
// invalid document returns a *ValidationError naming the offending field. Marshalling uses the
// same key names, so files written by either implementation load in the other.
//
// Differences from Zod, on purpose: Go cannot tell a missing key from an explicit null for
// non-pointer fields, so `"tags": null` decodes to an empty list instead of failing, and required
// string fields that are absent decode as "".

// ValidationError is returned when a decoded document breaks the schema.
type ValidationError struct {
	Path string
	Msg  string
}

func (e *ValidationError) Error() string { return e.Path + ": " + e.Msg }

func invalid(path, format string, args ...any) error {
	return &ValidationError{Path: path, Msg: fmt.Sprintf(format, args...)}
}

// ---- Progress ----------------------------------------------------------------------------

type ProgressKind string

const (
	ProgressNumber   ProgressKind = "number"
	ProgressStart    ProgressKind = "start"
	ProgressEnd      ProgressKind = "end"
	ProgressComplete ProgressKind = "complete"
	ProgressText     ProgressKind = "text"
)

// Struct fields are declared in sorted JSON-key order, as the store writes sorted keys; see
// TestJSONFieldsAreSorted.
type Progress struct {
	Kind   *ProgressKind `json:"kind"`
	Number *float64      `json:"number"`
	Raw    *string       `json:"raw"`
	Season *float64      `json:"season"`
}

func EmptyProgress() Progress { return Progress{} }

func (p Progress) validate(path string) error {
	if p.Kind != nil {
		switch *p.Kind {
		case ProgressNumber, ProgressStart, ProgressEnd, ProgressComplete, ProgressText:
		default:
			return invalid(path+".kind", "invalid value %q", *p.Kind)
		}
	}
	return nil
}

// ---- Link --------------------------------------------------------------------------------

type LinkKind string
type LinkSlot string

const (
	LinkYouTube LinkKind = "youtube"
	LinkSite    LinkKind = "site"
	LinkAniList LinkKind = "anilist"
	LinkOther   LinkKind = "other"

	SlotPrimary LinkSlot = "primary"
	SlotOther   LinkSlot = "other"
)

type Link struct {
	Channel *string  `json:"channel"`
	Date    *string  `json:"date"`
	Kind    LinkKind `json:"kind"`
	Label   *string  `json:"label"`
	Raw     *string  `json:"raw"`
	// Slot is which spreadsheet column it came from: the main link column or "Other Links".
	Slot LinkSlot `json:"slot,omitempty"`
	URL  *string  `json:"url"`
}

func (l Link) validate(path string) error {
	switch l.Kind {
	case LinkYouTube, LinkSite, LinkAniList, LinkOther:
	default:
		return invalid(path+".kind", "invalid value %q", l.Kind)
	}
	switch l.Slot {
	case "", SlotPrimary, SlotOther:
	default:
		return invalid(path+".slot", "invalid value %q", l.Slot)
	}
	return nil
}

// ---- Entry -------------------------------------------------------------------------------

type ImportRef struct {
	File       string  `json:"file"`
	ImportedAt string  `json:"importedAt"`
	Row        float64 `json:"row"`
	Sheet      string  `json:"sheet"`
}

type Notes struct {
	Extra1  *string `json:"extra1"`
	Extra2  *string `json:"extra2"`
	General *string `json:"general"`
}

type Entry struct {
	AltTitles    []string       `json:"altTitles"`
	CategoryID   string         `json:"categoryId"`
	CompletedAt  *string        `json:"completedAt,omitempty"`
	ConsumedUpTo Progress       `json:"consumedUpTo"`
	CreatedAt    string         `json:"createdAt"`
	Current      Progress       `json:"current"`
	External     map[string]any `json:"external"`
	Favorite     bool           `json:"favorite"`
	ID           string         `json:"id"`
	ImportRef    *ImportRef     `json:"importRef,omitempty"`
	Library      Library        `json:"library"`
	Links        []Link         `json:"links"`
	MediaType    MediaType      `json:"mediaType"`
	Notes        Notes          `json:"notes"`
	Score        *int           `json:"score"`
	Season       *string        `json:"season"`
	Sources      []string       `json:"sources"`
	StartedAt    *string        `json:"startedAt,omitempty"`
	Status       *Status        `json:"status"`
	Tags         []string       `json:"tags"`
	Title        string         `json:"title"`
	TotalUnits   *float64       `json:"totalUnits"`
	UpdatedAt    string         `json:"updatedAt"`
}

// NewEntry returns an Entry with every default applied (what Zod fills in for missing keys).
func NewEntry() Entry {
	return Entry{
		AltTitles: []string{}, MediaType: "unknown", Tags: []string{}, Sources: []string{},
		Links: []Link{}, External: map[string]any{},
	}
}

func (e *Entry) normalize() {
	if e.AltTitles == nil {
		e.AltTitles = []string{}
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	if e.Sources == nil {
		e.Sources = []string{}
	}
	if e.Links == nil {
		e.Links = []Link{}
	}
	if e.External == nil {
		e.External = map[string]any{}
	}
	if e.MediaType == "" {
		e.MediaType = "unknown"
	}
}

func (e *Entry) UnmarshalJSON(b []byte) error {
	type alias Entry
	a := alias(NewEntry())
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*e = Entry(a)
	e.normalize()
	return e.Validate()
}

func (e *Entry) Validate() error {
	if e.ID == "" {
		return invalid("id", "required")
	}
	if !validLibrary(e.Library) {
		return invalid("library", "invalid value %q", e.Library)
	}
	if e.CategoryID == "" {
		return invalid("categoryId", "required")
	}
	if e.Title == "" {
		return invalid("title", "required")
	}
	if !validMediaType(e.MediaType) {
		return invalid("mediaType", "invalid value %q", e.MediaType)
	}
	if e.Status != nil && !validStatus(*e.Status) {
		return invalid("status", "invalid value %q", *e.Status)
	}
	if e.Score != nil && (*e.Score < 1 || *e.Score > 10) {
		return invalid("score", "must be 1-10, got %d", *e.Score)
	}
	if err := e.Current.validate("current"); err != nil {
		return err
	}
	if err := e.ConsumedUpTo.validate("consumedUpTo"); err != nil {
		return err
	}
	for i, l := range e.Links {
		if err := l.validate(fmt.Sprintf("links[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}

// ---- Category ----------------------------------------------------------------------------

var reColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type Category struct {
	Color   string  `json:"color"`
	Icon    *string `json:"icon,omitempty"`
	ID      string  `json:"id"`
	Library Library `json:"library"`
	Name    string  `json:"name"`
	Order   int     `json:"order"`
	Slug    string  `json:"slug"`
}

func (c *Category) UnmarshalJSON(b []byte) error {
	type alias Category
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*c = Category(a)
	return c.Validate()
}

func (c *Category) Validate() error {
	switch {
	case c.ID == "":
		return invalid("id", "required")
	case !validLibrary(c.Library):
		return invalid("library", "invalid value %q", c.Library)
	case c.Name == "":
		return invalid("name", "required")
	case c.Slug == "":
		return invalid("slug", "required")
	case !reColor.MatchString(c.Color):
		return invalid("color", "must be #RRGGBB, got %q", c.Color)
	}
	return nil
}

// ---- Tag ---------------------------------------------------------------------------------

type Tag struct {
	Aliases []string `json:"aliases"`
	Color   *string  `json:"color,omitempty"`
	Hidden  *bool    `json:"hidden,omitempty"`
	ID      string   `json:"id"`
	Name    string   `json:"name"`
}

func (t *Tag) UnmarshalJSON(b []byte) error {
	type alias Tag
	a := alias{Aliases: []string{}}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if a.Aliases == nil {
		a.Aliases = []string{}
	}
	*t = Tag(a)
	switch {
	case t.ID == "":
		return invalid("id", "required")
	case t.Name == "":
		return invalid("name", "required")
	}
	return nil
}

// ---- Scales ------------------------------------------------------------------------------

func (s Scale) validate(path string) error {
	for k := range s.StatusLabels {
		if !validStatus(k) {
			return invalid(path+".statusLabels", "invalid status key %q", k)
		}
	}
	if len(s.ScoreLabels) != 10 {
		return invalid(path+".scoreLabels", "must have 10 labels, got %d", len(s.ScoreLabels))
	}
	return nil
}

func (s *Scales) UnmarshalJSON(b []byte) error {
	type alias Scales
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*s = Scales(a)
	if err := s.Reading.validate("reading"); err != nil {
		return err
	}
	return s.Watching.validate("watching")
}

// ---- Settings ----------------------------------------------------------------------------

type SavedView struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Query map[string]string `json:"query"`
}

type BackupSettings struct {
	Keep          int     `json:"keep"`
	OnLaunchHours float64 `json:"onLaunchHours"`
}

type Settings struct {
	Accent               string            `json:"accent"`
	AutoConfirmThreshold float64           `json:"autoConfirmThreshold"`
	Backup               BackupSettings    `json:"backup"`
	DefaultLibrary       string            `json:"defaultLibrary"`
	Density              string            `json:"density"`
	HiddenTags           []string          `json:"hiddenTags"`
	LabelOverrides       map[string]string `json:"labelOverrides"`
	SavedViews           []SavedView       `json:"savedViews"`
	Theme                string            `json:"theme"`
}

func DefaultSettings() Settings {
	return Settings{
		Theme: "dark", Accent: "#E5484D", Density: "comfortable", DefaultLibrary: "all",
		HiddenTags:           []string{},
		LabelOverrides:       map[string]string{"extra1": "Left off"},
		Backup:               BackupSettings{Keep: 20, OnLaunchHours: 6},
		AutoConfirmThreshold: 0.92,
		SavedViews:           []SavedView{},
	}
}

func (s *Settings) UnmarshalJSON(b []byte) error {
	type alias Settings
	a := alias(DefaultSettings())
	// Go merges JSON objects into a non-nil map; Zod replaces the default, so drop it first.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	if _, ok := probe["labelOverrides"]; ok {
		a.LabelOverrides = nil
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if a.HiddenTags == nil {
		a.HiddenTags = []string{}
	}
	if a.LabelOverrides == nil {
		a.LabelOverrides = map[string]string{}
	}
	if a.SavedViews == nil {
		a.SavedViews = []SavedView{}
	}
	*s = Settings(a)
	return s.Validate()
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func (s *Settings) Validate() error {
	switch {
	case !oneOf(s.Theme, "dark", "light", "system"):
		return invalid("theme", "invalid value %q", s.Theme)
	case !oneOf(s.Density, "comfortable", "compact"):
		return invalid("density", "invalid value %q", s.Density)
	case !oneOf(s.DefaultLibrary, "all", "reading", "watching"):
		return invalid("defaultLibrary", "invalid value %q", s.DefaultLibrary)
	case s.Backup.Keep < 1:
		return invalid("backup.keep", "must be >= 1")
	case s.AutoConfirmThreshold < 0 || s.AutoConfirmThreshold > 1:
		return invalid("autoConfirmThreshold", "must be between 0 and 1")
	}
	return nil
}

// ---- Meta / activity ---------------------------------------------------------------------

type Meta struct {
	AppVersion    string `json:"appVersion"`
	CreatedAt     string `json:"createdAt"`
	LastOpenedAt  string `json:"lastOpenedAt"`
	SchemaVersion int    `json:"schemaVersion"`
}

type ActivityRecord struct {
	At      string `json:"at"`
	EntryID string `json:"entryId"`
	Kind    string `json:"kind"`
	// From and To are arbitrary JSON; an explicit null is kept (unlike an absent key).
	From json.RawMessage `json:"from,omitempty"`
	To   json.RawMessage `json:"to,omitempty"`
}

// RawJSON encodes v for ActivityRecord.From / To.
func RawJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func (a *ActivityRecord) UnmarshalJSON(b []byte) error {
	type alias ActivityRecord
	var x alias
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	*a = ActivityRecord(x)
	if !oneOf(a.Kind, "progress", "status", "created", "imported") {
		return invalid("kind", "invalid value %q", a.Kind)
	}
	return nil
}

// ---- File helpers ------------------------------------------------------------------------

func decodeArray[T any](b []byte, what string) ([]T, error) {
	var syntax json.RawMessage
	if err := json.Unmarshal(b, &syntax); err != nil {
		return nil, err // a syntax error reports its position, like JSON.parse
	}
	if t := bytes.TrimSpace(b); len(t) == 0 || t[0] != '[' {
		return nil, invalid(what, "expected an array")
	}
	out := []T{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseEntryFile decodes and validates a data/<library>/<slug>.json document.
func ParseEntryFile(b []byte) ([]Entry, error)         { return decodeArray[Entry](b, "entries") }
func ParseCategoriesFile(b []byte) ([]Category, error) { return decodeArray[Category](b, "categories") }
func ParseTagsFile(b []byte) ([]Tag, error)            { return decodeArray[Tag](b, "tags") }

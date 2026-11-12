// Package shared is the Go port of packages/shared: domain schemas, constants and the
// spreadsheet-cell parsers. Behaviour matches the TypeScript original; see
// docs/go-port/phase-1.md for the few intentional differences.
package shared

// Single source of truth for the product name. Rename here only.
const (
	AppName       = "Lorekeeper"
	AppSlug       = "lorekeeper"
	SchemaVersion = 1
)

type Library string
type Status string
type MediaType string

const (
	LibraryReading  Library = "reading"
	LibraryWatching Library = "watching"
)

const (
	StatusPlanned    Status = "planned"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusOnHold     Status = "on_hold"
	StatusDropped    Status = "dropped"
	StatusRepeating  Status = "repeating"
)

var (
	Libraries = []Library{LibraryReading, LibraryWatching}
	Statuses  = []Status{
		StatusPlanned, StatusInProgress, StatusCompleted, StatusOnHold, StatusDropped, StatusRepeating,
	}
	MediaTypes = []MediaType{
		"manga", "manhwa", "manhua", "novel", "anime", "donghua", "tv", "cartoon", "kdrama",
		"documentary", "other", "unknown",
	}
)

// Score label sets found in the original workbooks.
var ScoreLabelSets = struct {
	ReadingMain  [10]string
	ReadingNewer [10]string
	Watching     [10]string
}{
	ReadingMain: [10]string{
		"Awful", "Terrible", "Bad", "Below Average", "Average", "Above Average", "Good", "Great",
		"Amazing", "Masterpiece",
	},
	ReadingNewer: [10]string{
		"Trash", "Very Bad", "Bad", "Below Average", "Mid", "Decent", "Good", "Great", "Amazing",
		"Masterpiece",
	},
	Watching: [10]string{
		"Terrible", "Bad", "Poor", "Mediocre", "Average", "Decent", "Good", "Great", "Excellent",
		"Masterpiece",
	},
}

type SeedCategory struct {
	Library Library
	Name    string
	Color   string
}

var SeedCategories = []SeedCategory{
	{LibraryReading, "Romance", "#8B2252"},
	{LibraryReading, "Isekai", "#4A148C"},
	{LibraryReading, "Action", "#B71C1C"},
	{LibraryReading, "Fantasy", "#004D40"},
	{LibraryReading, "Misc.", "#1A237E"},
	{LibraryWatching, "Anime", "#781E82"},
	{LibraryWatching, "Action Anime", "#B71C1C"},
	{LibraryWatching, "Romance Anime", "#8B2252"},
	{LibraryWatching, "Donghua", "#0F7B6C"},
	{LibraryWatching, "Action Donghua", "#B71C1C"},
	{LibraryWatching, "Romance Donghua", "#8B2252"},
	{LibraryWatching, "TV Shows", "#1565C0"},
	{LibraryWatching, "Cartoons", "#F57F17"},
	{LibraryWatching, "Sci-Fi", "#0D47A1"},
	{LibraryWatching, "K-Drama", "#00897B"},
	{LibraryWatching, "Horror", "#212121"},
	{LibraryWatching, "Comedy", "#E66400"},
	{LibraryWatching, "Documentary", "#4E342E"},
	{LibraryWatching, "Slice of Life", "#2E7D32"},
	{LibraryWatching, "Thriller", "#3E2723"},
	{LibraryWatching, "Misc.", "#1A237E"},
}

// Colors for newly created categories (from the original "Add Category" macro).
var NewCategoryPalette = map[string]string{
	"harem":   "#880E4F",
	"drama":   "#4E342E",
	"mystery": "#311B92",
	"sports":  "#1565C0",
}

const DefaultCategoryColor = "#37474F"

// Media type implied by a watching-library category name (lowercase key).
var WatchingMediaTypeByCategory = map[string]MediaType{
	"anime":           "anime",
	"action anime":    "anime",
	"romance anime":   "anime",
	"donghua":         "donghua",
	"action donghua":  "donghua",
	"romance donghua": "donghua",
	"tv shows":        "tv",
	"cartoons":        "cartoon",
	"k-drama":         "kdrama",
	"documentary":     "documentary",
}

func validLibrary(l Library) bool {
	for _, x := range Libraries {
		if x == l {
			return true
		}
	}
	return false
}

func validStatus(s Status) bool {
	for _, x := range Statuses {
		if x == s {
			return true
		}
	}
	return false
}

func validMediaType(m MediaType) bool {
	for _, x := range MediaTypes {
		if x == m {
			return true
		}
	}
	return false
}

// ValidStatus reports whether s is one of the known statuses.
func ValidStatus(s Status) bool { return validStatus(s) }

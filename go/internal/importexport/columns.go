// Package importexport is the Go port of packages/server/src/io: reading Excel and CSV workbooks,
// planning and committing imports (with undo), exporting XLSX/CSV/JSON, and the HTTP routes.
package importexport

import (
	"strings"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"golang.org/x/text/unicode/norm"
)

// Field is the app's meaning of a spreadsheet column ("" = not mapped).
type Field string

const (
	FTitle       Field = "title"
	FStatus      Field = "status"
	FScore       Field = "score"
	FTags        Field = "tags"
	FSeason      Field = "season"
	FCurrent     Field = "current"
	FConsumed    Field = "consumed"
	FYoutubeLink Field = "youtubeLink"
	FLink        Field = "link"
	FSources     Field = "sources"
	FGeneral     Field = "general"
	FExtra1      Field = "extra1"
	FExtra2      Field = "extra2"
	FOtherLinks  Field = "otherLinks"
	FCategory    Field = "category"
)

var Fields = []Field{FTitle, FStatus, FScore, FTags, FSeason, FCurrent, FConsumed, FYoutubeLink, FLink, FSources, FGeneral, FExtra1, FExtra2, FOtherLinks, FCategory}

func validField(f Field) bool {
	for _, x := range Fields {
		if x == f {
			return true
		}
	}
	return false
}

// HeaderKey is a case/whitespace/punctuation-insensitive key: "Score (1–10)" -> "score110".
func HeaderKey(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(norm.NFKC.String(h)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func oneOf(k string, list ...string) bool {
	for _, x := range list {
		if k == x {
			return true
		}
	}
	return false
}

// MatchHeader maps a column header to a field ("" when it is not recognised).
func MatchHeader(h string) Field {
	k := HeaderKey(h)
	switch {
	case k == "":
		return ""
	case k == "title" || k == "name":
		return FTitle
	case k == "status":
		return FStatus
	case strings.HasPrefix(k, "score") || k == "rating":
		return FScore
	case oneOf(k, "genrestags", "genres", "genre", "tags", "tag"):
		return FTags
	case k == "season":
		return FSeason
	case strings.HasPrefix(k, "watched"):
		return FConsumed
	case strings.HasPrefix(k, "currentlyon") || strings.HasPrefix(k, "current"):
		return FCurrent
	case strings.Contains(k, "youtube") && strings.Contains(k, "link"):
		return FYoutubeLink
	case k == "link" || k == "url":
		return FLink
	case oneOf(k, "readingmethod", "watchplatform", "platform", "source", "sources"):
		return FSources
	case k == "generalnotes" || k == "notes":
		return FGeneral
	case k == "extranotes1":
		return FExtra1
	case k == "extranotes2":
		return FExtra2
	case k == "otherlinks" || k == "otherlink":
		return FOtherLinks
	case k == "category":
		return FCategory
	}
	return ""
}

// DetectLibrary guesses the library from the headers ("" when they do not say).
func DetectLibrary(headers []string) shared.Library {
	for _, h := range headers {
		if strings.Contains(HeaderKey(h), "chapter") {
			return shared.LibraryReading
		}
	}
	for _, h := range headers {
		if strings.Contains(HeaderKey(h), "episode") {
			return shared.LibraryWatching
		}
	}
	return ""
}

// Layout is the column layout used when exporting a library (the original workbook layout).
type Layout struct {
	Headers []string
	Fields  []Field
	Widths  []float64
}

var Layouts = map[shared.Library]Layout{
	shared.LibraryReading: {
		Headers: []string{"Title", "Status", "Score (1–10)", "Genres / Tags", "Currently on Chapter", "Watched on YouTube Up To Chapter", "YouTube Link", "Reading Method", "General Notes", "Extra Notes 1", "Extra Notes 2", "Other Links"},
		Fields:  []Field{FTitle, FStatus, FScore, FTags, FCurrent, FConsumed, FYoutubeLink, FSources, FGeneral, FExtra1, FExtra2, FOtherLinks},
		Widths:  []float64{34, 14, 16, 34, 20, 28, 40, 18, 40, 40, 30, 36},
	},
	shared.LibraryWatching: {
		Headers: []string{"Title", "Status", "Score (1–10)", "Genres / Tags", "Season", "Currently on Episode", "Watched Until Episode", "Watch Platform", "Link", "General Notes", "Extra Notes 1", "Extra Notes 2"},
		Fields:  []Field{FTitle, FStatus, FScore, FTags, FSeason, FCurrent, FConsumed, FSources, FLink, FGeneral, FExtra1, FExtra2},
		Widths:  []float64{34, 14, 16, 34, 12, 20, 22, 18, 40, 40, 40, 30},
	},
}

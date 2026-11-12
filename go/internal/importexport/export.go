package importexport

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/search"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
	"github.com/xuri/excelize/v2"
)

func linkText(l shared.Link) string {
	switch {
	case l.Raw != nil:
		return *l.Raw
	case l.URL != nil:
		return *l.URL
	case l.Label != nil:
		return *l.Label
	}
	return ""
}

// CellFor is the cell value for one field of an entry: string, float64 or nil.
func CellFor(scales shared.Scales, e shared.Entry, f Field) any {
	str := func(p *string) any {
		if p == nil {
			return nil
		}
		return *p
	}
	switch f {
	case FTitle:
		return e.Title
	case FStatus:
		return nonEmpty(shared.StatusLabel(scales, e.Library, e.Status))
	case FScore:
		return nonEmpty(shared.ScoreLabel(scales, e.Library, e.Score))
	case FTags:
		if len(e.Tags) > 0 {
			return strings.Join(e.Tags, ", ")
		}
		return nil
	case FSeason:
		return str(e.Season)
	case FCurrent:
		return shared.ProgressCell(e.Current)
	case FConsumed:
		return shared.ProgressCell(e.ConsumedUpTo)
	case FSources:
		if len(e.Sources) > 0 {
			return strings.Join(e.Sources, ", ")
		}
		return nil
	case FGeneral:
		return str(e.Notes.General)
	case FExtra1:
		return str(e.Notes.Extra1)
	case FExtra2:
		return str(e.Notes.Extra2)
	case FYoutubeLink, FOtherLinks, FLink:
		var primary, other []shared.Link
		for _, l := range e.Links {
			if l.Slot == shared.SlotPrimary || (l.Slot == "" && l.Kind == shared.LinkYouTube) {
				primary = append(primary, l)
			} else {
				other = append(other, l)
			}
		}
		join := func(ls []shared.Link) any {
			if len(ls) == 0 {
				return nil
			}
			parts := make([]string, len(ls))
			for i, l := range ls {
				parts[i] = linkText(l)
			}
			return strings.Join(parts, "\n")
		}
		switch f {
		case FYoutubeLink:
			if len(primary) > 0 {
				return linkText(primary[0])
			}
			return nil
		case FOtherLinks:
			return join(other)
		}
		// watching "Link": every link, primary first (there is no Other Links column)
		return join(append(append([]shared.Link(nil), primary...), other...))
	}
	return nil
}

func nonEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var sheetNameBad = strings.NewReplacer(`\`, " ", "/", " ", "*", " ", "?", " ", ":", " ", "[", " ", "]", " ")

func safeSheetName(name string, used map[string]bool) string {
	base := shared.JSTrim(sheetNameBad.Replace(name))
	if r := []rune(base); len(r) > 31 {
		base = string(r[:31])
	}
	if base == "" {
		base = "Sheet"
	}
	n := base
	for i := 2; used[strings.ToLower(n)]; i++ {
		b := []rune(base)
		n = fmt.Sprintf("%s %d", string(b[:min(28, len(b))]), i)
	}
	used[strings.ToLower(n)] = true
	return n
}

// tint mixes a #RRGGBB color toward white; the result is upper-case hex without '#'.
func tint(hex string, amount float64) string {
	var r, g, b int
	fmt.Sscanf(strings.TrimPrefix(hex, "#"), "%02x%02x%02x", &r, &g, &b)
	mix := func(c int) int { return int(math.Round(float64(c) + float64(255-c)*amount)) }
	return strings.ToUpper(fmt.Sprintf("%02x%02x%02x", mix(r), mix(g), mix(b)))
}

func cellName(col, row int) string {
	n, _ := excelize.CoordinatesToCellName(col, row)
	return n
}

// ExportXLSX writes one workbook for a library in the original layout: a sheet per category with a
// banner in the category color, header row 2, frozen header, shaded rows and dropdown validations.
func ExportXLSX(s *store.Store, library shared.Library) ([]byte, error) {
	layout := Layouts[library]
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetDocProps(&excelize.DocProperties{Creator: shared.AppName}); err != nil {
		return nil, err
	}
	used := map[string]bool{}
	var cats []shared.Category
	for _, c := range s.Categories() {
		if c.Library == library {
			cats = append(cats, c)
		}
	}
	sort.SliceStable(cats, func(i, j int) bool { return cats[i].Order < cats[j].Order })
	scales := s.Scales()
	sc := scales.Reading
	if library == shared.LibraryWatching {
		sc = scales.Watching
	}
	var statusList []string
	for _, st := range shared.Statuses { // Object.values order: the order labels were declared in
		if l, ok := sc.StatusLabels[st]; ok {
			statusList = append(statusList, l)
		}
	}
	lastCol := len(layout.Headers)
	lastColName, _ := excelize.ColumnNumberToName(lastCol)

	for i, cat := range cats {
		name := safeSheetName(cat.Name, used)
		if i == 0 {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return nil, err
			}
		} else if _, err := f.NewSheet(name); err != nil {
			return nil, err
		}
		color := strings.ToUpper(strings.TrimPrefix(cat.Color, "#"))
		argb := "FF" + color
		text := strings.TrimPrefix(shared.ReadableTextColor(cat.Color), "#")
		argbPtr := argb
		if err := f.SetSheetProps(name, &excelize.SheetPropsOptions{TabColorRGB: &argbPtr}); err != nil {
			return nil, err
		}
		if err := f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 2, TopLeftCell: "A3", ActivePane: "bottomLeft",
			Selection: []excelize.Selection{{SQRef: "A3", ActiveCell: "A3", Pane: "bottomLeft"}}}); err != nil {
			return nil, err
		}

		banner, _ := f.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Bold: true, Size: 16, Color: text},
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{color}},
			Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "center"},
		})
		if err := f.MergeCell(name, "A1", lastColName+"1"); err != nil {
			return nil, err
		}
		f.SetCellValue(name, "A1", cat.Name+" Tracker")
		f.SetCellStyle(name, "A1", lastColName+"1", banner)
		f.SetRowHeight(name, 1, 30)

		header, _ := f.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Bold: true, Color: text},
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{tint(cat.Color, 0)}},
			Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "center", WrapText: true},
			Border:    []excelize.Border{{Type: "bottom", Color: "FFFFFF", Style: 1}},
		})
		for c, h := range layout.Headers {
			f.SetCellValue(name, cellName(c+1, 2), h)
		}
		f.SetCellStyle(name, "A2", lastColName+"2", header)
		f.SetRowHeight(name, 2, 22)
		for c, w := range layout.Widths {
			col, _ := excelize.ColumnNumberToName(c + 1)
			f.SetColWidth(name, col, col, w)
		}

		entries := slices.Clone(s.EntriesOf(cat.ID))
		sort.SliceStable(entries, func(a, b int) bool { return search.CompareLoose(entries[a].Title, entries[b].Title) < 0 })
		minRows := 200
		if library != shared.LibraryReading {
			minRows = 1000
		}
		lastRow := max(len(entries)+2, minRows)
		plain, _ := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}})
		shaded, _ := f.NewStyle(&excelize.Style{
			Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{tint(cat.Color, 0.9)}},
		})
		for r := 3; r <= lastRow; r++ {
			style := plain
			if (r-3)%2 == 1 {
				style = shaded
			}
			if err := f.SetCellStyle(name, cellName(1, r), cellName(lastCol, r), style); err != nil {
				return nil, err
			}
			if r-3 >= len(entries) {
				continue
			}
			for c, fld := range layout.Fields {
				if v := CellFor(scales, entries[r-3], fld); v != nil {
					if err := f.SetCellValue(name, cellName(c+1, r), v); err != nil {
						return nil, err
					}
				}
			}
		}
		addList := func(field Field, list []string) {
			col := slices.Index(layout.Fields, field) + 1
			dv := excelize.NewDataValidation(true)
			dv.Sqref = fmt.Sprintf("%s:%s", cellName(col, 3), cellName(col, lastRow))
			// Excel rejects an inline list longer than 255 characters; skip the dropdown then.
			if err := dv.SetDropList(list); err == nil {
				_ = f.AddDataValidation(name, dv)
			}
		}
		addList(FStatus, statusList)
		addList(FScore, sc.ScoreLabels)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return fixCoreProps(buf.Bytes())
}

// fixCoreProps replaces docProps/core.xml. excelize writes it without the cp: prefix and with an
// empty lastModifiedBy and a 2006 timestamp; Excel tolerates that but stricter readers (ExcelJS, so
// the Node build of this app) refuse the file. This writes the standard form with real timestamps.
func fixCoreProps(xlsx []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(xlsx), int64(len(xlsx)))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	core := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:creator>` + shared.AppName + `</dc:creator><cp:lastModifiedBy>` + shared.AppName + `</cp:lastModifiedBy>` +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + now + `</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">` + now + `</dcterms:modified></cp:coreProperties>`
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, zf := range zr.File {
		w, err := zw.Create(zf.Name)
		if err != nil {
			return nil, err
		}
		if zf.Name == "docProps/core.xml" {
			if _, err := w.Write([]byte(core)); err != nil {
				return nil, err
			}
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(w, rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func jsNumberString(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// csvField quotes like PapaParse's unparse: only when the text has a quote, comma, CR, LF or BOM,
// or starts or ends with a space.
func csvField(v any) string {
	var s string
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		s = x
	case float64:
		s = jsNumberString(x)
	default:
		s = fmt.Sprint(x)
	}
	if strings.ContainsAny(s, "\r\n\",") || strings.Contains(s, "\xef\xbb\xbf") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func unparseCSV(headers []string, rows [][]any, newline string) string {
	lines := make([]string, 0, len(rows)+1)
	hs := make([]any, len(headers))
	for i, h := range headers {
		hs[i] = h
	}
	for _, r := range append([][]any{hs}, rows...) {
		cells := make([]string, len(r))
		for i, c := range r {
			cells[i] = csvField(c)
		}
		lines = append(lines, strings.Join(cells, ","))
	}
	return strings.Join(lines, newline)
}

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

// ExportCSV writes entries as UTF-8 with a BOM (so Excel opens it cleanly) in the library layout,
// optionally with a trailing Category column.
func ExportCSV(s *store.Store, library shared.Library, entries []shared.Entry, withCategory bool) []byte {
	layout := Layouts[library]
	headers := slices.Clone(layout.Headers)
	if withCategory {
		headers = append(headers, "Category")
	}
	names := map[string]string{}
	for _, c := range s.Categories() {
		names[c.ID] = c.Name
	}
	scales := s.Scales()
	rows := make([][]any, len(entries))
	for i, e := range entries {
		row := make([]any, 0, len(headers))
		for _, f := range layout.Fields {
			row = append(row, CellFor(scales, e, f))
		}
		if withCategory {
			row = append(row, names[e.CategoryID])
		}
		rows[i] = row
	}
	return append(slices.Clone(utf8BOM), unparseCSV(headers, rows, "\r\n")...)
}

// CSVTemplate is an empty CSV with the library's headers and a Category column.
func CSVTemplate(library shared.Library) []byte {
	headers := append(slices.Clone(Layouts[library].Headers), "Category")
	return append(slices.Clone(utf8BOM), (unparseCSV(headers, nil, "\r\n") + "\r\n")...)
}

// ZipFiles builds a zip from path -> content, in sorted path order.
func ZipFiles(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(files[n]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

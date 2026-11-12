package importexport

import (
	"bytes"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
)

// ParsedRow is one data row. Row is the 1-based spreadsheet row number.
type ParsedRow struct {
	Row   int
	Cells []string
}

// ParsedSheet is a sheet (or a whole CSV) read into plain text cells, with its header mapping.
type ParsedSheet struct {
	Name string
	// Color is "#RRGGBB" from the tab color, when it is a plain RGB color.
	Color     string
	HeaderRow int
	Headers   []string
	Mapping   []Field
	Library   shared.Library
	Rows      []ParsedRow
	Skipped   string
	// Sparse sheets (xlsx) leave empty cells out, so an empty cell never claims a field that a
	// later column with the same meaning could fill. CSV cells are always present.
	Sparse bool
}

var reSkipName = regexp.MustCompile(`(?i)instruction|setup`)

func isBlank(s string) bool { return shared.JSTrim(s) == "" }

// findHeaderRow finds the header row (has Title and Status) within the first few rows; -1 if none.
func findHeaderRow(rows [][]string) int {
	for i := 0; i < min(len(rows), 6); i++ {
		hasTitle, hasStatus := false, false
		for _, c := range rows[i] {
			switch strings.ToLower(shared.JSTrim(c)) {
			case "title":
				hasTitle = true
			case "status":
				hasStatus = true
			}
		}
		if hasTitle && hasStatus {
			return i
		}
	}
	return -1
}

func build(name, color string, grid [][]string) ParsedSheet {
	base := ParsedSheet{Name: name, Color: color, Headers: []string{}, Mapping: []Field{}, Rows: []ParsedRow{}}
	if reSkipName.MatchString(name) {
		base.Skipped = "Instructions/setup sheet"
		return base
	}
	h := findHeaderRow(grid)
	if h < 0 {
		base.Skipped = "No header row with Title and Status"
		return base
	}
	headerCells := grid[h]
	width := len(headerCells)
	for width > 0 && isBlank(headerCells[width-1]) {
		width--
	}
	// Keep stray columns too (so we can warn about them): scan the data for the real width.
	rows := []ParsedRow{}
	maxData := width
	for i := h + 1; i < len(grid); i++ {
		cells := grid[i]
		last := len(cells)
		for last > 0 && isBlank(cells[last-1]) {
			last--
		}
		if last == 0 {
			continue
		}
		maxData = max(maxData, last)
		rows = append(rows, ParsedRow{Row: i + 1, Cells: cells})
	}
	headers := make([]string, maxData)
	mapping := make([]Field, maxData)
	for i := range headers {
		if i < len(headerCells) {
			headers[i] = shared.JSTrim(headerCells[i])
		}
		mapping[i] = MatchHeader(headers[i])
	}
	return ParsedSheet{Name: name, Color: color, HeaderRow: h + 1, Headers: headers, Mapping: mapping, Library: DetectLibrary(headers), Rows: rows}
}

func argbToHex(argb string) string {
	if len(argb) < 6 {
		return ""
	}
	hex := argb[len(argb)-6:]
	if _, err := strconv.ParseUint(hex, 16, 32); err != nil {
		return ""
	}
	return "#" + strings.ToUpper(hex)
}

var excelErrors = map[string]bool{"#N/A": true, "#REF!": true, "#VALUE!": true, "#DIV/0!": true, "#NAME?": true, "#NULL!": true, "#NUM!": true}

// isDateFormat reports whether a number format shows dates (built-in ids or a custom code with y/m/d).
func isDateFormat(numFmt int, custom *string) bool {
	switch {
	case numFmt >= 14 && numFmt <= 22, numFmt >= 27 && numFmt <= 36, numFmt >= 45 && numFmt <= 47, numFmt >= 50 && numFmt <= 58:
		return true
	}
	if custom == nil {
		return false
	}
	in := false
	for _, r := range strings.ToLower(*custom) {
		switch {
		case r == '"':
			in = !in
		case !in && (r == 'y' || r == 'd' || r == 'm' || r == 'h'):
			return true
		}
	}
	return false
}

// ReadXLSX reads every sheet of a workbook. Cells come back as the text of their stored value
// (numbers as written, dates as YYYY-MM-DD, formulas as their cached result, errors as empty).
func ReadXLSX(data []byte) ([]ParsedSheet, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []ParsedSheet
	dateStyle := map[int]bool{}
	isDate := func(sheet string, col, row int) bool {
		cell, err := excelize.CoordinatesToCellName(col, row)
		if err != nil {
			return false
		}
		id, err := f.GetCellStyle(sheet, cell)
		if err != nil || id == 0 {
			return false
		}
		if v, ok := dateStyle[id]; ok {
			return v
		}
		st, err := f.GetStyle(id)
		v := err == nil && st != nil && isDateFormat(st.NumFmt, st.CustomNumFmt)
		dateStyle[id] = v
		return v
	}
	for _, name := range f.GetSheetList() {
		if reSkipName.MatchString(name) {
			out = append(out, build(name, "", nil))
			continue
		}
		rows, err := f.Rows(name)
		if err != nil {
			return nil, err
		}
		var grid [][]string
		r := 0
		for rows.Next() {
			r++
			cols, err := rows.Columns(excelize.Options{RawCellValue: true})
			if err != nil {
				rows.Close()
				return nil, err
			}
			cells := make([]string, len(cols))
			for c, v := range cols {
				switch {
				case v == "" || excelErrors[v]:
				case isNumeric(v) && isDate(name, c+1, r):
					serial, _ := strconv.ParseFloat(v, 64)
					if t, err := excelize.ExcelDateToTime(serial, false); err == nil {
						cells[c] = t.Format("2006-01-02")
					} else {
						cells[c] = v
					}
				default:
					cells[c] = v
				}
			}
			grid = append(grid, cells)
		}
		rows.Close()
		color := ""
		if props, err := f.GetSheetProps(name); err == nil && props.TabColorRGB != nil {
			color = argbToHex(*props.TabColorRGB)
		}
		sh := build(name, color, grid)
		sh.Sparse = true
		out = append(out, sh)
	}
	return out, nil
}

func isNumeric(s string) bool {
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
}

// DecodeText strips a UTF-8 BOM and decodes UTF-8, falling back to Windows-1252.
func DecodeText(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	if utf8.Valid(b) {
		return string(b)
	}
	out, err := charmap.Windows1252.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// guessNewline ports PapaParse's line-ending guess: ignore quoted text, then pick "\n" unless
// carriage returns are the row separators ("\r\n" or a lone "\r").
func guessNewline(text string) string {
	if len(text) > 1<<20 {
		text = text[:1<<20]
	}
	text = reQuoted.ReplaceAllString(text, "")
	r := strings.Split(text, "\r")
	n := strings.Split(text, "\n")
	if nFirst := len(n) > 1 && len(n[0]) < len(r[0]); len(r) == 1 || nFirst {
		return "\n"
	}
	withN := 0
	for _, part := range r {
		if strings.HasPrefix(part, "\n") {
			withN++
		}
	}
	if float64(withN) >= float64(len(r))/2 {
		return "\r\n"
	}
	return "\r"
}

var reQuoted = regexp.MustCompile(`(?s)"(.*?)"`)

// parseCSV splits text into rows and fields like PapaParse: quoted fields keep their content exactly
// (embedded newlines, "" for a quote), unquoted fields end at the delimiter or the newline, and a
// trailing newline leaves a final empty row. limit > 0 stops after that many rows.
func parseCSV(text string, delim rune, newline string, limit int) [][]string {
	var rows [][]string
	var row []string
	d := string(delim)
	i := 0
	for i <= len(text) {
		var field strings.Builder
		if i < len(text) && text[i] == '"' {
			i++
			for i < len(text) {
				if text[i] == '"' {
					if i+1 < len(text) && text[i+1] == '"' {
						field.WriteByte('"')
						i += 2
						continue
					}
					i++
					break
				}
				field.WriteByte(text[i])
				i++
			}
			// be lenient about text between the closing quote and the next delimiter
			for i < len(text) && !strings.HasPrefix(text[i:], d) && !strings.HasPrefix(text[i:], newline) {
				field.WriteByte(text[i])
				i++
			}
		} else {
			for i < len(text) && !strings.HasPrefix(text[i:], d) && !strings.HasPrefix(text[i:], newline) {
				field.WriteByte(text[i])
				i++
			}
		}
		row = append(row, field.String())
		switch {
		case i >= len(text):
			rows = append(rows, row)
			return rows
		case strings.HasPrefix(text[i:], d):
			i += len(d)
		default:
			i += len(newline)
			rows = append(rows, row)
			row = nil
			if limit > 0 && len(rows) >= limit {
				return rows
			}
			if i >= len(text) { // a trailing newline leaves one empty row
				rows = append(rows, []string{""})
				return rows
			}
		}
	}
	return rows
}

func emptyLine(row []string) bool {
	for _, c := range row {
		if !isBlank(c) {
			return false
		}
	}
	return true
}

// guessDelimiter ports PapaParse's guess among , ; and tab: the delimiter whose first 10 rows have
// the most consistent field count (and more than one field on average).
func guessDelimiter(text, newline string) rune {
	var best rune
	bestDelta, maxFields := 0.0, 0.0
	for _, d := range []rune{',', ';', '\t'} {
		preview := parseCSV(text, d, newline, 10)
		delta, avg, empty := 0.0, 0.0, 0
		prev := -1
		for _, row := range preview {
			if emptyLine(row) {
				empty++
				continue
			}
			n := len(row)
			avg += float64(n)
			if prev < 0 {
				prev = n
			} else if n > 0 {
				delta += math.Abs(float64(n - prev))
				prev = n
			}
		}
		if len(preview) > 0 {
			avg /= float64(len(preview) - empty)
		}
		if (best == 0 || delta <= bestDelta) && (maxFields == 0 || avg > maxFields) && avg > 1.99 {
			best, bestDelta, maxFields = d, delta, avg
		}
	}
	if best == 0 {
		return ','
	}
	return best
}

// ReadCSV reads a .csv/.tsv/.txt file as one sheet named after the file.
func ReadCSV(data []byte, fileName string) []ParsedSheet {
	text := DecodeText(data)
	newline := guessNewline(text)
	records := parseCSV(text, guessDelimiter(text, newline), newline, 0)
	grid := make([][]string, 0, len(records))
	for _, rec := range records {
		if !emptyLine(rec) { // skipEmptyLines: 'greedy'
			grid = append(grid, rec)
		}
	}
	name := regexp.MustCompile(`\.[^.]+$`).ReplaceAllString(fileName, "")
	if name == "" {
		name = "Imported"
	}
	return []ParsedSheet{build(name, "", grid)}
}

// SeedColor is the fallback color for a category name when the tab color is missing or themed.
func SeedColor(library shared.Library, name string) string {
	for _, c := range shared.SeedCategories {
		if c.Library == library && strings.EqualFold(c.Name, name) {
			return c.Color
		}
	}
	return ""
}

// ReadSpreadsheet picks the reader by file extension.
func ReadSpreadsheet(data []byte, fileName string) ([]ParsedSheet, error) {
	lower := strings.ToLower(fileName)
	switch {
	case strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".xlsm"):
		return ReadXLSX(data)
	case strings.HasSuffix(lower, ".csv") || strings.HasSuffix(lower, ".tsv") || strings.HasSuffix(lower, ".txt"):
		return ReadCSV(data, fileName), nil
	}
	return nil, errors.New("Unsupported file type (use .xlsx, .xlsm, .csv or .tsv)")
}

package importexport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
	"github.com/xuri/excelize/v2"
)

// Ported from packages/server/test/import.test.ts.

type expectedSummary struct {
	Total       int            `json:"total"`
	ByCategory  map[string]int `json:"byCategory"`
	ByRawStatus map[string]int `json:"byRawStatus"`
	TagsRaw     int            `json:"tagsRaw"`
	TagsNorm    int            `json:"tagsNormalized"`
}

func expected(t *testing.T) (reading, watching expectedSummary) {
	t.Helper()
	b := synFile(t, "expected.json")
	var raw map[string]expectedSummary
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	return raw["reading"], raw["watching"]
}

func expectCounts(t *testing.T, r *CommitResult, exp expectedSummary) {
	t.Helper()
	if r.Summary.Entries != exp.Total || r.Counts.Created != exp.Total {
		t.Errorf("entries %d created %d, want %d", r.Summary.Entries, r.Counts.Created, exp.Total)
	}
	if !reflect.DeepEqual(r.Summary.ByCategory, exp.ByCategory) {
		t.Errorf("byCategory %v, want %v", r.Summary.ByCategory, exp.ByCategory)
	}
	if !reflect.DeepEqual(r.Summary.ByRawStatus, exp.ByRawStatus) {
		t.Errorf("byRawStatus %v, want %v", r.Summary.ByRawStatus, exp.ByRawStatus)
	}
	if r.Summary.DistinctTagsRaw != exp.TagsRaw || r.Summary.DistinctTagsNormalized != exp.TagsNorm {
		t.Errorf("tags raw %d norm %d, want %d / %d", r.Summary.DistinctTagsRaw, r.Summary.DistinctTagsNormalized, exp.TagsRaw, exp.TagsNorm)
	}
}

func warningCount(r *CommitResult, code WarningCode) int {
	n := 0
	for _, w := range r.Warnings {
		if w.Code == code {
			n++
		}
	}
	return n
}

func catNames(s *store.Store, lib shared.Library) []string {
	var out []string
	for _, c := range s.Categories() {
		if c.Library == lib {
			out = append(out, c.Name)
		}
	}
	return out
}

func catByName(s *store.Store, name string) shared.Category {
	for _, c := range s.Categories() {
		if c.Name == name {
			return c
		}
	}
	return shared.Category{}
}

func TestReadingWorkbookExactCountsAndStructure(t *testing.T) {
	rd, _ := expected(t)
	s := newStore(t)
	r := importBytes(t, s, "Reading_Tracker.xlsx", synFile(t, "Reading_Tracker.xlsx"), ImportOptions{})
	expectCounts(t, r, rd)
	if got := catNames(s, shared.LibraryReading); !reflect.DeepEqual(got, []string{"Romance", "Isekai", "Action", "Fantasy", "Misc."}) {
		t.Errorf("categories %v", got)
	}
	if c := catByName(s, "Romance").Color; c != "#8B2252" {
		t.Errorf("Romance color %s", c)
	}
	var skipped []string
	for _, w := range r.Warnings {
		if w.Code == WarnSheetSkipped {
			skipped = append(skipped, w.Sheet)
		}
	}
	if !reflect.DeepEqual(skipped, []string{"Add Category - Instructions"}) {
		t.Errorf("skipped sheets %v", skipped)
	}
	if warningCount(r, WarnNearDuplicate) == 0 {
		t.Error("expected a near-duplicate warning")
	}
	if r.Counts.CategoriesCreated != 5 {
		t.Errorf("categoriesCreated %d", r.Counts.CategoriesCreated)
	}
}

func TestWatchingWorkbookBlankStatusesAndThemeColors(t *testing.T) {
	_, wt := expected(t)
	s := newStore(t)
	r := importBytes(t, s, "Watching_Tracker.xlsx", synFile(t, "Watching_Tracker.xlsx"), ImportOptions{})
	expectCounts(t, r, wt)
	if n := warningCount(r, WarnBlankStatus); n != 6 {
		t.Errorf("blank status warnings %d", n)
	}
	planned := 0
	for _, e := range s.All() {
		if e.Status != nil && *e.Status == shared.StatusPlanned {
			planned++
		}
	}
	if planned != 6 || len(s.Categories()) != 16 {
		t.Errorf("planned %d categories %d", planned, len(s.Categories()))
	}
	if c := catByName(s, "Action Donghua").Color; c != "#B71C1C" {
		t.Errorf("Action Donghua (themed tab color) fell back to %s", c)
	}
	if c := catByName(s, "Romance Donghua").Color; c != "#8B2252" {
		t.Errorf("Romance Donghua fell back to %s", c)
	}
	for _, e := range s.EntriesOf("watching-action-donghua") {
		if e.MediaType != "donghua" {
			t.Errorf("media type %s", e.MediaType)
		}
	}
}

func TestRawProgressTextScoresSourcesAndLinks(t *testing.T) {
	s := newStore(t)
	importBytes(t, s, "Reading_Tracker.xlsx", synFile(t, "Reading_Tracker.xlsx"), ImportOptions{})
	importBytes(t, s, "Watching_Tracker.xlsx", synFile(t, "Watching_Tracker.xlsx"), ImportOptions{})
	raws := map[string]bool{}
	for _, e := range s.All() {
		for _, p := range []shared.Progress{e.Current, e.ConsumedUpTo} {
			if p.Raw != nil {
				raws[*p.Raw] = true
			}
		}
	}
	for _, must := range []string{"End of S1", "49?", "Beginning of S2", "S2 E1", "Done", "Completed", "End", "S1 E13"} {
		if !raws[must] {
			t.Errorf("progress text %q was not preserved", must)
		}
	}
	var sawLink, sawDesc, readingScore bool
	for _, e := range s.All() {
		if e.Current.Raw != nil && *e.Current.Raw == "S2 E1" && (*e.Current.Season != 2 || *e.Current.Number != 1) {
			t.Errorf("S2 E1 parsed as %+v", e.Current)
		}
		if e.ConsumedUpTo.Raw != nil && *e.ConsumedUpTo.Raw == "49?" && (*e.ConsumedUpTo.Number != 49 || *e.ConsumedUpTo.Kind != shared.ProgressText) {
			t.Errorf("49? parsed as %+v", e.ConsumedUpTo)
		}
		if e.Library != shared.LibraryReading {
			continue
		}
		if len(e.Sources) == 0 {
			t.Errorf("%s has no sources", e.Title)
		}
		readingScore = readingScore || e.Score != nil
		for _, l := range e.Links {
			if l.URL != nil {
				if !strings.HasPrefix(*l.URL, "http") {
					t.Errorf("link %q is not absolute", *l.URL)
				}
				sawLink = sawLink || strings.HasPrefix(*l.URL, "https://tapas.example")
			} else if l.Label != nil && l.Channel != nil && l.Date != nil {
				sawDesc = true
			}
		}
	}
	if !sawLink || !sawDesc || !readingScore {
		t.Errorf("links/desc/score coverage: %v %v %v", sawLink, sawDesc, readingScore)
	}
}

func TestScoresParseAcrossLabelSetsIncludingLeadingSpace(t *testing.T) {
	sheets, err := ReadXLSX(synFile(t, "Reading_Tracker.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	col := slices.Index(sheets[0].Mapping, FScore)
	leading, labelled := false, false
	for _, r := range sheets[0].Rows {
		if col >= len(r.Cells) || r.Cells[col] == "" {
			continue
		}
		c := r.Cells[col]
		leading = leading || strings.HasPrefix(c, " ")
		labelled = labelled || strings.Contains(c, "Above Average") || strings.Contains(c, "Awful")
		if shared.ParseScore(c) == nil {
			t.Errorf("score %q does not parse", c)
		}
	}
	if !leading || !labelled {
		t.Errorf("leading space %v, label sets %v", leading, labelled)
	}
}

func TestReimportIsIdempotentAndNeverOverwritesByDefault(t *testing.T) {
	data := synFile(t, "Reading_Tracker.xlsx")
	s := newStore(t)
	importBytes(t, s, "Reading_Tracker.xlsx", data, ImportOptions{})
	target := s.All()[0]
	if _, err := s.UpdateEntry(target.ID, map[string]any{"notes": map[string]any{"general": "MY EDIT"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(s.All())
	again := importBytes(t, s, "Reading_Tracker.xlsx", data, ImportOptions{})
	if again.Counts != (ReportCounts{Unchanged: 66}) {
		t.Errorf("counts %+v", again.Counts)
	}
	after, _ := json.Marshal(s.All())
	if !bytes.Equal(before, after) {
		t.Error("re-import changed entries")
	}
	if e, _ := s.GetEntry(target.ID); *e.Notes.General != "MY EDIT" {
		t.Error("user edit lost")
	}
}

func TestStrategiesSkipOverwriteKeepBoth(t *testing.T) {
	data := synFile(t, "Reading_Tracker.xlsx")
	s := newStore(t)
	importBytes(t, s, "r.xlsx", data, ImportOptions{})
	tgt := s.All()[0]
	s.UpdateEntry(tgt.ID, map[string]any{"score": 1, "notes": map[string]any{"general": "edited"}})
	if r := importBytes(t, s, "r.xlsx", data, ImportOptions{Strategy: StrategySkip}); r.Counts.Skipped != 66 {
		t.Errorf("skip counts %+v", r.Counts)
	}
	if e, _ := s.GetEntry(tgt.ID); *e.Notes.General != "edited" {
		t.Error("skip changed the entry")
	}
	if r := importBytes(t, s, "r.xlsx", data, ImportOptions{Strategy: StrategyOverwrite}); r.Counts.Updated == 0 {
		t.Error("overwrite updated nothing")
	}
	if e, _ := s.GetEntry(tgt.ID); e.Notes.General != nil && *e.Notes.General == "edited" {
		t.Error("overwrite kept the edit")
	}
	importBytes(t, s, "r.xlsx", data, ImportOptions{Strategy: StrategyKeepBoth})
	if len(s.All()) != 132 {
		t.Errorf("keep_both: %d entries", len(s.All()))
	}
}

func snapshotOf(s *store.Store) string {
	b, _ := json.Marshal(map[string]any{"e": s.All(), "c": s.Categories()})
	return string(b)
}

func TestUndoRestoresTheExactPreImportState(t *testing.T) {
	s := newStore(t)
	importBytes(t, s, "r.xlsx", synFile(t, "Reading_Tracker.xlsx"), ImportOptions{})
	snap := snapshotOf(s)
	r := importBytes(t, s, "w.xlsx", synFile(t, "Watching_Tracker.xlsx"), ImportOptions{})
	if len(s.All()) != 114 {
		t.Fatalf("entries %d", len(s.All()))
	}
	if err := UndoImport(s, r.UndoToken); err != nil {
		t.Fatal(err)
	}
	if snapshotOf(s) != snap {
		t.Error("state differs after undo")
	}
	var he *store.HTTPError
	if err := UndoImport(s, r.UndoToken); !errors.As(err, &he) || he.Code != "ALREADY_UNDONE" || he.Status != 409 {
		t.Errorf("second undo: %v", err)
	}
	re := store.New(s.Dir, store.Options{})
	if err := re.Open(); err != nil {
		t.Fatal(err)
	}
	if snapshotOf(re) != snap {
		t.Error("undo did not persist to disk")
	}
	if err := UndoImport(s, "nope"); !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("bad id: %v", err)
	}
	if err := UndoImport(s, "20200101-000000-abcdef"); !errors.As(err, &he) || he.Status != 404 {
		t.Errorf("unknown id: %v", err)
	}
}

func TestPreviewDoesNotWrite(t *testing.T) {
	s := newStore(t)
	id, _, err := StageUpload(s, "Reading_Tracker.xlsx", synFile(t, "Reading_Tracker.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := PreviewImport(s, id, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Counts.Create != 66 || len(s.All()) != 0 || len(s.Categories()) != 0 || len(p.TagSuggestions) == 0 {
		t.Errorf("counts %+v entries %d cats %d", p.Counts, len(s.All()), len(s.Categories()))
	}
	for _, sh := range p.Sheets {
		if sh.Name == "Action" {
			for _, m := range sh.Mapping {
				if m.Field == nil {
					t.Errorf("Action column %q unmapped", m.Header)
				}
			}
		}
	}
	for _, w := range p.Warnings {
		if w.Code == WarnStrayColumn {
			t.Error("unexpected stray column warning")
		}
	}
}

func normEntries(s *store.Store) []map[string]any {
	var out []map[string]any
	for _, e := range s.All() {
		b, _ := json.Marshal(e)
		m := map[string]any{}
		json.Unmarshal(b, &m)
		for _, k := range []string{"id", "createdAt", "updatedAt", "importRef"} {
			delete(m, k)
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["title"].(string) < out[j]["title"].(string) })
	return out
}

func TestRoundTripImportExportReimportIsLossless(t *testing.T) {
	for _, file := range []string{"Reading_Tracker.xlsx", "Watching_Tracker.xlsx"} {
		t.Run(file, func(t *testing.T) {
			a := newStore(t)
			importBytes(t, a, file, synFile(t, file), ImportOptions{})
			lib := a.All()[0].Library
			buf, err := ExportXLSX(a, lib)
			if err != nil {
				t.Fatal(err)
			}
			b := newStore(t)
			r := importBytes(t, b, "roundtrip.xlsx", buf, ImportOptions{})
			if r.Counts.Created != len(a.All()) {
				t.Fatalf("created %d, want %d", r.Counts.Created, len(a.All()))
			}
			if d := firstDiff(toGeneric(t, normEntries(a)), toGeneric(t, normEntries(b)), "entries"); d != "" {
				t.Error(d)
			}
			cats := func(s *store.Store) [][]any {
				var out [][]any
				for _, c := range s.Categories() {
					out = append(out, []any{c.Library, c.Name, c.Color, c.Order})
				}
				return out
			}
			if !reflect.DeepEqual(cats(a), cats(b)) {
				t.Errorf("categories %v vs %v", cats(a), cats(b))
			}
		})
	}
}

func TestXLSXExportHasBannerFrozenHeaderAndDropdowns(t *testing.T) {
	a := newStore(t)
	importBytes(t, a, "r.xlsx", synFile(t, "Reading_Tracker.xlsx"), ImportOptions{})
	buf, err := ExportXLSX(a, shared.LibraryReading)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cell := func(addr string) string { v, _ := f.GetCellValue("Romance", addr); return v }
	if cell("A1") != "Romance Tracker" || cell("A2") != "Title" || cell("C2") != "Score (1–10)" {
		t.Errorf("A1=%q A2=%q C2=%q", cell("A1"), cell("A2"), cell("C2"))
	}
	if p, err := f.GetPanes("Romance"); err != nil || !p.Freeze || p.YSplit != 2 {
		t.Errorf("panes %+v err %v", p, err)
	}
	if merged, _ := f.GetMergeCells("Romance"); len(merged) != 1 || merged[0].GetStartAxis() != "A1" {
		t.Errorf("banner merge %v", merged)
	}
	dvs, _ := f.GetDataValidations("Romance")
	var status, score bool
	for _, dv := range dvs {
		status = status || strings.Contains(dv.Formula1, "Plan to Read") && strings.Contains(dv.Formula1, "Re-Reading")
		score = score || strings.Contains(dv.Formula1, "10 - Masterpiece")
	}
	if !status || !score {
		t.Errorf("dropdowns: status %v score %v (%d rules)", status, score, len(dvs))
	}
	if idx, _ := f.GetSheetIndex("Add Category - Instructions"); idx >= 0 {
		t.Error("the instructions sheet must not be exported")
	}
	if props, _ := f.GetSheetProps("Romance"); props.TabColorRGB == nil || *props.TabColorRGB != "FF8B2252" {
		t.Errorf("tab color %v", props.TabColorRGB)
	}
	if v, _ := f.GetCellValue("Romance", "B3"); v == "" {
		t.Error("first entry has no status")
	}
}

func TestXLSXExportCorePropertiesAreStandard(t *testing.T) {
	s := newStore(t)
	s.CreateCategory(shared.LibraryReading, "A", "")
	buf, err := ExportXLSX(s, shared.LibraryReading)
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	for _, f := range zr.File {
		if f.Name != "docProps/core.xml" {
			continue
		}
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		x := string(b)
		if !strings.Contains(x, "<cp:coreProperties") || !strings.Contains(x, "<cp:lastModifiedBy>") || strings.Contains(x, "2006-09-16") {
			t.Errorf("core.xml: %s", x)
		}
		return
	}
	t.Error("no core.xml")
}

func TestXLSXExportWithNoCategoriesStillOpens(t *testing.T) {
	s := newStore(t)
	buf, err := ExportXLSX(s, shared.LibraryWatching)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := excelize.OpenReader(bytes.NewReader(buf)); err != nil {
		t.Fatal(err)
	}
}

// ---------- csv ----------

func TestCSVHandlesBOMQuotedNewlinesAndQuotes(t *testing.T) {
	sheets := ReadCSV(synFile(t, "sample-reading.csv"), "sample-reading.csv")
	sh := sheets[0]
	if sh.Library != shared.LibraryReading || sh.Headers[0] != "Title" || len(sh.Rows) != 2 {
		t.Fatalf("%+v", sh)
	}
	if sh.Rows[0].Cells[0] != `Café "Noir" Chronicle` || !strings.Contains(sh.Rows[0].Cells[6], "\n") {
		t.Errorf("cells %q", sh.Rows[0].Cells)
	}
}

func TestCSVSniffsSemicolonsAndFallsBackToWindows1252(t *testing.T) {
	semi := ReadCSV(synFile(t, "sample-semicolon.csv"), "x.csv")[0]
	if semi.Library != shared.LibraryWatching || semi.Rows[0].Cells[3] != "Fantasy" {
		t.Errorf("semicolon: %+v", semi)
	}
	cp := ReadCSV(synFile(t, "sample-cp1252.csv"), "x.csv")[0]
	if cp.Rows[0].Cells[0] != "École Maudite" {
		t.Errorf("cp1252: %q", cp.Rows[0].Cells[0])
	}
}

func TestCSVImportUsesTheFilenameAsCategoryAndExportsBackWithBOM(t *testing.T) {
	s := newStore(t)
	r := importBytes(t, s, "sample-reading.csv", synFile(t, "sample-reading.csv"), ImportOptions{})
	if r.Counts.Created != 2 || !reflect.DeepEqual(catNames(s, shared.LibraryReading), []string{"sample-reading"}) {
		t.Errorf("%+v %v", r.Counts, catNames(s, shared.LibraryReading))
	}
	out := ExportCSV(s, shared.LibraryReading, s.All(), true)
	if !bytes.HasPrefix(out, []byte{0xef, 0xbb, 0xbf}) {
		t.Error("missing BOM")
	}
	if back := ReadCSV(out, "back.csv")[0]; len(back.Rows) != 2 {
		t.Errorf("rows %d", len(back.Rows))
	}
}

func TestCSVWarnsAboutStrayAndUnknownColumns(t *testing.T) {
	s := newStore(t)
	id, _, _ := StageUpload(s, "x.csv", []byte("Title,Status,Currently on Chapter,,Mystery\nA,Reading,3,stray,huh\n"))
	p, err := PreviewImport(s, id, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var codes []WarningCode
	for _, w := range p.Warnings {
		codes = append(codes, w.Code)
	}
	if !slices.Contains(codes, WarnStrayColumn) || !slices.Contains(codes, WarnUnknownColumn) {
		t.Errorf("warnings %v", codes)
	}
}

func TestCSVNeedsALibraryWhenHeadersDoNotRevealOne(t *testing.T) {
	s := newStore(t)
	id, _, _ := StageUpload(s, "x.csv", []byte("Title,Status\nA,Reading\n"))
	p, err := PreviewImport(s, id, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Warnings[0].Code != WarnNoLibrary || p.Sheets[0].Library != nil {
		t.Errorf("%+v", p.Warnings)
	}
	r, err := CommitImport(s, id, ImportOptions{Library: shared.LibraryReading, Category: "Misc."})
	if err != nil || r.Counts.Created != 1 {
		t.Errorf("%+v %v", r, err)
	}
}

func TestCSVParsingMatchesPapaParseEdgeCases(t *testing.T) {
	cases := []struct {
		name, in string
		want     [][]string
	}{
		{"crlf rows", "a,b\r\nc,d\r\n", [][]string{{"a", "b"}, {"c", "d"}, {""}}},
		{"quoted crlf kept", "a,b\r\n\"x\r\ny\",z\r\n", [][]string{{"a", "b"}, {"x\r\ny", "z"}, {""}}},
		{"lf rows, quoted lf", "a,b\n\"x\ny\",z", [][]string{{"a", "b"}, {"x\ny", "z"}}},
		{"cr only rows", "a,b\rc,d", [][]string{{"a", "b"}, {"c", "d"}}},
		{"escaped quotes", "\"he said \"\"hi\"\"\",x\n", [][]string{{`he said "hi"`, "x"}, {""}}},
		{"quote inside unquoted field", "5\" pipe,x\n", [][]string{{`5" pipe`, "x"}, {""}}},
		{"empty fields and ragged rows", "a,,c\nd\n,e,f,g\n", [][]string{{"a", "", "c"}, {"d"}, {"", "e", "f", "g"}, {""}}},
	}
	for _, c := range cases {
		nl := guessNewline(c.in)
		got := parseCSV(c.in, ',', nl, 0)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if guessNewline("a,b\r\nc,d\r\n") != "\r\n" || guessNewline("a\nb") != "\n" || guessNewline("a\rb") != "\r" || guessNewline("plain") != "\n" {
		t.Error("newline guess")
	}
	if guessDelimiter("a;b;c\n1;2;3\n", "\n") != ';' || guessDelimiter("a\tb\n1\t2\n", "\n") != '\t' || guessDelimiter("a,b\n1,2\n", "\n") != ',' || guessDelimiter("single\nline\n", "\n") != ',' {
		t.Error("delimiter guess")
	}
}

func TestCSVExportQuotingMatchesPapaParse(t *testing.T) {
	cases := map[string]string{
		"plain": "plain", "a,b": `"a,b"`, `say "hi"`: `"say ""hi"""`, "two\nlines": "\"two\nlines\"",
		" lead": `" lead"`, "trail ": `"trail "`, "": "", "semi;colon": "semi;colon", "tab\tx": "tab\tx",
	}
	for in, want := range cases {
		if got := csvField(in); got != want {
			t.Errorf("csvField(%q) = %q, want %q", in, got, want)
		}
	}
	if csvField(nil) != "" || csvField(46.0) != "46" || csvField(1.5) != "1.5" {
		t.Error("non-string cells")
	}
	tpl := string(CSVTemplate(shared.LibraryReading))
	if !strings.Contains(tpl, "Watched on YouTube Up To Chapter") || !strings.HasSuffix(tpl, "Category\r\n") {
		t.Errorf("template %q", tpl)
	}
}

// ---------- columns, options, helpers ----------

func TestHeaderMatching(t *testing.T) {
	cases := map[string]Field{
		"Title": FTitle, " name ": FTitle, "Score (1–10)": FScore, "Rating": FScore, "Genres / Tags": FTags, "Tag": FTags,
		"Season": FSeason, "Watched Until Episode": FConsumed, "Watched on YouTube Up To Chapter": FConsumed,
		"Currently on Chapter": FCurrent, "Current ep": FCurrent, "YouTube Link": FYoutubeLink, "Link": FLink, "URL": FLink,
		"Reading Method": FSources, "Watch Platform": FSources, "General Notes": FGeneral, "Extra Notes 1": FExtra1,
		"extra notes 2": FExtra2, "Other Links": FOtherLinks, "Category": FCategory, "Mystery": "", "": "", "ＴＩＴＬＥ": FTitle,
	}
	for in, want := range cases {
		if got := MatchHeader(in); got != want {
			t.Errorf("MatchHeader(%q) = %q, want %q", in, got, want)
		}
	}
	if DetectLibrary([]string{"Title", "Currently on Chapter"}) != shared.LibraryReading || DetectLibrary([]string{"Title", "Episode"}) != shared.LibraryWatching || DetectLibrary([]string{"Title"}) != "" {
		t.Error("DetectLibrary")
	}
}

func TestImportOptionsValidation(t *testing.T) {
	ok, err := ParseImportOptions(map[string]any{
		"strategy": "overwrite", "library": "reading", "category": "X",
		"sheets":    map[string]any{"S": map[string]any{"include": false, "library": "watching", "mapping": []any{"title", nil, "status"}}},
		"statusMap": map[string]any{"Wishlist": "planned"},
	})
	if err != nil || ok.Strategy != StrategyOverwrite || ok.Sheets["S"].Mapping[1] != "" || ok.Sheets["S"].Include == nil || *ok.Sheets["S"].Include {
		t.Fatalf("%+v %v", ok, err)
	}
	if o, err := ParseImportOptions(nil); err != nil || o.Strategy != "" {
		t.Errorf("nil options: %v", err)
	}
	for name, bad := range map[string]any{
		"strategy": map[string]any{"strategy": "merge"}, "library": map[string]any{"library": "books"},
		"mapping field": map[string]any{"sheets": map[string]any{"S": map[string]any{"mapping": []any{"nope"}}}},
		"status map":    map[string]any{"statusMap": map[string]any{"x": "finished"}},
		"not an object": []any{1}, "category type": map[string]any{"category": 5},
		"include type": map[string]any{"sheets": map[string]any{"S": map[string]any{"include": "yes"}}},
	} {
		if _, err := ParseImportOptions(bad); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func TestSheetNamesAreSanitized(t *testing.T) {
	used := map[string]bool{}
	if got := safeSheetName("A/B:C?[x]", used); got != "A B C  x" {
		t.Errorf("got %q", got)
	}
	if got := safeSheetName("a b c  x", used); got == "A B C  x" || !strings.HasSuffix(got, " 2") {
		t.Errorf("duplicate (case-insensitive) should get a suffix: %q", got)
	}
	if got := safeSheetName("   ", used); got != "Sheet" {
		t.Errorf("blank name: %q", got)
	}
	if got := safeSheetName(strings.Repeat("x", 40), map[string]bool{}); len(got) != 31 {
		t.Errorf("long name: %d", len(got))
	}
}

func TestXLSXDatesBecomeISODatesAndNumbersStayNumbers(t *testing.T) {
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "Dates")
	f.SetSheetRow("Dates", "A1", &[]any{"Title", "Status", "Currently on Chapter", "General Notes"})
	f.SetSheetRow("Dates", "A2", &[]any{"Dated", "Reading", 46, "x"})
	dateStyle, _ := f.NewStyle(&excelize.Style{NumFmt: 14})
	f.SetCellValue("Dates", "D2", 46085.0) // 2026-03-04
	f.SetCellStyle("Dates", "D2", "D2", dateStyle)
	f.SetCellValue("Dates", "A3", "Error cell")
	f.SetCellFormula("Dates", "B3", "1/0")
	buf, _ := f.WriteToBuffer()
	sheets, err := ReadXLSX(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	row := sheets[0].Rows[0].Cells
	if row[2] != "46" || row[3] != "2026-03-04" {
		t.Errorf("cells %q", row)
	}
	if sheets[0].Rows[0].Row != 2 {
		t.Errorf("row number %d", sheets[0].Rows[0].Row)
	}
}

func TestReadSpreadsheetRejectsUnknownTypes(t *testing.T) {
	if _, err := ReadSpreadsheet([]byte("x"), "notes.pdf"); err == nil {
		t.Error("pdf must be rejected")
	}
	if _, err := ReadXLSX([]byte("not a zip")); err == nil {
		t.Error("garbage xlsx must fail")
	}
}

func TestStageUploadSanitizesFileNames(t *testing.T) {
	s := newStore(t)
	id, name, err := StageUpload(s, `..\..\evil name?.csv`, []byte("Title,Status\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(name, `/\?`) || !ValidImportID(id) {
		t.Errorf("name %q id %q", name, id)
	}
	if _, err := os.Stat(filepath.Join(s.ImportsDir(), id, "source.csv")); err != nil {
		t.Error(err)
	}
	if _, err := PreviewImport(s, "20200101-000000-abcdef", ImportOptions{}); err == nil {
		t.Error("unknown id must fail")
	}
}

// TestWriteInteropExports writes Go-made exports for scripts/check-go-exports.ts (Node) to read back.
// Run: INTEROP_OUT=/tmp/x go test -run TestWriteInteropExports ./internal/importexport
func TestWriteInteropExports(t *testing.T) {
	out := os.Getenv("INTEROP_OUT")
	if out == "" {
		t.Skip("set INTEROP_OUT to write the interop files")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	s := newStore(t)
	importBytes(t, s, "r.xlsx", synFile(t, "Reading_Tracker.xlsx"), ImportOptions{})
	importBytes(t, s, "w.xlsx", synFile(t, "Watching_Tracker.xlsx"), ImportOptions{})
	want := map[string]any{}
	for _, lib := range []shared.Library{shared.LibraryReading, shared.LibraryWatching} {
		x, err := ExportXLSX(s, lib)
		if err != nil {
			t.Fatal(err)
		}
		var es []shared.Entry
		for _, e := range s.All() {
			if e.Library == lib {
				es = append(es, e)
			}
		}
		os.WriteFile(filepath.Join(out, string(lib)+".xlsx"), x, 0o644)
		os.WriteFile(filepath.Join(out, string(lib)+".csv"), ExportCSV(s, lib, es, true), 0o644)
		var norm []map[string]any
		for _, e := range es {
			b, _ := json.Marshal(e)
			m := map[string]any{}
			json.Unmarshal(b, &m)
			for _, k := range []string{"id", "createdAt", "updatedAt", "importRef"} {
				delete(m, k)
			}
			norm = append(norm, m)
		}
		want[string(lib)] = norm
	}
	b, _ := json.Marshal(want)
	os.WriteFile(filepath.Join(out, "expected.json"), b, 0o644)
}

// Real workbooks are optional (gitignored private-fixtures/): skipped on CI and other machines.
// Failures only ever report counts, never titles or notes.
func TestPrivateFixturesLocalOnly(t *testing.T) {
	dir := filepath.Join(repoRoot, "private-fixtures")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Skip("no private-fixtures folder")
	}
	rd, wt := expected(t)
	s := newStore(t)
	n := 0
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".xlsx") || strings.HasPrefix(name, "~$") {
			continue
		}
		n++
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		sheets, err := ReadXLSX(data)
		if err != nil {
			t.Fatalf("workbook #%d: %v", n, err)
		}
		exp := wt
		for _, sh := range sheets {
			if sh.Library == shared.LibraryReading {
				exp = rd
				break
			}
		}
		r := importBytes(t, s, "private.xlsx", data, ImportOptions{})
		expectCounts(t, r, exp)
		if again := importBytes(t, s, "private.xlsx", data, ImportOptions{}); again.Counts.Created != 0 {
			t.Errorf("workbook #%d: re-import created %d", n, again.Counts.Created)
		}
	}
	if n == 0 {
		t.Skip("private-fixtures has no workbooks")
	}
	t.Logf("checked %d private workbook(s)", n)
}

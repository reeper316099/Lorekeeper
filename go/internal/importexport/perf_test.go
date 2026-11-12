package importexport

import (
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
	"github.com/xuri/excelize/v2"
)

// TestPerfImport mirrors scripts/perf-import.ts. Run:
//
//	PERF=1 go test -run TestPerfImport -v ./internal/importexport
func TestPerfImport(t *testing.T) {
	if os.Getenv("PERF") == "" {
		t.Skip("set PERF=1 to run")
	}
	timeIt := func(label string, runs int, fn func()) {
		s := make([]float64, runs)
		for i := range s {
			start := time.Now()
			fn()
			s[i] = float64(time.Since(start).Microseconds()) / 1000
		}
		sort.Float64s(s)
		fmt.Printf("%-46s %8.1f ms p50  (%d runs)\n", label, s[len(s)/2], runs)
	}
	fresh := func() *store.Store {
		st := store.New(t.TempDir(), store.Options{})
		if err := st.Open(); err != nil {
			t.Fatal(err)
		}
		return st
	}
	reading, watching := synFile(t, "Reading_Tracker.xlsx"), synFile(t, "Watching_Tracker.xlsx")
	fmt.Println("Go import/export timings")
	fmt.Println()
	timeIt("read reading workbook (66 rows, 6 sheets)", 7, func() { must(ReadXLSX(reading)) })
	timeIt("read watching workbook (48 rows)", 7, func() { must(ReadXLSX(watching)) })
	{
		st := fresh()
		sheets := must(ReadXLSX(reading))
		timeIt("plan reading import", 7, func() { must(BuildPlan(st, "r.xlsx", sheets, ImportOptions{})) })
		st.Close()
	}
	timeIt("commit reading import (stage+plan+write)", 5, func() {
		st := fresh()
		importBytes(t, st, "r.xlsx", reading, ImportOptions{})
		st.Close()
	})
	{
		st := fresh()
		importBytes(t, st, "r.xlsx", reading, ImportOptions{})
		timeIt("export reading xlsx (5 sheets)", 5, func() { must(ExportXLSX(st, shared.LibraryReading)) })
		timeIt("export reading csv", 7, func() { ExportCSV(st, shared.LibraryReading, st.All(), true) })
		st.Close()
	}
	words := []string{"Silent", "Crimson", "Hollow", "Gilded", "Fallen", "Wandering", "Eternal", "Ashen"}
	nouns := []string{"Crown", "Garden", "Oath", "Tower", "Contract", "Blade", "Archive", "Throne"}
	for _, n := range []int{2000, 10000} {
		f := excelize.NewFile()
		f.SetSheetName("Sheet1", "Big")
		f.SetSheetRow("Big", "A1", &[]any{"Title", "Status", "Score (1–10)", "Genres / Tags", "Currently on Chapter", "General Notes"})
		for i := 0; i < n; i++ {
			cell, _ := excelize.CoordinatesToCellName(1, i+2)
			f.SetSheetRow("Big", cell, &[]any{
				fmt.Sprintf("%s %s %d", words[i%8], nouns[(i>>3)%8], i), []string{"Reading", "Completed", "On Hold"}[i%3],
				fmt.Sprintf("%d - Good", i%10+1), fmt.Sprintf("Tag %d, Tag %d", i%40, (i*7)%40), i % 300, fmt.Sprintf("Notes %d", i),
			})
		}
		buf, _ := f.WriteToBuffer()
		data := buf.Bytes()
		timeIt(fmt.Sprintf("read %d-row workbook", n), 3, func() { must(ReadXLSX(data)) })
		timeIt(fmt.Sprintf("import %d-row workbook (stage+plan+commit)", n), 3, func() {
			st := fresh()
			importBytes(t, st, "big.xlsx", data, ImportOptions{})
			st.Close()
		})
	}
}

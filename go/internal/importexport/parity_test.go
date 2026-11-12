package importexport

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

type parityFile struct {
	FileName string `json:"fileName"`
	Path     string `json:"path"`
	Text     string `json:"text"`
}

type parityStep struct {
	Do      string         `json:"do"`
	File    parityFile     `json:"file"`
	Options map[string]any `json:"options"`
	Index   int            `json:"index"`
	Patch   map[string]any `json:"patch"`
}

type parityResult struct {
	Preview    map[string]any    `json:"preview"`
	Report     map[string]any    `json:"report"`
	Entries    []any             `json:"entries"`
	Categories []any             `json:"categories"`
	CSV        map[string]string `json:"csv"`
}

type parityScenario struct {
	Name    string         `json:"name"`
	Steps   []parityStep   `json:"steps"`
	Results []parityResult `json:"results"`
}

var skipKeys = map[string]bool{"id": true, "createdAt": true, "updatedAt": true, "importedAt": true}

func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			if !skipKeys[k] {
				out[k] = normalize(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalize(val)
		}
		return out
	}
	return v
}

// node-import.json holds what the Node importer produced for the same inputs
// (scripts/gen-go-import-fixtures.ts); the Go importer must give identical previews, reports,
// entries, categories and CSV exports.
func TestImporterMatchesNode(t *testing.T) {
	raw, err := os.ReadFile("testdata/node-import.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Scenarios []parityScenario `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Scenarios) < 10 {
		t.Fatalf("only %d scenarios", len(doc.Scenarios))
	}
	for _, sc := range doc.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			s := newStore(t)
			for i, step := range sc.Steps {
				want := sc.Results[i]
				if step.Do == "edit" {
					if _, err := s.UpdateEntry(s.All()[step.Index].ID, step.Patch); err != nil {
						t.Fatal(err)
					}
					continue
				}
				var data []byte
				if step.File.Path != "" {
					data = synFile(t, filepath.Base(step.File.Path))
				} else {
					data = []byte(step.File.Text)
				}
				opts, err := ParseImportOptions(step.Options)
				if err != nil {
					t.Fatal(err)
				}
				id, _, err := StageUpload(s, step.File.FileName, data)
				if err != nil {
					t.Fatal(err)
				}
				prev, err := PreviewImport(s, id, opts)
				if err != nil {
					t.Fatal(err)
				}
				report, err := CommitImport(s, id, opts)
				if err != nil {
					t.Fatal(err)
				}
				gotPrev := toGeneric(t, prev).(map[string]any)
				delete(gotPrev, "importId")
				if d := firstDiff(want.Preview, normalize(gotPrev), "preview"); d != "" {
					t.Errorf("step %d: %s", i, d)
				}
				gotReport := map[string]any{
					"counts": report.Counts, "summary": report.Summary, "warnings": report.Warnings,
					"tagSuggestions": report.TagSuggestions, "strategy": report.Strategy,
				}
				if d := firstDiff(want.Report, normalize(toGeneric(t, gotReport)), "report"); d != "" {
					t.Errorf("step %d: %s", i, d)
				}
				if i != len(sc.Steps)-1 {
					continue
				}
				if d := firstDiff(any(want.Entries), normalize(toGeneric(t, s.All())), "entries"); d != "" {
					t.Errorf("entries: %s", d)
				}
				cats := s.Categories()
				if d := firstDiff(any(want.Categories), toGeneric(t, cats), "categories"); d != "" {
					t.Errorf("categories: %s", d)
				}
				libs := map[shared.Library]bool{}
				for _, e := range s.All() {
					libs[e.Library] = true
				}
				for lib := range libs {
					var es []shared.Entry
					for _, e := range s.All() {
						if e.Library == lib {
							es = append(es, e)
						}
					}
					got := ExportCSV(s, lib, es, true)
					wantCSV, _ := base64.StdEncoding.DecodeString(want.CSV[string(lib)])
					if string(got) != string(wantCSV) {
						t.Errorf("csv export of %s differs:\n--- node\n%q\n--- go\n%q", lib, firstN(string(wantCSV)), firstN(string(got)))
					}
				}
			}
		})
	}
}

func firstN(s string) string {
	if len(s) > 400 {
		return s[:400] + "..."
	}
	return s
}

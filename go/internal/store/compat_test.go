package store

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// testdata/nodedata is a data folder written by the Node store (scripts/gen-go-store-fixtures.ts).
// These tests prove the Go store reads and writes exactly the same on-disk format.

const nodeData = "testdata/nodedata"

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpensNodeWrittenDataFolder(t *testing.T) {
	dir := t.TempDir()
	copyDir(t, nodeData, dir)
	s := New(dir, Options{})
	var errs []Event
	s.On(func(e Event) {
		if e.Name == EventFileError || e.Name == EventFileRestored {
			errs = append(errs, e)
		}
	})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("Node's files were reported as invalid: %+v", errs)
	}
	if len(s.Categories()) != 4 || len(s.All()) != 4 {
		t.Fatalf("categories=%d entries=%d", len(s.Categories()), len(s.All()))
	}
	var moon shared.Entry
	for _, e := range s.All() {
		if e.Title == "Moonlit Orchard" {
			moon = e
		}
	}
	if moon.ID == "" || *moon.Score != 8 || !moon.Favorite || len(moon.Links) != 2 {
		t.Fatalf("entry not loaded faithfully: %+v", moon)
	}
	if *moon.Links[1].Channel != "PanelPilot" || *moon.Links[1].Date != "3/3/26" || moon.Links[0].Slot != shared.SlotPrimary {
		t.Fatalf("links: %+v", moon.Links)
	}
	if moon.Current.Season == nil || *moon.Current.Season != 2 || *moon.Current.Number != 3 {
		t.Fatalf("progress: %+v", moon.Current)
	}
	if moon.External["averageScore"] != 79.5 || moon.External["nested"] == nil {
		t.Fatalf("passthrough external lost: %+v", moon.External)
	}
	if s.Settings().Theme != "light" || s.Settings().LabelOverrides["extra2"] != "Extras" || len(s.Settings().SavedViews) != 1 {
		t.Fatalf("settings: %+v", s.Settings())
	}
	for _, tg := range s.Tags() {
		if tg.Name == "Cozy Town" && len(tg.Aliases) != 1 {
			t.Fatalf("merged tag aliases: %+v", tg)
		}
	}
	acts := must(s.ReadActivity(""))
	if len(acts) < 5 {
		t.Fatalf("activity: %d records", len(acts))
	}
}

// Opening a Node folder must not rewrite any data file (no spurious churn, no format drift).
func TestOpenDoesNotRewriteNodeFiles(t *testing.T) {
	dir := t.TempDir()
	copyDir(t, nodeData, dir)
	before := snapshotFiles(t, dir)
	if err := New(dir, Options{}).Open(); err != nil {
		t.Fatal(err)
	}
	after := snapshotFiles(t, dir)
	for name, b := range before {
		if name == "meta.json" || strings.HasPrefix(name, "backups/") {
			continue // lastOpenedAt changes, and a launch backup is expected
		}
		if !bytes.Equal(b, after[name]) {
			t.Errorf("%s changed on open", name)
		}
	}
}

func snapshotFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, p)
			b, _ := os.ReadFile(p)
			out[filepath.ToSlash(rel)] = b
		}
		return nil
	})
	return out
}

// Parsing each Node file with the Go schemas and serializing it again must give the identical bytes.
func TestReserializingNodeFilesIsByteIdentical(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(nodeData, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(nodeData, p)
		rel = filepath.ToSlash(rel)
		orig, _ := os.ReadFile(p)
		var out []byte
		var perr error
		switch {
		case rel == "meta.json":
			var v shared.Meta
			if perr = json.Unmarshal(orig, &v); perr == nil {
				out, perr = StableMarshal(v)
			}
		case rel == "config/settings.json":
			var v shared.Settings
			if perr = json.Unmarshal(orig, &v); perr == nil {
				out, perr = StableMarshal(v)
			}
		case rel == "config/scales.json":
			var v shared.Scales
			if perr = json.Unmarshal(orig, &v); perr == nil {
				out, perr = StableMarshal(v)
			}
		case rel == "config/tags.json":
			var v []shared.Tag
			if v, perr = shared.ParseTagsFile(orig); perr == nil {
				out, perr = StableMarshal(v)
			}
		case rel == "config/categories.json":
			var v []shared.Category
			if v, perr = shared.ParseCategoriesFile(orig); perr == nil {
				out, perr = StableMarshal(v)
			}
		case strings.HasPrefix(rel, "data/"):
			var v []shared.Entry
			if v, perr = shared.ParseEntryFile(orig); perr == nil {
				out, perr = StableMarshal(v)
			}
		case strings.HasPrefix(rel, "activity/"):
			var lines []string
			for _, line := range strings.Split(strings.TrimRight(string(orig), "\n"), "\n") {
				var r shared.ActivityRecord
				if perr = json.Unmarshal([]byte(line), &r); perr != nil {
					break
				}
				var buf bytes.Buffer
				enc := json.NewEncoder(&buf)
				enc.SetEscapeHTML(false)
				enc.Encode(r)
				lines = append(lines, strings.TrimRight(buf.String(), "\n"))
			}
			out = []byte(strings.Join(lines, "\n") + "\n")
		default:
			t.Errorf("unexpected file %s", rel)
			return nil
		}
		if perr != nil {
			t.Errorf("%s: %v", rel, perr)
			return nil
		}
		checked++
		if !bytes.Equal(out, orig) {
			t.Errorf("%s differs after a Go round trip:\n--- node\n%s\n--- go\n%s", rel, firstDiff(orig, out), firstDiff(out, orig))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 10 {
		t.Fatalf("only %d files checked", checked)
	}
}

func firstDiff(a, b []byte) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := max(0, i-60)
	return string(a[lo:min(len(a), i+60)])
}

// TestWriteInteropFolder writes a folder with the Go store for scripts/check-go-data-dir.ts (Node)
// to open. Run: INTEROP_OUT=/some/dir go test -run TestWriteInteropFolder ./internal/store
func TestWriteInteropFolder(t *testing.T) {
	out := os.Getenv("INTEROP_OUT")
	if out == "" {
		t.Skip("set INTEROP_OUT to write the interop folder")
	}
	os.RemoveAll(out)
	s := New(out, Options{AppVersion: "go-port"})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rom := must(s.CreateCategory(shared.LibraryReading, "Romance", ""))
	ani := must(s.CreateCategory(shared.LibraryWatching, "Anime", ""))
	a := must(s.CreateEntry(EntryInput{
		"title": "Glass Meridian", "categoryId": rom.ID, "tags": []string{"Slow Burn", "Cozy Town"}, "score": 9,
		"links":    []any{map[string]any{"kind": "youtube", "url": "https://youtu.be/x", "label": nil, "raw": "youtu.be/x", "channel": nil, "date": nil, "slot": "primary"}},
		"external": map[string]any{"anilistId": 7, "extraKey": map[string]any{"k": []any{1, 2}}},
	}))
	must(s.SetProgress(a.ID, ProgressOp{Set: "S2 E3", HasSet: true}))
	must(s.CreateEntry(EntryInput{"title": "Tom & Jerry <Reborn>", "categoryId": ani.ID, "notes": map[string]any{"general": "Line sep"}}))
	b := must(s.CreateEntry(EntryInput{"title": "Moved Along", "categoryId": rom.ID, "tags": []string{"Slow Burn"}}))
	harem := must(s.CreateCategory(shared.LibraryReading, "Harem", ""))
	must(s.MoveEntry(b.ID, harem.ID))
	must(s.MergeTags([]string{"Slow Burn"}, "Cozy Town"))
	name := "Love Stories"
	must(s.UpdateCategory(rom.ID, CategoryPatch{Name: &name}))
	st := s.Settings()
	st.Theme = "light"
	must(s.SaveSettings(st))
	must(s.Backup("interop"))
}

// The single-pass StableMarshal must equal the generic key-sorting reference for every document type.
func TestStableMarshalMatchesGenericReference(t *testing.T) {
	filepath.WalkDir(nodeData, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		b, _ := os.ReadFile(p)
		var v any
		json.Unmarshal(b, &v)
		fast, _ := StableMarshal(v)
		slow, _ := StableMarshalGeneric(v)
		if !bytes.Equal(fast, slow) || !bytes.Equal(fast, b) {
			t.Errorf("%s: fast path differs from reference", p)
		}
		return nil
	})
}

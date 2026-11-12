package importexport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// repoRoot is the repository root (this package is go/internal/importexport).
const repoRoot = "../../.."

var synthetic = filepath.Join(repoRoot, "fixtures", "synthetic")

// synFile reads a synthetic fixture. The .xlsx files are generated (never committed): run
// `npm run fixtures:generate`. In CI (REQUIRE_FIXTURES=1) a missing file is a failure, not a skip.
func synFile(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(synthetic, name))
	if err != nil {
		if os.Getenv("REQUIRE_FIXTURES") != "" {
			t.Fatalf("missing fixture %s: %v", name, err)
		}
		t.Skipf("fixture %s is missing; run `npm run fixtures:generate` first", name)
	}
	return b
}

func newStore(t testing.TB) *store.Store {
	t.Helper()
	s := store.New(t.TempDir(), store.Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func importBytes(t testing.TB, s *store.Store, name string, data []byte, opts ImportOptions) *CommitResult {
	t.Helper()
	id, _, err := StageUpload(s, name, data)
	if err != nil {
		t.Fatal(err)
	}
	r, err := CommitImport(s, id, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// toGeneric turns any value into what JSON.parse(JSON.stringify(v)) would give.
func toGeneric(t testing.TB, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// firstDiff describes where two generic JSON values differ ("" when equal).
func firstDiff(a, b any, path string) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: %T vs %T", path, a, b)
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			xv, xok := x[k]
			yv, yok := y[k]
			if xok != yok {
				return fmt.Sprintf("%s.%s: present on one side only (%v vs %v)", path, k, xv, yv)
			}
			if d := firstDiff(xv, yv, path+"."+k); d != "" {
				return d
			}
		}
		return ""
	case []any:
		y, ok := b.([]any)
		if !ok {
			return fmt.Sprintf("%s: %T vs %T", path, a, b)
		}
		if len(x) != len(y) {
			return fmt.Sprintf("%s: length %d vs %d", path, len(x), len(y))
		}
		for i := range x {
			if d := firstDiff(x[i], y[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
		return ""
	}
	if !reflect.DeepEqual(a, b) {
		return fmt.Sprintf("%s: %v vs %v", path, a, b)
	}
	return ""
}

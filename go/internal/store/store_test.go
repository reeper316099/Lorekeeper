package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// Ported from packages/server/test/storage.test.ts, plus extra coverage for the Go version.

func mk(t *testing.T, opts ...Options) (*Store, string) {
	t.Helper()
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	dir := t.TempDir()
	s := New(dir, o)
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func readJSON(t *testing.T, file string) any {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func tmpFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			out = append(out, e.Name())
		}
	}
	return out
}

// ---------- atomic IO ----------

func TestStableMarshal(t *testing.T) {
	got, err := StableMarshal(map[string]any{"b": 1, "a": map[string]any{"d": 1}})
	if err != nil || string(got) != "{\n  \"a\": {\n    \"d\": 1\n  },\n  \"b\": 1\n}\n" {
		t.Fatalf("got %q err %v", got, err)
	}
	// JSON.stringify does not escape < > & and writes U+2028 raw; "\\u2028" (a literal backslash) stays escaped.
	got, _ = StableMarshal(map[string]string{"x": "a<b>&c\u2028d\\u2028"})
	if string(got) != "{\n  \"x\": \"a<b>&c\u2028d\\\\u2028\"\n}\n" {
		t.Fatalf("escaping: %q", got)
	}
	if got, _ := StableMarshal([]string{}); string(got) != "[]\n" {
		t.Fatalf("empty array: %q", got)
	}
}

func TestAtomicWriteLeavesNoTmpFiles(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.json")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := AtomicWrite(f, []byte(`{"i":1}`)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if v := readJSON(t, f).(map[string]any); v["i"] == nil {
		t.Fatal("missing i")
	}
	if n := tmpFiles(t, dir); len(n) != 0 {
		t.Fatalf("tmp files left: %v", n)
	}
}

func TestAtomicWriteRetriesLockedRename(t *testing.T) {
	dir := t.TempDir()
	realRename, realDelay := renameFn, renameRetryDelay
	defer func() { renameFn, renameRetryDelay = realRename, realDelay }()
	renameRetryDelay = time.Millisecond

	failures := 2
	renameFn = func(a, b string) error {
		if failures > 0 {
			failures--
			if failures == 1 {
				return &os.LinkError{Op: "rename", Old: a, New: b, Err: syscall.EPERM}
			}
			return &os.LinkError{Op: "rename", Old: a, New: b, Err: syscall.EBUSY}
		}
		return realRename(a, b)
	}
	if err := AtomicWrite(filepath.Join(dir, "y.json"), []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if v := readJSON(t, filepath.Join(dir, "y.json")).(map[string]any); v["ok"] != true {
		t.Fatal("content lost")
	}

	failures = 99
	renameFn = func(a, b string) error {
		return &os.LinkError{Op: "rename", Old: a, New: b, Err: syscall.EBUSY}
	}
	if err := AtomicWrite(filepath.Join(dir, "z.json"), []byte(`{}`)); err == nil || !errors.Is(err, syscall.EBUSY) {
		t.Fatalf("expected the lock error after retries, got %v", err)
	}
	if n := tmpFiles(t, dir); len(n) != 0 {
		t.Fatalf("tmp left after failure: %v", n)
	}

	calls := 0
	renameFn = func(a, b string) error {
		calls++
		return &os.LinkError{Op: "rename", Old: a, New: b, Err: syscall.ENOENT}
	}
	if err := AtomicWrite(filepath.Join(dir, "w.json"), []byte(`{}`)); err == nil || calls != 1 {
		t.Fatalf("other errors must not be retried: err=%v calls=%d", err, calls)
	}
}

func TestKeyedMutexSerializesPerKeyInOrder(t *testing.T) {
	var m KeyedMutex
	var mu sync.Mutex
	var order []int
	add := func(n int) { mu.Lock(); order = append(order, n); mu.Unlock() }
	tickets := []*Ticket{m.Enter("k"), m.Enter("k"), m.Enter("k"), m.Enter("k")}
	other := m.Enter("other") // a different key is not blocked
	var wg sync.WaitGroup
	run := func(i int, fn func() error) {
		wg.Add(1)
		go func() { defer wg.Done(); _ = tickets[i].Do(fn) }()
	}
	// started in reverse so only the ticket order can explain the result
	run(3, func() error { add(4); return nil })
	run(2, func() error { add(3); return errors.New("x") })
	run(1, func() error { add(2); return nil })
	run(0, func() error { time.Sleep(20 * time.Millisecond); add(1); return nil })
	if err := other.Do(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if !reflect.DeepEqual(order, []int{1, 2, 3, 4}) {
		t.Fatalf("order %v", order)
	}
}

// ---------- Store ----------

func TestStoreCreatesDocumentedLayout(t *testing.T) {
	s, dir := mk(t)
	for _, f := range []string{"meta.json", "config/settings.json", "config/categories.json", "config/tags.json", "config/scales.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if s.Meta().SchemaVersion != 1 {
		t.Fatalf("schema %d", s.Meta().SchemaVersion)
	}
}

func TestConcurrentWritesAllLandAndPersist(t *testing.T) {
	s, dir := mk(t)
	cat := must(s.CreateCategory(shared.LibraryReading, "Romance", ""))
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.CreateEntry(EntryInput{"title": "T" + string(rune('A'+i)), "categoryId": cat.ID}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := len(readJSON(t, filepath.Join(dir, "data/reading/romance.json")).([]any)); n != 25 {
		t.Fatalf("on disk: %d", n)
	}
	re := New(dir, Options{})
	if err := re.Open(); err != nil {
		t.Fatal(err)
	}
	if len(re.All()) != 25 {
		t.Fatalf("reopened: %d", len(re.All()))
	}
}

func TestQuarantinesCorruptFileAndRestoresFromBackup(t *testing.T) {
	s, dir := mk(t)
	cat := must(s.CreateCategory(shared.LibraryReading, "Action", ""))
	must(s.CreateEntry(EntryInput{"title": "Keep me", "categoryId": cat.ID}))
	must(s.Backup("test"))
	if err := os.WriteFile(filepath.Join(dir, "data/reading/action.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	again := New(dir, Options{})
	var errs []FileError
	again.On(func(e Event) {
		if e.Name == EventFileError {
			errs = append(errs, e.Data.(FileError))
		}
	})
	if err := again.Open(); err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 || errs[0].File != "data/reading/action.json" || !strings.Contains(errs[0].Message, "line 1") {
		t.Fatalf("errors: %+v", errs)
	}
	if all := again.All(); len(all) != 1 || all[0].Title != "Keep me" {
		t.Fatalf("restored: %+v", all)
	}
	if ents, _ := os.ReadDir(filepath.Join(dir, "backups/corrupt")); len(ents) != 1 {
		t.Fatalf("quarantine files: %d", len(ents))
	}
}

func TestCorruptFileWithNoBackupFallsBackToEmpty(t *testing.T) {
	s, dir := mk(t)
	must(s.CreateCategory(shared.LibraryReading, "Action", ""))
	if err := os.WriteFile(filepath.Join(dir, "data/reading/action.json"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	again := New(dir, Options{})
	if err := again.Open(); err != nil {
		t.Fatal(err)
	}
	if len(again.All()) != 0 {
		t.Fatal("expected empty")
	}
}

func TestInvalidSchemaIsQuarantinedToo(t *testing.T) {
	s, dir := mk(t)
	cat := must(s.CreateCategory(shared.LibraryReading, "Action", ""))
	must(s.CreateEntry(EntryInput{"title": "Fine", "categoryId": cat.ID}))
	bad := `[{"id":"x","library":"reading","categoryId":"reading-action","title":"T","score":99}]`
	if err := os.WriteFile(filepath.Join(dir, "data/reading/action.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	again := New(dir, Options{})
	var msg string
	again.On(func(e Event) {
		if e.Name == EventFileError {
			msg = e.Data.(FileError).Message
		}
	})
	if err := again.Open(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "score") || len(again.All()) != 0 {
		t.Fatalf("msg=%q entries=%d", msg, len(again.All()))
	}
}

func TestMovesEntriesBetweenCategories(t *testing.T) {
	s, dir := mk(t)
	a := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	b := must(s.CreateCategory(shared.LibraryReading, "B", ""))
	e := must(s.CreateEntry(EntryInput{"title": "X", "categoryId": a.ID}))
	must(s.MoveEntry(e.ID, b.ID))
	if len(s.EntriesOf(a.ID)) != 0 {
		t.Fatal("still in A")
	}
	if n := len(readJSON(t, filepath.Join(dir, "data/reading/b.json")).([]any)); n != 1 {
		t.Fatalf("B on disk: %d", n)
	}
	w := must(s.CreateCategory(shared.LibraryWatching, "W", ""))
	_, err := s.MoveEntry(e.ID, w.ID)
	var he *HTTPError
	if !errors.As(err, &he) || he.Code != "LIBRARY_MISMATCH" || he.Status != 422 {
		t.Fatalf("got %v", err)
	}
}

func TestMoveRollsBackWhenSecondWriteFails(t *testing.T) {
	s, _ := mk(t)
	a := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	b := must(s.CreateCategory(shared.LibraryReading, "B", ""))
	e := must(s.CreateEntry(EntryInput{"title": "X", "categoryId": a.ID}))
	real := s.writeFile
	n := 0
	s.writeFile = func(f string, text []byte) error {
		n++
		if n == 2 {
			return errors.New("disk full")
		}
		return real(f, text)
	}
	if _, err := s.MoveEntry(e.ID, b.ID); err == nil || err.Error() != "disk full" {
		t.Fatalf("got %v", err)
	}
	s.writeFile = real
	if got := s.EntriesOf(a.ID); len(got) != 1 || got[0].ID != e.ID {
		t.Fatalf("A: %+v", got)
	}
	if len(s.EntriesOf(b.ID)) != 0 {
		t.Fatal("B should be empty")
	}
}

func TestProgressDeltaWritesActivityAndStartsPlannedEntries(t *testing.T) {
	s, _ := mk(t)
	c := must(s.CreateCategory(shared.LibraryWatching, "Anime", ""))
	e := must(s.CreateEntry(EntryInput{"title": "Show", "categoryId": c.ID}))
	if e.MediaType != "anime" {
		t.Fatalf("media type %q", e.MediaType)
	}
	out := must(s.SetProgress(e.ID, ProgressOp{Delta: 1}))
	if out.Current.Number == nil || *out.Current.Number != 1 || out.Status == nil || *out.Status != shared.StatusInProgress {
		t.Fatalf("got %+v", out)
	}
	kinds := map[string]bool{}
	for _, a := range must(s.ReadActivity("")) {
		kinds[a.Kind] = true
	}
	if !kinds["created"] || !kinds["progress"] || !kinds["status"] {
		t.Fatalf("activity kinds %v", kinds)
	}
}

func TestProgressSetAndNonNumericDelta(t *testing.T) {
	s, _ := mk(t)
	c := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	e := must(s.CreateEntry(EntryInput{"title": "X", "categoryId": c.ID}))
	out := must(s.SetProgress(e.ID, ProgressOp{Set: "End of S1", HasSet: true}))
	if *out.Current.Kind != shared.ProgressEnd {
		t.Fatalf("kind %v", *out.Current.Kind)
	}
	_, err := s.SetProgress(e.ID, ProgressOp{Delta: 1})
	var he *HTTPError
	if !errors.As(err, &he) || he.Code != "UNPARSEABLE_PROGRESS" {
		t.Fatalf("got %v", err)
	}
	out = must(s.SetProgress(e.ID, ProgressOp{Set: 5.0, HasSet: true}))
	out = must(s.SetProgress(e.ID, ProgressOp{Delta: -10}))
	if *out.Current.Number != 0 || *out.Current.Raw != "0" {
		t.Fatalf("progress must not go below 0: %+v", out.Current)
	}
}

func TestMergeTagsRemembersAliasesAndCanonicalizes(t *testing.T) {
	s, _ := mk(t)
	c := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	e1 := must(s.CreateEntry(EntryInput{"title": "one", "categoryId": c.ID, "tags": []string{"Sorcery", "Overlord Lead"}}))
	e2 := must(s.CreateEntry(EntryInput{"title": "two", "categoryId": c.ID, "tags": []string{"Sorcrey", "Sorcery"}}))
	must(s.MergeTags([]string{"Sorcrey"}, "Sorcery"))
	if got := must(s.GetEntryOK(e2.ID)).Tags; !reflect.DeepEqual(got, []string{"Sorcery"}) {
		t.Fatalf("e2 tags %v", got)
	}
	if got := must(s.GetEntryOK(e1.ID)).Tags; !reflect.DeepEqual(got, []string{"Sorcery", "Overlord Lead"}) {
		t.Fatalf("e1 tags %v", got)
	}
	var aliases []string
	for _, tg := range s.Tags() {
		if tg.Name == "Sorcery" {
			aliases = tg.Aliases
		}
		if tg.Name == "Sorcrey" {
			t.Fatal("merged tag still listed")
		}
	}
	if !reflect.DeepEqual(aliases, []string{"Sorcrey"}) {
		t.Fatalf("aliases %v", aliases)
	}
	if got := s.CanonicalTags([]string{"sorcrey", "new"}); !reflect.DeepEqual(got, []string{"Sorcery", "new"}) {
		t.Fatalf("canonical %v", got)
	}
}

// GetEntryOK is a test helper returning an error instead of a bool.
func (s *Store) GetEntryOK(id string) (shared.Entry, error) {
	if e, ok := s.GetEntry(id); ok {
		return e, nil
	}
	return shared.Entry{}, errors.New("not found: " + id)
}

func waitEvent(t *testing.T, s *Store, name string, trigger func()) Event {
	t.Helper()
	ch := make(chan Event, 4)
	off := s.On(func(e Event) {
		if e.Name == name {
			select {
			case ch <- e:
			default:
			}
		}
	})
	defer off()
	trigger()
	select {
	case e := <-ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
		return Event{}
	}
}

func TestReloadsHandEditsAndKeepsMemoryOnInvalidEdits(t *testing.T) {
	s, dir := mk(t, Options{Watch: true})
	c := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	e := must(s.CreateEntry(EntryInput{"title": "Orig", "categoryId": c.ID}))
	file := filepath.Join(dir, "data/reading/a.json")
	time.Sleep(300 * time.Millisecond)
	edited := readJSON(t, file).([]any)
	edited[0].(map[string]any)["title"] = "Hand edited"
	b, _ := json.Marshal(edited)
	waitEvent(t, s, EventExternalChange, func() { os.WriteFile(file, b, 0o644) })
	if got := must(s.GetEntryOK(e.ID)).Title; got != "Hand edited" {
		t.Fatalf("title %q", got)
	}
	ev := waitEvent(t, s, EventFileError, func() { os.WriteFile(file, []byte("{ broken"), 0o644) })
	if fe := ev.Data.(FileError); !fe.Kept {
		t.Fatalf("expected kept: %+v", fe)
	}
	if got := must(s.GetEntryOK(e.ID)).Title; got != "Hand edited" {
		t.Fatalf("memory lost: %q", got)
	}
}

func TestOwnWritesDoNotTriggerExternalChange(t *testing.T) {
	s, _ := mk(t, Options{Watch: true})
	c := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	fired := make(chan struct{}, 1)
	s.On(func(e Event) {
		if e.Name == EventExternalChange {
			fired <- struct{}{}
		}
	})
	must(s.CreateEntry(EntryInput{"title": "X", "categoryId": c.ID}))
	select {
	case <-fired:
		t.Fatal("own write was reported as an external change")
	case <-time.After(700 * time.Millisecond):
	}
}

// ---------- backups & migrations ----------

func TestBackupsCreateListPruneRestore(t *testing.T) {
	s, dir := mk(t)
	c := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	e := must(s.CreateEntry(EntryInput{"title": "Before", "categoryId": c.ID}))
	b := must(s.Backup("manual"))
	must(s.UpdateEntry(e.ID, map[string]any{"title": "After"}))
	if err := s.RestoreBackup(b.Name); err != nil {
		t.Fatal(err)
	}
	if got := must(s.GetEntryOK(e.ID)).Title; got != "Before" {
		t.Fatalf("title %q", got)
	}
	for i := 0; i < 4; i++ {
		must(CreateBackup(dir, "x", 3))
	}
	if n := len(must(ListBackups(dir))); n != 3 {
		t.Fatalf("kept %d", n)
	}
	list := must(ListBackups(dir))
	if backupKey(list[0].Name) <= backupKey(list[1].Name) {
		t.Fatalf("not newest first: %v", list)
	}
}

func TestRejectsUnsafeZipPaths(t *testing.T) {
	for _, p := range []string{"../x", "/etc/passwd", "config/../../x", "C:/x", "misc/file.json", `data\x`, "data//x", ""} {
		if IsSafeZipPath(p) {
			t.Errorf("%q must be unsafe", p)
		}
	}
	if !IsSafeZipPath("data/reading/a.json") {
		t.Error("data/reading/a.json must be safe")
	}
}

func TestRunsOrderedMigrationsAfterBackupAndRefusesNewerData(t *testing.T) {
	_, dir := mk(t)
	var ran []int
	backed := 0
	v, err := Migrate(dir, 1, MigrateOptions{
		Target: 3,
		Backup: func(string) error { backed++; return nil },
		Migrations: []Migration{
			{From: 2, To: 3, Run: func(string) error { ran = append(ran, 2); return nil }},
			{From: 1, To: 2, Run: func(string) error { ran = append(ran, 1); return nil }},
		},
	})
	if err != nil || v != 3 || !reflect.DeepEqual(ran, []int{1, 2}) || backed != 1 {
		t.Fatalf("v=%d ran=%v backed=%d err=%v", v, ran, backed, err)
	}
	if got := readJSON(t, filepath.Join(dir, "meta.json")).(map[string]any)["schemaVersion"]; got != float64(3) {
		t.Fatalf("meta schemaVersion %v", got)
	}
	if _, err := Migrate(dir, 9, MigrateOptions{Backup: func(string) error { return nil }}); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("got %v", err)
	}
}

func TestOpenRefusesDataFromNewerVersion(t *testing.T) {
	_, dir := mk(t)
	meta := readJSON(t, filepath.Join(dir, "meta.json")).(map[string]any)
	meta["schemaVersion"] = 99
	b, _ := json.Marshal(meta)
	os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644)
	if err := New(dir, Options{}).Open(); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("got %v", err)
	}
}

// ---------- coalescing ----------

func TestCoalescesBurstsButPersistsFinalState(t *testing.T) {
	s, dir := mk(t)
	c := must(s.CreateCategory(shared.LibraryReading, "Burst", ""))
	e := must(s.CreateEntry(EntryInput{"title": "Counter", "categoryId": c.ID}))
	real := s.writeFile
	var mu sync.Mutex
	writes := 0
	s.writeFile = func(f string, text []byte) error {
		mu.Lock()
		writes++
		mu.Unlock()
		time.Sleep(15 * time.Millisecond)
		return real(f, text)
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.SetProgress(e.ID, ProgressOp{Delta: 1}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	s.writeFile = real
	if writes >= 10 {
		t.Fatalf("expected coalescing, got %d writes", writes)
	}
	disk := readJSON(t, filepath.Join(dir, "data/reading/burst.json")).([]any)
	if n := disk[0].(map[string]any)["current"].(map[string]any)["number"]; n != float64(30) {
		t.Fatalf("final number on disk: %v", n)
	}
}

// ---------- extra coverage ----------

func TestUpdateEntryMergesObjectsAndIgnoresProtectedKeys(t *testing.T) {
	s, _ := mk(t)
	c := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	e := must(s.CreateEntry(EntryInput{"title": "X", "categoryId": c.ID, "notes": map[string]any{"general": "g", "extra1": "left"}}))
	out := must(s.UpdateEntry(e.ID, map[string]any{
		"notes": map[string]any{"extra2": "new"}, "id": "hacked", "categoryId": "nope", "score": 7,
	}))
	if out.ID != e.ID || out.CategoryID != c.ID {
		t.Fatal("protected keys changed")
	}
	if *out.Notes.General != "g" || *out.Notes.Extra1 != "left" || *out.Notes.Extra2 != "new" || *out.Score != 7 {
		t.Fatalf("notes not merged: %+v", out.Notes)
	}
	if _, err := s.UpdateEntry(e.ID, map[string]any{"score": 11}); err == nil {
		t.Fatal("invalid score must be rejected")
	}
	if got := must(s.GetEntryOK(e.ID)); *got.Score != 7 {
		t.Fatal("rejected update changed the entry")
	}
}

func TestBulkOperationsAndBackupBeforeDelete(t *testing.T) {
	s, dir := mk(t)
	a := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	b := must(s.CreateCategory(shared.LibraryReading, "B", ""))
	var ids []string
	for _, title := range []string{"one", "two", "three"} {
		ids = append(ids, must(s.CreateEntry(EntryInput{"title": title, "categoryId": a.ID})).ID)
	}
	n := must(s.Bulk(BulkOp{IDs: ids, Action: "set_status", Value: "dropped"}))
	n2 := must(s.Bulk(BulkOp{IDs: ids[:2], Action: "add_tag", Value: "Fav"}))
	n3 := must(s.Bulk(BulkOp{IDs: ids[:1], Action: "remove_tag", Value: "fav"}))
	n4 := must(s.Bulk(BulkOp{IDs: ids[:2], Action: "move", Value: b.ID}))
	if n != 3 || n2 != 2 || n3 != 1 || n4 != 2 || len(s.EntriesOf(b.ID)) != 2 {
		t.Fatalf("n=%d %d %d %d", n, n2, n3, n4)
	}
	if got := must(s.GetEntryOK(ids[1])).Tags; !reflect.DeepEqual(got, []string{"Fav"}) {
		t.Fatalf("tags %v", got)
	}
	before := len(must(ListBackups(dir)))
	if del := must(s.Bulk(BulkOp{IDs: ids[1:], Action: "delete"})); del != 2 {
		t.Fatalf("deleted %d", del)
	}
	if len(must(ListBackups(dir))) != before+1 || len(s.All()) != 1 {
		t.Fatal("expected a pre-bulk-delete backup and one remaining entry")
	}
}

func TestCategoryLifecycle(t *testing.T) {
	s, dir := mk(t)
	a := must(s.CreateCategory(shared.LibraryReading, "Romance", ""))
	dup := must(s.CreateCategory(shared.LibraryReading, "Romance", ""))
	if a.Slug != "romance" || dup.Slug != "romance-2" || dup.ID != "reading-romance-2" || dup.Order != 1 {
		t.Fatalf("%+v %+v", a, dup)
	}
	if h := must(s.CreateCategory(shared.LibraryReading, "Harem", "")); h.Color != "#880E4F" {
		t.Fatalf("palette color %q", h.Color)
	}
	e := must(s.CreateEntry(EntryInput{"title": "X", "categoryId": a.ID}))

	name := "Love Stories"
	ren := must(s.UpdateCategory(a.ID, CategoryPatch{Name: &name}))
	if ren.Slug != "love-stories" {
		t.Fatalf("slug %q", ren.Slug)
	}
	if _, err := os.Stat(filepath.Join(dir, "data/reading/romance.json")); !os.IsNotExist(err) {
		t.Fatal("old file should be removed")
	}
	if n := len(readJSON(t, filepath.Join(dir, "data/reading/love-stories.json")).([]any)); n != 1 {
		t.Fatal("entries not moved to new file")
	}

	err := s.DeleteCategory(a.ID, "")
	var he *HTTPError
	if !errors.As(err, &he) || he.Code != "CATEGORY_NOT_EMPTY" || he.Status != 409 {
		t.Fatalf("got %v", err)
	}
	if err := s.DeleteCategory(a.ID, dup.ID); err != nil {
		t.Fatal(err)
	}
	if got := must(s.GetEntryOK(e.ID)); got.CategoryID != dup.ID {
		t.Fatal("entry not moved")
	}
	for _, c := range s.Categories() {
		if c.ID == a.ID {
			t.Fatal("category still listed")
		}
	}
	if ens := must(s.EnsureCategory(shared.LibraryReading, "  harem ", "")); ens.Name != "Harem" {
		t.Fatalf("ensure created a duplicate: %+v", ens)
	}
}

func TestUpdateTagRenameRewritesEntries(t *testing.T) {
	s, _ := mk(t)
	c := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	e := must(s.CreateEntry(EntryInput{"title": "X", "categoryId": c.ID, "tags": []string{"Old", "Keep"}}))
	nn := "Fresh"
	must(s.UpdateTag("old", TagPatch{Name: &nn}))
	if got := must(s.GetEntryOK(e.ID)).Tags; !reflect.DeepEqual(got, []string{"Fresh", "Keep"}) {
		t.Fatalf("tags %v", got)
	}
	keep := "Keep"
	_, err := s.UpdateTag("Fresh", TagPatch{Name: &keep})
	var he *HTTPError
	if !errors.As(err, &he) || he.Code != "TAG_EXISTS" {
		t.Fatalf("got %v", err)
	}
	if _, err := s.UpdateTag("missing", TagPatch{}); err == nil {
		t.Fatal("expected not found")
	}
}

func TestSettingsAndScalesPersist(t *testing.T) {
	s, dir := mk(t)
	st := s.Settings()
	st.Theme = "light"
	st.LabelOverrides = map[string]string{"extra2": "Extras"}
	must(s.SaveSettings(st))
	bad := st
	bad.Theme = "blue"
	if _, err := s.SaveSettings(bad); err == nil {
		t.Fatal("invalid theme must be rejected")
	}
	sc := s.Scales()
	sc.Reading.StatusLabels[shared.StatusPlanned] = "Queued"
	must(s.SaveScales(sc))
	re := New(dir, Options{})
	if err := re.Open(); err != nil {
		t.Fatal(err)
	}
	if re.Settings().Theme != "light" || re.Settings().LabelOverrides["extra2"] != "Extras" || len(re.Settings().LabelOverrides) != 1 {
		t.Fatalf("settings %+v", re.Settings())
	}
	if re.Scales().Reading.StatusLabels[shared.StatusPlanned] != "Queued" {
		t.Fatal("scales not persisted")
	}
}

func TestActivityLogSurvivesTornLinesAndFiltersBySince(t *testing.T) {
	s, dir := mk(t)
	mustErr(s.LogActivity(shared.ActivityRecord{At: "2026-01-05T00:00:00.000Z", EntryID: "a", Kind: "created"}))
	mustErr(s.LogActivity(shared.ActivityRecord{At: "2026-02-05T00:00:00.000Z", EntryID: "b", Kind: "created"}))
	f, _ := os.OpenFile(filepath.Join(dir, "activity/2026-02.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"at":"2026-02-06","entry` + "\n")
	f.Close()
	if n := len(must(s.ReadActivity(""))); n != 2 {
		t.Fatalf("all: %d", n)
	}
	if got := must(s.ReadActivity("2026-02-01T00:00:00.000Z")); len(got) != 1 || got[0].EntryID != "b" {
		t.Fatalf("since: %+v", got)
	}
}

func TestLaunchBackupOnlyWhenStaleAndNotEmpty(t *testing.T) {
	s, dir := mk(t)
	c := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	must(s.CreateEntry(EntryInput{"title": "X", "categoryId": c.ID}))
	s.Close()
	re := New(dir, Options{})
	if err := re.Open(); err != nil {
		t.Fatal(err)
	}
	if n := len(must(ListBackups(dir))); n != 1 {
		t.Fatalf("expected one launch backup, got %d", n)
	}
	if err := New(dir, Options{}).Open(); err != nil {
		t.Fatal(err)
	}
	if n := len(must(ListBackups(dir))); n != 1 {
		t.Fatalf("a fresh backup must not be repeated, got %d", n)
	}
}

// TestConcurrentReadersAndWriters is meant for `go test -race`.
func TestConcurrentReadersAndWriters(t *testing.T) {
	s, _ := mk(t)
	c := must(s.CreateCategory(shared.LibraryReading, "A", ""))
	e := must(s.CreateEntry(EntryInput{"title": "X", "categoryId": c.ID}))
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = s.All()
					_, _ = s.GetEntry(e.ID)
					_ = s.Tags()
					_ = s.TagUsage()
				}
			}
		}()
	}
	var ww sync.WaitGroup
	for w := 0; w < 4; w++ {
		ww.Add(1)
		go func() {
			defer ww.Done()
			for i := 0; i < 6; i++ {
				if _, err := s.SetProgress(e.ID, ProgressOp{Delta: 1}); err != nil {
					t.Error(err)
				}
				if _, err := s.CreateEntry(EntryInput{"title": "N", "categoryId": c.ID, "tags": []string{"t"}}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	ww.Wait()
	close(stop)
	wg.Wait()
	if got := must(s.GetEntryOK(e.ID)); *got.Current.Number != 24 {
		t.Fatalf("lost updates: %v", *got.Current.Number)
	}
}

func mustErr(err error) {
	if err != nil {
		panic(err)
	}
}

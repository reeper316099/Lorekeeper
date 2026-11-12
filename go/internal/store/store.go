package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// HTTPError is a failure the API layer maps straight to a response (same codes as the Node store).
type HTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e *HTTPError) Error() string { return e.Message }

func notFound(format string, args ...any) error {
	return &HTTPError{404, "NOT_FOUND", fmt.Sprintf(format, args...)}
}

// Event names emitted by the Store.
const (
	EventChange         = "change"
	EventFileError      = "file-error"
	EventFileRestored   = "file-restored"
	EventBackup         = "backup"
	EventExternalChange = "external-change"
)

type Event struct {
	Name string
	Data any
}

// FileError is the payload of EventFileError.
type FileError struct {
	File        string  `json:"file"`
	Message     string  `json:"message"`
	Quarantined *string `json:"quarantined"`
	Kept        bool    `json:"kept,omitempty"`
}

type Options struct {
	AppVersion    string
	Migrations    []Migration
	SchemaVersion int
	Watch         bool
}

// Store is the in-memory source of truth for one data folder. Entries are immutable once stored
// (updates replace them), so slices returned by readers are safe to use while writes continue.
type Store struct {
	Dir  string
	opts Options

	mu         sync.RWMutex
	meta       shared.Meta
	settings   shared.Settings
	scales     shared.Scales
	categories []shared.Category
	tags       []shared.Tag
	byCat      map[string][]shared.Entry
	version    uint64
	allCache   struct {
		v    uint64
		list []shared.Entry
		ok   bool
	}

	fileMu      KeyedMutex
	lastWritten sync.Map // file -> string
	flushMu     sync.Mutex
	flushes     map[string]*flushState

	handlersMu sync.Mutex
	handlers   map[int]func(Event)
	nextHandle int

	watcher *watcher

	// writeFile is the single choke point for persisting a file; tests replace it to inject failures.
	writeFile func(file string, text []byte) error
}

func New(dir string, opts Options) *Store {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	s := &Store{Dir: abs, opts: opts, byCat: map[string][]shared.Entry{}, flushes: map[string]*flushState{}, handlers: map[int]func(Event){}}
	s.writeFile = s.writeFileImpl
	return s
}

// On registers a handler called synchronously for every event; handlers must not block. The
// returned function unregisters it.
func (s *Store) On(fn func(Event)) (off func()) {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()
	id := s.nextHandle
	s.nextHandle++
	s.handlers[id] = fn
	return func() {
		s.handlersMu.Lock()
		delete(s.handlers, id)
		s.handlersMu.Unlock()
	}
}

func (s *Store) emit(name string, data any) {
	s.handlersMu.Lock()
	hs := make([]func(Event), 0, len(s.handlers))
	for _, h := range s.handlers {
		hs = append(hs, h)
	}
	s.handlersMu.Unlock()
	for _, h := range hs {
		h(Event{name, data})
	}
}

func (s *Store) p(segs ...string) string { return filepath.Join(append([]string{s.Dir}, segs...)...) }

func (s *Store) catFile(c shared.Category) string {
	return s.p("data", string(c.Library), c.Slug+".json")
}

func (s *Store) keep() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings.Backup.Keep == 0 {
		return 20
	}
	return s.settings.Backup.Keep
}

// ---------- lifecycle ----------

func (s *Store) Open() error {
	for _, d := range []string{"config", "data/reading", "data/watching", "activity", "backups", "exports", "imports", "logs"} {
		if err := os.MkdirAll(s.p(filepath.FromSlash(d)), 0o755); err != nil {
			return err
		}
	}
	now := ISO(time.Now())
	var meta *shared.Meta
	if text, err := os.ReadFile(s.p("meta.json")); err == nil {
		var m shared.Meta
		if json.Unmarshal(text, &m) == nil {
			meta = &m
		} else {
			s.quarantine(s.p("meta.json"))
		}
	}
	target := s.opts.SchemaVersion
	if target == 0 {
		target = shared.SchemaVersion
	}
	appVersion := s.opts.AppVersion
	fresh := meta == nil
	if fresh {
		av := appVersion
		if av == "" {
			av = "0.1.0"
		}
		meta = &shared.Meta{SchemaVersion: target, AppVersion: av, CreatedAt: now, LastOpenedAt: now}
	} else {
		v, err := Migrate(s.Dir, meta.SchemaVersion, MigrateOptions{
			Target:     target,
			Migrations: s.opts.Migrations,
			Backup: func(reason string) error {
				_, err := CreateBackup(s.Dir, reason, s.keep())
				return err
			},
		})
		if err != nil {
			return err
		}
		meta.SchemaVersion = v
	}
	meta.LastOpenedAt = now
	if appVersion != "" {
		meta.AppVersion = appVersion
	}
	s.mu.Lock()
	s.meta = *meta
	s.mu.Unlock()
	if err := s.writeJSON(s.p("meta.json"), meta); err != nil {
		return err
	}

	if err := s.LoadAll(); err != nil {
		return err
	}

	s.mu.RLock()
	hours := s.settings.Backup.OnLaunchHours
	s.mu.RUnlock()
	list, _ := ListBackups(s.Dir)
	stale := len(list) == 0
	if !stale {
		if t, err := time.Parse(time.RFC3339Nano, list[0].CreatedAt); err != nil || time.Since(t) > time.Duration(hours*float64(time.Hour)) {
			stale = true
		}
	}
	if stale && len(s.All()) > 0 {
		if _, err := s.Backup("launch"); err != nil {
			return err
		}
	}
	if s.opts.Watch {
		return s.startWatching()
	}
	return nil
}

func (s *Store) Close() error {
	if s.watcher != nil {
		s.watcher.close()
		s.watcher = nil
	}
	return nil
}

func readValidated[T any](s *Store, rel string, parse func([]byte) (T, error), fallback T) (T, error) {
	file := s.p(filepath.FromSlash(rel))
	text, err := os.ReadFile(file)
	if err != nil {
		return fallback, s.writeJSON(file, fallback)
	}
	if v, perr := parse(text); perr == nil {
		return v, nil
	} else {
		q := s.quarantine(file)
		s.emit(EventFileError, FileError{File: rel, Message: describeParseError(text, perr), Quarantined: q})
	}
	if bak, ok := LatestBackupText(s.Dir, rel); ok {
		if v, perr := parse(bak); perr == nil {
			if err := s.writeJSON(file, v); err != nil {
				return v, err
			}
			s.emit(EventFileRestored, map[string]string{"file": rel})
			return v, nil
		}
	}
	return fallback, s.writeJSON(file, fallback)
}

func decode[T any](b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// describeParseError names the offending field, or the line and column of a JSON syntax error.
func describeParseError(text []byte, err error) string {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		before := text[:min(int(se.Offset), len(text))]
		line := strings.Count(string(before), "\n") + 1
		col := int(se.Offset) - strings.LastIndexByte(string(before), '\n')
		return fmt.Sprintf("%s (line %d, column %d)", se.Error(), line, col)
	}
	return err.Error()
}

// LoadAll (re)reads every file, quarantining and restoring corrupt ones.
func (s *Store) LoadAll() error {
	settings, err := readValidated(s, "config/settings.json", decode[shared.Settings], shared.DefaultSettings())
	if err != nil {
		return err
	}
	scales, err := readValidated(s, "config/scales.json", decode[shared.Scales], shared.DefaultScales())
	if err != nil {
		return err
	}
	cats, err := readValidated(s, "config/categories.json", shared.ParseCategoriesFile, []shared.Category{})
	if err != nil {
		return err
	}
	tags, err := readValidated(s, "config/tags.json", shared.ParseTagsFile, []shared.Tag{})
	if err != nil {
		return err
	}
	byCat := map[string][]shared.Entry{}
	for _, c := range cats {
		list, err := readValidated(s, fmt.Sprintf("data/%s/%s.json", c.Library, c.Slug), shared.ParseEntryFile, []shared.Entry{})
		if err != nil {
			return err
		}
		byCat[c.ID] = list
	}
	s.mu.Lock()
	s.settings, s.scales, s.categories, s.tags, s.byCat = settings, scales, cats, tags, byCat
	s.bumpLocked()
	s.mu.Unlock()
	return nil
}

// ---------- file IO ----------

func (s *Store) writeFileImpl(file string, text []byte) error {
	return s.fileMu.Run(file, func() error {
		s.lastWritten.Store(file, string(text))
		return AtomicWrite(file, text)
	})
}

// writeJSON persists data as stable JSON. The data is serialized immediately (a snapshot).
func (s *Store) writeJSON(file string, data any) error {
	text, err := StableMarshal(data)
	if err != nil {
		return err
	}
	return s.writeFile(file, text)
}

func (s *Store) quarantine(file string) *string {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	dest := s.p("backups", "corrupt", fmt.Sprintf("%s.%d.bad", filepath.Base(file), time.Now().UnixMilli()))
	if os.MkdirAll(filepath.Dir(dest), 0o755) != nil || os.WriteFile(dest, b, 0o644) != nil {
		return nil
	}
	return &dest
}

func (s *Store) rel(file string) string {
	r, err := filepath.Rel(s.Dir, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(r)
}

// ---------- change plumbing ----------

func (s *Store) bumpLocked() {
	s.version++
	s.allCache.ok = false
}

// Version bumps on every change; caches key off it.
func (s *Store) Version() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

type flight struct {
	done chan struct{}
	err  error
}

func (f *flight) wait() error { <-f.done; return f.err }

type flushState struct{ inflight, queued *flight }

// persist writes a file with burst coalescing: while one write runs, any number of further
// requests share a single follow-up write that serializes the *latest* data. Every caller still
// returns only after a write that includes its change has finished.
func (s *Store) persist(file string, get func() ([]byte, error)) error {
	s.flushMu.Lock()
	st := s.flushes[file]
	if st == nil {
		st = &flushState{}
		s.flushes[file] = st
	}
	if st.queued != nil {
		f := st.queued
		s.flushMu.Unlock()
		return f.wait()
	}
	if st.inflight == nil {
		f := &flight{done: make(chan struct{})}
		s.launchLocked(file, st, get, f)
		s.flushMu.Unlock()
		return f.wait()
	}
	prev := st.inflight
	q := &flight{done: make(chan struct{})}
	st.queued = q
	s.flushMu.Unlock()
	go func() {
		<-prev.done
		s.flushMu.Lock()
		st.queued = nil
		s.launchLocked(file, st, get, q)
		s.flushMu.Unlock()
	}()
	return q.wait()
}

func (s *Store) launchLocked(file string, st *flushState, get func() ([]byte, error), f *flight) {
	st.inflight = f
	go func() {
		text, err := get()
		if err == nil {
			err = s.writeFile(file, text)
		}
		f.err = err
		s.flushMu.Lock()
		if st.inflight == f {
			st.inflight = nil
		}
		s.flushMu.Unlock()
		close(f.done)
	}()
}

// persistCat writes one category file with coalescing.
func (s *Store) persistCat(c shared.Category) error {
	id := c.ID
	return s.persist(s.catFile(c), func() ([]byte, error) {
		// Entries are immutable and lists are replaced, never edited in place, so a reference taken
		// under the lock is a safe snapshot to serialize without it: writes never block reads.
		s.mu.RLock()
		list := s.byCat[id]
		s.mu.RUnlock()
		if list == nil {
			list = []shared.Entry{}
		}
		return StableMarshal(list)
	})
}

func (s *Store) commit(catIDs []string, event string) error {
	s.mu.Lock()
	s.bumpLocked()
	seen := map[string]bool{}
	var cats []shared.Category
	for _, id := range catIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		for _, c := range s.categories {
			if c.ID == id {
				cats = append(cats, c)
			}
		}
	}
	s.mu.Unlock()
	for _, c := range cats {
		if err := s.persistCat(c); err != nil {
			return err
		}
	}
	s.emit(event, nil)
	return nil
}

// ---------- backups ----------

func (s *Store) Backup(reason string) (BackupInfo, error) {
	b, err := CreateBackup(s.Dir, reason, s.keep())
	if err == nil {
		s.emit(EventBackup, b)
	}
	return b, err
}

func (s *Store) ListBackups() ([]BackupInfo, error) { return ListBackups(s.Dir) }

func (s *Store) RestoreBackup(name string) error {
	if _, err := s.Backup("pre-restore"); err != nil {
		return err
	}
	if _, err := RestoreBackupFiles(s.Dir, name); err != nil {
		return err
	}
	s.lastWritten.Range(func(k, _ any) bool { s.lastWritten.Delete(k); return true })
	if err := s.LoadAll(); err != nil {
		return err
	}
	s.emit(EventChange, nil)
	return nil
}

// ---------- read access ----------

func (s *Store) Meta() shared.Meta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta
}

func (s *Store) Settings() shared.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Store) Scales() shared.Scales {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scales
}

func (s *Store) Categories() []shared.Category {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]shared.Category{}, s.categories...)
}

func (s *Store) Tags() []shared.Tag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]shared.Tag{}, s.tags...)
}

// ImportsDir / ExportsDir are helpers for the importers.
func (s *Store) ImportsDir() string                    { return s.p("imports") }
func (s *Store) ExportsDir() string                    { return s.p("exports") }
func (s *Store) CategoryFile(c shared.Category) string { return s.catFile(c) }

// CategoryEntries is one category's entry list as of a Snapshot.
type CategoryEntries struct {
	ID      string
	Entries []shared.Entry
}

// Snapshot returns the version and every category's entry list, in category order. The lists are
// immutable (the store replaces lists, never edits them), so they are safe to read without a lock.
func (s *Store) Snapshot() (version uint64, lists []CategoryEntries) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.categories {
		lists = append(lists, CategoryEntries{ID: c.ID, Entries: s.byCat[c.ID]})
	}
	return s.version, lists
}

// Emit sends an event to every subscriber (used by background jobs).
func (s *Store) Emit(name string, data any) { s.emit(name, data) }

// EventJob is emitted by background jobs (AniList matching).
const EventJob = "job"

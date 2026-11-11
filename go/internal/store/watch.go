package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// settleDelay is how long a file must be quiet before a hand edit is read (chokidar's
// awaitWriteFinish equivalent).
const settleDelay = 150 * time.Millisecond

type watcher struct {
	w      *fsnotify.Watcher
	done   chan struct{}
	mu     sync.Mutex
	timers map[string]*time.Timer
	closed bool
}

func (s *Store) startWatching() error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, d := range []string{s.p("config"), s.p("data", "reading"), s.p("data", "watching")} {
		if err := fw.Add(d); err != nil {
			fw.Close()
			return err
		}
	}
	w := &watcher{w: fw, done: make(chan struct{}), timers: map[string]*time.Timer{}}
	s.watcher = w
	go func() {
		defer close(w.done)
		for {
			select {
			case ev, ok := <-fw.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 || strings.HasSuffix(ev.Name, ".tmp") || !strings.HasSuffix(ev.Name, ".json") {
					continue
				}
				w.debounce(ev.Name, func() { s.onExternalChange(ev.Name) })
			case _, ok := <-fw.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return nil
}

func (w *watcher) debounce(file string, fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if t := w.timers[file]; t != nil {
		t.Stop()
	}
	w.timers[file] = time.AfterFunc(settleDelay, fn)
}

func (w *watcher) close() {
	w.mu.Lock()
	w.closed = true
	for _, t := range w.timers {
		t.Stop()
	}
	w.mu.Unlock()
	w.w.Close()
	<-w.done
}

func applyExternal[T any](s *Store, rel, file string, text []byte, parse func([]byte) (T, error), set func(T)) {
	v, err := parse(text)
	if err != nil {
		s.emit(EventFileError, FileError{File: rel, Message: describeParseError(text, err), Kept: true})
		return
	}
	s.lastWritten.Store(file, string(text))
	s.mu.Lock()
	set(v)
	s.bumpLocked()
	s.mu.Unlock()
	s.emit(EventExternalChange, map[string]string{"file": rel})
}

// onExternalChange handles a file changed outside the app. Valid edits replace the in-memory data;
// invalid ones are reported and the previous data is kept.
func (s *Store) onExternalChange(file string) {
	text, err := os.ReadFile(file)
	if err != nil {
		return
	}
	if last, ok := s.lastWritten.Load(file); ok && last.(string) == string(text) {
		return
	}
	rel := s.rel(file)
	switch rel {
	case "config/settings.json":
		applyExternal(s, rel, file, text, decode[shared.Settings], func(v shared.Settings) { s.settings = v })
	case "config/scales.json":
		applyExternal(s, rel, file, text, decode[shared.Scales], func(v shared.Scales) { s.scales = v })
	case "config/tags.json":
		applyExternal(s, rel, file, text, shared.ParseTagsFile, func(v []shared.Tag) { s.tags = v })
	case "config/categories.json":
		applyExternal(s, rel, file, text, shared.ParseCategoriesFile, func(v []shared.Category) { s.categories = v })
	default:
		s.mu.RLock()
		var cat *shared.Category
		for _, c := range s.categories {
			if filepath.Clean(s.catFile(c)) == filepath.Clean(file) {
				c := c
				cat = &c
			}
		}
		s.mu.RUnlock()
		if cat != nil {
			applyExternal(s, rel, file, text, shared.ParseEntryFile, func(v []shared.Entry) { s.byCat[cat.ID] = v })
		}
	}
}

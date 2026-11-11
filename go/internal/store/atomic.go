// Package store is the Go port of packages/server/src/storage: the on-disk JSON store with atomic
// writes, per-file serialization, write coalescing, backups, migrations, corruption recovery and
// a file watcher. The on-disk format is identical to the Node implementation (see
// docs/DATA_FORMAT.md), so a data folder written by either opens in the other.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// StableMarshal writes stable-key, 2-space JSON (sorted keys, trailing newline) so files are
// diff-friendly and hand-editable. Output matches the TypeScript stableStringify byte for byte,
// including not escaping <, > and & and writing U+2028/9 raw (as JSON.stringify does).
//
// Go sorts map keys but writes struct fields in declaration order, so the structs in
// internal/shared declare their fields in sorted JSON-key order (checked by
// TestJSONFieldsAreSorted there). That lets this encode in a single pass; StableMarshalGeneric is
// the slow reference that sorts anything.
func StableMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return rawLineSeparators(buf.Bytes()), nil
}

// StableMarshalGeneric sorts keys of any value by round-tripping it through a generic map. Slower;
// used by tests to check StableMarshal.
func StableMarshalGeneric(v any) ([]byte, error) {
	first, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(first))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	return StableMarshal(generic)
}

// rawLineSeparators turns the   /   escapes Go always emits back into the raw characters
// JSON.stringify writes. A backslash pair is skipped as a unit so "\\u2028" is left alone.
func rawLineSeparators(b []byte) []byte {
	if !bytes.Contains(b, []byte(`\u202`)) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\\' && i+1 < len(b) {
			if b[i+1] == 'u' && i+5 < len(b) && b[i+2] == '2' && b[i+3] == '0' && b[i+4] == '2' && (b[i+5] == '8' || b[i+5] == '9') {
				if b[i+5] == '8' {
					out = append(out, " "...)
				} else {
					out = append(out, " "...)
				}
				i += 5
				continue
			}
			out = append(out, b[i], b[i+1])
			i++
			continue
		}
		out = append(out, b[i])
	}
	return out
}

// renameFn is os.Rename; tests replace it to simulate a locked file.
var renameFn = os.Rename

var renameRetryDelay = 15 * time.Millisecond

// isRetryableRename reports whether a failed rename is one Windows raises while another handle
// (antivirus, a search indexer, a concurrent writer) briefly has the target open.
func isRetryableRename(err error) bool {
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EBUSY) {
		return true
	}
	if runtime.GOOS == "windows" {
		var errno syscall.Errno
		if errors.As(err, &errno) {
			return errno == 5 || errno == 32 || errno == 33 // access denied, sharing/lock violation
		}
	}
	return false
}

func renameWithRetry(from, to string, attempts int) error {
	for i := 0; ; i++ {
		err := renameFn(from, to)
		if err == nil {
			return nil
		}
		if i >= attempts-1 || !isRetryableRename(err) {
			return err
		}
		time.Sleep(renameRetryDelay * time.Duration(i+1))
	}
}

// AtomicWrite writes to a sibling tmp file, fsyncs, then renames over the target. It never leaves
// a half-written file.
func AtomicWrite(file string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.%s.tmp", file, os.Getpid(), strconv.FormatUint(rand.Uint64(), 36))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(content)
	if werr == nil {
		werr = f.Sync()
	}
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	if err := renameWithRetry(tmp, file, 12); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// KeyedMutex serializes work per key (e.g. per file path), first come first served.
type KeyedMutex struct {
	mu    sync.Mutex
	tails map[string]chan struct{}
}

// Ticket is a place in a key's queue, taken by Enter.
type Ticket struct {
	m    *KeyedMutex
	key  string
	prev chan struct{}
	done chan struct{}
}

// Enter takes the next place in the queue for key without blocking. Do must be called exactly once.
func (m *KeyedMutex) Enter(key string) *Ticket {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tails == nil {
		m.tails = map[string]chan struct{}{}
	}
	t := &Ticket{m: m, key: key, prev: m.tails[key], done: make(chan struct{})}
	m.tails[key] = t.done
	return t
}

// Do waits for every earlier ticket on the key, runs fn, then lets the next one go.
func (t *Ticket) Do(fn func() error) error {
	if t.prev != nil {
		<-t.prev
	}
	defer func() {
		t.m.mu.Lock()
		if t.m.tails[t.key] == t.done {
			delete(t.m.tails, t.key)
		}
		t.m.mu.Unlock()
		close(t.done)
	}()
	return fn()
}

// Run executes fn once every earlier Run for the same key has finished.
func (m *KeyedMutex) Run(key string, fn func() error) error { return m.Enter(key).Do(fn) }

package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func removeIfExists(file string) error {
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// LogActivity appends one record to activity/YYYY-MM.jsonl.
func (s *Store) LogActivity(rec shared.ActivityRecord) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return err
	}
	month := rec.At
	if len(month) > 7 {
		month = month[:7]
	}
	file := s.p("activity", month+".jsonl")
	return s.fileMu.Run(file, func() error {
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, werr := f.Write(buf.Bytes())
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return werr
	})
}

// ReadActivity returns records at or after sinceISO (all when empty). Torn lines are skipped.
func (s *Store) ReadActivity(sinceISO string) ([]shared.ActivityRecord, error) {
	names, _ := filepath.Glob(s.p("activity", "*.jsonl"))
	sort.Strings(names)
	out := []shared.ActivityRecord{}
	for _, name := range names {
		if sinceISO != "" && len(sinceISO) >= 7 && filepath.Base(name)[:7] < sinceISO[:7] {
			continue
		}
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var r shared.ActivityRecord
			if json.Unmarshal([]byte(line), &r) != nil {
				continue
			}
			if sinceISO == "" || r.At >= sinceISO {
				out = append(out, r)
			}
		}
		f.Close()
	}
	return out, nil
}

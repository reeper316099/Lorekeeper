package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	logMaxBytes = 1 << 20
	logKeep     = 3
)

// Logger is a tiny rotating file logger (logs/app.log, 1 MB, 3 old files). It never fails the caller
// and must never be given entry content (titles, notes, tags).
type Logger struct {
	mu   sync.Mutex
	file string
}

func NewLogger(dataDir string) *Logger {
	dir := filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(dir, 0o755) // logging must never crash the app
	return &Logger{file: filepath.Join(dir, "app.log")}
}

func (l *Logger) rotate() {
	st, err := os.Stat(l.file)
	if err != nil || st.Size() < logMaxBytes {
		return
	}
	os.Remove(fmt.Sprintf("%s.%d", l.file, logKeep))
	for i := logKeep - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", l.file, i), fmt.Sprintf("%s.%d", l.file, i+1))
	}
	os.Rename(l.file, l.file+".1")
}

// Log writes "<ISO time> LEVEL message" (level is info, warn or error).
func (l *Logger) Log(level, message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rotate()
	f, err := os.OpenFile(l.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s %s\n", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), upper(level), message)
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

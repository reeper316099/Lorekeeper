package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrLocked means another process is using the data folder.
var ErrLocked = errors.New("another Lorekeeper window is already using this data folder")

// LockDataDir is a second guard (beyond the OS-level single-instance check): one process per data
// folder, even when the folder is shared. The lock file holds the pid, as the Electron build did, so
// both builds respect each other's lock. A lock left by a crashed process is taken over.
func LockDataDir(dataDir string) (release func(), err error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	lock := filepath.Join(dataDir, ".lorekeeper.lock")
	if b, err := os.ReadFile(lock); err == nil {
		if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); pid > 0 && pid != os.Getpid() && processAlive(pid) {
			return nil, ErrLocked
		}
	}
	if err := os.WriteFile(lock, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return nil, err
	}
	return func() { os.Remove(lock) }, nil
}

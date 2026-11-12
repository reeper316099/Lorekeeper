package store

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BackupDirs are the folders a backup covers.
var BackupDirs = []string{"config", "data", "activity"}

var backupNameRe = regexp.MustCompile(`^backup-(\d{8}-\d{6})(?:-(\d+))?\.zip$`)

// maxBackupFile caps one file read out of a backup zip (zip-bomb guard).
const maxBackupFile = 512 << 20

type BackupInfo struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
	Reason    string `json:"reason,omitempty"`
}

func IsValidBackupName(name string) bool { return backupNameRe.MatchString(name) }

// ISO formats a time like JavaScript's Date.toISOString().
func ISO(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func walk(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return filepath.SkipAll
			}
			return nil
		}
		if d.Type().IsRegular() && !strings.HasSuffix(d.Name(), ".tmp") {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

func stamp(t time.Time) string { return t.Format("20060102-150405") }

// CreateBackup zips config/, data/ and activity/ into backups/ and prunes old ones.
func CreateBackup(dataDir, reason string, keep int) (BackupInfo, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, content []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(content)
		return err
	}
	for _, top := range BackupDirs {
		files, _ := walk(filepath.Join(dataDir, top))
		sort.Strings(files)
		for _, f := range files {
			rel, err := filepath.Rel(dataDir, f)
			if err != nil {
				return BackupInfo{}, err
			}
			b, err := os.ReadFile(f)
			if err != nil {
				return BackupInfo{}, err
			}
			if err := add(filepath.ToSlash(rel), b); err != nil {
				return BackupInfo{}, err
			}
		}
	}
	createdAt := ISO(time.Now())
	info, _ := json.Marshal(map[string]string{"reason": reason, "createdAt": createdAt})
	if err := add("backup-info.json", info); err != nil {
		return BackupInfo{}, err
	}
	if err := zw.Close(); err != nil {
		return BackupInfo{}, err
	}
	dir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BackupInfo{}, err
	}
	st0 := stamp(time.Now())
	existing, _ := ListBackups(dataDir)
	next, seen := 0, false
	for _, b := range existing {
		if strings.HasPrefix(b.Name, "backup-"+st0) {
			seen = true
			n := 0
			if m := backupNameRe.FindStringSubmatch(b.Name); m != nil && m[2] != "" {
				n, _ = strconv.Atoi(m[2])
			}
			next = max(next, n+1)
		}
	}
	name := "backup-" + st0 + ".zip"
	if seen && next > 0 {
		name = fmt.Sprintf("backup-%s-%d.zip", st0, next)
	}
	if err := AtomicWrite(filepath.Join(dir, name), buf.Bytes()); err != nil {
		return BackupInfo{}, err
	}
	if err := PruneBackups(dataDir, keep); err != nil {
		return BackupInfo{}, err
	}
	st, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return BackupInfo{}, err
	}
	return BackupInfo{Name: name, Size: st.Size(), CreatedAt: createdAt, Reason: reason}, nil
}

func backupKey(name string) string {
	m := backupNameRe.FindStringSubmatch(name)
	n := 0
	if m[2] != "" {
		n, _ = strconv.Atoi(m[2])
	}
	return fmt.Sprintf("%s-%06d", m[1], n)
}

// ListBackups returns backups newest first.
func ListBackups(dataDir string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if err != nil {
		return []BackupInfo{}, nil
	}
	out := []BackupInfo{}
	for _, e := range entries {
		if !IsValidBackupName(e.Name()) {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{Name: e.Name(), Size: st.Size(), CreatedAt: ISO(st.ModTime())})
	}
	sort.Slice(out, func(i, j int) bool { return backupKey(out[i].Name) > backupKey(out[j].Name) })
	return out, nil
}

// PruneBackups keeps the newest `keep` backups.
func PruneBackups(dataDir string, keep int) error {
	all, _ := ListBackups(dataDir)
	if keep < 0 || len(all) <= keep {
		return nil
	}
	for _, b := range all[keep:] {
		if err := os.Remove(filepath.Join(dataDir, "backups", b.Name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// IsSafeZipPath accepts only relative paths inside the known top-level folders: no traversal,
// no absolute paths, no backslashes.
func IsSafeZipPath(p string) bool {
	if strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return false
	}
	if len(p) >= 2 && p[1] == ':' && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z') {
		return false
	}
	parts := strings.Split(p, "/")
	for _, s := range parts {
		if s == ".." || s == "" {
			return false
		}
	}
	for _, top := range BackupDirs {
		if parts[0] == top {
			return true
		}
	}
	return false
}

// ReadBackup returns the safe files of a backup zip.
func ReadBackup(dataDir, name string) (map[string][]byte, error) {
	if !IsValidBackupName(name) {
		return nil, errors.New("Invalid backup name")
	}
	zr, err := zip.OpenReader(filepath.Join(dataDir, "backups", name))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !IsSafeZipPath(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxBackupFile+1))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if len(b) > maxBackupFile {
			return nil, fmt.Errorf("backup file %s is too large", f.Name)
		}
		out[f.Name] = b
	}
	return out, nil
}

// RestoreBackupFiles replaces config/, data/ and activity/ with the contents of a backup.
func RestoreBackupFiles(dataDir, name string) ([]string, error) {
	files, err := ReadBackup(dataDir, name)
	if err != nil {
		return nil, err
	}
	for _, top := range BackupDirs {
		if err := os.RemoveAll(filepath.Join(dataDir, top)); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(files))
	for rel, b := range files {
		if err := AtomicWrite(filepath.Join(dataDir, filepath.FromSlash(rel)), b); err != nil {
			return nil, err
		}
		names = append(names, rel)
	}
	sort.Strings(names)
	return names, nil
}

// LatestBackupText reads one file's text from the newest backup that has it (corrupt-file recovery).
func LatestBackupText(dataDir, rel string) ([]byte, bool) {
	all, _ := ListBackups(dataDir)
	for _, b := range all {
		files, err := ReadBackup(dataDir, b.Name)
		if err != nil {
			continue
		}
		if hit, ok := files[rel]; ok {
			return hit, true
		}
	}
	return nil, false
}

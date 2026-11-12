package desktop

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// CopyDataFolder copies config/, data/, activity/ and meta.json from one data folder to another
// (the "copy my data" option when choosing a new data folder). Backups, imports and caches stay behind.
func CopyDataFolder(from, to string) error {
	for _, sub := range []string{"config", "data", "activity"} {
		src := filepath.Join(from, sub)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyTree(src, filepath.Join(to, sub)); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(from, "meta.json")); err == nil {
		return copyFile(filepath.Join(from, "meta.json"), filepath.Join(to, "meta.json"))
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

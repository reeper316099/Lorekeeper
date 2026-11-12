package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/reeper316099/Lorekeeper/go/internal/shared"
)

// Migration upgrades the data folder from schema From to To.
type Migration struct {
	From, To int
	Run      func(dataDir string) error
}

// Migrations is the ordered list. Add new migrations here when shared.SchemaVersion is bumped.
var Migrations []Migration

type MigrateOptions struct {
	Target     int // 0 = shared.SchemaVersion
	Migrations []Migration
	Backup     func(reason string) error
}

// Migrate runs the ordered migrations after taking a backup, and refuses data written by a newer
// version. It returns the schema version reached.
func Migrate(dataDir string, current int, opts MigrateOptions) (int, error) {
	target := opts.Target
	if target == 0 {
		target = shared.SchemaVersion
	}
	list := opts.Migrations
	if list == nil {
		list = Migrations
	}
	list = append([]Migration(nil), list...)
	sort.Slice(list, func(i, j int) bool { return list[i].From < list[j].From })
	if current > target {
		return current, fmt.Errorf("Data was written by a newer version (schema %d > %d).", current, target)
	}
	v := current
	if v == target {
		return v, nil
	}
	if err := opts.Backup(fmt.Sprintf("pre-migration-v%d-to-v%d", v, target)); err != nil {
		return v, err
	}
	for v < target {
		var m *Migration
		for i := range list {
			if list[i].From == v {
				m = &list[i]
				break
			}
		}
		if m == nil {
			return v, fmt.Errorf("No migration from schema %d", v)
		}
		if err := m.Run(dataDir); err != nil {
			return v, err
		}
		v = m.To
		metaFile := filepath.Join(dataDir, "meta.json")
		meta := map[string]any{}
		if b, err := os.ReadFile(metaFile); err == nil {
			_ = json.Unmarshal(b, &meta)
		}
		meta["schemaVersion"] = v
		text, err := StableMarshal(meta)
		if err != nil {
			return v, err
		}
		if err := AtomicWrite(metaFile, text); err != nil {
			return v, err
		}
	}
	return v, nil
}

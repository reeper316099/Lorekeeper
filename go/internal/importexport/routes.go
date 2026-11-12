package importexport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/reeper316099/Lorekeeper/go/internal/api"
	"github.com/reeper316099/Lorekeeper/go/internal/search"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

const (
	maxUpload   = 50 << 20
	xlsxType    = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	csvType     = "text/csv; charset=utf-8"
	multipartMB = maxUpload + 1<<20 // a file plus form overhead
)

var reUploadExt = regexp.MustCompile(`(?i)\.(xlsx|xlsm|csv|tsv|txt)$`)

func invalid(path, format string, args ...any) error {
	return &shared.ValidationError{Path: path, Msg: fmt.Sprintf(format, args...)}
}

func asObject(v any, path string) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, invalid(path, "Expected object")
	}
	return m, nil
}

func optString(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", invalid(key, "Expected string")
	}
	return s, nil
}

func optLibrary(m map[string]any, key string) (shared.Library, error) {
	s, err := optString(m, key)
	if err != nil || s == "" {
		return "", err
	}
	if s != "reading" && s != "watching" {
		return "", invalid(key, "Invalid enum value")
	}
	return shared.Library(s), nil
}

// ParseImportOptions validates the "options" object of a preview/commit request.
func ParseImportOptions(raw any) (ImportOptions, error) {
	var o ImportOptions
	m, err := asObject(raw, "options")
	if err != nil {
		return o, err
	}
	if s, err := optString(m, "strategy"); err != nil {
		return o, err
	} else if s != "" {
		switch Strategy(s) {
		case StrategySkip, StrategyFill, StrategyOverwrite, StrategyKeepBoth:
			o.Strategy = Strategy(s)
		default:
			return o, invalid("strategy", "Invalid enum value")
		}
	}
	if o.Library, err = optLibrary(m, "library"); err != nil {
		return o, err
	}
	if o.Category, err = optString(m, "category"); err != nil {
		return o, err
	}
	if sv, ok := m["sheets"]; ok && sv != nil {
		sm, err := asObject(sv, "sheets")
		if err != nil {
			return o, err
		}
		o.Sheets = map[string]SheetOptions{}
		for name, v := range sm {
			one, err := asObject(v, "sheets."+name)
			if err != nil {
				return o, err
			}
			var so SheetOptions
			if inc, ok := one["include"]; ok && inc != nil {
				b, ok := inc.(bool)
				if !ok {
					return o, invalid("sheets."+name+".include", "Expected boolean")
				}
				so.Include = &b
			}
			if so.Library, err = optLibrary(one, "library"); err != nil {
				return o, err
			}
			if so.Category, err = optString(one, "category"); err != nil {
				return o, err
			}
			if mv, ok := one["mapping"]; ok && mv != nil {
				arr, ok := mv.([]any)
				if !ok {
					return o, invalid("sheets."+name+".mapping", "Expected array")
				}
				so.Mapping = make([]Field, len(arr))
				for i, x := range arr {
					if x == nil {
						continue
					}
					s, ok := x.(string)
					if !ok || !validField(Field(s)) {
						return o, invalid(fmt.Sprintf("sheets.%s.mapping[%d]", name, i), "Invalid enum value")
					}
					so.Mapping[i] = Field(s)
				}
			}
			o.Sheets[name] = so
		}
	}
	if sv, ok := m["statusMap"]; ok && sv != nil {
		sm, err := asObject(sv, "statusMap")
		if err != nil {
			return o, err
		}
		o.StatusMap = map[string]shared.Status{}
		for raw, v := range sm {
			s, ok := v.(string)
			if !ok || !shared.ValidStatus(shared.Status(s)) {
				return o, invalid("statusMap."+raw, "Invalid enum value")
			}
			o.StatusMap[raw] = shared.Status(s)
		}
	}
	return o, nil
}

// importBody reads { importId, options } shared by preview and commit.
func importBody(c *api.Ctx) (string, ImportOptions, error) {
	body, err := c.Object()
	if err != nil {
		return "", ImportOptions{}, err
	}
	id, ok := body["importId"].(string)
	if !ok {
		return "", ImportOptions{}, invalid("importId", "Required")
	}
	opts, err := ParseImportOptions(body["options"])
	return id, opts, err
}

func requireLibrary(c *api.Ctx) (shared.Library, error) {
	q := c.Query()
	if !q.Has("library") {
		return "", invalid("library", "Required")
	}
	switch l := q.Get("library"); l {
	case "reading", "watching":
		return shared.Library(l), nil
	}
	return "", invalid("library", "Invalid enum value")
}

// Register adds the import and export routes (use as api.Options.Extend).
func Register(rt *api.Router, srv *api.Server) {
	s := srv.Store

	rt.HandleLimit(http.MethodPost, "/import/upload", multipartMB, func(c *api.Ctx) (any, error) {
		c.R.Body = http.MaxBytesReader(c.W, c.R.Body, multipartMB)
		mr, err := c.R.MultipartReader()
		if err != nil {
			return nil, &store.HTTPError{Status: 406, Code: "BAD_REQUEST", Message: "the request is not multipart"}
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				return nil, &store.HTTPError{Status: 400, Code: "VALIDATION", Message: "No file uploaded"}
			}
			if err != nil {
				return nil, err
			}
			if part.FileName() == "" {
				continue
			}
			if !reUploadExt.MatchString(part.FileName()) {
				return nil, &store.HTTPError{Status: 400, Code: "VALIDATION", Message: "Unsupported file type"}
			}
			data, err := io.ReadAll(io.LimitReader(part, maxUpload+1))
			if err != nil {
				return nil, err
			}
			if len(data) > maxUpload {
				return nil, &store.HTTPError{Status: 413, Code: "BAD_REQUEST", Message: "request file too large"}
			}
			id, name, err := StageUpload(s, part.FileName(), data)
			if err != nil {
				return nil, err
			}
			c.Status = http.StatusCreated
			return map[string]string{"importId": id, "fileName": name}, nil
		}
	})

	// Parsed rows + mapping + warnings, no writes to the library.
	rt.POST("/import/preview", func(c *api.Ctx) (any, error) {
		id, opts, err := importBody(c)
		if err != nil {
			return nil, err
		}
		return PreviewImport(s, id, opts)
	})
	rt.POST("/import/commit", func(c *api.Ctx) (any, error) {
		id, opts, err := importBody(c)
		if err != nil {
			return nil, err
		}
		return CommitImport(s, id, opts)
	})
	rt.POST("/import/undo/:id", func(c *api.Ctx) (any, error) {
		if err := UndoImport(s, c.Params["id"]); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})

	rt.GET("/export/xlsx", func(c *api.Ctx) (any, error) {
		lib, err := requireLibrary(c)
		if err != nil {
			return nil, err
		}
		b, err := ExportXLSX(s, lib)
		return &api.Download{Name: string(lib) + "-tracker.xlsx", ContentType: xlsxType, Body: b}, err
	})
	rt.GET("/export/csv", func(c *api.Ctx) (any, error) {
		lib, err := requireLibrary(c)
		if err != nil {
			return nil, err
		}
		q, err := search.ParseEntryQuery(c.Query())
		if err != nil {
			return nil, err
		}
		q.Library, q.Limit = string(lib), 1000
		var all []shared.Entry
		for q.Page = 1; ; q.Page++ {
			r := srv.Querier.Query(q)
			all = append(all, r.Items...)
			if len(all) >= r.Total || len(r.Items) == 0 {
				break
			}
		}
		return &api.Download{Name: string(lib) + ".csv", ContentType: csvType, Body: ExportCSV(s, lib, all, true)}, nil
	})
	rt.GET("/export/csv-template", func(c *api.Ctx) (any, error) {
		lib, err := requireLibrary(c)
		if err != nil {
			return nil, err
		}
		return &api.Download{Name: string(lib) + "-template.csv", ContentType: csvType, Body: CSVTemplate(lib)}, nil
	})
	// Full backup zip of config/ + data/ + activity/.
	rt.GET("/export/json", func(c *api.Ctx) (any, error) {
		info, err := store.CreateBackup(s.Dir, "export", 1000)
		if err != nil {
			return nil, err
		}
		files, err := store.ReadBackup(s.Dir, info.Name)
		if err != nil {
			return nil, err
		}
		b, err := ZipFiles(files)
		return &api.Download{Name: info.Name, ContentType: "application/zip", Body: b}, err
	})

	rt.GET("/imports/:id/report", func(c *api.Ctx) (any, error) {
		b, err := ReadReport(s, c.Params["id"])
		if err != nil {
			return nil, err
		}
		return json.RawMessage(b), nil
	})
}

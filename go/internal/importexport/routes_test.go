package importexport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/reeper316099/Lorekeeper/go/internal/api"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

const token = "test-token-123"

type apiEnv struct {
	t  *testing.T
	st *store.Store
	h  http.Handler
}

func newAPI(t *testing.T) *apiEnv {
	t.Helper()
	st := newStore(t)
	srv := api.New(api.Options{Store: st, Token: token, Extend: Register})
	t.Cleanup(srv.Close)
	return &apiEnv{t, st, srv.Handler()}
}

func (e *apiEnv) do(method, url string, body []byte, contentType, tok string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1"+url, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *apiEnv) json(method, url string, payload any) (int, map[string]any) {
	var b []byte
	ct := ""
	if payload != nil {
		b, _ = json.Marshal(payload)
		ct = "application/json"
	}
	rec := e.do(method, url, b, ct, token)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func multipartBody(t *testing.T, field, filename string, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if filename != "" {
		fw, _ := w.CreateFormFile(field, filename)
		fw.Write(data)
	}
	w.WriteField("note", "ignored text field")
	w.Close()
	return buf.Bytes(), w.FormDataContentType()
}

func TestImportAPIUploadPreviewCommitExportUndo(t *testing.T) {
	e := newAPI(t)
	body, ct := multipartBody(t, "file", "w.xlsx", synFile(t, "Watching_Tracker.xlsx"))
	if rec := e.do("POST", "/import/upload", body, ct, ""); rec.Code != 401 {
		t.Fatalf("upload without a token: %d", rec.Code)
	}
	rec := e.do("POST", "/import/upload", body, ct, token)
	if rec.Code != 201 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var up map[string]any
	json.Unmarshal(rec.Body.Bytes(), &up)
	id := up["importId"].(string)

	_, pv := e.json("POST", "/import/preview", map[string]any{"importId": id})
	if pv["counts"].(map[string]any)["create"] != 48.0 {
		t.Fatalf("preview: %v", pv["counts"])
	}
	code, cm := e.json("POST", "/import/commit", map[string]any{"importId": id, "options": map[string]any{"strategy": "fill"}})
	if code != 200 || cm["counts"].(map[string]any)["created"] != 48.0 || cm["undoToken"] != id {
		t.Fatalf("commit: %d %v", code, cm)
	}
	if code, rep := e.json("GET", "/imports/"+id+"/report", nil); code != 200 || rep["importId"] != id {
		t.Errorf("report: %d %v", code, rep)
	}

	x := e.do("GET", "/export/xlsx?library=watching", nil, "", token)
	if x.Code != 200 || !strings.Contains(x.Header().Get("Content-Type"), "spreadsheetml") || !strings.Contains(x.Header().Get("Content-Disposition"), "watching-tracker.xlsx") {
		t.Errorf("xlsx: %d %v", x.Code, x.Header())
	}
	if !bytes.HasPrefix(x.Body.Bytes(), []byte("PK")) {
		t.Error("xlsx is not a zip")
	}
	csv := e.do("GET", "/export/csv?library=watching&status=completed", nil, "", token)
	if n := len(strings.Split(csv.Body.String(), "\r\n")); n != 1+18 {
		t.Errorf("csv lines %d", n)
	}
	if ct := csv.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("csv type %q", ct)
	}
	zipRec := e.do("GET", "/export/json", nil, "", token)
	zr, err := zip.NewReader(bytes.NewReader(zipRec.Body.Bytes()), int64(zipRec.Body.Len()))
	if err != nil || zipRec.Body.Bytes()[0] != 'P' {
		t.Fatalf("json export is not a zip: %v", err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if !strings.Contains(strings.Join(names, " "), "config/categories.json") || strings.Contains(strings.Join(names, " "), "backup-info.json") {
		t.Errorf("zip entries %v", names)
	}
	if tpl := e.do("GET", "/export/csv-template?library=reading", nil, "", token); !strings.Contains(tpl.Body.String(), "Watched on YouTube Up To Chapter") {
		t.Error("template")
	}
	if code, _ := e.json("POST", "/import/undo/"+id, nil); code != 200 || len(e.st.All()) != 0 {
		t.Errorf("undo: %d, %d entries left", code, len(e.st.All()))
	}
	if code, _ := e.json("POST", "/import/undo/..%2F..", nil); code < 400 {
		t.Errorf("path traversal id: %d", code)
	}
	if code, _ := e.json("GET", "/imports/..%2F..%2Fx/report", nil); code != 400 {
		t.Errorf("report with a bad id: %d", code)
	}
	if code, _ := e.json("GET", "/imports/20200101-000000-abcdef/report", nil); code != 404 {
		t.Errorf("report that does not exist: %d", code)
	}
}

func TestImportAPIErrors(t *testing.T) {
	e := newAPI(t)
	check := func(name string, rec *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		var b map[string]map[string]any
		json.Unmarshal(rec.Body.Bytes(), &b)
		if rec.Code != status || b["error"]["code"] != code {
			t.Errorf("%s: %d %s, want %d %s", name, rec.Code, rec.Body.String(), status, code)
		}
	}
	noFile, ct := multipartBody(t, "file", "", nil)
	check("no file", e.do("POST", "/import/upload", noFile, ct, token), 400, "VALIDATION")
	pdf, ct := multipartBody(t, "file", "x.pdf", []byte("%PDF"))
	check("unsupported type", e.do("POST", "/import/upload", pdf, ct, token), 400, "VALIDATION")
	check("not multipart", e.do("POST", "/import/upload", []byte("{}"), "application/json", token), 406, "BAD_REQUEST")

	check("preview without id", e.do("POST", "/import/preview", []byte(`{}`), "application/json", token), 400, "VALIDATION")
	check("preview unknown id", e.do("POST", "/import/preview", []byte(`{"importId":"20200101-000000-abcdef"}`), "application/json", token), 404, "NOT_FOUND")
	check("preview bad id", e.do("POST", "/import/preview", []byte(`{"importId":"x"}`), "application/json", token), 400, "VALIDATION")
	check("bad options", e.do("POST", "/import/commit", []byte(`{"importId":"x","options":{"strategy":"merge"}}`), "application/json", token), 400, "VALIDATION")
	check("export without library", e.do("GET", "/export/xlsx", nil, "", token), 400, "VALIDATION")
	check("export bad library", e.do("GET", "/export/csv?library=books", nil, "", token), 400, "VALIDATION")
	check("csv bad filter", e.do("GET", "/export/csv?library=reading&limit=x", nil, "", token), 400, "VALIDATION")
}

func TestExportCSVPaginatesAndFilters(t *testing.T) {
	e := newAPI(t)
	cat, _ := e.st.CreateCategory("reading", "Big", "")
	var built []shared.Entry
	for i := 0; i < 2300; i++ {
		ent, err := e.st.BuildEntry(map[string]any{"title": fmt.Sprintf("Title %04d", i), "categoryId": cat.ID})
		if err != nil {
			t.Fatal(err)
		}
		built = append(built, ent)
	}
	if err := e.st.InsertEntries(built); err != nil {
		t.Fatal(err)
	}
	rec := e.do("GET", "/export/csv?library=reading", nil, "", token)
	lines := strings.Split(strings.TrimRight(rec.Body.String(), "\r\n"), "\r\n")
	if len(lines) != 2301 {
		t.Errorf("exported %d lines, want header + 2300 (pages of 1000 must be stitched together)", len(lines))
	}
	if !strings.HasPrefix(lines[1], "Title 0000,") || !strings.HasPrefix(lines[2300], "Title 2299,") {
		t.Errorf("order: %q ... %q", lines[1], lines[2300])
	}
	if rec := e.do("GET", "/export/csv?library=reading&q=0007&limit=5", nil, "", token); len(strings.Split(strings.TrimRight(rec.Body.String(), "\r\n"), "\r\n")) != 2 {
		t.Errorf("filtered export: %q", rec.Body.String())
	}
}

func TestExportJSONLeavesAnExportBackup(t *testing.T) {
	e := newAPI(t)
	e.st.CreateCategory("reading", "A", "")
	if rec := e.do("GET", "/export/json", nil, "", token); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	list, _ := e.st.ListBackups()
	if len(list) == 0 {
		t.Error("expected a backup file from the export")
	}
	if _, err := os.Stat(e.st.ImportsDir()); err != nil {
		t.Error(err)
	}
}

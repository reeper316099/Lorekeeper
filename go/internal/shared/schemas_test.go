package shared

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

type schemaCase struct {
	Schema string          `json:"schema"`
	Input  json.RawMessage `json:"input"`
	Ok     bool            `json:"ok"`
	Output any             `json:"output"`
}

func decodeSchema(name string, in []byte) (any, error) {
	switch name {
	case "EntrySchema":
		var v Entry
		return &v, json.Unmarshal(in, &v)
	case "CategorySchema":
		var v Category
		return &v, json.Unmarshal(in, &v)
	case "TagSchema":
		var v Tag
		return &v, json.Unmarshal(in, &v)
	case "ScalesSchema":
		var v Scales
		return &v, json.Unmarshal(in, &v)
	case "SettingsSchema":
		var v Settings
		return &v, json.Unmarshal(in, &v)
	case "MetaSchema":
		var v Meta
		return &v, json.Unmarshal(in, &v)
	case "ActivityRecordSchema":
		var v ActivityRecord
		return &v, json.Unmarshal(in, &v)
	}
	panic("unknown schema " + name)
}

// schemas.json holds Zod's verdict (and parsed output) for valid and invalid documents; Go must
// accept and reject the same ones and produce the same JSON.
func TestSchemasMatchZod(t *testing.T) {
	raw, err := os.ReadFile("testdata/schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []schemaCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 30 {
		t.Fatalf("only %d cases", len(cases))
	}
	for i, c := range cases {
		got, err := decodeSchema(c.Schema, c.Input)
		if !c.Ok {
			if err == nil {
				t.Errorf("case %d %s: Zod rejects %s but Go accepted it", i, c.Schema, c.Input)
			}
			continue
		}
		if err != nil {
			t.Errorf("case %d %s: Zod accepts %s but Go failed: %v", i, c.Schema, c.Input, err)
			continue
		}
		if g := roundTrip(t, got); !reflect.DeepEqual(g, c.Output) {
			t.Errorf("case %d %s output differs\n  zod: %#v\n  go:  %#v", i, c.Schema, c.Output, g)
		}
	}
}

func TestValidationErrorNamesField(t *testing.T) {
	var e Entry
	err := json.Unmarshal([]byte(`{"id":"a","library":"reading","categoryId":"c","title":"T","score":11}`), &e)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Path != "score" {
		t.Fatalf("got %v", err)
	}
}

func TestFileHelpers(t *testing.T) {
	entries, err := ParseEntryFile([]byte(`[{"id":"a","library":"watching","categoryId":"c","title":"T"}]`))
	if err != nil || len(entries) != 1 || entries[0].MediaType != "unknown" || entries[0].Tags == nil {
		t.Fatalf("entries %+v err %v", entries, err)
	}
	if _, err := ParseEntryFile([]byte(`{"not":"an array"}`)); err == nil {
		t.Error("object must be rejected")
	}
	if _, err := ParseEntryFile([]byte(`[{"id":"","library":"watching","categoryId":"c","title":"T"}]`)); err == nil {
		t.Error("invalid entry must be rejected")
	}
	if got, err := ParseEntryFile([]byte(`[]`)); err != nil || got == nil || len(got) != 0 {
		t.Errorf("empty file: %v %v", got, err)
	}
	if _, err := ParseCategoriesFile([]byte(`[{"id":"c","library":"reading","name":"N","slug":"n","color":"#112233","order":0}]`)); err != nil {
		t.Error(err)
	}
	if _, err := ParseTagsFile([]byte(`[{"id":"t","name":"N"}]`)); err != nil {
		t.Error(err)
	}
}

func TestDefaultsAreFresh(t *testing.T) {
	a, b := DefaultSettings(), DefaultSettings()
	a.LabelOverrides["x"] = "y"
	if _, ok := b.LabelOverrides["x"]; ok {
		t.Error("DefaultSettings shares its map between calls")
	}
	s1, s2 := DefaultScales(), DefaultScales()
	s1.Reading.StatusLabels[StatusPlanned] = "changed"
	if s2.Reading.StatusLabels[StatusPlanned] == "changed" {
		t.Error("DefaultScales shares its map between calls")
	}
}

// The store writes JSON with sorted keys in a single pass, which only works when every struct
// declares its fields in sorted JSON-key order.
func TestJSONFieldsAreSorted(t *testing.T) {
	types := []any{
		Entry{}, Progress{}, Link{}, ImportRef{}, Notes{}, Category{}, Tag{}, Scale{}, Scales{},
		Settings{}, SavedView{}, BackupSettings{}, Meta{},
	}
	for _, v := range types {
		rt := reflect.TypeOf(v)
		prev := ""
		for i := 0; i < rt.NumField(); i++ {
			name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
			if name <= prev {
				t.Errorf("%s: field %q is not after %q (keep JSON keys sorted)", rt.Name(), name, prev)
			}
			prev = name
		}
	}
}

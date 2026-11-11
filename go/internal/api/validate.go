package api

import (
	"math"
	"regexp"
)

// Small helpers that do what the Zod schemas did in the Fastify routes.

var reColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type fields struct {
	m      map[string]any
	issues []issue
}

func (f *fields) fail(path, msg string) { f.issues = append(f.issues, issue{[]string{path}, msg}) }

func (f *fields) err() error {
	if len(f.issues) == 0 {
		return nil
	}
	return &valError{f.issues}
}

func (f *fields) has(k string) bool { _, ok := f.m[k]; return ok && f.m[k] != nil }

// str reads a string field. minLen 1 makes it non-empty; required makes absence an error.
func (f *fields) str(k string, required bool, minLen int) string {
	v, ok := f.m[k]
	if !ok || v == nil {
		if required {
			f.fail(k, "Required")
		}
		return ""
	}
	s, isStr := v.(string)
	if !isStr {
		f.fail(k, "Expected string")
		return ""
	}
	if len(s) < minLen {
		f.fail(k, "String must contain at least 1 character(s)")
	}
	return s
}

func (f *fields) strPtr(k string, minLen int) *string {
	if !f.has(k) {
		return nil
	}
	s := f.str(k, false, minLen)
	return &s
}

func (f *fields) color(k string) *string {
	s := f.strPtr(k, 0)
	if s != nil && !reColor.MatchString(*s) {
		f.fail(k, "Invalid")
	}
	return s
}

func (f *fields) oneOf(k string, required bool, allowed ...string) string {
	s := f.str(k, required, 0)
	if s == "" {
		return ""
	}
	for _, a := range allowed {
		if s == a {
			return s
		}
	}
	f.fail(k, "Invalid enum value")
	return ""
}

func (f *fields) integer(k string) (int, bool) {
	v, ok := f.m[k]
	if !ok || v == nil {
		return 0, false
	}
	n, isNum := v.(float64)
	if !isNum || n != math.Trunc(n) {
		f.fail(k, "Expected integer")
		return 0, false
	}
	return int(n), true
}

func (f *fields) boolPtr(k string) *bool {
	v, ok := f.m[k]
	if !ok || v == nil {
		return nil
	}
	b, isBool := v.(bool)
	if !isBool {
		f.fail(k, "Expected boolean")
		return nil
	}
	return &b
}

func (f *fields) strings(k string, required bool, minItems int) []string {
	v, ok := f.m[k]
	if !ok || v == nil {
		if required {
			f.fail(k, "Required")
		}
		return nil
	}
	arr, isArr := v.([]any)
	if !isArr {
		f.fail(k, "Expected array")
		return nil
	}
	if len(arr) < minItems {
		f.fail(k, "Array must contain at least 1 element(s)")
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, isStr := x.(string)
		if !isStr {
			f.fail(k, "Expected string")
			return nil
		}
		out = append(out, s)
	}
	return out
}

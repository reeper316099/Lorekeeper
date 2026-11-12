package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/reeper316099/Lorekeeper/go/internal/search"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

type issue struct {
	Path    []string `json:"path"`
	Message string   `json:"message"`
}

// valError is a request that does not match the schema (HTTP 400, code VALIDATION).
type valError struct{ issues []issue }

func (e *valError) Error() string {
	if len(e.issues) == 0 {
		return "Invalid request"
	}
	return fmt.Sprintf("%v: %s", e.issues[0].Path, e.issues[0].Message)
}

func invalidf(path, format string, args ...any) *valError {
	var p []string
	if path != "" {
		p = []string{path}
	}
	return &valError{[]issue{{p, fmt.Sprintf(format, args...)}}}
}

// badRequest is a malformed request (HTTP 400, code BAD_REQUEST).
type badRequest struct{ msg string }

func (e *badRequest) Error() string { return e.msg }

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details"`
}

// classify maps any error to the uniform response: {"error":{"code","message","details"}}.
func classify(err error) (int, errorBody) {
	var (
		he  *store.HTTPError
		ve  *valError
		sv  *shared.ValidationError
		qe  *search.QueryError
		br  *badRequest
		ute *json.UnmarshalTypeError
		mbe *http.MaxBytesError
	)
	switch {
	case errors.As(err, &he):
		return he.Status, errorBody{he.Code, he.Message, he.Details}
	case errors.As(err, &ve):
		return 400, errorBody{"VALIDATION", "Invalid request", ve.issues}
	case errors.As(err, &sv):
		return 400, errorBody{"VALIDATION", "Invalid request", []issue{{[]string{sv.Path}, sv.Msg}}}
	case errors.As(err, &qe):
		return 400, errorBody{"VALIDATION", "Invalid request", []issue{{[]string{qe.Path}, qe.Msg}}}
	case errors.As(err, &ute):
		return 400, errorBody{"VALIDATION", "Invalid request", []issue{{[]string{ute.Field}, "Expected " + ute.Type.String()}}}
	case errors.As(err, &br):
		return 400, errorBody{"BAD_REQUEST", br.msg, nil}
	case errors.As(err, &mbe):
		return 413, errorBody{"BAD_REQUEST", "Request body is too large", nil}
	}
	return 500, errorBody{"INTERNAL", "Internal error", nil}
}

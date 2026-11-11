package shared

import (
	"encoding/json"
	"strings"
)

var statusAliases = map[string]Status{
	"finished":      StatusCompleted,
	"complete":      StatusCompleted,
	"completed":     StatusCompleted,
	"done":          StatusCompleted,
	"reading":       StatusInProgress,
	"watching":      StatusInProgress,
	"current":       StatusInProgress,
	"in progress":   StatusInProgress,
	"plan to read":  StatusPlanned,
	"plan to watch": StatusPlanned,
	"planned":       StatusPlanned,
	"planning":      StatusPlanned,
	"backlog":       StatusPlanned,
	"on hold":       StatusOnHold,
	"onhold":        StatusOnHold,
	"paused":        StatusOnHold,
	"dropped":       StatusDropped,
	"re-reading":    StatusRepeating,
	"re-watching":   StatusRepeating,
	"rereading":     StatusRepeating,
	"rewatching":    StatusRepeating,
	"repeating":     StatusRepeating,
}

// StatusParse is the result of ParseStatus: blank -> planned (Blank so the importer can warn),
// unknown -> Ok=false with the raw text.
type StatusParse struct {
	Ok     bool
	Status Status
	Blank  bool
	Raw    string
}

// MarshalJSON matches the TypeScript union: {ok,status,blank} or {ok:false,raw}.
func (p StatusParse) MarshalJSON() ([]byte, error) {
	if !p.Ok {
		return json.Marshal(map[string]any{"ok": false, "raw": p.Raw})
	}
	return json.Marshal(map[string]any{"ok": true, "status": p.Status, "blank": p.Blank})
}

func ParseStatus(input any) StatusParse {
	raw := ""
	if input != nil {
		raw = jsTrim(jsString(input))
	}
	if raw == "" {
		return StatusParse{Ok: true, Status: StatusPlanned, Blank: true}
	}
	if s, ok := statusAliases[jsCollapse(strings.ToLower(raw))]; ok {
		return StatusParse{Ok: true, Status: s}
	}
	return StatusParse{Raw: raw}
}

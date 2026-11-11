package shared

import (
	"regexp"
	"strconv"
)

var (
	reProgNum      = regexp.MustCompile(`^\d+(\.\d+)?$`)
	reProgSeasonEp = regexp.MustCompile(`(?i)^s(?:eason)?[` + jsWS + `]*(\d+)[` + jsWS + `]*[,:-]?[` + jsWS + `]*e(?:p(?:isode)?)?[` + jsWS + `]*(\d+)$`)
	reProgComplete = regexp.MustCompile(`(?i)^(done|completed?|finished\??|complete)$`)
	reProgEnd      = regexp.MustCompile(`(?i)^end(ed)?$`)
	reProgEndOf    = regexp.MustCompile(`(?i)^end(?:ed)?[` + jsWS + `]+of[` + jsWS + `]+(?:s|season[` + jsWS + `]*)(\d+)$`)
	reProgBegin    = regexp.MustCompile(`(?i)^(?:beginning|start|begin)(?:[` + jsWS + `]+of)?[` + jsWS + `]+(?:s|season[` + jsWS + `]*)(\d+)$`)
	reProgGuess    = regexp.MustCompile(`^(\d+)[` + jsWS + `]*\?$`)
	reIntOnly      = regexp.MustCompile(`^\d+$`)
)

func num(s string) *float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return &f
}

func kind(k ProgressKind) *ProgressKind { return &k }

// ParseProgress parses a "Currently on ..." / "Watched until ..." cell. The raw text is always kept.
func ParseProgress(input any) Progress {
	if input == nil {
		return EmptyProgress()
	}
	raw := jsTrim(jsString(input))
	if raw == "" {
		return EmptyProgress()
	}
	text := Progress{Raw: &raw, Kind: kind(ProgressText)}

	if reProgNum.MatchString(raw) {
		text.Number, text.Kind = num(raw), kind(ProgressNumber)
		return text
	}
	if m := reProgSeasonEp.FindStringSubmatch(raw); m != nil {
		return Progress{Raw: &raw, Season: num(m[1]), Number: num(m[2]), Kind: kind(ProgressNumber)}
	}
	if reProgComplete.MatchString(raw) {
		text.Kind = kind(ProgressComplete)
		return text
	}
	if reProgEnd.MatchString(raw) {
		text.Kind = kind(ProgressEnd)
		return text
	}
	if m := reProgEndOf.FindStringSubmatch(raw); m != nil {
		text.Season, text.Kind = num(m[1]), kind(ProgressEnd)
		return text
	}
	if m := reProgBegin.FindStringSubmatch(raw); m != nil {
		text.Season, text.Kind = num(m[1]), kind(ProgressStart)
		return text
	}
	if m := reProgGuess.FindStringSubmatch(raw); m != nil {
		text.Number = num(m[1])
		return text
	}
	return text
}

// ProgressNumberOf is the numeric position used for "behind by N" math (nil when unknown).
func ProgressNumberOf(p Progress) *float64 { return p.Number }

// BehindBy is how far the consumed-up-to marker lags/leads the current position, when both are numeric.
func BehindBy(current, consumedUpTo Progress) *float64 {
	if current.Number == nil || consumedUpTo.Number == nil {
		return nil
	}
	if !sameFloat(current.Season, consumedUpTo.Season) {
		return nil
	}
	d := *current.Number - *consumedUpTo.Number
	return &d
}

func sameFloat(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ProgressCell is the value to write back into a spreadsheet cell: numbers as numbers (float64),
// text verbatim (string), nil when empty.
func ProgressCell(p Progress) any {
	if p.Raw == nil {
		return nil
	}
	if p.Kind != nil && *p.Kind == ProgressNumber && p.Season == nil && reIntOnly.MatchString(*p.Raw) {
		f, _ := strconv.ParseFloat(*p.Raw, 64)
		return f
	}
	return *p.Raw
}

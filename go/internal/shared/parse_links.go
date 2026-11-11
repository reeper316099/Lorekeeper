package shared

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	reURL        = regexp.MustCompile(`(?i)https?://[^` + jsWS + `<>"]+`)
	reBareDomain = regexp.MustCompile(`(?i)^(?:www\.)?[a-z0-9-]+(?:\.[a-z0-9-]+)*\.[a-z]{2,}(?::\d+)?(?:[/?#]` + reNotWS + `*)?$`)
	reDate       = regexp.MustCompile(`\((\d{1,2}/\d{1,2}/\d{2,4})\)`)
	reChannel    = regexp.MustCompile(`(?i)\bBy:[` + jsWS + `]*(` + reNotNL + `+?)[` + jsWS + `]*$`)
	reTrailPunct = regexp.MustCompile(`[),.;]+$`)
)

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func kindFor(u *string, hint LinkKind) LinkKind {
	if u != nil {
		host := hostOf(*u)
		if host != "" && (host == "youtu.be" || strings.HasSuffix(host, "youtube.com")) {
			return LinkYouTube
		}
		if host != "" && strings.HasSuffix(host, "anilist.co") {
			return LinkAniList
		}
		if hint == LinkYouTube {
			return LinkYouTube
		}
		return LinkSite
	}
	return hint
}

func firstGroup(re *regexp.Regexp, s string) *string {
	if m := re.FindStringSubmatch(s); m != nil {
		return &m[1]
	}
	return nil
}

// ParseLink parses a link cell. Returns nil for empty cells. Raw is always preserved.
func ParseLink(input any, hint LinkKind) *Link {
	if input == nil {
		return nil
	}
	raw := jsTrim(jsString(input))
	if raw == "" {
		return nil
	}
	date := firstGroup(reDate, raw)
	channel := firstGroup(reChannel, raw)

	if embedded := reURL.FindString(raw); embedded != "" {
		u := reTrailPunct.ReplaceAllString(embedded, "")
		var label *string
		if raw != embedded {
			label = &raw
		}
		return &Link{Kind: kindFor(&u, hint), URL: &u, Label: label, Raw: &raw, Channel: channel, Date: date}
	}
	if !reHasWS.MatchString(raw) && reBareDomain.MatchString(raw) {
		u := "https://" + raw
		return &Link{Kind: kindFor(&u, hint), URL: &u, Raw: &raw, Channel: channel, Date: date}
	}
	return &Link{Kind: hint, Label: &raw, Raw: &raw, Channel: channel, Date: date}
}

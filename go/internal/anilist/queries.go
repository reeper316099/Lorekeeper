// Package anilist is the Go port of packages/server/src/anilist: a rate-limited, cached,
// cancellable AniList GraphQL client, the title matcher, the review queue, background jobs, the
// account-list import, and the HTTP routes. v1 never logs in (no OAuth).
package anilist

const media = `
  id idMal type format status episodes chapters volumes countryOfOrigin seasonYear averageScore isAdult siteUrl
  title { romaji english native }
  synonyms
  startDate { year }
  description(asHtml: false)
  coverImage { large extraLarge }
  bannerImage
  genres
  tags { name rank isMediaSpoiler }
`

const SearchQuery = `
query ($search: String, $type: MediaType, $country: CountryCode, $perPage: Int) {
  Page(page: 1, perPage: $perPage) {
    media(search: $search, type: $type, countryOfOrigin: $country, sort: SEARCH_MATCH) { ` + media + ` }
  }
}`

// SearchQueryAnyCountry is used when there is no country filter: AniList treats an explicit
// countryOfOrigin: null as a filter and returns nothing, so the argument has to be left out.
const SearchQueryAnyCountry = `
query ($search: String, $type: MediaType, $perPage: Int) {
  Page(page: 1, perPage: $perPage) {
    media(search: $search, type: $type, sort: SEARCH_MATCH) { ` + media + ` }
  }
}`

const ByIDQuery = `query ($id: Int) { Media(id: $id) { ` + media + ` } }`

const ListQuery = `
query ($userName: String, $type: MediaType, $chunk: Int) {
  MediaListCollection(userName: $userName, type: $type, chunk: $chunk, perChunk: 500) {
    hasNextChunk
    lists {
      name isCustomList
      entries {
        status progress progressVolumes repeat notes
        score(format: POINT_10)
        startedAt { year month day }
        completedAt { year month day }
        media { ` + media + ` }
      }
    }
  }
}`

type Title struct {
	English *string `json:"english"`
	Native  *string `json:"native"`
	Romaji  *string `json:"romaji"`
}

type StartDate struct {
	Year *int `json:"year"`
}

type CoverImage struct {
	ExtraLarge *string `json:"extraLarge"`
	Large      *string `json:"large"`
}

type MediaTag struct {
	IsMediaSpoiler bool   `json:"isMediaSpoiler"`
	Name           string `json:"name"`
	Rank           *int   `json:"rank"`
}

// Media is one AniList title (fields are the ones the app asks for; keys sorted for stable files).
type Media struct {
	AverageScore    *int        `json:"averageScore"`
	BannerImage     *string     `json:"bannerImage"`
	Chapters        *int        `json:"chapters"`
	CountryOfOrigin *string     `json:"countryOfOrigin"`
	CoverImage      *CoverImage `json:"coverImage"`
	Description     *string     `json:"description"`
	Episodes        *int        `json:"episodes"`
	Format          *string     `json:"format"`
	Genres          []string    `json:"genres"`
	ID              int         `json:"id"`
	IDMal           *int        `json:"idMal"`
	IsAdult         bool        `json:"isAdult"`
	SeasonYear      *int        `json:"seasonYear"`
	SiteURL         *string     `json:"siteUrl"`
	StartDate       *StartDate  `json:"startDate"`
	Status          *string     `json:"status"`
	Synonyms        []string    `json:"synonyms"`
	Tags            []MediaTag  `json:"tags"`
	Title           Title       `json:"title"`
	Type            string      `json:"type"` // ANIME | MANGA
	Volumes         *int        `json:"volumes"`
}

type Date struct {
	Day   *int `json:"day"`
	Month *int `json:"month"`
	Year  *int `json:"year"`
}

// ListEntry is one row of an AniList media list.
type ListEntry struct {
	CompletedAt     *Date    `json:"completedAt"`
	Media           Media    `json:"media"`
	Notes           *string  `json:"notes"`
	Progress        *float64 `json:"progress"`
	ProgressVolumes *float64 `json:"progressVolumes"`
	Repeat          *int     `json:"repeat"`
	Score           *float64 `json:"score"`
	StartedAt       *Date    `json:"startedAt"`
	Status          string   `json:"status"` // CURRENT | PLANNING | COMPLETED | DROPPED | PAUSED | REPEATING
}

type listCollection struct {
	MediaListCollection struct {
		HasNextChunk bool `json:"hasNextChunk"`
		Lists        []struct {
			Entries      []ListEntry `json:"entries"`
			IsCustomList bool        `json:"isCustomList"`
			Name         string      `json:"name"`
		} `json:"lists"`
	} `json:"MediaListCollection"`
}

func str(s string) *string { return &s }

# AniList integration (v1: no login)

Uses the public GraphQL API at `https://graphql.anilist.co`. v1 never asks for an account: it matches your titles to
AniList, caches metadata and covers locally, and can import a public list by username. Two-way sync (OAuth) is v2.

Verified against the live schema and the official rate-limit docs when this was written:

- **Rate limit**: the docs say 90 requests/minute, and note the API is currently _degraded to 30/minute_ (the live
  `X-RateLimit-Limit` header confirmed 30). The client defaults to **30/min**, never exceeds the lower of that and
  whatever the headers report, queues callers FIFO, and honours `429` + `Retry-After` / `X-RateLimit-Reset` (the docs
  describe a one-minute timeout). Raise it with the `maxPerMinute` client option once AniList restores 90.
- **Retries**: `5xx` and network errors back off 1s, 2s, 4s; GraphQL / `4xx` errors are not retried.
- **Offline**: no network means `OFFLINE` (HTTP 503). The UI keeps working; cached data (even past its TTL) is served
  instead of failing, and matching/import jobs stop with an "unreachable" state.
- **Cache**: responses live under `anilist/cache/` for 7 days (`media-<id>.json`, `search-<hash>.json`, images as
  `img-<hash>.<ext>`). Searches also prime the per-media cache so confirming a candidate costs no extra request.
- **Images**: only `https://s4.anilist.co` is allowed. Candidate artwork is fetched through our own proxy
  (`/api/v1/anilist/image`) and cached on disk; confirmed matches get `covers/<anilistId>.<ext>`. The page CSP stays
  `img-src 'self'`.

## Matching

| Library / category                          | Search                                                     |
| ------------------------------------------- | ---------------------------------------------------------- |
| Reading, media type manhwa / manhua / manga | `MANGA`, country `KR` / `CN` / `JP`                        |
| Reading, unknown type                       | `MANGA`, any country (the type is inferred from the match) |
| Donghua categories                          | `ANIME`, country `CN`                                      |
| Anime / other watching categories           | `ANIME`                                                    |
| TV Shows, Cartoons, K-Drama, Documentary    | not linkable: marked "Not on AniList", never searched      |

Candidates are ranked by title similarity against romaji, English, native and synonyms (plus the entry's own alternate
titles), nudged by country and format. A match is applied **automatically only** when the best candidate is at least the
confidence threshold (Settings, default 92%) _and_ at least 8 points ahead of the runner-up. Everything else waits in the
review queue: confirm a candidate, search again, paste an AniList URL or ID, "None of these", or "Never match".
Confirmed matches are locked and never re-matched; "never" and "no match" are remembered across runs.

On a match the entry gets `external.*` (id, MAL id, format, country, status, year, score, synopsis), alternate titles, the
cached cover, `totalUnits` (only if you had none), an AniList link and, if its type was unknown, a media type. AniList
genres and tags are only offered as **suggested tags** (spoilers excluded) behind an explicit button; your own tags are
never overwritten.

## Import from an account

Enter a public username, choose which category anime/manga land in, and pick a duplicate strategy (same as the spreadsheet
importer: fill empty fields, skip, overwrite, keep both). Status, progress, notes, dates and the 10-point score
(`score(format: POINT_10)`) are mapped; titles are matched to existing entries by AniList id or any known title. The preview
writes nothing; the commit downloads covers (cancellable, nothing is written if you cancel), takes a snapshot first and
can be undone like a spreadsheet import. Custom lists are ignored because they repeat entries from the status lists.
Private lists cannot be read without logging in.

## Files

`anilist/match-queue.json` holds the review queue; `anilist/cache/`, `covers/` hold cached data. No entry content is ever
logged. `LOREKEEPER_ANILIST_ENDPOINT` points the client at a local mock for development and tests.

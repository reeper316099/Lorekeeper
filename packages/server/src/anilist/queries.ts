const MEDIA = `
  id idMal type format status episodes chapters volumes countryOfOrigin seasonYear averageScore isAdult siteUrl
  title { romaji english native }
  synonyms
  startDate { year }
  description(asHtml: false)
  coverImage { large extraLarge }
  bannerImage
  genres
  tags { name rank isMediaSpoiler }
`;

export const SEARCH_QUERY = `
query ($search: String, $type: MediaType, $country: CountryCode, $perPage: Int) {
  Page(page: 1, perPage: $perPage) {
    media(search: $search, type: $type, countryOfOrigin: $country, sort: SEARCH_MATCH) { ${MEDIA} }
  }
}`;

export const BY_ID_QUERY = `query ($id: Int) { Media(id: $id) { ${MEDIA} } }`;

export const LIST_QUERY = `
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
        media { ${MEDIA} }
      }
    }
  }
}`;

export interface AniMedia {
  id: number;
  idMal: number | null;
  type: 'ANIME' | 'MANGA';
  format: string | null;
  status: string | null;
  episodes: number | null;
  chapters: number | null;
  volumes: number | null;
  countryOfOrigin: string | null;
  seasonYear: number | null;
  averageScore: number | null;
  isAdult: boolean;
  siteUrl: string | null;
  title: { romaji: string | null; english: string | null; native: string | null };
  synonyms: string[];
  startDate: { year: number | null } | null;
  description: string | null;
  coverImage: { large: string | null; extraLarge: string | null } | null;
  bannerImage: string | null;
  genres: string[];
  tags: { name: string; rank: number | null; isMediaSpoiler: boolean }[];
}

export interface AniDate {
  year: number | null;
  month: number | null;
  day: number | null;
}
export interface AniListEntry {
  status: 'CURRENT' | 'PLANNING' | 'COMPLETED' | 'DROPPED' | 'PAUSED' | 'REPEATING';
  progress: number | null;
  progressVolumes: number | null;
  repeat: number | null;
  notes: string | null;
  score: number | null;
  startedAt: AniDate | null;
  completedAt: AniDate | null;
  media: AniMedia;
}
export interface ListCollection {
  MediaListCollection: {
    hasNextChunk: boolean;
    lists: { name: string; isCustomList: boolean; entries: AniListEntry[] }[];
  };
}

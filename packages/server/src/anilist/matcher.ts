import {
  isNearDuplicateTitle,
  normalizeTitle,
  titleSimilarity,
  type Category,
  type Entry,
} from '@lorekeeper/shared';
import type { AniMedia } from './queries.js';

export interface SearchPlan {
  linkable: boolean;
  reason?: string;
  type?: 'ANIME' | 'MANGA';
  country?: 'KR' | 'CN' | 'JP' | null;
}

export interface Candidate {
  id: number;
  title: string;
  titles: string[];
  year: number | null;
  format: string | null;
  type: 'ANIME' | 'MANGA';
  country: string | null;
  chapters: number | null;
  episodes: number | null;
  cover: string | null;
  siteUrl: string | null;
  confidence: number;
}

const UNLINKABLE: Record<string, string> = {
  tv: 'TV shows are not on AniList',
  cartoon: 'Cartoons are not on AniList',
  kdrama: 'K-dramas are not on AniList',
  documentary: 'Documentaries are not on AniList',
};

/** Which AniList search (if any) fits this entry. */
export function planSearch(
  entry: Pick<Entry, 'library' | 'mediaType'>,
  _category?: Pick<Category, 'name'>,
): SearchPlan {
  if (entry.library === 'reading') {
    const country =
      entry.mediaType === 'manhwa'
        ? 'KR'
        : entry.mediaType === 'manhua'
          ? 'CN'
          : entry.mediaType === 'manga'
            ? 'JP'
            : null;
    return { linkable: true, type: 'MANGA', country };
  }
  if (UNLINKABLE[entry.mediaType]) return { linkable: false, reason: UNLINKABLE[entry.mediaType] };
  if (entry.mediaType === 'donghua') return { linkable: true, type: 'ANIME', country: 'CN' };
  return { linkable: true, type: 'ANIME', country: null };
}

export const mediaTitles = (m: AniMedia): string[] =>
  [m.title.romaji, m.title.english, m.title.native, ...m.synonyms].filter(
    (t): t is string => !!t && t.trim() !== '',
  );

/** 0..1 confidence that `media` is the title the user tracks. */
export function scoreCandidate(
  entry: Pick<Entry, 'title' | 'altTitles'>,
  media: AniMedia,
  plan: SearchPlan,
): number {
  const mine = [entry.title, ...entry.altTitles];
  let best = 0;
  for (const a of mine) {
    for (const b of mediaTitles(media)) {
      let s = titleSimilarity(a, b);
      if (normalizeTitle(a) === normalizeTitle(b)) s = 1;
      else if (isNearDuplicateTitle(a, b)) s = Math.max(s, 0.8);
      best = Math.max(best, s);
    }
  }
  let score = best;
  if (plan.country) score += media.countryOfOrigin === plan.country ? 0.04 : -0.15;
  if (media.format === 'MUSIC') score -= 0.2;
  if (media.format === 'NOVEL' && plan.type === 'MANGA') score -= 0.12;
  if (media.format === 'SPECIAL' || media.format === 'TV_SHORT') score -= 0.04;
  return Math.max(0, Math.min(1, score));
}

export function toCandidate(
  entry: Pick<Entry, 'title' | 'altTitles'>,
  media: AniMedia,
  plan: SearchPlan,
): Candidate {
  return {
    id: media.id,
    title: media.title.romaji ?? media.title.english ?? media.title.native ?? `#${media.id}`,
    titles: mediaTitles(media),
    year: media.seasonYear ?? media.startDate?.year ?? null,
    format: media.format,
    type: media.type,
    country: media.countryOfOrigin,
    chapters: media.chapters,
    episodes: media.episodes,
    cover: media.coverImage?.large ?? null,
    siteUrl: media.siteUrl,
    confidence: Math.round(scoreCandidate(entry, media, plan) * 1000) / 1000,
  };
}

export function rank(
  entry: Pick<Entry, 'title' | 'altTitles'>,
  media: AniMedia[],
  plan: SearchPlan,
  limit = 5,
): Candidate[] {
  return media
    .map((m) => toCandidate(entry, m, plan))
    .sort((a, b) => b.confidence - a.confidence)
    .slice(0, limit);
}

/** Only confirm automatically when the best candidate is clearly ahead of the rest. */
export function shouldAutoConfirm(
  candidates: Candidate[],
  threshold: number,
  margin = 0.08,
): boolean {
  const [first, second] = candidates;
  if (!first || first.confidence < threshold) return false;
  return !second || first.confidence - second.confidence >= margin;
}

/** Accepts "12345", "anilist.co/anime/12345/Some-Title" or a full URL. */
export function parseAniListRef(input: string): { id: number; type?: 'ANIME' | 'MANGA' } | null {
  const t = input.trim();
  if (/^\d+$/.test(t)) return { id: Number(t) };
  const m = /anilist\.co\/(anime|manga)\/(\d+)/i.exec(t);
  return m ? { id: Number(m[2]), type: m[1]!.toLowerCase() === 'anime' ? 'ANIME' : 'MANGA' } : null;
}

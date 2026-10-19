import type { Library } from '@lorekeeper/shared';

export type Field =
  | 'title'
  | 'status'
  | 'score'
  | 'tags'
  | 'season'
  | 'current'
  | 'consumed'
  | 'youtubeLink'
  | 'link'
  | 'sources'
  | 'general'
  | 'extra1'
  | 'extra2'
  | 'otherLinks'
  | 'category';

export const FIELDS: Field[] = [
  'title',
  'status',
  'score',
  'tags',
  'season',
  'current',
  'consumed',
  'youtubeLink',
  'link',
  'sources',
  'general',
  'extra1',
  'extra2',
  'otherLinks',
  'category',
];

/** Case/whitespace/punctuation-insensitive key: "Score (1–10)" → "score110". */
export function headerKey(h: unknown): string {
  return String(h ?? '')
    .normalize('NFKC')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '');
}

export function matchHeader(h: unknown): Field | null {
  const k = headerKey(h);
  if (!k) return null;
  if (k === 'title' || k === 'name') return 'title';
  if (k === 'status') return 'status';
  if (k.startsWith('score') || k === 'rating') return 'score';
  if (['genrestags', 'genres', 'genre', 'tags', 'tag'].includes(k)) return 'tags';
  if (k === 'season') return 'season';
  if (k.startsWith('watched')) return 'consumed';
  if (k.startsWith('currentlyon') || k.startsWith('current')) return 'current';
  if (k.includes('youtube') && k.includes('link')) return 'youtubeLink';
  if (k === 'link' || k === 'url') return 'link';
  if (['readingmethod', 'watchplatform', 'platform', 'source', 'sources'].includes(k))
    return 'sources';
  if (k === 'generalnotes' || k === 'notes') return 'general';
  if (k === 'extranotes1') return 'extra1';
  if (k === 'extranotes2') return 'extra2';
  if (k === 'otherlinks' || k === 'otherlink') return 'otherLinks';
  if (k === 'category') return 'category';
  return null;
}

export function detectLibrary(headers: unknown[]): Library | null {
  const keys = headers.map(headerKey);
  if (keys.some((k) => k.includes('chapter'))) return 'reading';
  if (keys.some((k) => k.includes('episode'))) return 'watching';
  return null;
}

export interface Layout {
  headers: string[];
  fields: Field[];
  widths: number[];
}

export const LAYOUTS: Record<Library, Layout> = {
  reading: {
    headers: [
      'Title',
      'Status',
      'Score (1–10)',
      'Genres / Tags',
      'Currently on Chapter',
      'Watched on YouTube Up To Chapter',
      'YouTube Link',
      'Reading Method',
      'General Notes',
      'Extra Notes 1',
      'Extra Notes 2',
      'Other Links',
    ],
    fields: [
      'title',
      'status',
      'score',
      'tags',
      'current',
      'consumed',
      'youtubeLink',
      'sources',
      'general',
      'extra1',
      'extra2',
      'otherLinks',
    ],
    widths: [34, 14, 16, 34, 20, 28, 40, 18, 40, 40, 30, 36],
  },
  watching: {
    headers: [
      'Title',
      'Status',
      'Score (1–10)',
      'Genres / Tags',
      'Season',
      'Currently on Episode',
      'Watched Until Episode',
      'Watch Platform',
      'Link',
      'General Notes',
      'Extra Notes 1',
      'Extra Notes 2',
    ],
    fields: [
      'title',
      'status',
      'score',
      'tags',
      'season',
      'current',
      'consumed',
      'sources',
      'link',
      'general',
      'extra1',
      'extra2',
    ],
    widths: [34, 14, 16, 34, 12, 20, 22, 18, 40, 40, 40, 30],
  },
};

/** Single source of truth for the product name. Rename here only. */
export const APP_NAME = 'Lorekeeper';
export const APP_SLUG = 'lorekeeper';
export const SCHEMA_VERSION = 1;

export const LIBRARIES = ['reading', 'watching'] as const;
export const STATUSES = [
  'planned',
  'in_progress',
  'completed',
  'on_hold',
  'dropped',
  'repeating',
] as const;
export const MEDIA_TYPES = [
  'manga',
  'manhwa',
  'manhua',
  'novel',
  'anime',
  'donghua',
  'tv',
  'cartoon',
  'kdrama',
  'documentary',
  'other',
  'unknown',
] as const;

export const SCORE_VALUES = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10] as const;

/** Score label sets found in the original workbooks. */
export const SCORE_LABEL_SETS = {
  readingMain: [
    'Awful',
    'Terrible',
    'Bad',
    'Below Average',
    'Average',
    'Above Average',
    'Good',
    'Great',
    'Amazing',
    'Masterpiece',
  ],
  readingNewer: [
    'Trash',
    'Very Bad',
    'Bad',
    'Below Average',
    'Mid',
    'Decent',
    'Good',
    'Great',
    'Amazing',
    'Masterpiece',
  ],
  watching: [
    'Terrible',
    'Bad',
    'Poor',
    'Mediocre',
    'Average',
    'Decent',
    'Good',
    'Great',
    'Excellent',
    'Masterpiece',
  ],
} as const;

export const SEED_CATEGORIES: ReadonlyArray<{
  library: (typeof LIBRARIES)[number];
  name: string;
  color: string;
}> = [
  { library: 'reading', name: 'Romance', color: '#8B2252' },
  { library: 'reading', name: 'Isekai', color: '#4A148C' },
  { library: 'reading', name: 'Action', color: '#B71C1C' },
  { library: 'reading', name: 'Fantasy', color: '#004D40' },
  { library: 'reading', name: 'Misc.', color: '#1A237E' },
  { library: 'watching', name: 'Anime', color: '#781E82' },
  { library: 'watching', name: 'Action Anime', color: '#B71C1C' },
  { library: 'watching', name: 'Romance Anime', color: '#8B2252' },
  { library: 'watching', name: 'Donghua', color: '#0F7B6C' },
  { library: 'watching', name: 'Action Donghua', color: '#B71C1C' },
  { library: 'watching', name: 'Romance Donghua', color: '#8B2252' },
  { library: 'watching', name: 'TV Shows', color: '#1565C0' },
  { library: 'watching', name: 'Cartoons', color: '#F57F17' },
  { library: 'watching', name: 'Sci-Fi', color: '#0D47A1' },
  { library: 'watching', name: 'K-Drama', color: '#00897B' },
  { library: 'watching', name: 'Horror', color: '#212121' },
  { library: 'watching', name: 'Comedy', color: '#E66400' },
  { library: 'watching', name: 'Documentary', color: '#4E342E' },
  { library: 'watching', name: 'Slice of Life', color: '#2E7D32' },
  { library: 'watching', name: 'Thriller', color: '#3E2723' },
  { library: 'watching', name: 'Misc.', color: '#1A237E' },
];

/** Colors for newly created categories (from the original "Add Category" macro). */
export const NEW_CATEGORY_PALETTE: Record<string, string> = {
  harem: '#880E4F',
  drama: '#4E342E',
  mystery: '#311B92',
  sports: '#1565C0',
};
export const DEFAULT_CATEGORY_COLOR = '#37474F';

/** Media type implied by a watching-library category name. */
export const WATCHING_MEDIA_TYPE_BY_CATEGORY: Record<string, (typeof MEDIA_TYPES)[number]> = {
  anime: 'anime',
  'action anime': 'anime',
  'romance anime': 'anime',
  donghua: 'donghua',
  'action donghua': 'donghua',
  'romance donghua': 'donghua',
  'tv shows': 'tv',
  cartoons: 'cartoon',
  'k-drama': 'kdrama',
  documentary: 'documentary',
};

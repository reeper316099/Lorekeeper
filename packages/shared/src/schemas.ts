import { z } from 'zod';
import { LIBRARIES, MEDIA_TYPES, STATUSES } from './constants.js';

export const LibrarySchema = z.enum(LIBRARIES);
export const StatusSchema = z.enum(STATUSES);
export const MediaTypeSchema = z.enum(MEDIA_TYPES);

export const ProgressSchema = z.object({
  raw: z.string().nullable(),
  number: z.number().nullable(),
  season: z.number().nullable(),
  kind: z.enum(['number', 'start', 'end', 'complete', 'text']).nullable(),
});
export type Progress = z.infer<typeof ProgressSchema>;
export const emptyProgress = (): Progress => ({
  raw: null,
  number: null,
  season: null,
  kind: null,
});

export const LinkSchema = z.object({
  kind: z.enum(['youtube', 'site', 'anilist', 'other']),
  url: z.string().nullable(),
  label: z.string().nullable(),
  raw: z.string().nullable(),
  /** which spreadsheet column it came from: the main link column or "Other Links" */
  slot: z.enum(['primary', 'other']).optional(),
  channel: z.string().nullable().optional(),
  date: z.string().nullable().optional(),
});
export type Link = z.infer<typeof LinkSchema>;

export const ExternalSchema = z
  .object({
    anilistId: z.number().optional(),
    malId: z.number().optional(),
    matchConfidence: z.number().optional(),
    matchLocked: z.boolean().optional(),
    cover: z.string().optional(),
    bannerUrl: z.string().optional(),
    format: z.string().optional(),
    countryOfOrigin: z.string().optional(),
    apiStatus: z.string().optional(),
    year: z.number().optional(),
    averageScore: z.number().optional(),
    description: z.string().optional(),
  })
  .passthrough();

export const ImportRefSchema = z.object({
  file: z.string(),
  sheet: z.string(),
  row: z.number(),
  importedAt: z.string(),
});

export const EntrySchema = z.object({
  id: z.string().min(1),
  library: LibrarySchema,
  categoryId: z.string().min(1),
  title: z.string().min(1),
  altTitles: z.array(z.string()).default([]),
  mediaType: MediaTypeSchema.default('unknown'),
  status: StatusSchema.nullable().default(null),
  score: z.number().int().min(1).max(10).nullable().default(null),
  tags: z.array(z.string()).default([]),
  season: z.string().nullable().default(null),
  current: ProgressSchema.default(emptyProgress()),
  consumedUpTo: ProgressSchema.default(emptyProgress()),
  totalUnits: z.number().nullable().default(null),
  sources: z.array(z.string()).default([]),
  links: z.array(LinkSchema).default([]),
  notes: z
    .object({
      general: z.string().nullable().default(null),
      extra1: z.string().nullable().default(null),
      extra2: z.string().nullable().default(null),
    })
    .default({ general: null, extra1: null, extra2: null }),
  external: ExternalSchema.default({}),
  favorite: z.boolean().default(false),
  createdAt: z.string(),
  updatedAt: z.string(),
  startedAt: z.string().optional(),
  completedAt: z.string().optional(),
  importRef: ImportRefSchema.optional(),
});
export type Entry = z.infer<typeof EntrySchema>;
export type Library = z.infer<typeof LibrarySchema>;
export type Status = z.infer<typeof StatusSchema>;
export type MediaType = z.infer<typeof MediaTypeSchema>;

export const EntryFileSchema = z.array(EntrySchema);

export const CategorySchema = z.object({
  id: z.string().min(1),
  library: LibrarySchema,
  name: z.string().min(1),
  slug: z.string().min(1),
  color: z.string().regex(/^#[0-9a-fA-F]{6}$/),
  icon: z.string().optional(),
  order: z.number().int(),
});
export type Category = z.infer<typeof CategorySchema>;
export const CategoriesFileSchema = z.array(CategorySchema);

export const TagSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  color: z.string().optional(),
  aliases: z.array(z.string()).default([]),
  hidden: z.boolean().optional(),
});
export type Tag = z.infer<typeof TagSchema>;
export const TagsFileSchema = z.array(TagSchema);

const ScaleSchema = z.object({
  statusLabels: z.record(StatusSchema, z.string()),
  /** index 0 = score 1 */
  scoreLabels: z.array(z.string()).length(10),
});
export const ScalesSchema = z.object({ reading: ScaleSchema, watching: ScaleSchema });
export type Scales = z.infer<typeof ScalesSchema>;

export const SettingsSchema = z.object({
  theme: z.enum(['dark', 'light', 'system']).default('dark'),
  accent: z.string().default('#E5484D'),
  density: z.enum(['comfortable', 'compact']).default('comfortable'),
  defaultLibrary: z.enum(['all', 'reading', 'watching']).default('all'),
  hiddenTags: z.array(z.string()).default([]),
  labelOverrides: z.record(z.string(), z.string()).default({ extra1: 'Left off' }),
  backup: z
    .object({ keep: z.number().int().min(1).default(20), onLaunchHours: z.number().default(6) })
    .default({ keep: 20, onLaunchHours: 6 }),
  autoConfirmThreshold: z.number().min(0).max(1).default(0.92),
  savedViews: z
    .array(z.object({ id: z.string(), name: z.string(), query: z.record(z.string(), z.string()) }))
    .default([]),
});
export type Settings = z.infer<typeof SettingsSchema>;

export const MetaSchema = z.object({
  schemaVersion: z.number().int(),
  appVersion: z.string(),
  createdAt: z.string(),
  lastOpenedAt: z.string(),
});
export type Meta = z.infer<typeof MetaSchema>;

export const ActivityRecordSchema = z.object({
  at: z.string(),
  entryId: z.string(),
  kind: z.enum(['progress', 'status', 'created', 'imported']),
  from: z.unknown().optional(),
  to: z.unknown().optional(),
});
export type ActivityRecord = z.infer<typeof ActivityRecordSchema>;

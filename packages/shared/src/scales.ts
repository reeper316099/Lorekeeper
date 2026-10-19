import { SCORE_LABEL_SETS } from './constants.js';
import type { Library, Scales, Status } from './schemas.js';

export const DEFAULT_SCALES: Scales = {
  reading: {
    statusLabels: {
      planned: 'Plan to Read',
      in_progress: 'Reading',
      completed: 'Completed',
      on_hold: 'On Hold',
      dropped: 'Dropped',
      repeating: 'Re-Reading',
    },
    scoreLabels: SCORE_LABEL_SETS.readingNewer.map((l, i) => `${i + 1} - ${l}`),
  },
  watching: {
    statusLabels: {
      planned: 'Plan to Watch',
      in_progress: 'Watching',
      completed: 'Finished',
      on_hold: 'On Hold',
      dropped: 'Dropped',
      repeating: 'Re-Watching',
    },
    scoreLabels: SCORE_LABEL_SETS.watching.map((l, i) => `${i + 1} - ${l}`),
  },
};

export function statusLabel(scales: Scales, library: Library, status: Status | null): string {
  return status ? scales[library].statusLabels[status] : '';
}

export function scoreLabel(scales: Scales, library: Library, score: number | null): string {
  return score ? (scales[library].scoreLabels[score - 1] ?? String(score)) : '';
}

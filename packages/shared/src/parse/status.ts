import type { Status } from '../schemas.js';

const MAP: Record<string, Status> = {
  finished: 'completed',
  complete: 'completed',
  completed: 'completed',
  done: 'completed',
  reading: 'in_progress',
  watching: 'in_progress',
  current: 'in_progress',
  'in progress': 'in_progress',
  'plan to read': 'planned',
  'plan to watch': 'planned',
  planned: 'planned',
  planning: 'planned',
  backlog: 'planned',
  'on hold': 'on_hold',
  onhold: 'on_hold',
  paused: 'on_hold',
  dropped: 'dropped',
  're-reading': 'repeating',
  're-watching': 'repeating',
  rereading: 'repeating',
  rewatching: 'repeating',
  repeating: 'repeating',
};

export type StatusParse =
  | { ok: true; status: Status; blank: false }
  | { ok: true; status: 'planned'; blank: true }
  | { ok: false; raw: string };

/** Blank → planned (flagged so the importer can warn); unknown → ok:false. */
export function parseStatus(input: unknown): StatusParse {
  const raw = input === null || input === undefined ? '' : String(input).trim();
  if (!raw) return { ok: true, status: 'planned', blank: true };
  const hit = MAP[raw.toLowerCase().replace(/\s+/g, ' ')];
  return hit ? { ok: true, status: hit, blank: false } : { ok: false, raw };
}

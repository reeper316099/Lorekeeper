import { emptyProgress, type Progress } from '../schemas.js';

/** Parse a "Currently on …" / "Watched until …" cell. The raw text is always kept. */
export function parseProgress(input: unknown): Progress {
  if (input === null || input === undefined) return emptyProgress();
  const raw = typeof input === 'number' ? String(input) : String(input).trim();
  if (raw === '') return emptyProgress();
  const out: Progress = { raw, number: null, season: null, kind: 'text' };

  if (/^\d+(\.\d+)?$/.test(raw)) return { ...out, number: Number(raw), kind: 'number' };

  const se = /^s(?:eason)?\s*(\d+)\s*[,:-]?\s*e(?:p(?:isode)?)?\s*(\d+)$/i.exec(raw);
  if (se) return { raw, season: Number(se[1]), number: Number(se[2]), kind: 'number' };

  if (/^(done|completed?|finished\??|complete)$/i.test(raw)) return { ...out, kind: 'complete' };
  if (/^end(ed)?$/i.test(raw)) return { ...out, kind: 'end' };

  const endOf = /^end(?:ed)?\s+of\s+(?:s|season\s*)(\d+)$/i.exec(raw);
  if (endOf) return { ...out, season: Number(endOf[1]), kind: 'end' };
  const begin = /^(?:beginning|start|begin)(?:\s+of)?\s+(?:s|season\s*)(\d+)$/i.exec(raw);
  if (begin) return { ...out, season: Number(begin[1]), kind: 'start' };

  const guess = /^(\d+)\s*\?$/.exec(raw);
  if (guess) return { ...out, number: Number(guess[1]), kind: 'text' };

  return out;
}

/** Numeric position used for "behind by N" math (null when unknown). */
export function progressNumber(p: Progress): number | null {
  return p.number;
}

/** How far the consumed-up-to marker lags/leads the current position, when both are numeric. */
export function behindBy(current: Progress, consumedUpTo: Progress): number | null {
  if (current.number === null || consumedUpTo.number === null) return null;
  if (current.season !== consumedUpTo.season) return null;
  return current.number - consumedUpTo.number;
}

/** Value to write back into a spreadsheet cell: numbers as numbers, text verbatim. */
export function progressCell(p: Progress): string | number | null {
  if (p.raw === null) return null;
  return p.kind === 'number' && p.season === null && /^\d+$/.test(p.raw) ? Number(p.raw) : p.raw;
}

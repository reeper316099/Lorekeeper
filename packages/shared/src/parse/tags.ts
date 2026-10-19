import { damerau } from './text.js';

/** Split a free-text tag cell: trim, drop empties, case-insensitive dedupe (first casing wins). */
export function splitTags(input: unknown): string[] {
  if (input === null || input === undefined) return [];
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of String(input).split(/[,;\n]/)) {
    const t = part.replace(/\s+/g, ' ').trim();
    if (!t) continue;
    const key = t.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(t);
  }
  return out;
}

export interface MergeSuggestion {
  from: string;
  to: string;
  reason: 'case' | 'punctuation' | 'variant' | 'typo';
  distance: number;
}

const alnum = (s: string) => s.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '');
const baseNoParens = (s: string) => alnum(s.replace(/\(.*?\)/g, ''));

/**
 * Suggest merges for near-duplicate tags. `usage` maps tag name → count. The less-used tag is
 * suggested to merge into the more-used one. Nothing is ever applied automatically.
 */
export function suggestTagMerges(
  usage: Map<string, number> | Record<string, number>,
): MergeSuggestion[] {
  const entries = usage instanceof Map ? [...usage] : Object.entries(usage);
  const out: MergeSuggestion[] = [];
  for (let i = 0; i < entries.length; i++) {
    for (let j = i + 1; j < entries.length; j++) {
      const [a, ca] = entries[i]!;
      const [b, cb] = entries[j]!;
      let reason: MergeSuggestion['reason'] | null = null;
      let distance = 0;
      const la = a.toLowerCase().trim();
      const lb = b.toLowerCase().trim();
      if (la === lb) reason = 'case';
      else if (alnum(a) === alnum(b)) reason = 'punctuation';
      else if (/\(/.test(a + b) && baseNoParens(a) === baseNoParens(b)) reason = 'variant';
      else {
        const x = alnum(a);
        const y = alnum(b);
        const limit = Math.min(x.length, y.length) >= 6 ? 2 : 1;
        if (x.length >= 4 && y.length >= 4 && x[0] === y[0]) {
          distance = damerau(x, y);
          if (distance <= limit) reason = 'typo';
        }
      }
      if (!reason) continue;
      const keepA = ca > cb || (ca === cb && a.length >= b.length && a <= b);
      out.push(keepA ? { from: b, to: a, reason, distance } : { from: a, to: b, reason, distance });
    }
  }
  return out;
}

import { levenshtein } from './text.js';

/** Dedupe key: NFKC, lowercase, no diacritics/punctuation/leading article/extra spaces. */
export function normalizeTitle(title: string): string {
  const base = title.normalize('NFKC').toLowerCase().trim();
  const stripped = base
    .normalize('NFD')
    .replace(/\p{M}+/gu, '')
    .replace(/[\p{P}\p{S}]+/gu, '')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/^(the|a|an) /, '');
  return stripped || base;
}

/** 0..1 similarity of two titles after normalization (1 = identical). */
export function titleSimilarity(a: string, b: string): number {
  const x = normalizeTitle(a);
  const y = normalizeTitle(b);
  if (!x.length && !y.length) return 1;
  return 1 - levenshtein(x, y) / Math.max(x.length, y.length);
}

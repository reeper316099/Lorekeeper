import { damerau, levenshtein } from './text.js';

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

/** Fuzzy "might be the same series" test: high similarity, or most words of the shorter title appear in the other. */
export function isNearDuplicateTitle(a: string, b: string): boolean {
  if (titleSimilarity(a, b) >= 0.85) return true;
  const ta = normalizeTitle(a).split(' ').filter(Boolean);
  const tb = normalizeTitle(b).split(' ').filter(Boolean);
  const [small, big] = ta.length <= tb.length ? [ta, tb] : [tb, ta];
  if (small.length < 3) return false;
  const hit = small.filter((w) =>
    big.some((x) => x === w || (w.length >= 5 && x.length >= 5 && damerau(w, x) <= 1)),
  ).length;
  return hit / small.length >= 0.8;
}

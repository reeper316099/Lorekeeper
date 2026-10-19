/** Parse "7 - Good", " 9 - Amazing", 8, "8/10" → integer 1..10, else null. */
export function parseScore(input: unknown): number | null {
  if (input === null || input === undefined) return null;
  const m = /^\s*(\d{1,2})(?!\d)/.exec(String(input));
  if (!m) return null;
  const n = Number(m[1]);
  if (n < 1) return null;
  return Math.min(10, n);
}

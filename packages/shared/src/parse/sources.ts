const CANON: Record<string, string> = { youtube: 'YouTube', inkreader: 'Inkreader', yt: 'YouTube' };

/** "Youtube, Inkreader" → ['YouTube','Inkreader']; case-insensitive dedupe. */
export function parseSources(input: unknown): string[] {
  if (input === null || input === undefined) return [];
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of String(input).split(/[,;/\n]/)) {
    const t = part.replace(/\s+/g, ' ').trim();
    if (!t) continue;
    const key = t.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(CANON[key] ?? t);
  }
  return out;
}

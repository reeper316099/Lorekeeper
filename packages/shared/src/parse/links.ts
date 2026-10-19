import type { Link } from '../schemas.js';

const URL_RE = /https?:\/\/[^\s<>"]+/i;
const BARE_DOMAIN_RE = /^(?:www\.)?[a-z0-9-]+(?:\.[a-z0-9-]+)*\.[a-z]{2,}(?::\d+)?(?:[/?#]\S*)?$/i;
const DATE_RE = /\((\d{1,2}\/\d{1,2}\/\d{2,4})\)/;
const CHANNEL_RE = /\bBy:\s*(.+?)\s*$/i;

function hostOf(url: string): string | null {
  try {
    return new URL(url).hostname.replace(/^www\./, '').toLowerCase();
  } catch {
    return null;
  }
}

function kindFor(url: string | null, hint: Link['kind']): Link['kind'] {
  if (url) {
    const host = hostOf(url);
    if (host && (host === 'youtu.be' || host.endsWith('youtube.com'))) return 'youtube';
    if (host && host.endsWith('anilist.co')) return 'anilist';
    if (hint === 'youtube') return 'youtube';
    return 'site';
  }
  return hint;
}

/** Parse a link cell. Returns null for empty cells. `raw` is always preserved. */
export function parseLink(input: unknown, hint: Link['kind'] = 'other'): Link | null {
  if (input === null || input === undefined) return null;
  const raw = String(input).trim();
  if (!raw) return null;

  const date = DATE_RE.exec(raw)?.[1] ?? null;
  const channel = CHANNEL_RE.exec(raw)?.[1] ?? null;

  const embedded = URL_RE.exec(raw)?.[0];
  if (embedded) {
    const url = embedded.replace(/[),.;]+$/, '');
    const label = raw === embedded ? null : raw;
    return { kind: kindFor(url, hint), url, label, raw, channel, date };
  }
  if (!/\s/.test(raw) && BARE_DOMAIN_RE.test(raw)) {
    const url = `https://${raw}`;
    return { kind: kindFor(url, hint), url, label: null, raw, channel, date };
  }
  return { kind: hint, url: null, label: raw, raw, channel, date };
}

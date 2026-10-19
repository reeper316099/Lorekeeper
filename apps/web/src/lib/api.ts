import type { Category, Entry, Scales, Settings, Tag } from '@lorekeeper/shared';

interface Bridge {
  baseUrl: string;
  token: string;
}

/** The Electron preload exposes `window.lorekeeper`; in a plain browser (dev) fall back to env. */
function bridge(): Bridge {
  const w = window as unknown as { lorekeeper?: Bridge };
  return (
    w.lorekeeper ?? {
      baseUrl: import.meta.env.VITE_API_URL ?? 'http://127.0.0.1:4317',
      token: import.meta.env.VITE_API_TOKEN ?? 'dev-token',
    }
  );
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const b = bridge();
  const res = await fetch(`${b.baseUrl}/api/v1${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${b.token}`,
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const j = await res.json().catch(() => null);
    throw new ApiError(res.status, j?.error?.code ?? 'ERROR', j?.error?.message ?? res.statusText);
  }
  return res.json() as Promise<T>;
}

export function eventsUrl(): string {
  const b = bridge();
  return `${b.baseUrl}/api/v1/events?token=${encodeURIComponent(b.token)}`;
}

export type CategoryRow = Category & { count: number };
export type TagRow = Tag & { count: number };
export interface EntryPage {
  items: Entry[];
  total: number;
  page: number;
  limit: number;
}

export const get = <T>(p: string) => api<T>('GET', p);
export type { Category, Entry, Scales, Settings, Tag };

function conn(): { baseUrl: string; token: string } {
  const b = (window as unknown as { lorekeeper?: { baseUrl: string; token: string } }).lorekeeper;
  return (
    b ?? {
      baseUrl: import.meta.env.VITE_API_URL ?? 'http://127.0.0.1:4317',
      token: import.meta.env.VITE_API_TOKEN ?? 'dev-token',
    }
  );
}

/** URL for a cached cover file (e.g. "123.jpg"). <img> cannot send headers, so the token rides in the query. */
export const coverUrl = (file: string): string => {
  const c = conn();
  return `${c.baseUrl}/api/v1/anilist/cover/${encodeURIComponent(file)}?token=${encodeURIComponent(c.token)}`;
};

/** AniList artwork shown before a match is confirmed goes through our allow-listed, disk-cached proxy. */
export const remoteImageUrl = (url: string): string => {
  const c = conn();
  return `${c.baseUrl}/api/v1/anilist/image?url=${encodeURIComponent(url)}&token=${encodeURIComponent(c.token)}`;
};

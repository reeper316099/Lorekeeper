import { ApiError } from './api';

export async function bridgeFetch(file: File): Promise<{ importId: string }> {
  const w = window as unknown as { lorekeeper?: { baseUrl: string; token: string } };
  const base = w.lorekeeper?.baseUrl ?? import.meta.env.VITE_API_URL ?? 'http://127.0.0.1:4317';
  const token = w.lorekeeper?.token ?? import.meta.env.VITE_API_TOKEN ?? 'dev-token';
  const fd = new FormData();
  fd.append('file', file, file.name);
  const res = await fetch(`${base}/api/v1/import/upload`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: fd,
  });
  if (!res.ok) {
    const j = await res.json().catch(() => null);
    throw new ApiError(res.status, j?.error?.code ?? 'ERROR', j?.error?.message ?? res.statusText);
  }
  return res.json();
}

/** Authenticated download (the API needs a bearer token, so a plain link will not do). */
export async function download(path: string, fallbackName: string): Promise<void> {
  const w = window as unknown as { lorekeeper?: { baseUrl: string; token: string } };
  const base = w.lorekeeper?.baseUrl ?? import.meta.env.VITE_API_URL ?? 'http://127.0.0.1:4317';
  const token = w.lorekeeper?.token ?? import.meta.env.VITE_API_TOKEN ?? 'dev-token';
  const res = await fetch(`${base}/api/v1${path}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) throw new ApiError(res.status, 'DOWNLOAD', res.statusText);
  const name =
    /filename="([^"]+)"/.exec(res.headers.get('content-disposition') ?? '')?.[1] ?? fallbackName;
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 5000);
}

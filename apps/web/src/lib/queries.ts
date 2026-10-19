import { useEffect } from 'react';
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from '@tanstack/react-query';
import {
  behindBy,
  parseProgress,
  type Entry,
  type Scales,
  type Settings,
} from '@lorekeeper/shared';
import { api, eventsUrl, get, type CategoryRow, type EntryPage, type TagRow } from './api';
import { filtersToParams, useUI, type Filters } from './store';

const PAGE = 200;

export const useCategories = () =>
  useQuery({ queryKey: ['categories'], queryFn: () => get<CategoryRow[]>('/categories') });
export const useTags = () =>
  useQuery({ queryKey: ['tags'], queryFn: () => get<TagRow[]>('/tags') });
export const useSettings = () =>
  useQuery({ queryKey: ['settings'], queryFn: () => get<Settings>('/settings') });
export const useScales = () =>
  useQuery({ queryKey: ['scales'], queryFn: () => get<Scales>('/scales') });
export const useMeta = () =>
  useQuery({ queryKey: ['meta'], queryFn: () => get<{ entries: number }>('/meta') });

export function useEntries(filters: Filters) {
  const qs = filtersToParams(filters).toString();
  const q = useInfiniteQuery({
    queryKey: ['entries', qs],
    initialPageParam: 1,
    queryFn: ({ pageParam }) => get<EntryPage>(`/entries?${qs}&limit=${PAGE}&page=${pageParam}`),
    getNextPageParam: (last) => (last.page * last.limit < last.total ? last.page + 1 : undefined),
    placeholderData: (prev) => prev,
  });
  const items = q.data?.pages.flatMap((p) => p.items) ?? [];
  return { ...q, items, total: q.data?.pages[0]?.total ?? 0 };
}

export function useEntry(id: string | null) {
  return useQuery({
    queryKey: ['entry', id],
    queryFn: () => get<Entry>(`/entries/${id}`),
    enabled: !!id,
  });
}

export function invalidateAll(qc: QueryClient) {
  for (const k of ['entries', 'entry', 'categories', 'tags', 'stats', 'meta', 'activity'])
    void qc.invalidateQueries({ queryKey: [k] });
}

/** Subscribe to server-sent change events (hand edits, other windows) and refresh. */
export function useServerEvents() {
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  useEffect(() => {
    let es: EventSource | null = null;
    try {
      es = new EventSource(eventsUrl());
    } catch {
      return;
    }
    const refresh = () => invalidateAll(qc);
    es.addEventListener('change', refresh);
    es.addEventListener('external-change', refresh);
    es.addEventListener('file-restored', refresh);
    es.addEventListener('file-error', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data);
        toast({
          tone: 'error',
          message: `${d.file}: ${d.message}${d.kept ? ' (kept previous data)' : ''}`,
        });
      } catch {
        /* ignore */
      }
    });
    return () => es?.close();
  }, [qc, toast]);
}

function patchCache(qc: QueryClient, id: string, fn: (e: Entry) => Entry) {
  qc.setQueriesData<InfiniteData<EntryPage>>(
    { queryKey: ['entries'] },
    (old) =>
      old && {
        ...old,
        pages: old.pages.map((p) => ({
          ...p,
          items: p.items.map((e) => (e.id === id ? fn(e) : e)),
        })),
      },
  );
  qc.setQueryData<Entry>(['entry', id], (old) => old && fn(old));
}

/** Apply a patch optimistically; roll back by refetching on failure. */
export function useUpdateEntry() {
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Record<string, unknown> }) =>
      api<Entry>('PATCH', `/entries/${id}`, patch),
    onMutate: ({ id, patch }) =>
      patchCache(qc, id, (e) => ({ ...e, ...(patch as Partial<Entry>) })),
    onSuccess: (entry) => {
      patchCache(qc, entry.id, () => entry);
      void qc.invalidateQueries({ queryKey: ['stats'] });
      void qc.invalidateQueries({ queryKey: ['tags'] });
    },
    onError: (err) => {
      toast({ tone: 'error', message: (err as Error).message });
      invalidateAll(qc);
    },
  });
}

export function useProgress() {
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: { delta?: number; set?: string | number } }) =>
      api<Entry>('POST', `/entries/${id}/progress`, body),
    onMutate: ({ id, body }) => {
      if (body.delta === undefined) return;
      patchCache(qc, id, (e) => {
        const n = Math.max(0, (e.current.number ?? 0) + body.delta!);
        return {
          ...e,
          current: { raw: String(n), number: n, season: e.current.season, kind: 'number' },
          status: e.status ?? 'in_progress',
        };
      });
    },
    onSuccess: (entry) => {
      patchCache(qc, entry.id, () => entry);
      void qc.invalidateQueries({ queryKey: ['activity'] });
      void qc.invalidateQueries({ queryKey: ['stats'] });
    },
    onError: (err) => {
      toast({ tone: 'error', message: (err as Error).message });
      invalidateAll(qc);
    },
  });
}

export function useDeleteEntry() {
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  return useMutation({
    mutationFn: (entry: Entry) => api<Entry>('DELETE', `/entries/${entry.id}`),
    onSuccess: (entry) => {
      invalidateAll(qc);
      toast({
        message: `Deleted “${entry.title}”`,
        action: {
          label: 'Undo',
          run: () => void api('POST', '/entries/restore', entry).then(() => invalidateAll(qc)),
        },
      });
    },
  });
}

export const progressFrom = (raw: string) => parseProgress(raw.trim());
export { behindBy };

/** Returns a function that invalidates the given top-level query keys. */
export function useQueryClientInvalidate() {
  const qc = useQueryClient();
  return (keys: string[]) => keys.forEach((k) => void qc.invalidateQueries({ queryKey: [k] }));
}

import { useQuery } from '@tanstack/react-query';
import type { Entry } from '@lorekeeper/shared';
import { get } from './api';

export interface Candidate {
  id: number;
  title: string;
  titles: string[];
  year: number | null;
  format: string | null;
  type: 'ANIME' | 'MANGA';
  country: string | null;
  chapters: number | null;
  episodes: number | null;
  cover: string | null;
  siteUrl: string | null;
  confidence: number;
}
export interface QueueItem {
  entry: Entry;
  status: 'pending' | 'confirmed' | 'rejected' | 'never' | 'unlinkable';
  candidates: Candidate[];
  suggestedTags: string[];
  auto?: boolean;
  reason?: string;
}
export interface Job {
  id: string;
  kind: 'match' | 'import';
  state: 'running' | 'done' | 'cancelled' | 'error' | 'offline';
  total: number;
  done: number;
  message: string | null;
  result?: Record<string, number | string>;
}
export interface AniStatus {
  queue: Record<QueueItem['status'], number>;
  matched: number;
  total: number;
  offline: boolean;
  requestsPerMinute: number;
  autoConfirmThreshold: number;
  matchJob: Job | null;
  importJob: Job | null;
}

export const useAniStatus = () =>
  useQuery({
    queryKey: ['anilist', 'status'],
    queryFn: () => get<AniStatus>('/anilist/status'),
    refetchInterval: (q) => (q.state.data?.matchJob || q.state.data?.importJob ? 800 : false),
  });

export const useAniQueue = (status: QueueItem['status']) =>
  useQuery({
    queryKey: ['anilist', 'queue', status],
    queryFn: () => get<QueueItem[]>(`/anilist/queue?status=${status}`),
  });

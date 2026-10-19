import { useQuery } from '@tanstack/react-query';
import { get } from './api';

export interface Brief {
  id: string;
  title: string;
  categoryId: string;
  library: 'reading' | 'watching';
  status: string | null;
  score: number | null;
  current: { raw: string | null; number: number | null };
  consumedUpTo: { raw: string | null };
  totalUnits: number | null;
  leftOff: string | null;
  updatedAt: string;
  behindBy: number | null;
}
export interface Overview {
  totals: {
    entries: number;
    reading: number;
    watching: number;
    completed: number;
    behind: number;
    avgScore: number | null;
  };
  byStatus: Record<string, number>;
  byCategory: { id: string; name: string; color: string; library: string; count: number }[];
  scoreDistribution: { score: number; count: number }[];
  topTags: { name: string; count: number }[];
  tagCooccurrence: { a: string; b: string; count: number }[];
  inProgress: Brief[];
  behindList: Brief[];
  backlog: number;
  backlogList: Brief[];
  recentlyUpdated: Brief[];
  highestRated: Brief[];
  needsDecision: Brief[];
  heatmap: Record<string, number>;
}

export const useOverview = (library: 'all' | 'reading' | 'watching') =>
  useQuery({
    queryKey: ['stats', library],
    queryFn: () =>
      get<Overview>(`/stats/overview${library === 'all' ? '' : `?library=${library}`}`),
  });

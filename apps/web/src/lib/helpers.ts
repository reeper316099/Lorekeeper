import { useMemo } from 'react';
import { behindBy, type Entry, type Status } from '@lorekeeper/shared';
import { useCategories, useScales, useSettings } from './queries';

export function useLookup() {
  const cats = useCategories().data;
  const scales = useScales().data;
  const settings = useSettings().data;
  return useMemo(() => {
    const catMap = new Map((cats ?? []).map((c) => [c.id, c]));
    const labels = settings?.labelOverrides ?? { extra1: 'Left off' };
    return {
      cats: cats ?? [],
      catMap,
      color: (e: Pick<Entry, 'categoryId'>) => catMap.get(e.categoryId)?.color ?? '#37474F',
      catName: (e: Pick<Entry, 'categoryId'>) => catMap.get(e.categoryId)?.name ?? '',
      statusLabel: (e: Pick<Entry, 'library'>, s: Status) =>
        scales?.[e.library].statusLabels[s] ?? s,
      statusOptions: (lib: 'reading' | 'watching') =>
        Object.entries(scales?.[lib].statusLabels ?? {}) as [Status, string][],
      scoreLabels: (lib: 'reading' | 'watching') => scales?.[lib].scoreLabels ?? [],
      noteLabel: (k: 'general' | 'extra1' | 'extra2') =>
        labels[k] ?? { general: 'Notes', extra1: 'Left off', extra2: 'Extra notes' }[k],
      hiddenTags: new Set((settings?.hiddenTags ?? []).map((t) => t.toLowerCase())),
    };
  }, [cats, scales, settings]);
}

export const behindOf = (e: Entry): number => {
  const b = behindBy(e.current, e.consumedUpTo);
  return b !== null && b > 0 ? b : 0;
};

export function isTyping(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el) return false;
  return el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName);
}

export function timeAgo(iso: string): string {
  const s = (Date.now() - Date.parse(iso)) / 1000;
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  if (s < 86400 * 30) return `${Math.floor(s / 86400)}d ago`;
  return new Date(iso).toLocaleDateString();
}

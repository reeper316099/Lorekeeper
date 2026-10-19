import { behindBy, type Entry } from '@lorekeeper/shared';
import type { Store } from './storage/store.js';

export async function overview(store: Store, library?: 'reading' | 'watching') {
  const all = store.all().filter((e) => !library || e.library === library);
  const count = <K extends string>(keyOf: (e: Entry) => K) => {
    const m: Record<string, number> = {};
    for (const e of all) m[keyOf(e)] = (m[keyOf(e)] ?? 0) + 1;
    return m;
  };
  const scored = all.filter((e) => e.score !== null);
  const scoreDistribution = Array.from({ length: 10 }, (_, i) => ({
    score: i + 1,
    count: scored.filter((e) => e.score === i + 1).length,
  }));

  const tagCounts = new Map<string, number>();
  for (const e of all) for (const t of e.tags) tagCounts.set(t, (tagCounts.get(t) ?? 0) + 1);
  const topTags = [...tagCounts]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, 25)
    .map(([name, count]) => ({ name, count }));

  const pairs = new Map<string, number>();
  for (const e of all) {
    const t = [...e.tags].sort();
    for (let i = 0; i < t.length; i++)
      for (let j = i + 1; j < t.length; j++) {
        const k = `${t[i]}\u0000${t[j]}`;
        pairs.set(k, (pairs.get(k) ?? 0) + 1);
      }
  }
  const tagCooccurrence = [...pairs]
    .sort((a, b) => b[1] - a[1])
    .slice(0, 40)
    .map(([k, count]) => {
      const [a, b] = k.split('\u0000');
      return { a, b, count };
    });

  const brief = (e: Entry) => ({
    id: e.id,
    title: e.title,
    categoryId: e.categoryId,
    library: e.library,
    status: e.status,
    score: e.score,
    current: e.current,
    consumedUpTo: e.consumedUpTo,
    totalUnits: e.totalUnits,
    leftOff: e.notes.extra1,
    cover: e.external.cover ?? null,
    updatedAt: e.updatedAt,
    behindBy: behindBy(e.current, e.consumedUpTo),
  });
  const byUpdated = [...all].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));

  const since = new Date(Date.now() - 365 * 86400_000).toISOString();
  const heat: Record<string, number> = {};
  for (const r of await store.readActivity(since))
    heat[r.at.slice(0, 10)] = (heat[r.at.slice(0, 10)] ?? 0) + 1;

  return {
    totals: {
      entries: all.length,
      reading: all.filter((e) => e.library === 'reading').length,
      watching: all.filter((e) => e.library === 'watching').length,
      completed: all.filter((e) => e.status === 'completed').length,
      behind: all.filter((e) => (behindBy(e.current, e.consumedUpTo) ?? 0) > 0).length,
      avgScore: scored.length
        ? scored.reduce((s, e) => s + (e.score ?? 0), 0) / scored.length
        : null,
    },
    byStatus: count((e) => e.status ?? 'none'),
    byLibrary: count((e) => e.library),
    byCategory: store.categories
      .filter((c) => !library || c.library === library)
      .map((c) => ({
        id: c.id,
        name: c.name,
        color: c.color,
        library: c.library,
        count: store.entriesOf(c.id).length,
      })),
    scoreDistribution,
    topTags,
    tagCooccurrence,
    inProgress: byUpdated
      .filter((e) => e.status === 'in_progress')
      .slice(0, 30)
      .map(brief),
    behindList: all.filter((e) => (behindBy(e.current, e.consumedUpTo) ?? 0) > 0).map(brief),
    backlog: all.filter((e) => e.status === 'planned').length,
    backlogList: byUpdated
      .filter((e) => e.status === 'planned')
      .slice(0, 10)
      .map(brief),
    recentlyUpdated: byUpdated.slice(0, 10).map(brief),
    highestRated: [...scored]
      .sort((a, b) => b.score! - a.score!)
      .slice(0, 10)
      .map(brief),
    needsDecision: all.filter((e) => e.status === 'dropped' || e.status === 'on_hold').map(brief),
    heatmap: heat,
  };
}

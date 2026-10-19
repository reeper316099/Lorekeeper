import { useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { timeAgo, useLookup } from '../lib/helpers';
import { invalidateAll, useProgress } from '../lib/queries';
import { useOverview, type Brief } from '../lib/stats';
import { useUI } from '../lib/store';
import { Donut, HBars, Heatmap, Sparkline, VBars } from './charts';
import { Cover, Empty, GENERIC_STATUS, Icon } from './ui';

const STATUS_COLORS: Record<string, string> = {
  in_progress: 'var(--accent)',
  completed: 'var(--success)',
  planned: 'var(--text-muted)',
  on_hold: 'var(--warning)',
  dropped: 'var(--danger)',
  repeating: 'var(--secondary)',
  none: 'var(--surface-3)',
};

function Card({
  title,
  children,
  span = 1,
  action,
}: {
  title: string;
  children: React.ReactNode;
  span?: number;
  action?: React.ReactNode;
}) {
  return (
    <section className="dcard" style={{ gridColumn: `span ${span}` }}>
      <header>
        <h3>{title}</h3>
        {action}
      </header>
      {children}
    </section>
  );
}

function Stat({
  label,
  value,
  hint,
  spark,
}: {
  label: string;
  value: string | number;
  hint?: string;
  spark?: number[];
}) {
  return (
    <div className="stat">
      <div className="muted label">{label}</div>
      <div className="stat-value">{value}</div>
      <div className="muted" style={{ fontSize: 'var(--text-xs)' }}>
        {hint}
      </div>
      {spark && (
        <div className="stat-spark">
          <Sparkline values={spark} />
        </div>
      )}
    </div>
  );
}

function ShelfCard({ b }: { b: Brief }) {
  const look = useLookup();
  const progress = useProgress();
  const { set } = useUI();
  const color = look.color(b);
  const pct =
    b.current.number && b.totalUnits
      ? Math.min(100, (b.current.number / b.totalUnits) * 100)
      : null;
  return (
    <article className="shelf-card">
      <Cover entry={b} color={color} size={46} radius={9} />
      <div style={{ minWidth: 0, display: 'grid', gap: 5 }}>
        <button
          className="linklike truncate"
          onClick={() => set({ drawerId: b.id })}
          title={b.title}
        >
          {b.title}
        </button>
        <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <button
            className="step"
            aria-label={`Decrease progress for ${b.title}`}
            onClick={() => progress.mutate({ id: b.id, body: { delta: -1 } })}
          >
            <Icon name="minus" size={11} />
          </button>
          <strong style={{ fontVariantNumeric: 'tabular-nums' }}>{b.current.raw ?? '—'}</strong>
          <span className="muted">/ {b.totalUnits ?? '?'}</span>
          <button
            className="step"
            aria-label={`Add one to ${b.title}`}
            onClick={() => progress.mutate({ id: b.id, body: { delta: 1 } })}
          >
            <Icon name="plus" size={11} />
          </button>
          {(b.behindBy ?? 0) > 0 && <span className="chip behind">{b.behindBy} behind</span>}
        </div>
        {pct !== null && (
          <div className="bar">
            <i style={{ width: `${pct}%` }} />
          </div>
        )}
        {b.leftOff && (
          <div className="muted truncate" style={{ fontSize: 'var(--text-xs)' }} title={b.leftOff}>
            ↳ {b.leftOff}
          </div>
        )}
      </div>
    </article>
  );
}

function MiniList({
  items,
  empty,
  right,
}: {
  items: Brief[];
  empty: string;
  right: (b: Brief) => React.ReactNode;
}) {
  const { set } = useUI();
  const look = useLookup();
  if (!items.length)
    return (
      <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
        {empty}
      </div>
    );
  return (
    <ul className="minilist">
      {items.map((b) => (
        <li key={b.id}>
          <span className="cat-dot" style={{ background: look.color(b) }} />
          <button className="linklike truncate" onClick={() => set({ drawerId: b.id })}>
            {b.title}
          </button>
          <span
            className="muted"
            style={{ marginLeft: 'auto', fontSize: 'var(--text-xs)', whiteSpace: 'nowrap' }}
          >
            {right(b)}
          </span>
        </li>
      ))}
    </ul>
  );
}

function Welcome() {
  const { set, toast } = useUI();
  const qc = useQueryClient();
  const startBlank = async () => {
    const starters: [string, string][] = [
      ['reading', 'Romance'],
      ['reading', 'Action'],
      ['reading', 'Fantasy'],
      ['reading', 'Misc.'],
      ['watching', 'Anime'],
      ['watching', 'TV Shows'],
      ['watching', 'Misc.'],
    ];
    for (const [library, name] of starters) await api('POST', '/categories', { library, name });
    invalidateAll(qc);
    toast({ tone: 'success', message: 'Starter categories created. Add your first title!' });
    set({ route: 'library', quickAdd: true });
  };
  return (
    <Empty title="Welcome to your lorekeeper" icon="book">
      Bring in your existing trackers, or start with a clean slate.
      <br />
      <span style={{ display: 'inline-flex', gap: 8, marginTop: 14 }}>
        <button className="btn primary" onClick={() => set({ route: 'import' })}>
          <Icon name="import" size={14} /> Import my spreadsheets
        </button>
        <button className="btn" onClick={() => void startBlank()}>
          Start blank
        </button>
      </span>
    </Empty>
  );
}

export function Dashboard() {
  const [lib, setLib] = useState<'all' | 'reading' | 'watching'>('all');
  const { data: o, isLoading } = useOverview(lib);
  const { set, setFilters, resetFilters } = useUI();
  const look = useLookup();
  const [focusTag, setFocusTag] = useState('');

  const spark = useMemo(() => {
    const out: number[] = [];
    for (let i = 29; i >= 0; i--)
      out.push(o?.heatmap[new Date(Date.now() - i * 86400_000).toISOString().slice(0, 10)] ?? 0);
    return out;
  }, [o]);

  if (isLoading || !o)
    return (
      <div className="page-pad">
        <div className="skeleton" style={{ height: 120 }} />
      </div>
    );
  if (o.totals.entries === 0 && look.cats.length === 0) return <Welcome />;

  const openLibrary = (patch: Parameters<typeof setFilters>[0]) => {
    resetFilters();
    setFilters(patch);
    set({ route: 'library' });
  };
  const tag = focusTag || o.topTags[0]?.name || '';
  const withTag = o.tagCooccurrence
    .filter((p) => p.a === tag || p.b === tag)
    .map((p) => ({ label: p.a === tag ? p.b : p.a, value: p.count }))
    .slice(0, 8);
  const statusSlices = Object.entries(o.byStatus).map(([k, v]) => ({
    label: k === 'none' ? 'No status' : (GENERIC_STATUS[k as keyof typeof GENERIC_STATUS] ?? k),
    value: v,
    color: STATUS_COLORS[k] ?? 'var(--surface-3)',
  }));

  return (
    <div className="page-pad dash">
      <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
        <h1 style={{ fontSize: 'var(--text-2xl)' }}>Dashboard</h1>
        <div className="seg" role="group" aria-label="Library">
          {(['all', 'reading', 'watching'] as const).map((l) => (
            <button key={l} aria-pressed={lib === l} onClick={() => setLib(l)}>
              {l === 'all' ? 'All' : l === 'reading' ? 'Reading' : 'Watching'}
            </button>
          ))}
        </div>
      </div>

      <div className="stats">
        <Stat
          label="Total titles"
          value={o.totals.entries.toLocaleString()}
          hint="in your library"
          spark={spark}
        />
        <Stat
          label="Reading vs watching"
          value={`${o.totals.reading} / ${o.totals.watching}`}
          hint="reading / watching"
        />
        <Stat
          label="Completed"
          value={o.totals.completed}
          hint={
            o.totals.entries
              ? `${Math.round((o.totals.completed / o.totals.entries) * 100)}% of library`
              : ''
          }
        />
        <Stat
          label="Average score"
          value={o.totals.avgScore ? o.totals.avgScore.toFixed(1) : '—'}
          hint="out of 10"
        />
        <button className="stat stat-btn" onClick={() => openLibrary({ behind: true })}>
          <div className="muted label">Behind the recap</div>
          <div
            className="stat-value"
            style={{ color: o.totals.behind ? 'var(--warning)' : undefined }}
          >
            {o.totals.behind}
          </div>
          <div className="muted" style={{ fontSize: 'var(--text-xs)' }}>
            view them →
          </div>
        </button>
      </div>

      <Card
        title="Continue"
        span={3}
        action={
          <button
            className="btn ghost sm"
            onClick={() =>
              openLibrary({ status: ['in_progress'], sort: 'updatedAt', order: 'desc' })
            }
          >
            See all
          </button>
        }
      >
        {o.inProgress.length ? (
          <div className="shelf">
            {o.inProgress.slice(0, 12).map((b) => (
              <ShelfCard key={b.id} b={b} />
            ))}
          </div>
        ) : (
          <div className="muted">Nothing in progress. Start something from your backlog.</div>
        )}
      </Card>

      <div className="dgrid">
        <Card title="Status">
          <Donut slices={statusSlices} />
        </Card>
        <Card title="Score distribution">
          <VBars
            data={o.scoreDistribution.map((d) => ({ label: String(d.score), value: d.count }))}
          />
        </Card>
        <Card title="Titles per category">
          <HBars
            data={o.byCategory
              .filter((c) => c.count > 0)
              .sort((a, b) => b.count - a.count)
              .map((c) => ({ label: c.name, value: c.count, color: c.color }))}
            onPick={(name) => {
              const c = o.byCategory.find((x) => x.name === name);
              if (c) openLibrary({ category: [c.id] });
            }}
          />
        </Card>
        <Card title="Top tags">
          <HBars
            data={o.topTags.slice(0, 10).map((t) => ({ label: t.name, value: t.count }))}
            onPick={(name) => openLibrary({ tags: [name] })}
          />
        </Card>
        <Card
          title="Goes with…"
          action={
            <select
              className="select inline"
              aria-label="Tag"
              value={tag}
              onChange={(e) => setFocusTag(e.target.value)}
            >
              {o.topTags.map((t) => (
                <option key={t.name}>{t.name}</option>
              ))}
            </select>
          }
        >
          {withTag.length ? (
            <HBars data={withTag} />
          ) : (
            <div className="muted">Not enough tagged titles yet.</div>
          )}
        </Card>
        <Card title="Activity">
          <Heatmap data={o.heatmap} />
        </Card>
      </div>

      <div className="dgrid">
        <Card title="Recently updated">
          <MiniList
            items={o.recentlyUpdated}
            empty="Nothing yet"
            right={(b) => timeAgo(b.updatedAt)}
          />
        </Card>
        <Card title={`Backlog (${o.backlog})`}>
          <MiniList items={o.backlogList} empty="Backlog is clear" right={() => ''} />
        </Card>
        <Card title="Highest rated">
          <MiniList
            items={o.highestRated}
            empty="Score some titles"
            right={(b) => `★ ${b.score}`}
          />
        </Card>
        <Card title="Needs a decision">
          <MiniList
            items={o.needsDecision.slice(0, 10)}
            empty="Nothing on hold or dropped"
            right={(b) => (b.status === 'dropped' ? 'Dropped' : 'On hold')}
          />
        </Card>
      </div>
    </div>
  );
}

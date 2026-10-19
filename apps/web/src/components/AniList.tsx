import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { Entry } from '@lorekeeper/shared';
import { api, ApiError, remoteImageUrl } from '../lib/api';
import { useLookup } from '../lib/helpers';
import {
  useAniQueue,
  useAniStatus,
  type Candidate,
  type Job,
  type QueueItem,
} from '../lib/anilist';
import { invalidateAll, useCategories } from '../lib/queries';
import { useUI } from '../lib/store';
import { Cover, Icon } from './ui';

const refreshAni = (qc: ReturnType<typeof useQueryClient>) => {
  void qc.invalidateQueries({ queryKey: ['anilist'] });
  invalidateAll(qc);
};

function CandidateCard({
  c,
  onConfirm,
  busy,
}: {
  c: Candidate;
  onConfirm: (c: Candidate) => void;
  busy: boolean;
}) {
  const pct = Math.round(c.confidence * 100);
  const units =
    c.type === 'MANGA'
      ? c.chapters
        ? `${c.chapters} ch`
        : null
      : c.episodes
        ? `${c.episodes} ep`
        : null;
  return (
    <article className="cand">
      <div className="cand-art">
        {c.cover ? (
          <img
            src={remoteImageUrl(c.cover)}
            alt=""
            loading="lazy"
            onError={(ev) => ((ev.target as HTMLImageElement).style.display = 'none')}
          />
        ) : (
          <Icon name="book" size={22} />
        )}
      </div>
      <div className="cand-body">
        <a
          href={c.siteUrl ?? '#'}
          target="_blank"
          rel="noopener noreferrer"
          className="cand-title"
          title={c.titles.join(' · ')}
        >
          {c.title}
        </a>
        <div className="muted" style={{ fontSize: 'var(--text-xs)' }}>
          {[c.year, c.format?.replace('_', ' '), c.country, units].filter(Boolean).join(' · ')}
        </div>
        <div className="bar" title={`${pct}% match`}>
          <i style={{ width: `${pct}%` }} />
        </div>
        <button className="btn sm primary" disabled={busy} onClick={() => onConfirm(c)}>
          Match · {pct}%
        </button>
      </div>
    </article>
  );
}

/** Candidates + search-again + paste-a-link, shared by the review queue and the entry drawer. */
export function MatchPicker({
  entry,
  initial,
  onDone,
}: {
  entry: Entry;
  initial?: Candidate[];
  onDone?: () => void;
}) {
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  const [cands, setCands] = useState<Candidate[] | null>(initial ?? null);
  const [busy, setBusy] = useState(false);
  const [q, setQ] = useState('');
  const [ref, setRef] = useState('');
  const fail = (e: unknown) =>
    toast({
      tone: 'error',
      message:
        e instanceof ApiError && e.code === 'OFFLINE'
          ? 'AniList is offline. Check your connection.'
          : (e as Error).message,
    });
  const run = async (fn: () => Promise<unknown>, msg?: string) => {
    setBusy(true);
    try {
      await fn();
      refreshAni(qc);
      if (msg) toast({ tone: 'success', message: msg });
      onDone?.();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const search = async (query?: string) => {
    setBusy(true);
    try {
      setCands(
        await api<Candidate[]>(
          'GET',
          `/anilist/search?entryId=${entry.id}${query ? `&q=${encodeURIComponent(query)}` : ''}`,
        ),
      );
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div style={{ display: 'grid', gap: 10 }}>
      {cands === null ? (
        <button className="btn" onClick={() => void search()} disabled={busy}>
          <Icon name="search" size={14} /> {busy ? 'Searching…' : 'Find matches on AniList'}
        </button>
      ) : cands.length === 0 ? (
        <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
          No candidates found. Try another search or paste a link.
        </div>
      ) : (
        <div className="cands">
          {cands.map((c) => (
            <CandidateCard
              key={c.id}
              c={c}
              busy={busy}
              onConfirm={(x) =>
                void run(
                  () => api('POST', `/anilist/match/${entry.id}/confirm`, { mediaId: x.id }),
                  `Matched “${entry.title}”`,
                )
              }
            />
          ))}
        </div>
      )}
      <form
        style={{ display: 'flex', gap: 6 }}
        onSubmit={(e) => {
          e.preventDefault();
          if (q.trim()) void search(q.trim());
        }}
      >
        <input
          className="input"
          placeholder="Search another title…"
          aria-label="Search AniList"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <button className="btn sm" disabled={busy || !q.trim()}>
          Search
        </button>
      </form>
      <form
        style={{ display: 'flex', gap: 6 }}
        onSubmit={(e) => {
          e.preventDefault();
          if (ref.trim())
            void run(
              () => api('POST', `/anilist/match/${entry.id}/set`, { ref: ref.trim() }),
              'Linked',
            );
        }}
      >
        <input
          className="input"
          placeholder="…or paste an AniList URL or ID"
          aria-label="AniList URL or ID"
          value={ref}
          onChange={(e) => setRef(e.target.value)}
        />
        <button className="btn sm" disabled={busy || !ref.trim()}>
          Link
        </button>
      </form>
    </div>
  );
}

/** The AniList section of the entry drawer. */
export function AniListPanel({ entry }: { entry: Entry }) {
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  const [more, setMore] = useState(false);
  const x = entry.external;
  const act = async (path: string, msg: string) => {
    try {
      await api('POST', path);
      refreshAni(qc);
      toast({ tone: 'success', message: msg });
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    }
  };
  if (!x.anilistId) {
    return (
      <div className="callout" style={{ display: 'grid', gap: 10 }}>
        <strong style={{ color: 'var(--text)' }}>AniList</strong>
        <MatchPicker entry={entry} />
      </div>
    );
  }
  const link =
    entry.links.find((l) => l.kind === 'anilist')?.url ??
    `https://anilist.co/${entry.library === 'reading' ? 'manga' : 'anime'}/${x.anilistId}`;
  const desc = x.description ?? '';
  return (
    <div className="callout" style={{ display: 'grid', gap: 10, color: 'var(--text)' }}>
      <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <strong>AniList</strong>
        <span className="chip tone-completed">
          matched
          {x.matchConfidence && x.matchConfidence < 1
            ? ` · ${Math.round(x.matchConfidence * 100)}%`
            : ''}
        </span>
        {[
          x.format?.replace('_', ' '),
          x.year,
          x.averageScore ? `★ ${x.averageScore}` : null,
          x.apiStatus?.toLowerCase().replace('_', ' '),
        ]
          .filter(Boolean)
          .map((t) => (
            <span key={String(t)} className="chip">
              {t}
            </span>
          ))}
        <a
          href={link}
          target="_blank"
          rel="noopener noreferrer"
          style={{ color: 'var(--accent-text)', marginLeft: 'auto' }}
        >
          Open on AniList ↗
        </a>
      </div>
      {desc && (
        <div>
          <p
            style={{
              margin: 0,
              whiteSpace: 'pre-line',
              ...(more
                ? {}
                : {
                    display: '-webkit-box',
                    WebkitLineClamp: 4,
                    WebkitBoxOrient: 'vertical',
                    overflow: 'hidden',
                  }),
            }}
          >
            {desc}
          </p>
          {desc.length > 220 && (
            <button
              className="linklike"
              style={{ color: 'var(--accent-text)' }}
              onClick={() => setMore(!more)}
            >
              {more ? 'Show less' : 'Read more'}
            </button>
          )}
        </div>
      )}
      {entry.altTitles.length > 0 && (
        <div className="tags-cell" style={{ flexWrap: 'wrap' }}>
          {entry.altTitles.slice(0, 6).map((t) => (
            <span key={t} className="chip tag">
              {t}
            </span>
          ))}
        </div>
      )}
      <div style={{ display: 'flex', gap: 8 }}>
        <button
          className="btn sm"
          onClick={() =>
            void act(`/anilist/entries/${entry.id}/suggested-tags`, 'Added suggested tags')
          }
        >
          Add suggested tags
        </button>
        <button
          className="btn sm ghost"
          onClick={() => void act(`/anilist/entries/${entry.id}/unmatch`, 'Match removed')}
        >
          Unmatch
        </button>
      </div>
    </div>
  );
}

function JobBar({ job, onCancel }: { job: Job; onCancel: () => void }) {
  const pct = job.total ? Math.round((job.done / job.total) * 100) : 0;
  return (
    <div className="dcard" role="status" aria-live="polite">
      <header>
        <h3>{job.kind === 'match' ? 'Matching titles' : 'Importing'}…</h3>
        <button className="btn sm" onClick={onCancel}>
          Cancel
        </button>
      </header>
      <div className="bar" style={{ height: 8 }}>
        <i style={{ width: `${pct}%` }} />
      </div>
      <div className="muted truncate" style={{ fontSize: 'var(--text-sm)' }}>
        {job.done} / {job.total}
        {job.message ? ` · ${job.message}` : ''}
      </div>
    </div>
  );
}

function QueueRow({ item }: { item: QueueItem }) {
  const qc = useQueryClient();
  const look = useLookup();
  const { set } = useUI();
  const e = item.entry;
  const act = async (path: string) => {
    await api('POST', path);
    refreshAni(qc);
  };
  return (
    <li className="qrow">
      <div className="qmine">
        <Cover entry={e} color={look.color(e)} size={44} radius={9} />
        <div style={{ minWidth: 0 }}>
          <button className="linklike truncate" onClick={() => set({ drawerId: e.id })}>
            {e.title}
          </button>
          <div className="muted" style={{ fontSize: 'var(--text-xs)' }}>
            {look.catName(e)}
            {e.mediaType !== 'unknown' ? ` · ${e.mediaType}` : ''}
          </div>
          {item.reason && (
            <div className="muted" style={{ fontSize: 'var(--text-xs)' }}>
              {item.reason}
            </div>
          )}
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 6 }}>
          {item.status === 'pending' && (
            <button
              className="btn sm ghost"
              onClick={() => void act(`/anilist/match/${e.id}/reject`)}
            >
              None of these
            </button>
          )}
          {item.status !== 'never' && item.status !== 'unlinkable' && (
            <button
              className="btn sm ghost"
              onClick={() => void act(`/anilist/match/${e.id}/never`)}
            >
              Never match
            </button>
          )}
          {(item.status === 'rejected' || item.status === 'never') && (
            <button className="btn sm" onClick={() => void act(`/anilist/match/${e.id}/retry`)}>
              Search again
            </button>
          )}
        </div>
      </div>
      {(item.status === 'pending' || item.status === 'rejected') && (
        <MatchPicker entry={e} initial={item.status === 'pending' ? item.candidates : undefined} />
      )}
    </li>
  );
}

function QueueTab() {
  const [status, setStatus] = useState<QueueItem['status']>('pending');
  const { data: st } = useAniStatus();
  const q = useAniQueue(status);
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  const job = st?.matchJob;
  const start = async () => {
    try {
      await api('POST', '/anilist/match/run', {});
      refreshAni(qc);
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    }
  };
  // Refresh the queue when a run finishes.
  useEffect(() => {
    if (!job) refreshAni(qc);
  }, [job, qc]);
  const labels: Record<QueueItem['status'], string> = {
    pending: 'To review',
    confirmed: 'Matched',
    rejected: 'No match',
    never: 'Never',
    unlinkable: 'Not on AniList',
  };
  return (
    <div style={{ display: 'grid', gap: 16 }}>
      <div
        className="dcard"
        style={{
          display: 'flex',
          flexDirection: 'row',
          alignItems: 'center',
          gap: 16,
          flexWrap: 'wrap',
        }}
      >
        <div>
          <div style={{ fontWeight: 600 }}>
            {st ? `${st.matched} of ${st.total} titles linked` : '…'}
          </div>
          <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
            Clear matches (≥ {st ? Math.round(st.autoConfirmThreshold * 100) : 92}%) link
            automatically; the rest wait here for you. Up to {st?.requestsPerMinute ?? 30} requests
            a minute.
          </div>
        </div>
        {st?.offline && <span className="chip behind">AniList unreachable</span>}
        <button
          className="btn primary"
          style={{ marginLeft: 'auto' }}
          disabled={!!job}
          onClick={() => void start()}
        >
          <Icon name="bolt" size={14} /> Match my library
        </button>
      </div>
      {job && (
        <JobBar
          job={job}
          onCancel={() =>
            void api('POST', `/anilist/jobs/${job.id}/cancel`).then(() => refreshAni(qc))
          }
        />
      )}
      <div className="seg" role="tablist" aria-label="Queue" style={{ justifySelf: 'start' }}>
        {(Object.keys(labels) as QueueItem['status'][]).map((s) => (
          <button key={s} role="tab" aria-selected={status === s} onClick={() => setStatus(s)}>
            {labels[s]} {st ? `(${st.queue[s]})` : ''}
          </button>
        ))}
      </div>
      {q.isLoading ? (
        <div className="skeleton" style={{ height: 100 }} />
      ) : q.data?.length ? (
        <ul className="qlist">
          {q.data.map((it) => (
            <QueueRow key={it.entry.id} item={it} />
          ))}
        </ul>
      ) : (
        <div className="muted">
          {status === 'pending'
            ? 'Nothing to review. Run matching to find candidates.'
            : 'Nothing here.'}
        </div>
      )}
    </div>
  );
}

function ImportTab() {
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  const cats = useCategories().data ?? [];
  const { data: st } = useAniStatus();
  const [username, setUsername] = useState('');
  const [anime, setAnime] = useState(true);
  const [manga, setManga] = useState(true);
  const [animeCat, setAnimeCat] = useState('');
  const [mangaCat, setMangaCat] = useState('');
  const [strategy, setStrategy] = useState('fill');
  const [preview, setPreview] = useState<{
    previewId: string;
    counts: { fetched: number; create: number; update: number; unchanged: number; skipped: number };
    sample: {
      title: string;
      status: string;
      score: number | null;
      progress: string | null;
      library: string;
    }[];
    warnings: string[];
  } | null>(null);
  const [busy, setBusy] = useState(false);
  const [jobId, setJobId] = useState<string | null>(null);
  const [result, setResult] = useState<Job | null>(null);
  const job = st?.importJob ?? null;
  const readingCats = cats.filter((c) => c.library === 'reading');
  const watchingCats = cats.filter((c) => c.library === 'watching');
  useEffect(() => {
    if (!animeCat && watchingCats[0])
      setAnimeCat((watchingCats.find((c) => c.name === 'Anime') ?? watchingCats[0]).id);
  }, [watchingCats, animeCat]);
  useEffect(() => {
    if (!mangaCat && readingCats[0]) setMangaCat(readingCats[0].id);
  }, [readingCats, mangaCat]);

  // When the running job disappears from status, fetch its final state.
  useEffect(() => {
    if (jobId && !job)
      void api<Job>('GET', `/anilist/jobs/${jobId}`).then((j) => {
        if (j.state !== 'running') {
          setResult(j);
          setJobId(null);
          refreshAni(qc);
        }
      });
  }, [job, jobId, qc]);
  useEffect(() => {
    if (job) setJobId(job.id);
  }, [job]);

  const body = () => ({
    username: username.trim(),
    types: [...(anime ? ['ANIME'] : []), ...(manga ? ['MANGA'] : [])],
    strategy,
    animeCategoryId: animeCat,
    mangaCategoryId: mangaCat,
  });
  const doPreview = async () => {
    setBusy(true);
    setResult(null);
    try {
      setPreview(await api('POST', '/anilist/import/preview', body()));
    } catch (e) {
      toast({
        tone: 'error',
        message:
          e instanceof ApiError && e.code === 'OFFLINE'
            ? 'AniList is offline. Check your connection.'
            : (e as Error).message,
      });
      setPreview(null);
    } finally {
      setBusy(false);
    }
  };
  const commit = async () => {
    if (!preview) return;
    try {
      const j = await api<Job>('POST', '/anilist/import/commit', { previewId: preview.previewId });
      setJobId(j.id);
      setPreview(null);
      refreshAni(qc);
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    }
  };
  const undo = async (token: string) => {
    await api('POST', `/import/undo/${token}`);
    setResult(null);
    refreshAni(qc);
    toast({ message: 'Import undone' });
  };

  return (
    <div style={{ display: 'grid', gap: 16, maxWidth: 820 }}>
      <section className="dcard">
        <header>
          <h3>Import a public AniList account</h3>
        </header>
        <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
          No login needed. Lists marked private on AniList cannot be read.
        </div>
        <div className="row2">
          <div className="field">
            <label htmlFor="ani-user">AniList username</label>
            <input
              id="ani-user"
              className="input"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="username"
            />
          </div>
          <div className="field">
            <label htmlFor="ani-strat">If a title already exists</label>
            <select
              id="ani-strat"
              className="select"
              value={strategy}
              onChange={(e) => setStrategy(e.target.value)}
            >
              <option value="fill">Fill empty fields only</option>
              <option value="skip">Skip it</option>
              <option value="overwrite">Overwrite with AniList values</option>
              <option value="keep_both">Keep both</option>
            </select>
          </div>
        </div>
        <div className="row2">
          <div className="field">
            <label className="pop-item">
              <input type="checkbox" checked={anime} onChange={(e) => setAnime(e.target.checked)} />{' '}
              Anime
            </label>
            <select
              className="select"
              aria-label="Anime category"
              disabled={!anime}
              value={animeCat}
              onChange={(e) => setAnimeCat(e.target.value)}
            >
              {watchingCats.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label className="pop-item">
              <input type="checkbox" checked={manga} onChange={(e) => setManga(e.target.checked)} />{' '}
              Manga
            </label>
            <select
              className="select"
              aria-label="Manga category"
              disabled={!manga}
              value={mangaCat}
              onChange={(e) => setMangaCat(e.target.value)}
            >
              {readingCats.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div>
          <button
            className="btn primary"
            disabled={busy || !username.trim() || (!anime && !manga)}
            onClick={() => void doPreview()}
          >
            {busy ? 'Fetching…' : 'Preview import'}
          </button>
        </div>
      </section>

      {preview && (
        <section className="dcard">
          <header>
            <h3>Preview</h3>
          </header>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <span className="chip">{preview.counts.fetched} on AniList</span>
            <span className="chip tone-completed">{preview.counts.create} new</span>
            <span className="chip tone-in_progress">{preview.counts.update} updated</span>
            <span className="chip">{preview.counts.unchanged} unchanged</span>
            <span className="chip">{preview.counts.skipped} skipped</span>
          </div>
          {preview.sample.length > 0 && (
            <ul className="minilist">
              {preview.sample.map((s, i) => (
                <li key={i}>
                  <span className="truncate">{s.title}</span>
                  <span
                    className="muted"
                    style={{ marginLeft: 'auto', fontSize: 'var(--text-xs)' }}
                  >
                    {s.status?.replace('_', ' ')}
                    {s.score ? ` · ★${s.score}` : ''}
                    {s.progress ? ` · ${s.progress}` : ''}
                  </span>
                </li>
              ))}
            </ul>
          )}
          {preview.warnings.map((w) => (
            <div key={w} className="callout">
              {w}
            </div>
          ))}
          <div>
            <button
              className="btn primary"
              disabled={preview.counts.create + preview.counts.update === 0}
              onClick={() => void commit()}
            >
              Import {preview.counts.create + preview.counts.update} titles
            </button>
          </div>
        </section>
      )}
      {job && (
        <JobBar
          job={job}
          onCancel={() =>
            void api('POST', `/anilist/jobs/${job.id}/cancel`).then(() => refreshAni(qc))
          }
        />
      )}
      {result && (
        <section className="dcard" role="status">
          <header>
            <h3>
              {result.state === 'done'
                ? 'Import complete'
                : result.state === 'cancelled'
                  ? 'Cancelled'
                  : result.state === 'offline'
                    ? 'AniList is offline'
                    : 'Import failed'}
            </h3>
          </header>
          {result.state === 'done' ? (
            <div>
              {result.result?.created} new, {result.result?.updated} updated,{' '}
              {result.result?.unchanged} unchanged.
            </div>
          ) : (
            <div className="muted">{result.message}</div>
          )}
          {result.state === 'done' && typeof result.result?.undoToken === 'string' && (
            <div>
              <button className="btn" onClick={() => void undo(String(result.result!.undoToken))}>
                Undo import
              </button>
            </div>
          )}
        </section>
      )}
    </div>
  );
}

export function AniListPage() {
  const [tab, setTab] = useState<'queue' | 'import'>('queue');
  return (
    <div className="page-pad" style={{ display: 'grid', gap: 18, alignContent: 'start' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
        <h1>AniList</h1>
        <div className="seg" role="tablist" aria-label="AniList sections">
          <button role="tab" aria-selected={tab === 'queue'} onClick={() => setTab('queue')}>
            Match review
          </button>
          <button role="tab" aria-selected={tab === 'import'} onClick={() => setTab('import')}>
            Import from account
          </button>
        </div>
      </div>
      {tab === 'queue' ? <QueueTab /> : <ImportTab />}
    </div>
  );
}

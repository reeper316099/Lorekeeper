import { useEffect, useRef, useState } from 'react';
import { useFocusTrap } from '../lib/focus';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  emptyProgress,
  parseLink,
  type ActivityRecord,
  type Entry,
  type Link,
} from '@lorekeeper/shared';
import { api, get } from '../lib/api';
import { behindOf, timeAgo, useLookup } from '../lib/helpers';
import {
  invalidateAll,
  progressFrom,
  useDeleteEntry,
  useEntry,
  useProgress,
  useTags,
  useUpdateEntry,
} from '../lib/queries';
import { useUI } from '../lib/store';
import { AniListPanel } from './AniList';
import { Cover, Icon } from './ui';
import { ScoreSelect, StatusSelect } from './cells';

function TagEditor({ tags, onChange }: { tags: string[]; onChange: (t: string[]) => void }) {
  const all = useTags().data ?? [];
  const [text, setText] = useState('');
  const lc = text.trim().toLowerCase();
  const suggestions = lc
    ? all
        .filter(
          (t) =>
            t.name.toLowerCase().includes(lc) &&
            !tags.some((x) => x.toLowerCase() === t.name.toLowerCase()),
        )
        .slice(0, 6)
    : [];
  const add = (t: string) => {
    const v = t.trim();
    if (v && !tags.some((x) => x.toLowerCase() === v.toLowerCase())) onChange([...tags, v]);
    setText('');
  };
  return (
    <div className="tag-editor">
      {tags.map((t) => (
        <span key={t} className="chip tag">
          {t}
          <button
            aria-label={`Remove tag ${t}`}
            onClick={() => onChange(tags.filter((x) => x !== t))}
          >
            ×
          </button>
        </span>
      ))}
      <input
        value={text}
        placeholder={tags.length ? '' : 'Add tags…'}
        aria-label="Add tag"
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ',') {
            e.preventDefault();
            add(text);
          } else if (e.key === 'Backspace' && !text && tags.length) onChange(tags.slice(0, -1));
        }}
        onBlur={() => text.trim() && add(text)}
      />
      {suggestions.length > 0 && (
        <div className="pop" style={{ top: '100%', width: '100%' }} role="listbox">
          {suggestions.map((s) => (
            <button
              key={s.id}
              className="pop-item"
              role="option"
              aria-selected="false"
              onMouseDown={(e) => {
                e.preventDefault();
                add(s.name);
              }}
            >
              {s.name} <span className="nav-count">{s.count}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

type LinkHint = 'site' | 'youtube' | 'other';

/** View, add, edit and remove the entry's links. Pasted text is normalized (scheme-less URLs get https://). */
function LinksEditor({ links, onChange }: { links: Link[]; onChange: (links: Link[]) => void }) {
  const [text, setText] = useState('');
  const [hint, setHint] = useState<LinkHint>('site');
  const [editing, setEditing] = useState<number | null>(null);
  const [draft, setDraft] = useState('');

  const add = () => {
    const l = parseLink(text, hint);
    if (!l) return;
    // The first link fills the main spreadsheet column ("YouTube Link"/"Link"), later ones "Other Links".
    onChange([
      ...links,
      { ...l, slot: links.some((x) => x.slot === 'primary') ? 'other' : 'primary' },
    ]);
    setText('');
  };
  const remove = (i: number) => onChange(links.filter((_, j) => j !== i));
  const save = (i: number) => {
    const old = links[i]!;
    const l = parseLink(draft, old.kind === 'anilist' ? 'other' : old.kind);
    if (!l) remove(i);
    else onChange(links.map((x, j) => (j === i ? { ...l, slot: old.slot } : x)));
    setEditing(null);
  };

  return (
    <div className="field">
      <span className="label">Links</span>
      {links.length === 0 && (
        <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
          No links yet. Add a YouTube recap, a reading site, or any note.
        </div>
      )}
      <div style={{ display: 'grid', gap: 6 }}>
        {links.map((l, i) => {
          const label = l.label ?? l.url ?? l.raw ?? '';
          return editing === i ? (
            <form
              key={i}
              style={{ display: 'flex', gap: 6 }}
              onSubmit={(e) => {
                e.preventDefault();
                save(i);
              }}
            >
              <input
                className="input"
                aria-label="Edit link"
                value={draft}
                autoFocus
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => e.key === 'Escape' && (e.stopPropagation(), setEditing(null))}
              />
              <button className="btn sm primary">Save</button>
              <button type="button" className="btn sm ghost" onClick={() => setEditing(null)}>
                Cancel
              </button>
            </form>
          ) : (
            <div key={i} style={{ display: 'flex', gap: 8, alignItems: 'center', minWidth: 0 }}>
              <span
                style={{ color: l.kind === 'youtube' ? 'var(--danger-ink)' : 'var(--text-muted)' }}
              >
                <Icon name={l.kind === 'youtube' ? 'bolt' : 'link'} size={15} />
              </span>
              {l.url ? (
                <a
                  href={l.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="truncate"
                  style={{ color: 'var(--accent-text)' }}
                  title={l.url}
                >
                  {label}
                </a>
              ) : (
                <span className="truncate" title={label}>
                  {label}
                </span>
              )}
              {l.channel && <span className="chip">{l.channel}</span>}
              {l.date && (
                <span className="muted" style={{ fontSize: 'var(--text-xs)' }}>
                  {l.date}
                </span>
              )}
              <span style={{ marginLeft: 'auto', display: 'flex', gap: 2, flex: 'none' }}>
                <button
                  className="btn ghost sm"
                  aria-label={`Edit link ${label}`}
                  onClick={() => {
                    setDraft(l.raw ?? l.url ?? l.label ?? '');
                    setEditing(i);
                  }}
                >
                  Edit
                </button>
                <button
                  className="btn ghost sm icon"
                  aria-label={`Remove link ${label}`}
                  onClick={() => remove(i)}
                >
                  <Icon name="close" size={13} />
                </button>
              </span>
            </div>
          );
        })}
      </div>
      <form
        style={{ display: 'flex', gap: 6 }}
        onSubmit={(e) => {
          e.preventDefault();
          add();
        }}
      >
        <select
          className="select"
          style={{ width: 'auto' }}
          aria-label="Link type"
          value={hint}
          onChange={(e) => setHint(e.target.value as LinkHint)}
        >
          <option value="site">Website</option>
          <option value="youtube">YouTube</option>
          <option value="other">Other</option>
        </select>
        <input
          className="input"
          aria-label="Add a link"
          placeholder="Paste a link or note…"
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        <button className="btn sm" disabled={!text.trim()}>
          Add
        </button>
      </form>
    </div>
  );
}

function Body({ entry }: { entry: Entry }) {
  const look = useLookup();
  const update = useUpdateEntry();
  const progress = useProgress();
  const del = useDeleteEntry();
  const qc = useQueryClient();
  const { set } = useUI();
  const color = look.color(entry);
  const patch = (p: Record<string, unknown>) => update.mutate({ id: entry.id, patch: p });
  const activity = useQuery({
    queryKey: ['activity', entry.id],
    queryFn: () => get<ActivityRecord[]>(`/entries/${entry.id}/activity`),
  });
  const [title, setTitle] = useState(entry.title);
  const [cur, setCur] = useState(entry.current.raw ?? '');
  const [yt, setYt] = useState(entry.consumedUpTo.raw ?? '');
  const [src, setSrc] = useState(entry.sources.join(', '));
  const [notes, setNotes] = useState(entry.notes);
  const behind = behindOf(entry);
  const units = entry.library === 'reading' ? 'chapter' : 'episode';
  const sameLib = look.cats.filter((c) => c.library === entry.library);

  const commitProgress = (
    raw: string,
    key: 'current' | 'consumedUpTo',
    original: string | null,
  ) => {
    if (raw.trim() === (original ?? '')) return;
    if (key === 'current' && raw.trim())
      progress.mutate({ id: entry.id, body: { set: raw.trim() } });
    else patch({ [key]: raw.trim() ? progressFrom(raw) : emptyProgress() });
  };
  const saveNotes = (k: keyof Entry['notes']) => {
    if ((notes[k] ?? null) !== (entry.notes[k] ?? null))
      patch({ notes: { [k]: notes[k]?.trim() ? notes[k] : null } });
  };

  return (
    <>
      <div className="drawer-head">
        <Cover entry={entry} color={color} size={54} radius={10} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <input
            className="input"
            style={{
              fontFamily: 'var(--font-display)',
              fontSize: 'var(--text-lg)',
              fontWeight: 600,
              background: 'transparent',
              border: '1px solid transparent',
            }}
            aria-label="Title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onBlur={() => title.trim() && title !== entry.title && patch({ title: title.trim() })}
          />
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', paddingLeft: 10 }}>
            <span
              className="chip"
              style={{ background: `color-mix(in oklab, ${color} 30%, transparent)` }}
            >
              <span className="cat-dot" style={{ background: color }} />
              {look.catName(entry)}
            </span>
            <span className="muted" style={{ fontSize: 'var(--text-xs)' }}>
              {entry.mediaType !== 'unknown' ? entry.mediaType : entry.library}
            </span>
          </div>
        </div>
        <button
          className="btn ghost icon"
          aria-label="Toggle favorite"
          aria-pressed={entry.favorite}
          onClick={() => patch({ favorite: !entry.favorite })}
          style={{ color: entry.favorite ? 'var(--warning)' : undefined }}
        >
          <Icon name="star" />
        </button>
        <button
          className="btn ghost icon"
          aria-label="Close"
          onClick={() => set({ drawerId: null })}
        >
          <Icon name="close" />
        </button>
      </div>

      <div className="drawer-body">
        <div className="row2">
          <div className="field">
            <span className="label">Status</span>
            <div>
              <StatusSelect entry={entry} />
            </div>
          </div>
          <div className="field">
            <span className="label">Score</span>
            <div>
              <ScoreSelect entry={entry} />
            </div>
          </div>
        </div>

        <div className="field">
          <label htmlFor="cat">Category</label>
          <select
            id="cat"
            className="select"
            value={entry.categoryId}
            onChange={(e) =>
              void api('POST', `/entries/${entry.id}/move`, { categoryId: e.target.value }).then(
                () => invalidateAll(qc),
              )
            }
          >
            {sameLib.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </div>

        <div className="row2">
          <div className="field">
            <label htmlFor="cur">Currently on {units}</label>
            <div style={{ display: 'flex', gap: 6 }}>
              <button
                className="btn icon"
                aria-label="Decrease"
                disabled={entry.current.kind !== null && entry.current.number === null}
                onClick={() => progress.mutate({ id: entry.id, body: { delta: -1 } })}
              >
                <Icon name="minus" size={14} />
              </button>
              <input
                id="cur"
                className="input"
                value={cur}
                onChange={(e) => setCur(e.target.value)}
                onBlur={() => commitProgress(cur, 'current', entry.current.raw)}
              />
              <button
                className="btn icon"
                aria-label="Increase"
                disabled={entry.current.kind !== null && entry.current.number === null}
                onClick={() => {
                  progress.mutate({ id: entry.id, body: { delta: 1 } });
                  setCur(String((entry.current.number ?? 0) + 1));
                }}
              >
                <Icon name="plus" size={14} />
              </button>
            </div>
          </div>
          <div className="field">
            <label htmlFor="yt">
              {entry.library === 'reading' ? 'Watched on YouTube up to' : 'Watched until'}
            </label>
            <input
              id="yt"
              className="input"
              value={yt}
              onChange={(e) => setYt(e.target.value)}
              onBlur={() => commitProgress(yt, 'consumedUpTo', entry.consumedUpTo.raw)}
            />
          </div>
        </div>
        {behind > 0 && (
          <div className="chip behind" style={{ alignSelf: 'flex-start' }}>
            You’re {behind} {units}
            {behind === 1 ? '' : 's'} ahead of the recap you watch
          </div>
        )}

        <div className="row2">
          <div className="field">
            <label htmlFor="tot">Total {units}s</label>
            <input
              id="tot"
              className="input"
              type="number"
              min={0}
              defaultValue={entry.totalUnits ?? ''}
              onBlur={(e) => {
                const v = e.target.value ? Number(e.target.value) : null;
                if (v !== entry.totalUnits) patch({ totalUnits: v });
              }}
            />
          </div>
          {entry.library === 'watching' && (
            <div className="field">
              <label htmlFor="season">Season</label>
              <input
                id="season"
                className="input"
                defaultValue={entry.season ?? ''}
                onBlur={(e) => {
                  const v = e.target.value.trim() || null;
                  if (v !== entry.season) patch({ season: v });
                }}
              />
            </div>
          )}
        </div>

        <div className="field">
          <label htmlFor="src">
            {entry.library === 'reading' ? 'Reading method' : 'Watch platform'}
          </label>
          <input
            id="src"
            className="input"
            value={src}
            placeholder="Comma separated"
            onChange={(e) => setSrc(e.target.value)}
            onBlur={() => {
              const v = src
                .split(',')
                .map((s) => s.trim())
                .filter(Boolean);
              if (v.join() !== entry.sources.join()) patch({ sources: v });
            }}
          />
        </div>

        <div className="field">
          <span className="label">Tags</span>
          <TagEditor tags={entry.tags} onChange={(t) => patch({ tags: t })} />
        </div>

        {(['extra1', 'general', 'extra2'] as const).map((k) => (
          <div className="field" key={k}>
            <label htmlFor={`n-${k}`}>{look.noteLabel(k)}</label>
            <textarea
              id={`n-${k}`}
              className="textarea"
              value={notes[k] ?? ''}
              onChange={(e) => setNotes({ ...notes, [k]: e.target.value })}
              onBlur={() => saveNotes(k)}
            />
          </div>
        ))}

        <LinksEditor links={entry.links} onChange={(links) => patch({ links })} />

        <AniListPanel entry={entry} />

        <div className="field">
          <span className="label">Activity</span>
          <div className="timeline">
            {activity.data?.length ? (
              <ul>
                {activity.data.slice(0, 12).map((a, i) => (
                  <li key={i}>
                    <span style={{ width: 70 }}>{timeAgo(a.at)}</span>
                    <span>
                      {a.kind === 'progress'
                        ? `Progress ${String(a.from ?? '—')} → ${String(a.to ?? '—')}`
                        : a.kind === 'status'
                          ? `Status ${String(a.from ?? '—')} → ${String(a.to ?? '—')}`
                          : a.kind}
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <span className="muted">No activity yet</span>
            )}
          </div>
        </div>
      </div>

      <div className="drawer-foot">
        <span className="muted" style={{ fontSize: 'var(--text-xs)', alignSelf: 'center' }}>
          Updated {timeAgo(entry.updatedAt)}
        </span>
        <div className="spacer" />
        <button
          className="btn danger"
          onClick={() => {
            set({ drawerId: null });
            del.mutate(entry);
          }}
        >
          <Icon name="trash" size={14} /> Delete
        </button>
      </div>
    </>
  );
}

export function Drawer() {
  const { drawerId, set } = useUI();
  const { data: entry, isError } = useEntry(drawerId);
  const panelRef = useRef<HTMLElement>(null);
  useFocusTrap(panelRef, !!drawerId);
  useEffect(() => {
    if (!drawerId) return;
    const key = (e: KeyboardEvent) =>
      e.key === 'Escape' &&
      !(e.target as HTMLElement)?.closest?.('.pop') &&
      set({ drawerId: null });
    document.addEventListener('keydown', key);
    return () => document.removeEventListener('keydown', key);
  }, [drawerId, set]);
  useEffect(() => {
    if (isError) set({ drawerId: null });
  }, [isError, set]);
  if (!drawerId) return null;
  return (
    <>
      <div
        className="scrim"
        style={{ background: 'transparent', backdropFilter: 'none' }}
        onClick={() => set({ drawerId: null })}
      />
      <aside
        ref={panelRef}
        className="drawer"
        role="dialog"
        aria-modal="true"
        aria-label="Title details"
        tabIndex={-1}
      >
        {entry ? (
          <Body key={entry.id} entry={entry} />
        ) : (
          <div style={{ padding: 24 }}>
            <div className="skeleton" style={{ height: 60 }} />
          </div>
        )}
      </aside>
    </>
  );
}

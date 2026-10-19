import type { Entry, Status } from '@lorekeeper/shared';
import { useLookup, behindOf } from '../lib/helpers';
import { useProgress, useUpdateEntry } from '../lib/queries';
import { Icon } from './ui';

export function StatusSelect({ entry }: { entry: Entry }) {
  const { statusOptions } = useLookup();
  const update = useUpdateEntry();
  return (
    <select
      className={`select inline chip tone-${entry.status ?? 'planned'}`}
      aria-label="Status"
      value={entry.status ?? ''}
      onChange={(e) =>
        update.mutate({
          id: entry.id,
          patch: { status: (e.target.value || null) as Status | null },
        })
      }
    >
      <option value="">—</option>
      {statusOptions(entry.library).map(([v, l]) => (
        <option key={v} value={v}>
          {l}
        </option>
      ))}
    </select>
  );
}

export function ScoreSelect({ entry }: { entry: Entry }) {
  const { scoreLabels } = useLookup();
  const update = useUpdateEntry();
  return (
    <select
      className="select inline"
      aria-label="Score"
      value={entry.score ?? ''}
      onChange={(e) =>
        update.mutate({
          id: entry.id,
          patch: { score: e.target.value ? Number(e.target.value) : null },
        })
      }
    >
      <option value="">Unscored</option>
      {scoreLabels(entry.library).map((l, i) => (
        <option key={i} value={i + 1}>
          {l}
        </option>
      ))}
    </select>
  );
}

export function ProgressCell({ entry, compact }: { entry: Entry; compact?: boolean }) {
  const progress = useProgress();
  const behind = behindOf(entry);
  const total = entry.totalUnits;
  const step = (delta: number) => progress.mutate({ id: entry.id, body: { delta } });
  const locked = entry.current.kind !== null && entry.current.number === null;
  return (
    <div className="progress-cell">
      <button
        className="step"
        aria-label="Decrease progress"
        onClick={() => step(-1)}
        disabled={locked}
      >
        <Icon name="minus" size={12} />
      </button>
      <span title={entry.current.raw ?? 'No progress yet'}>
        <strong>{entry.current.raw ?? '—'}</strong>
        {!compact && <span className="muted"> / {total ?? '?'}</span>}
      </span>
      <button
        className="step"
        aria-label="Increase progress"
        onClick={() => step(1)}
        disabled={locked}
      >
        <Icon name="plus" size={12} />
      </button>
      {behind > 0 && (
        <span className="chip behind" title="Your position is ahead of the recap/video you watch">
          {behind} behind
        </span>
      )}
    </div>
  );
}

export function TagChips({ tags, max = 3 }: { tags: string[]; max?: number }) {
  const { hiddenTags } = useLookup();
  const shown = tags.filter((t) => !hiddenTags.has(t.toLowerCase()));
  return (
    <div className="tags-cell">
      {shown.slice(0, max).map((t) => (
        <span key={t} className="chip tag">
          {t}
        </span>
      ))}
      {shown.length > max && <span className="chip tag">+{shown.length - max}</span>}
    </div>
  );
}

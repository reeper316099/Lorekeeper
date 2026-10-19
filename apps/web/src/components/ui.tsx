import { useEffect, useRef, useState, type ReactNode } from 'react';
import { readableTextColor, type Entry, type Status } from '@lorekeeper/shared';
import { coverUrl } from '../lib/api';
import { useUI } from '../lib/store';

const PATHS: Record<string, string> = {
  search:
    'M11 4a7 7 0 1 0 4.2 12.6l4 4 1.4-1.4-4-4A7 7 0 0 0 11 4Zm0 2a5 5 0 1 1 0 10 5 5 0 0 1 0-10Z',
  plus: 'M11 5h2v6h6v2h-6v6h-2v-6H5v-2h6V5Z',
  minus: 'M5 11h14v2H5v-2Z',
  close:
    'm6.4 5 5.6 5.6L17.6 5 19 6.4 13.4 12l5.6 5.6-1.4 1.4-5.6-5.6L6.4 19 5 17.6l5.6-5.6L5 6.4 6.4 5Z',
  library: 'M4 4h4v16H4V4Zm6 0h4v16h-4V4Zm6 2.5 3.8-1 3.7 13.8-3.8 1L16 6.5Z',
  dash: 'M4 4h7v9H4V4Zm9 0h7v5h-7V4ZM4 15h7v5H4v-5Zm9-4h7v9h-7v-9Z',
  tag: 'M3 12V4h8l10 10-8 8L3 12Zm5-5.5A1.5 1.5 0 1 0 8 9.5a1.5 1.5 0 0 0 0-3Z',
  folder: 'M3 5h7l2 2h9v12H3V5Z',
  import: 'M11 3h2v9.2l3.3-3.3 1.4 1.4L12 16l-5.7-5.7 1.4-1.4 3.3 3.3V3ZM4 18h16v3H4v-3Z',
  link: 'M10 13a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1 1.4 1.4 1-1a2 2 0 1 1 2.9 2.8l-3 3a2 2 0 0 1-2.8 0L10 13Zm4 -2a4 4 0 0 0-5.7 0l-3 3A4 4 0 0 0 11 19.7l1-1-1.4-1.4-1 1a2 2 0 1 1-2.9-2.8l3-3a2 2 0 0 1 2.8 0L14 11Z',
  settings:
    'M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7ZM10.6 2h2.8l.5 2.6 1.6.7 2.2-1.5 2 2-1.5 2.2.7 1.6 2.6.5v2.8l-2.6.5-.7 1.6 1.5 2.2-2 2-2.2-1.5-1.6.7-.5 2.6h-2.8l-.5-2.6-1.6-.7-2.2 1.5-2-2 1.5-2.2-.7-1.6L2 13.4v-2.8l2.6-.5.7-1.6-1.5-2.2 2-2 2.2 1.5 1.6-.7.5-2.6Z',
  grid: 'M4 4h7v7H4V4Zm9 0h7v7h-7V4ZM4 13h7v7H4v-7Zm9 0h7v7h-7v-7Z',
  rows: 'M4 5h16v3H4V5Zm0 5.5h16v3H4v-3ZM4 16h16v3H4v-3Z',
  sun: 'M12 7a5 5 0 1 0 0 10 5 5 0 0 0 0-10Zm-1-5h2v3h-2V2Zm0 17h2v3h-2v-3ZM2 11h3v2H2v-2Zm17 0h3v2h-3v-2ZM4.9 6.3l1.4-1.4 2.1 2.1-1.4 1.4-2.1-2.1Zm10.7 10.7 1.4-1.4 2.1 2.1-1.4 1.4-2.1-2.1Zm0-9.3 2.1-2.1 1.4 1.4-2.1 2.1-1.4-1.4ZM4.9 17.7l2.1-2.1 1.4 1.4-2.1 2.1-1.4-1.4Z',
  moon: 'M20 14.5A8.5 8.5 0 0 1 9.5 4 8.5 8.5 0 1 0 20 14.5Z',
  star: 'm12 3 2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1L3.2 9.5l6.1-.9L12 3Z',
  filter: 'M3 5h18l-7 8v6l-4 2v-8L3 5Z',
  chevron: 'm7 10 5 5 5-5H7Z',
  check: 'm9.5 16.2-4-4L4 13.7l5.5 5.5L20 8.7 18.5 7.2l-9 9Z',
  trash: 'M9 3h6l1 2h4v2H4V5h4l1-2ZM6 8h12l-1 13H7L6 8Z',
  book: 'M5 3h12a2 2 0 0 1 2 2v16H7a2 2 0 0 1-2-2V3Zm2 2v12.2c.3-.1.6-.2 1-.2h9V5H7Z',
  bolt: 'M13 2 4 14h6l-1 8 9-12h-6l1-8Z',
};

export function Icon({ name, size = 18 }: { name: keyof typeof PATHS | string; size?: number }) {
  return (
    <svg
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d={PATHS[name] ?? PATHS.book} />
    </svg>
  );
}

export function initials(title: string): string {
  const words = title
    .replace(/[^\p{L}\p{N} ]/gu, '')
    .split(/\s+/)
    .filter(Boolean);
  return ((words[0]?.[0] ?? '?') + (words[1]?.[0] ?? '')).toUpperCase();
}

export function Cover({
  entry,
  color,
  size = 36,
  radius,
}: {
  entry: Pick<Entry, 'title'> & { external?: { cover?: string }; cover?: string | null };
  color: string;
  size?: number;
  radius?: number;
}) {
  const file = entry.external?.cover ?? entry.cover ?? null;
  return (
    <span
      className="cover"
      style={{
        width: size,
        height: Math.round(size * 1.3),
        borderRadius: radius,
        background: `linear-gradient(145deg, ${color}, color-mix(in oklab, ${color} 55%, #000))`,
        color: readableTextColor(color),
      }}
      aria-hidden="true"
    >
      {file ? (
        <img
          src={coverUrl(file)}
          onError={(ev) => ((ev.target as HTMLImageElement).style.display = 'none')}
          alt=""
          loading="lazy"
          style={{
            position: 'absolute',
            inset: 0,
            width: '100%',
            height: '100%',
            objectFit: 'cover',
          }}
        />
      ) : (
        initials(entry.title)
      )}
    </span>
  );
}

export const STATUS_ORDER: Status[] = [
  'in_progress',
  'planned',
  'completed',
  'on_hold',
  'dropped',
  'repeating',
];
export const GENERIC_STATUS: Record<Status, string> = {
  planned: 'Planned',
  in_progress: 'In progress',
  completed: 'Completed',
  on_hold: 'On hold',
  dropped: 'Dropped',
  repeating: 'Repeating',
};

/** Popover that closes on outside click / Escape. */
export function Popover({
  trigger,
  children,
  align = 'left',
}: {
  trigger: (p: { open: boolean; toggle: () => void }) => ReactNode;
  children: (close: () => void) => ReactNode;
  align?: 'left' | 'right';
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const down = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    const key = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('mousedown', down);
    document.addEventListener('keydown', key);
    return () => {
      document.removeEventListener('mousedown', down);
      document.removeEventListener('keydown', key);
    };
  }, [open]);
  return (
    <div className="pop-wrap" ref={ref}>
      {trigger({ open, toggle: () => setOpen((o) => !o) })}
      {open && (
        <div
          className="pop"
          data-align={align}
          style={align === 'right' ? { left: 'auto', right: 0 } : undefined}
        >
          {children(() => setOpen(false))}
        </div>
      )}
    </div>
  );
}

export interface Option {
  value: string;
  label: string;
  color?: string;
  count?: number;
}

export function MultiSelect({
  label,
  options,
  selected,
  onChange,
  searchable,
}: {
  label: string;
  options: Option[];
  selected: string[];
  onChange: (v: string[]) => void;
  searchable?: boolean;
}) {
  const [q, setQ] = useState('');
  const shown = options.filter((o) => o.label.toLowerCase().includes(q.toLowerCase()));
  return (
    <Popover
      trigger={({ toggle, open }) => (
        <button className="btn sm" onClick={toggle} aria-expanded={open} aria-haspopup="listbox">
          {label} {selected.length > 0 && <span className="badge-n">{selected.length}</span>}
          <Icon name="chevron" size={14} />
        </button>
      )}
    >
      {() => (
        <div role="listbox" aria-label={label} aria-multiselectable="true">
          {searchable && (
            <input
              className="input"
              placeholder={`Search ${label.toLowerCase()}…`}
              value={q}
              onChange={(e) => setQ(e.target.value)}
              autoFocus
            />
          )}
          {shown.map((o) => (
            <label
              key={o.value}
              className="pop-item"
              role="option"
              aria-selected={selected.includes(o.value)}
            >
              <input
                type="checkbox"
                checked={selected.includes(o.value)}
                onChange={() =>
                  onChange(
                    selected.includes(o.value)
                      ? selected.filter((x) => x !== o.value)
                      : [...selected, o.value],
                  )
                }
              />
              {o.color && <span className="cat-dot" style={{ background: o.color }} />}
              <span className="truncate">{o.label}</span>
              {o.count !== undefined && <span className="nav-count">{o.count}</span>}
            </label>
          ))}
          {shown.length === 0 && (
            <div className="muted" style={{ padding: 8 }}>
              Nothing found
            </div>
          )}
        </div>
      )}
    </Popover>
  );
}

export function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  useEffect(() => {
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    document.addEventListener('keydown', key);
    return () => document.removeEventListener('keydown', key);
  }, [onClose]);
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <div className="modal" role="dialog" aria-modal="true" aria-label={title}>
        <h2>{title}</h2>
        {children}
      </div>
    </>
  );
}

export function Toasts() {
  const { toasts, dismissToast } = useUI();
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast ${t.tone ?? ''}`}>
          <span>{t.message}</span>
          {t.action && (
            <button
              onClick={() => {
                t.action!.run();
                dismissToast(t.id);
              }}
            >
              {t.action.label}
            </button>
          )}
        </div>
      ))}
    </div>
  );
}

export function Empty({
  title,
  children,
  icon = 'book',
}: {
  title: string;
  children?: ReactNode;
  icon?: string;
}) {
  return (
    <div className="empty">
      <div className="brand-mark" style={{ width: 52, height: 52, borderRadius: 16 }}>
        <Icon name={icon} size={26} />
      </div>
      <h2>{title}</h2>
      <div className="muted">{children}</div>
    </div>
  );
}

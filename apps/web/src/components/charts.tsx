import { useState } from 'react';

/** Small SVG charts themed from CSS tokens. Everything is keyboard-reachable via titles and lists. */

export function Sparkline({
  values,
  width = 120,
  height = 36,
}: {
  values: number[];
  width?: number;
  height?: number;
}) {
  const max = Math.max(1, ...values);
  const step = width / Math.max(1, values.length - 1);
  const pts = values.map(
    (v, i) => `${(i * step).toFixed(1)},${(height - 3 - (v / max) * (height - 6)).toFixed(1)}`,
  );
  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label="Activity over the last 30 days"
    >
      <defs>
        <linearGradient id="spark" x1="0" x2="0" y1="0" y2="1">
          <stop offset="0" stopColor="var(--accent)" stopOpacity="0.35" />
          <stop offset="1" stopColor="var(--accent)" stopOpacity="0" />
        </linearGradient>
      </defs>
      <polygon points={`0,${height} ${pts.join(' ')} ${width},${height}`} fill="url(#spark)" />
      <polyline
        points={pts.join(' ')}
        fill="none"
        stroke="var(--accent)"
        strokeWidth="2"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

export interface Slice {
  label: string;
  value: number;
  color: string;
}

export function Donut({
  slices,
  size = 170,
  centerLabel,
}: {
  slices: Slice[];
  size?: number;
  centerLabel?: string;
}) {
  const total = slices.reduce((s, x) => s + x.value, 0);
  const r = size / 2 - 14;
  const c = 2 * Math.PI * r;
  let offset = 0;
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 20, flexWrap: 'wrap' }}>
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${size} ${size}`}
        role="img"
        aria-label="Status breakdown"
      >
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke="var(--surface-3)"
          strokeWidth="18"
        />
        {total > 0 &&
          slices
            .filter((s) => s.value > 0)
            .map((s) => {
              const len = (s.value / total) * c;
              const el = (
                <circle
                  key={s.label}
                  cx={size / 2}
                  cy={size / 2}
                  r={r}
                  fill="none"
                  stroke={s.color}
                  strokeWidth="18"
                  strokeDasharray={`${Math.max(0, len - 2)} ${c - len + 2}`}
                  strokeDashoffset={-offset}
                  transform={`rotate(-90 ${size / 2} ${size / 2})`}
                >
                  <title>{`${s.label}: ${s.value}`}</title>
                </circle>
              );
              offset += len;
              return el;
            })}
        <text
          x="50%"
          y="48%"
          textAnchor="middle"
          fontSize="26"
          fontWeight="700"
          fill="var(--text)"
          fontFamily="var(--font-display)"
        >
          {total}
        </text>
        <text x="50%" y="62%" textAnchor="middle" fontSize="11" fill="var(--text-muted)">
          {centerLabel ?? 'titles'}
        </text>
      </svg>
      <ul
        style={{
          listStyle: 'none',
          margin: 0,
          padding: 0,
          display: 'grid',
          gap: 6,
          fontSize: 'var(--text-sm)',
        }}
      >
        {slices.map((s) => (
          <li key={s.label} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span className="cat-dot" style={{ background: s.color }} /> {s.label}
            <span
              className="muted"
              style={{ marginLeft: 'auto', paddingLeft: 16, fontVariantNumeric: 'tabular-nums' }}
            >
              {s.value}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function VBars({
  data,
  height = 150,
}: {
  data: { label: string; value: number }[];
  height?: number;
}) {
  const max = Math.max(1, ...data.map((d) => d.value));
  return (
    <div
      role="img"
      aria-label="Distribution"
      style={{ display: 'flex', alignItems: 'flex-end', gap: 8, height: height + 22 }}
    >
      {data.map((d) => (
        <div
          key={d.label}
          style={{
            flex: 1,
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            gap: 4,
          }}
          title={`${d.label}: ${d.value}`}
        >
          <span className="muted" style={{ fontSize: 'var(--text-xs)' }}>
            {d.value || ''}
          </span>
          <div
            style={{
              width: '100%',
              height: Math.max(3, (d.value / max) * height),
              borderRadius: '6px 6px 2px 2px',
              background: d.value ? 'var(--gradient-primary)' : 'var(--surface-3)',
              transition: 'height 300ms var(--ease)',
            }}
          />
          <span className="muted" style={{ fontSize: 'var(--text-xs)' }}>
            {d.label}
          </span>
        </div>
      ))}
    </div>
  );
}

export function HBars({
  data,
  onPick,
}: {
  data: { label: string; value: number; color?: string }[];
  onPick?: (label: string) => void;
}) {
  const max = Math.max(1, ...data.map((d) => d.value));
  return (
    <div style={{ display: 'grid', gap: 7 }}>
      {data.map((d) => (
        <button
          key={d.label}
          onClick={() => onPick?.(d.label)}
          disabled={!onPick}
          title={`${d.label}: ${d.value}`}
          style={{
            display: 'grid',
            gridTemplateColumns: '110px 1fr 30px',
            gap: 10,
            alignItems: 'center',
            background: 'none',
            border: 0,
            padding: 0,
            textAlign: 'left',
            cursor: onPick ? 'pointer' : 'default',
          }}
        >
          <span className="truncate" style={{ fontSize: 'var(--text-sm)' }}>
            {d.label}
          </span>
          <span className="bar" style={{ height: 8 }}>
            <i style={{ width: `${(d.value / max) * 100}%`, background: d.color ?? undefined }} />
          </span>
          <span className="muted" style={{ fontSize: 'var(--text-xs)', textAlign: 'right' }}>
            {d.value}
          </span>
        </button>
      ))}
    </div>
  );
}

/** GitHub-style yearly heatmap (last ~26 weeks) from { 'YYYY-MM-DD': count }. */
export function Heatmap({ data, weeks = 26 }: { data: Record<string, number>; weeks?: number }) {
  const [hover, setHover] = useState<string | null>(null);
  const today = new Date();
  today.setHours(12, 0, 0, 0);
  const start = new Date(today);
  start.setDate(start.getDate() - (weeks * 7 - 1) - today.getDay());
  const max = Math.max(1, ...Object.values(data));
  const cols: { key: string; n: number }[][] = [];
  for (let w = 0; w < weeks + 1; w++) {
    const col: { key: string; n: number }[] = [];
    for (let d = 0; d < 7; d++) {
      const dt = new Date(start);
      dt.setDate(start.getDate() + w * 7 + d);
      if (dt > today) break;
      const key = dt.toISOString().slice(0, 10);
      col.push({ key, n: data[key] ?? 0 });
    }
    cols.push(col);
  }
  return (
    <div>
      <div style={{ display: 'flex', gap: 3 }} role="img" aria-label="Activity heatmap">
        {cols.map((col, i) => (
          <div key={i} style={{ display: 'grid', gap: 3 }}>
            {col.map((c) => (
              <span
                key={c.key}
                onMouseEnter={() => setHover(`${c.key}: ${c.n} update${c.n === 1 ? '' : 's'}`)}
                onMouseLeave={() => setHover(null)}
                style={{
                  width: 12,
                  height: 12,
                  borderRadius: 3,
                  background: c.n
                    ? `color-mix(in oklab, var(--accent) ${25 + (c.n / max) * 75}%, var(--surface-3))`
                    : 'var(--surface-3)',
                }}
              />
            ))}
          </div>
        ))}
      </div>
      <div className="muted" style={{ fontSize: 'var(--text-xs)', marginTop: 8, minHeight: 16 }}>
        {hover ?? 'Hover a day'}
      </div>
    </div>
  );
}

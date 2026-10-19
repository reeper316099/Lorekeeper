import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { invalidateAll } from '../lib/queries';
import { useUI } from '../lib/store';
import { Empty, Icon } from './ui';

const ACCENTS = [
  '#f2603c',
  '#e8486f',
  '#f5b544',
  '#3ecf8e',
  '#2fb8c9',
  '#4f8cff',
  '#9b7bff',
  '#d46bd6',
  '#a3b18a',
  '#c9a27e',
];

export function SettingsPage() {
  const { theme, accent, density, set } = useUI();
  return (
    <div className="page-pad" style={{ maxWidth: 640, display: 'grid', gap: 24 }}>
      <h1>Settings</h1>
      <div className="field">
        <span className="label">Theme</span>
        <div className="seg" role="group" aria-label="Theme" style={{ justifySelf: 'start' }}>
          {(['dark', 'light', 'system'] as const).map((t) => (
            <button key={t} aria-pressed={theme === t} onClick={() => set({ theme: t })}>
              {t[0]!.toUpperCase() + t.slice(1)}
            </button>
          ))}
        </div>
      </div>
      <div className="field">
        <span className="label">Accent</span>
        <div className="swatches">
          {ACCENTS.map((c) => (
            <button
              key={c}
              className="swatch"
              aria-label={`Accent ${c}`}
              aria-pressed={accent === c}
              style={{ background: c }}
              onClick={() => set({ accent: c })}
            />
          ))}
          <input
            type="color"
            aria-label="Custom accent"
            value={accent}
            onChange={(e) => set({ accent: e.target.value })}
            style={{ width: 28, height: 28, border: 0, background: 'none', padding: 0 }}
          />
        </div>
      </div>
      <div className="field">
        <span className="label">Density</span>
        <div className="seg" role="group" aria-label="Density" style={{ justifySelf: 'start' }}>
          {(['comfortable', 'compact'] as const).map((d) => (
            <button key={d} aria-pressed={density === d} onClick={() => set({ density: d })}>
              {d[0]!.toUpperCase() + d.slice(1)}
            </button>
          ))}
        </div>
      </div>
      <div className="callout">
        Score label sets, hidden tags, backups and data folder settings arrive in the next
        milestone.
      </div>
    </div>
  );
}

interface PreviewResult {
  importId: string;
  fileName: string;
  counts: { create: number; update: number; unchanged: number; skipped: number };
  warnings: { code: string; sheet: string; row?: number; title?: string; message: string }[];
  summary: { entries: number; byCategory: Record<string, number> };
}

/** Minimal import flow (upload → preview → commit → undo). The full wizard lands in M4. */
export function ImportPage() {
  const { toast, set } = useUI();
  const qc = useQueryClient();
  const [preview, setPreview] = useState<PreviewResult | null>(null);
  const [done, setDone] = useState<{ undoToken: string; counts: Record<string, number> } | null>(
    null,
  );
  const [busy, setBusy] = useState(false);

  const upload = async (file: File) => {
    setBusy(true);
    setDone(null);
    try {
      const { bridgeFetch } = await import('../lib/upload');
      const { importId } = await bridgeFetch(file);
      setPreview(await api<PreviewResult>('POST', '/import/preview', { importId }));
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };
  const commit = async () => {
    if (!preview) return;
    setBusy(true);
    try {
      const r = await api<{ undoToken: string; counts: Record<string, number> }>(
        'POST',
        '/import/commit',
        { importId: preview.importId, options: { strategy: 'fill' } },
      );
      setDone(r);
      setPreview(null);
      invalidateAll(qc);
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };
  const undo = async () => {
    if (!done) return;
    await api('POST', `/import/undo/${done.undoToken}`);
    setDone(null);
    invalidateAll(qc);
    toast({ message: 'Import undone' });
  };

  return (
    <div className="page-pad" style={{ maxWidth: 760, display: 'grid', gap: 20 }}>
      <h1>Import &amp; export</h1>
      <label
        className="callout"
        style={{
          display: 'grid',
          placeItems: 'center',
          gap: 8,
          padding: 40,
          cursor: 'pointer',
          textAlign: 'center',
        }}
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault();
          const f = e.dataTransfer.files[0];
          if (f) void upload(f);
        }}
      >
        <Icon name="import" size={28} />
        <strong style={{ color: 'var(--text)' }}>
          Drop an .xlsx or .csv here, or click to choose
        </strong>
        <span>Existing titles are kept; empty fields are filled in.</span>
        <input
          type="file"
          accept=".xlsx,.xlsm,.csv,.tsv"
          hidden
          onChange={(e) => e.target.files?.[0] && void upload(e.target.files[0])}
        />
      </label>
      {busy && <div className="skeleton" style={{ height: 40 }} />}
      {preview && (
        <section className="callout" style={{ display: 'grid', gap: 10, color: 'var(--text)' }}>
          <h2>{preview.fileName}</h2>
          <div>
            {preview.counts.create} new · {preview.counts.update} updated ·{' '}
            {preview.counts.unchanged} unchanged · {preview.counts.skipped} skipped
          </div>
          <div className="tags-cell" style={{ flexWrap: 'wrap' }}>
            {Object.entries(preview.summary.byCategory).map(([k, v]) => (
              <span key={k} className="chip">
                {k}: {v}
              </span>
            ))}
          </div>
          {preview.warnings.length > 0 && (
            <details>
              <summary>{preview.warnings.length} warnings</summary>
              <ul style={{ maxHeight: 180, overflow: 'auto' }}>
                {preview.warnings.map((w, i) => (
                  <li key={i}>
                    {w.sheet}
                    {w.row ? ` row ${w.row}` : ''}: {w.message}
                  </li>
                ))}
              </ul>
            </details>
          )}
          <div>
            <button className="btn primary" onClick={() => void commit()} disabled={busy}>
              Import {preview.counts.create + preview.counts.update} titles
            </button>
          </div>
        </section>
      )}
      {done && (
        <section
          className="callout"
          style={{ color: 'var(--text)', display: 'flex', gap: 12, alignItems: 'center' }}
        >
          <span>
            Imported: {done.counts.created} new, {done.counts.updated} updated,{' '}
            {done.counts.unchanged} unchanged.
          </span>
          <button className="btn" onClick={() => void undo()}>
            Undo
          </button>
          <button className="btn primary" onClick={() => set({ route: 'library' })}>
            Open library
          </button>
        </section>
      )}
    </div>
  );
}

export function Soon({ title, milestone }: { title: string; milestone: string }) {
  return <Empty title={title}>This screen is planned for {milestone}.</Empty>;
}

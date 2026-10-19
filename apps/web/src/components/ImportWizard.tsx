import { useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, get } from '../lib/api';
import { invalidateAll } from '../lib/queries';
import { useUI } from '../lib/store';
import { bridgeFetch, download } from '../lib/upload';
import { Icon } from './ui';

const FIELD_LABELS: Record<string, string> = {
  title: 'Title',
  status: 'Status',
  score: 'Score',
  tags: 'Tags',
  season: 'Season',
  current: 'Currently on',
  consumed: 'Watched up to',
  youtubeLink: 'YouTube link',
  link: 'Link',
  sources: 'Method / platform',
  general: 'General notes',
  extra1: 'Extra notes 1 (left off)',
  extra2: 'Extra notes 2',
  otherLinks: 'Other links',
  category: 'Category',
};

interface SheetPlan {
  name: string;
  skipped?: string;
  library: 'reading' | 'watching' | null;
  libraryDetected: string | null;
  headerRow: number;
  rows: number;
  categoryName: string;
  categoryExists: boolean;
  mapping: { col: number; header: string; field: string | null }[];
}
interface Warning {
  code: string;
  sheet: string;
  row?: number;
  title?: string;
  message: string;
}
interface Preview {
  importId: string;
  fileName: string;
  sheets: SheetPlan[];
  newCategories: { library: string; name: string }[];
  counts: { create: number; update: number; unchanged: number; skipped: number };
  warnings: Warning[];
  summary: {
    entries: number;
    byCategory: Record<string, number>;
    byStatus: Record<string, number>;
    distinctTagsRaw: number;
    distinctTagsNormalized: number;
  };
  tagSuggestions: { from: string; to: string }[];
}
interface Report {
  importId: string;
  undoToken: string;
  counts: Record<string, number>;
  warnings: Warning[];
  tagSuggestions: { from: string; to: string }[];
}
interface Options {
  strategy: 'skip' | 'fill' | 'overwrite' | 'keep_both';
  sheets: Record<
    string,
    {
      include?: boolean;
      library?: 'reading' | 'watching';
      category?: string;
      mapping?: (string | null)[];
    }
  >;
  statusMap: Record<string, string>;
}

const STEPS = ['Choose file', 'Review mapping', 'Warnings & duplicates', 'Import', 'Report'];

function Stepper({ step }: { step: number }) {
  return (
    <ol className="stepper" aria-label="Import steps">
      {STEPS.map((s, i) => (
        <li
          key={s}
          aria-current={i === step ? 'step' : undefined}
          className={i < step ? 'done' : ''}
        >
          <span>{i < step ? '✓' : i + 1}</span>
          {s}
        </li>
      ))}
    </ol>
  );
}

function Backups() {
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  const list = useQuery({
    queryKey: ['backups'],
    queryFn: () => get<{ name: string; size: number; createdAt: string }[]>('/backups'),
  });
  const create = async () => {
    await api('POST', '/backups');
    void qc.invalidateQueries({ queryKey: ['backups'] });
    toast({ tone: 'success', message: 'Backup created' });
  };
  const restore = async (name: string) => {
    if (!confirm(`Restore ${name}? Your current data is backed up first.`)) return;
    await api('POST', '/backup/restore', { name });
    invalidateAll(qc);
    void qc.invalidateQueries({ queryKey: ['backups'] });
    toast({ tone: 'success', message: 'Backup restored' });
  };
  return (
    <section className="dcard">
      <header>
        <h3>Backups</h3>
        <button className="btn sm" onClick={() => void create()}>
          <Icon name="plus" size={13} /> Back up now
        </button>
      </header>
      <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
        Automatic snapshots happen on launch, before imports and before bulk deletes.
      </div>
      <ul className="mgr-list">
        {(list.data ?? []).map((b) => (
          <li key={b.name} className="mgr-row">
            <span className="truncate">{b.name}</span>
            <span className="muted" style={{ fontSize: 'var(--text-xs)' }}>
              {(b.size / 1024).toFixed(0)} KB
            </span>
            <button
              className="btn sm"
              style={{ marginLeft: 'auto' }}
              onClick={() => void restore(b.name)}
            >
              Restore
            </button>
          </li>
        ))}
        {list.data?.length === 0 && <li className="muted">No backups yet.</li>}
      </ul>
    </section>
  );
}

function ExportPanel() {
  const toast = useUI((s) => s.toast);
  const dl = (path: string, name: string) =>
    download(path, name).catch((e) => toast({ tone: 'error', message: (e as Error).message }));
  return (
    <section className="dcard">
      <header>
        <h3>Export</h3>
      </header>
      <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
        XLSX keeps your original layout, colors and dropdowns. CSV opens cleanly in Excel.
      </div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button
          className="btn"
          onClick={() => void dl('/export/xlsx?library=reading', 'reading.xlsx')}
        >
          Reading .xlsx
        </button>
        <button
          className="btn"
          onClick={() => void dl('/export/xlsx?library=watching', 'watching.xlsx')}
        >
          Watching .xlsx
        </button>
        <button
          className="btn"
          onClick={() => void dl('/export/csv?library=reading', 'reading.csv')}
        >
          Reading .csv
        </button>
        <button
          className="btn"
          onClick={() => void dl('/export/csv?library=watching', 'watching.csv')}
        >
          Watching .csv
        </button>
        <button className="btn" onClick={() => void dl('/export/json', 'backup.zip')}>
          Full backup (.zip)
        </button>
      </div>
      <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
        Templates:{' '}
        <button
          className="linklike"
          style={{ color: 'var(--accent-text)' }}
          onClick={() => void dl('/export/csv-template?library=reading', 'reading-template.csv')}
        >
          reading CSV
        </button>
        {' · '}
        <button
          className="linklike"
          style={{ color: 'var(--accent-text)' }}
          onClick={() => void dl('/export/csv-template?library=watching', 'watching-template.csv')}
        >
          watching CSV
        </button>
      </div>
    </section>
  );
}

export function ImportPage() {
  const { toast, set } = useUI();
  const qc = useQueryClient();
  const [step, setStep] = useState(0);
  const [busy, setBusy] = useState(false);
  const [importId, setImportId] = useState<string | null>(null);
  const [opts, setOpts] = useState<Options>({ strategy: 'fill', sheets: {}, statusMap: {} });
  const [preview, setPreview] = useState<Preview | null>(null);
  const [report, setReport] = useState<Report | null>(null);

  const fail = (e: unknown) => toast({ tone: 'error', message: (e as Error).message });
  const runPreview = async (id: string, o: Options) => {
    setBusy(true);
    try {
      setPreview(await api<Preview>('POST', '/import/preview', { importId: id, options: o }));
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  useEffect(() => {
    if (importId && step >= 1 && step <= 3) void runPreview(importId, opts);
  }, [opts, importId]);

  const upload = async (file: File) => {
    setBusy(true);
    try {
      const { importId: id } = await bridgeFetch(file);
      setImportId(id);
      const fresh: Options = { strategy: 'fill', sheets: {}, statusMap: {} };
      setOpts(fresh);
      await runPreview(id, fresh);
      setStep(1);
    } catch (e) {
      fail(e);
      setBusy(false);
    }
  };
  const commit = async () => {
    if (!importId) return;
    setBusy(true);
    try {
      setReport(await api<Report>('POST', '/import/commit', { importId, options: opts }));
      invalidateAll(qc);
      setStep(4);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const undo = async () => {
    if (!report) return;
    try {
      await api('POST', `/import/undo/${report.undoToken}`);
      invalidateAll(qc);
      toast({ message: 'Import undone' });
      reset();
    } catch (e) {
      fail(e);
    }
  };
  const reset = () => {
    setStep(0);
    setImportId(null);
    setPreview(null);
    setReport(null);
  };
  const patchSheet = (name: string, p: Options['sheets'][string]) =>
    setOpts((o) => ({ ...o, sheets: { ...o.sheets, [name]: { ...o.sheets[name], ...p } } }));
  const unknownStatuses = [
    ...new Set(
      (preview?.warnings ?? [])
        .filter((w) => w.code === 'UNKNOWN_STATUS')
        .map((w) => /"(.+?)"/.exec(w.message)?.[1])
        .filter(Boolean) as string[],
    ),
  ];
  const importable = (preview?.counts.create ?? 0) + (preview?.counts.update ?? 0);

  return (
    <div className="page-pad" style={{ maxWidth: 980, display: 'grid', gap: 20 }}>
      <h1>Import &amp; export</h1>
      <Stepper step={step} />

      {step === 0 && (
        <label
          className="callout dropzone"
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            const f = e.dataTransfer.files[0];
            if (f) void upload(f);
          }}
        >
          <Icon name="import" size={30} />
          <strong style={{ color: 'var(--text)' }}>
            Drop an .xlsx, .xlsm, .csv or .tsv file, or click to choose
          </strong>
          <span>
            Each sheet becomes a category. Re-importing is safe: existing titles are matched, never
            duplicated.
          </span>
          <input
            type="file"
            accept=".xlsx,.xlsm,.csv,.tsv"
            hidden
            onChange={(e) => e.target.files?.[0] && void upload(e.target.files[0])}
          />
        </label>
      )}

      {step === 1 && preview && (
        <>
          <section className="dcard">
            <header>
              <h3>{preview.fileName}</h3>
              <span className="muted">{preview.summary.entries} rows found</span>
            </header>
            <div style={{ display: 'grid', gap: 12 }}>
              {preview.sheets.map((s) => {
                const o = opts.sheets[s.name] ?? {};
                const include = o.include !== false && !s.skipped;
                return (
                  <details key={s.name} className="sheet" open={!s.skipped && s.rows > 0}>
                    <summary>
                      <input
                        type="checkbox"
                        aria-label={`Include ${s.name}`}
                        checked={include}
                        disabled={!!s.skipped && s.skipped !== 'Excluded' && !s.library}
                        onClick={(e) => e.stopPropagation()}
                        onChange={(e) => patchSheet(s.name, { include: e.target.checked })}
                      />
                      <strong>{s.name}</strong>
                      <span className="muted">
                        {s.skipped ? s.skipped : `${s.rows} rows · header row ${s.headerRow}`}
                      </span>
                      {!s.skipped && (
                        <span className="chip">
                          {s.categoryExists ? 'existing category' : 'new category'}
                        </span>
                      )}
                    </summary>
                    <div className="row2" style={{ margin: '12px 0' }}>
                      <div className="field">
                        <label htmlFor={`lib-${s.name}`}>Library</label>
                        <select
                          id={`lib-${s.name}`}
                          className="select"
                          value={s.library ?? ''}
                          onChange={(e) =>
                            patchSheet(s.name, { library: e.target.value as 'reading' })
                          }
                        >
                          <option value="" disabled>
                            Choose…
                          </option>
                          <option value="reading">Reading</option>
                          <option value="watching">Watching</option>
                        </select>
                      </div>
                      <div className="field">
                        <label htmlFor={`cat-${s.name}`}>Category</label>
                        <input
                          id={`cat-${s.name}`}
                          className="input"
                          value={o.category ?? s.categoryName}
                          onChange={(e) => patchSheet(s.name, { category: e.target.value })}
                        />
                      </div>
                    </div>
                    <table className="maptable">
                      <thead>
                        <tr>
                          <th>Spreadsheet column</th>
                          <th>Import as</th>
                        </tr>
                      </thead>
                      <tbody>
                        {s.mapping.map((m) => (
                          <tr key={m.col}>
                            <td>{m.header || <span className="muted">(unlabeled)</span>}</td>
                            <td>
                              <select
                                className="select"
                                aria-label={`Map ${m.header || 'column ' + (m.col + 1)}`}
                                value={(o.mapping ?? s.mapping.map((x) => x.field))[m.col] ?? ''}
                                onChange={(e) => {
                                  const base = o.mapping ?? s.mapping.map((x) => x.field);
                                  const next = [...base];
                                  next[m.col] = e.target.value || null;
                                  patchSheet(s.name, { mapping: next });
                                }}
                              >
                                <option value="">Ignore</option>
                                {Object.entries(FIELD_LABELS).map(([k, v]) => (
                                  <option key={k} value={k}>
                                    {v}
                                  </option>
                                ))}
                              </select>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </details>
                );
              })}
            </div>
          </section>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn" onClick={reset}>
              Cancel
            </button>
            <div className="spacer" />
            <button className="btn primary" onClick={() => setStep(2)}>
              Next: warnings
            </button>
          </div>
        </>
      )}

      {step === 2 && preview && (
        <>
          <section className="dcard">
            <header>
              <h3>What will happen</h3>
              {busy && <span className="muted">Updating…</span>}
            </header>
            <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
              <span className="chip tone-completed">{preview.counts.create} new</span>
              <span className="chip tone-in_progress">{preview.counts.update} updated</span>
              <span className="chip">{preview.counts.unchanged} unchanged</span>
              <span className="chip">{preview.counts.skipped} skipped</span>
              <span className="chip">
                {preview.summary.distinctTagsRaw} tags as written ·{' '}
                {preview.summary.distinctTagsNormalized} after case-merge
              </span>
            </div>
            <div className="field" style={{ maxWidth: 420 }}>
              <label htmlFor="strategy">When a title already exists</label>
              <select
                id="strategy"
                className="select"
                value={opts.strategy}
                onChange={(e) =>
                  setOpts((o) => ({ ...o, strategy: e.target.value as Options['strategy'] }))
                }
              >
                <option value="fill">Fill empty fields only (recommended)</option>
                <option value="skip">Skip it</option>
                <option value="overwrite">Overwrite with the spreadsheet values</option>
                <option value="keep_both">Keep both</option>
              </select>
            </div>
          </section>
          {unknownStatuses.length > 0 && (
            <section className="dcard">
              <header>
                <h3>Unrecognised statuses</h3>
              </header>
              {unknownStatuses.map((raw) => (
                <div key={raw} className="mgr-row">
                  <span className="chip">{raw}</span> →
                  <select
                    className="select"
                    style={{ width: 'auto' }}
                    aria-label={`Status for ${raw}`}
                    value={opts.statusMap[raw] ?? ''}
                    onChange={(e) =>
                      setOpts((o) => ({
                        ...o,
                        statusMap: { ...o.statusMap, [raw]: e.target.value },
                      }))
                    }
                  >
                    <option value="">Planned (default)</option>
                    {['planned', 'in_progress', 'completed', 'on_hold', 'dropped', 'repeating'].map(
                      (s) => (
                        <option key={s} value={s}>
                          {s.replace('_', ' ')}
                        </option>
                      ),
                    )}
                  </select>
                </div>
              ))}
            </section>
          )}
          <section className="dcard">
            <header>
              <h3>{preview.warnings.length} warnings</h3>
            </header>
            {preview.warnings.length === 0 ? (
              <div className="muted">All clear.</div>
            ) : (
              <ul className="warnlist">
                {preview.warnings.map((w, i) => (
                  <li key={i}>
                    <span className="chip">{w.code.replace('_', ' ').toLowerCase()}</span> {w.sheet}
                    {w.row ? ` · row ${w.row}` : ''}
                    {w.title ? ` · ${w.title}` : ''} — {w.message}
                  </li>
                ))}
              </ul>
            )}
          </section>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn" onClick={() => setStep(1)}>
              Back
            </button>
            <div className="spacer" />
            <button className="btn primary" disabled={busy} onClick={() => setStep(3)}>
              Next: import
            </button>
          </div>
        </>
      )}

      {step === 3 && preview && (
        <section className="dcard" style={{ maxWidth: 560 }}>
          <header>
            <h3>Ready to import</h3>
          </header>
          <div>
            {importable} titles will be written ({preview.counts.create} new,{' '}
            {preview.counts.update} updated). A snapshot is taken first so you can undo.
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn" onClick={() => setStep(2)}>
              Back
            </button>
            <div className="spacer" />
            <button className="btn primary" disabled={busy} onClick={() => void commit()}>
              {busy ? 'Importing…' : `Import ${importable} titles`}
            </button>
          </div>
        </section>
      )}

      {step === 4 && report && (
        <section className="dcard">
          <header>
            <h3>Import complete</h3>
          </header>
          <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
            <span className="chip tone-completed">{report.counts.created} created</span>
            <span className="chip tone-in_progress">{report.counts.updated} updated</span>
            <span className="chip">{report.counts.unchanged} unchanged</span>
            <span className="chip">{report.counts.categoriesCreated} categories created</span>
            <span className="chip">{report.warnings.length} warnings</span>
          </div>
          {report.tagSuggestions.length > 0 && (
            <div className="callout">
              {report.tagSuggestions.length} tags look like typos or duplicates.{' '}
              <button
                className="linklike"
                style={{ color: 'var(--accent-text)' }}
                onClick={() => set({ route: 'tags' })}
              >
                Review tag cleanup →
              </button>
            </div>
          )}
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn" onClick={() => void undo()}>
              Undo import
            </button>
            <button className="btn" onClick={reset}>
              Import another
            </button>
            <div className="spacer" />
            <button className="btn primary" onClick={() => set({ route: 'library' })}>
              Open library
            </button>
          </div>
        </section>
      )}

      {step === 0 && (
        <>
          <ExportPanel />
          <Backups />
        </>
      )}
    </div>
  );
}

import { useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { Scales, Settings } from '@lorekeeper/shared';
import { api, get } from '../lib/api';
import { useMeta, useScales, useSettings, useQueryClientInvalidate } from '../lib/queries';
import { getBridge } from '../lib/bridge';
import { useUI } from '../lib/store';

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
const STATUSES = [
  'planned',
  'in_progress',
  'completed',
  'on_hold',
  'dropped',
  'repeating',
] as const;

/** Appearance lives in the UI store (instant) and is mirrored to settings.json. */
export function useSaveSettings() {
  const refresh = useQueryClientInvalidate();
  const toast = useUI((s) => s.toast);
  const { data } = useSettings();
  return async (patch: Partial<Settings>) => {
    if (!data) return;
    try {
      await api('PUT', '/settings', { ...data, ...patch });
      refresh(['settings']);
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    }
  };
}

/** Pull appearance from settings.json once it loads (so hand edits and other devices win). */
export function useSyncSettings() {
  const { data } = useSettings();
  const set = useUI((s) => s.set);
  useEffect(() => {
    if (data) set({ theme: data.theme, accent: data.accent, density: data.density });
  }, [data, set]);
}

function AniListData() {
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  const stats = useQuery({
    queryKey: ['anilist', 'cache'],
    queryFn: () => get<{ files: number; bytes: number }>('/anilist/cache'),
  });
  const mb = ((stats.data?.bytes ?? 0) / 1024 / 1024).toFixed(1);
  const clear = async () => {
    if (!confirm('Delete cached AniList data? Covers of matched titles are kept.')) return;
    try {
      const r = await api<{ files: number; coversRemoved: number }>('POST', '/anilist/cache/clear');
      void qc.invalidateQueries({ queryKey: ['anilist'] });
      toast({
        tone: 'success',
        message: `Removed ${r.files} cached files and ${r.coversRemoved} unused covers`,
      });
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    }
  };
  return (
    <section className="dcard">
      <header>
        <h3>AniList data</h3>
      </header>
      <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
        Cached responses and artwork speed things up and work offline. Unmatching a title deletes
        its cover and cached data automatically.
      </div>
      <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
        <span>{stats.data ? `${stats.data.files} files · ${mb} MB cached` : '…'}</span>
        <button className="btn" onClick={() => void clear()}>
          Clear AniList cache
        </button>
      </div>
    </section>
  );
}

export function SettingsPage() {
  const { theme, accent, density, set } = useUI();
  const save = useSaveSettings();
  const settings = useSettings().data;
  const scales = useScales().data;
  const meta = useMeta().data as
    { dataDir?: string; appVersion?: string; schemaVersion?: number } | undefined;
  const qc = useQueryClient();
  const toast = useUI((s) => s.toast);
  const [draft, setDraft] = useState<Scales | null>(null);
  useEffect(() => {
    if (scales) setDraft(scales);
  }, [scales]);
  const bridge = getBridge();

  const appearance = (patch: {
    theme?: typeof theme;
    accent?: string;
    density?: typeof density;
  }) => {
    set(patch);
    void save(patch);
  };
  const saveScales = async () => {
    if (!draft) return;
    try {
      await api('PUT', '/scales', draft);
      void qc.invalidateQueries({ queryKey: ['scales'] });
      toast({ tone: 'success', message: 'Labels saved' });
    } catch (e) {
      toast({ tone: 'error', message: (e as Error).message });
    }
  };
  if (!settings)
    return (
      <div className="page-pad">
        <div className="skeleton" style={{ height: 80 }} />
      </div>
    );

  return (
    <div className="page-pad" style={{ maxWidth: 760, display: 'grid', gap: 22 }}>
      <h1>Settings</h1>

      <section className="dcard">
        <header>
          <h3>Appearance</h3>
        </header>
        <div className="field">
          <span className="label">Theme</span>
          <div className="seg" role="group" aria-label="Theme" style={{ justifySelf: 'start' }}>
            {(['dark', 'light', 'system'] as const).map((t) => (
              <button key={t} aria-pressed={theme === t} onClick={() => appearance({ theme: t })}>
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
                onClick={() => appearance({ accent: c })}
              />
            ))}
            <input
              type="color"
              aria-label="Custom accent"
              value={accent}
              onChange={(e) => appearance({ accent: e.target.value })}
              style={{ width: 28, height: 28, border: 0, background: 'none', padding: 0 }}
            />
          </div>
        </div>
        <div className="row2">
          <div className="field">
            <span className="label">Density</span>
            <div className="seg" role="group" aria-label="Density" style={{ justifySelf: 'start' }}>
              {(['comfortable', 'compact'] as const).map((d) => (
                <button
                  key={d}
                  aria-pressed={density === d}
                  onClick={() => appearance({ density: d })}
                >
                  {d[0]!.toUpperCase() + d.slice(1)}
                </button>
              ))}
            </div>
          </div>
          <div className="field">
            <label htmlFor="deflib">Default library</label>
            <select
              id="deflib"
              className="select"
              value={settings.defaultLibrary}
              onChange={(e) =>
                void save({ defaultLibrary: e.target.value as Settings['defaultLibrary'] })
              }
            >
              <option value="all">All</option>
              <option value="reading">Reading</option>
              <option value="watching">Watching</option>
            </select>
          </div>
        </div>
      </section>

      <section className="dcard">
        <header>
          <h3>Field labels</h3>
        </header>
        <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
          Rename the three note fields. “Extra notes 1” defaults to “Left off” and shows on cards.
        </div>
        {(['general', 'extra1', 'extra2'] as const).map((k) => (
          <div className="field" key={k}>
            <label htmlFor={`lbl-${k}`}>
              {{ general: 'General notes', extra1: 'Extra notes 1', extra2: 'Extra notes 2' }[k]}
            </label>
            <input
              id={`lbl-${k}`}
              className="input"
              defaultValue={settings.labelOverrides[k] ?? ''}
              placeholder={{ general: 'Notes', extra1: 'Left off', extra2: 'Extra notes' }[k]}
              onBlur={(e) => {
                const v = e.target.value.trim();
                const next = { ...settings.labelOverrides };
                if (v) next[k] = v;
                else delete next[k];
                void save({ labelOverrides: next });
              }}
            />
          </div>
        ))}
      </section>

      {draft &&
        (['reading', 'watching'] as const).map((lib) => (
          <section className="dcard" key={lib}>
            <header>
              <h3>{lib === 'reading' ? 'Reading' : 'Watching'} labels</h3>
            </header>
            <div className="row2">
              <div className="field">
                <span className="label">Status names</span>
                {STATUSES.map((s) => (
                  <input
                    key={s}
                    className="input"
                    aria-label={`${lib} ${s} label`}
                    value={draft[lib].statusLabels[s] ?? ''}
                    onChange={(e) =>
                      setDraft({
                        ...draft,
                        [lib]: {
                          ...draft[lib],
                          statusLabels: { ...draft[lib].statusLabels, [s]: e.target.value },
                        },
                      })
                    }
                  />
                ))}
              </div>
              <div className="field">
                <span className="label">Score labels (1 → 10)</span>
                {draft[lib].scoreLabels.map((l, i) => (
                  <input
                    key={i}
                    className="input"
                    aria-label={`${lib} score ${i + 1} label`}
                    value={l}
                    onChange={(e) => {
                      const next = [...draft[lib].scoreLabels];
                      next[i] = e.target.value;
                      setDraft({ ...draft, [lib]: { ...draft[lib], scoreLabels: next } });
                    }}
                  />
                ))}
              </div>
            </div>
          </section>
        ))}
      {draft && (
        <div>
          <button className="btn primary" onClick={() => void saveScales()}>
            Save label sets
          </button>
        </div>
      )}

      <section className="dcard">
        <header>
          <h3>Backups</h3>
        </header>
        <div className="row2">
          <div className="field">
            <label htmlFor="keep">Keep the last</label>
            <input
              id="keep"
              className="input"
              type="number"
              min={1}
              defaultValue={settings.backup.keep}
              onBlur={(e) =>
                void save({
                  backup: { ...settings.backup, keep: Math.max(1, Number(e.target.value) || 20) },
                })
              }
            />
          </div>
          <div className="field">
            <label htmlFor="hrs">Backup on launch if older than (hours)</label>
            <input
              id="hrs"
              className="input"
              type="number"
              min={0}
              defaultValue={settings.backup.onLaunchHours}
              onBlur={(e) =>
                void save({
                  backup: {
                    ...settings.backup,
                    onLaunchHours: Math.max(0, Number(e.target.value) || 6),
                  },
                })
              }
            />
          </div>
        </div>
      </section>

      <AniListData />

      <section className="dcard">
        <header>
          <h3>Data</h3>
        </header>
        <div className="field">
          <span className="label">Data folder</span>
          <code className="truncate" style={{ userSelect: 'all' }}>
            {meta?.dataDir ?? '…'}
          </code>
        </div>
        <div>
          <button
            className="btn"
            disabled={!bridge?.openDataDir}
            onClick={() => bridge?.openDataDir?.()}
            title={bridge?.openDataDir ? '' : 'Available in the desktop app'}
          >
            Open data folder
          </button>{' '}
          <button
            className="btn"
            disabled={!bridge}
            title={bridge ? '' : 'Available in the desktop app'}
            onClick={() => {
              if (!bridge) return;
              const copy = confirm(
                'Copy your current data to the new folder?\n\nOK = copy it over, Cancel = start with an empty folder (your current data stays where it is).',
              );
              void bridge.chooseDataDir(copy);
            }}
          >
            Change folder…
          </button>
        </div>
        <div className="muted" style={{ fontSize: 'var(--text-sm)' }}>
          Lorekeeper {meta?.appVersion} · data schema v{meta?.schemaVersion}. Everything stays on
          this computer.
        </div>
      </section>
    </div>
  );
}

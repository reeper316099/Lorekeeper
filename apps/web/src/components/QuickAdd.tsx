import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { Entry, Library, Status } from '@lorekeeper/shared';
import { api } from '../lib/api';
import { useLookup } from '../lib/helpers';
import { invalidateAll } from '../lib/queries';
import { useUI } from '../lib/store';
import { Modal } from './ui';

export function QuickAdd() {
  const { quickAdd, set, filters, toast } = useUI();
  const look = useLookup();
  const qc = useQueryClient();
  const [title, setTitle] = useState('');
  const [library, setLibrary] = useState<Library>('reading');
  const [categoryId, setCategoryId] = useState('');
  const [status, setStatus] = useState<Status>('planned');
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!quickAdd) return;
    const lib: Library = filters.library === 'watching' ? 'watching' : 'reading';
    setLibrary(lib);
    setTitle('');
    setStatus('planned');
  }, [quickAdd, filters.library]);

  const cats = look.cats.filter((c) => c.library === library);
  useEffect(() => {
    if (!cats.some((c) => c.id === categoryId))
      setCategoryId(
        filters.category.find((id) => cats.some((c) => c.id === id)) ?? cats[0]?.id ?? '',
      );
  }, [library, cats.length]);

  if (!quickAdd) return null;
  const close = () => set({ quickAdd: false });
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim() || !categoryId) return;
    setBusy(true);
    try {
      const created = await api<Entry>('POST', '/entries', {
        title: title.trim(),
        categoryId,
        status,
      });
      invalidateAll(qc);
      toast({
        tone: 'success',
        message: `Added “${created.title}”`,
        action: { label: 'Open', run: () => set({ drawerId: created.id }) },
      });
      close();
    } catch (err) {
      toast({ tone: 'error', message: (err as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title="Add a title" onClose={close}>
      <form onSubmit={submit} style={{ display: 'grid', gap: 14 }}>
        <div className="field">
          <label htmlFor="qa-title">Title</label>
          <input
            id="qa-title"
            ref={ref}
            className="input"
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="What are you tracking?"
          />
        </div>
        <div className="seg" role="group" aria-label="Library" style={{ justifySelf: 'start' }}>
          <button
            type="button"
            aria-pressed={library === 'reading'}
            onClick={() => setLibrary('reading')}
          >
            Reading
          </button>
          <button
            type="button"
            aria-pressed={library === 'watching'}
            onClick={() => setLibrary('watching')}
          >
            Watching
          </button>
        </div>
        <div className="row2">
          <div className="field">
            <label htmlFor="qa-cat">Category</label>
            <select
              id="qa-cat"
              className="select"
              value={categoryId}
              onChange={(e) => setCategoryId(e.target.value)}
            >
              {cats.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="qa-status">Status</label>
            <select
              id="qa-status"
              className="select"
              value={status}
              onChange={(e) => setStatus(e.target.value as Status)}
            >
              {look.statusOptions(library).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
          </div>
        </div>
        {cats.length === 0 && (
          <div className="callout">
            No categories in this library yet. Import a spreadsheet or create a category first.
          </div>
        )}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <button type="button" className="btn" onClick={close}>
            Cancel
          </button>
          <button className="btn primary" disabled={busy || !title.trim() || !categoryId}>
            Add title
          </button>
        </div>
      </form>
    </Modal>
  );
}

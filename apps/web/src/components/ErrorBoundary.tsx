import { Component, type ReactNode } from 'react';
import { Empty, Icon } from './ui';

/** Friendly "can't load" card for failed queries. A network failure usually means the background service stopped. */
export function QueryError({
  what,
  error,
  onRetry,
}: {
  what: string;
  error: unknown;
  onRetry: () => void;
}) {
  const offline = error instanceof TypeError; // fetch() rejects with TypeError when nothing answers
  return (
    <div role="alert">
      <Empty title={`Couldn’t load ${what}`} icon="close">
        {offline
          ? 'Lorekeeper’s background service isn’t responding. Your data is safe on disk; try again in a moment, or restart the app.'
          : (error as Error)?.message || 'Something went wrong.'}
        <br />
        <button className="btn" style={{ marginTop: 12 }} onClick={onRetry}>
          <Icon name="check" size={14} /> Try again
        </button>
      </Empty>
    </div>
  );
}

/** Catches render errors so one broken screen never blanks the whole app. */
export class ErrorBoundary extends Component<
  { children: ReactNode; resetKey?: string },
  { error: Error | null }
> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidUpdate(prev: { resetKey?: string }) {
    if (this.state.error && prev.resetKey !== this.props.resetKey) this.setState({ error: null });
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div role="alert">
        <Empty title="This screen hit a problem" icon="close">
          Nothing was lost; your data is stored safely on disk.
          <br />
          <code style={{ fontSize: 'var(--text-xs)' }}>{this.state.error.message}</code>
          <br />
          <span style={{ display: 'inline-flex', gap: 8, marginTop: 12 }}>
            <button className="btn primary" onClick={() => this.setState({ error: null })}>
              Try again
            </button>
            <button className="btn" onClick={() => location.reload()}>
              Reload app
            </button>
          </span>
        </Empty>
      </div>
    );
  }
}

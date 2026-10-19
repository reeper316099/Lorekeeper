import { randomUUID } from 'node:crypto';

export interface Job {
  id: string;
  kind: 'match' | 'import';
  state: 'running' | 'done' | 'cancelled' | 'error' | 'offline';
  total: number;
  done: number;
  message: string | null;
  result?: unknown;
  startedAt: string;
}

/** In-memory registry of long-running AniList jobs, with cancellation and progress events. */
export class JobRegistry {
  private jobs = new Map<string, Job>();
  private controllers = new Map<string, AbortController>();

  constructor(private onChange: (job: Job) => void = () => {}) {}

  create(kind: Job['kind'], total = 0): { job: Job; signal: AbortSignal } {
    const job: Job = {
      id: randomUUID(),
      kind,
      state: 'running',
      total,
      done: 0,
      message: null,
      startedAt: new Date().toISOString(),
    };
    const ctl = new AbortController();
    this.jobs.set(job.id, job);
    this.controllers.set(job.id, ctl);
    this.onChange(job);
    return { job, signal: ctl.signal };
  }

  get(id: string): Job | undefined {
    return this.jobs.get(id);
  }

  update(job: Job, patch: Partial<Job>): void {
    Object.assign(job, patch);
    this.onChange(job);
  }

  cancel(id: string): boolean {
    const job = this.jobs.get(id);
    if (!job || job.state !== 'running') return false;
    this.controllers.get(id)?.abort();
    return true;
  }

  /** The newest running job of a kind, if any (the UI re-attaches to it after a reload). */
  running(kind: Job['kind']): Job | undefined {
    return [...this.jobs.values()].reverse().find((j) => j.kind === kind && j.state === 'running');
  }
}

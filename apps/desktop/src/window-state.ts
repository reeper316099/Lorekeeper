import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { screen, type BrowserWindow, type Rectangle } from 'electron';

interface State extends Partial<Rectangle> {
  maximized?: boolean;
}

export const MIN_SIZE = { width: 1000, height: 680 };
const DEFAULT = { width: 1320, height: 840 };

const visible = (r: Rectangle) =>
  screen.getAllDisplays().some((d) => {
    const w = d.workArea;
    return (
      r.x < w.x + w.width - 80 &&
      r.x + r.width > w.x + 80 &&
      r.y >= w.y - 10 &&
      r.y < w.y + w.height - 80
    );
  });

/** Remember size/position/maximized between launches; ignore positions that are now off-screen. */
export function loadWindowState(userData: string): State & { width: number; height: number } {
  const file = path.join(userData, 'window-state.json');
  try {
    if (existsSync(file)) {
      const s = JSON.parse(readFileSync(file, 'utf8')) as State;
      const width = Math.max(MIN_SIZE.width, Number(s.width) || DEFAULT.width);
      const height = Math.max(MIN_SIZE.height, Number(s.height) || DEFAULT.height);
      const rect = { x: Number(s.x), y: Number(s.y), width, height };
      const ok = Number.isFinite(rect.x) && Number.isFinite(rect.y) && visible(rect as Rectangle);
      return { width, height, ...(ok ? { x: rect.x, y: rect.y } : {}), maximized: !!s.maximized };
    }
  } catch {
    /* fall through to defaults */
  }
  return { ...DEFAULT };
}

export function trackWindowState(win: BrowserWindow, userData: string): void {
  const file = path.join(userData, 'window-state.json');
  let timer: NodeJS.Timeout | undefined;
  const save = () => {
    try {
      if (win.isDestroyed() || win.isMinimized() || win.isFullScreen()) return;
      const maximized = win.isMaximized();
      const b = maximized ? (JSON.parse(readExisting(file)) as State) : win.getNormalBounds();
      writeFileSync(file, JSON.stringify({ ...b, maximized }));
    } catch {
      /* ignore */
    }
  };
  const later = () => {
    clearTimeout(timer);
    timer = setTimeout(save, 400);
  };
  for (const ev of ['resize', 'move', 'maximize', 'unmaximize'] as const)
    win.on(ev as 'resize', later);
  win.on('close', save);
}

function readExisting(file: string): string {
  try {
    return readFileSync(file, 'utf8');
  } catch {
    return '{}';
  }
}

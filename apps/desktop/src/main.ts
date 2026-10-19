import { randomBytes } from 'node:crypto';
import { cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  Menu,
  nativeTheme,
  session,
  shell,
  type IpcMainEvent,
  type IpcMainInvokeEvent,
} from 'electron';
import { APP_NAME } from '@lorekeeper/shared';
import { buildApp, registerIoRoutes, Store } from '@lorekeeper/server';
import { createLogger } from './logger';
import { buildMenu } from './menu';
import { readBootstrap, resolveDataDir } from './paths';
import { loadWindowState, MIN_SIZE, trackWindowState } from './window-state';

// Must happen before anything asks for userData so the folder is named after the app.
app.setName(APP_NAME);
if (process.env.LOREKEEPER_USER_DATA)
  app.setPath('userData', path.resolve(process.env.LOREKEEPER_USER_DATA));

const HEADLESS = process.env.LOREKEEPER_HEADLESS === '1'; // used by the e2e smoke test
const DEV_URL = process.env.LOREKEEPER_DEV_URL; // Vite dev server, only when developing
const WEB_DIR = path.join(__dirname, 'web');

let win: BrowserWindow | null = null;
let closeServer: (() => Promise<void>) | null = null;
let releaseLock: (() => void) | null = null;

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', () => {
    if (!win) return;
    if (win.isMinimized()) win.restore();
    win.focus();
  });
  void start();
}

/** Second guard (beyond Electron's per-profile lock): one process per data folder, even when it is shared. */
function lockDataDir(dataDir: string): boolean {
  const lock = path.join(dataDir, '.lorekeeper.lock');
  mkdirSync(dataDir, { recursive: true });
  if (existsSync(lock)) {
    const pid = Number(readFileSync(lock, 'utf8'));
    if (pid && pid !== process.pid) {
      try {
        process.kill(pid, 0);
        return false; // that process is still alive
      } catch {
        /* stale lock from a crash */
      }
    }
  }
  writeFileSync(lock, String(process.pid));
  releaseLock = () => rmSync(lock, { force: true });
  return true;
}

async function start(): Promise<void> {
  await app.whenReady();
  const userData = app.getPath('userData');
  const dataDir = resolveDataDir({
    env: process.env.LOREKEEPER_DATA_DIR,
    bootstrapDir: readBootstrap(userData).dataDir,
    userData,
  });

  if (!lockDataDir(dataDir)) {
    dialog.showErrorBox(
      APP_NAME,
      `Another ${APP_NAME} window is already using this data folder:\n${dataDir}`,
    );
    app.exit(1);
    return;
  }
  const log = createLogger(dataDir);
  log('info', `starting ${APP_NAME} ${app.getVersion()} on ${process.platform}`);

  const store = new Store(dataDir, { watch: true, appVersion: app.getVersion() });
  try {
    await store.open();
  } catch (err) {
    log('error', `failed to open data: ${(err as Error).message}`);
    dialog.showErrorBox(APP_NAME, `Could not open your data folder:\n${(err as Error).message}`);
    releaseLock?.();
    app.exit(1);
    return;
  }
  store.on('file-error', (e: { file: string; message: string }) =>
    log('warn', `invalid file ${e.file}: ${e.message}`),
  );
  store.on('file-restored', (e: { file: string }) => log('warn', `restored ${e.file} from backup`));

  const token = randomBytes(32).toString('hex');
  const api = await buildApp({
    store,
    token,
    staticDir: existsSync(WEB_DIR) ? WEB_DIR : undefined,
    allowedOrigins: DEV_URL ? [new URL(DEV_URL).origin] : [],
    extend: registerIoRoutes,
  });
  await api.listen({ host: '127.0.0.1', port: 0 });
  const port = (api.server.address() as { port: number }).port;
  const baseUrl = `http://127.0.0.1:${port}`;
  const appOrigin = DEV_URL ? new URL(DEV_URL).origin : baseUrl;
  log('info', `api listening on port ${port}`);
  closeServer = async () => {
    await api.close();
    await store.close();
  };

  hardenSession(appOrigin, baseUrl);
  registerIpc({ baseUrl, token, dataDir, userData, appOrigin, log });
  Menu.setApplicationMenu(buildMenu({ dataDir, dev: !app.isPackaged }));
  createWindow({ userData, url: DEV_URL ?? baseUrl, appOrigin });

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0)
      createWindow({ userData, url: DEV_URL ?? baseUrl, appOrigin });
  });
}

function hardenSession(appOrigin: string, baseUrl: string): void {
  const ses = session.defaultSession;
  ses.setPermissionRequestHandler((_wc, _perm, cb) => cb(false));
  ses.setPermissionCheckHandler(() => false);
  if (!DEV_URL) {
    const csp =
      "default-src 'self'; script-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; connect-src 'self'; font-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'";
    ses.webRequest.onHeadersReceived((details, cb) => {
      const headers = { ...details.responseHeaders };
      if (details.url.startsWith(baseUrl) && !details.url.startsWith(`${baseUrl}/api/`))
        headers['Content-Security-Policy'] = [csp];
      cb({ responseHeaders: headers });
    });
  }
  void appOrigin;
}

function registerIpc(ctx: {
  baseUrl: string;
  token: string;
  dataDir: string;
  userData: string;
  appOrigin: string;
  log: ReturnType<typeof createLogger>;
}): void {
  // Only our own page may talk to the main process.
  const trusted = (e: IpcMainEvent | IpcMainInvokeEvent) => {
    try {
      return new URL(e.senderFrame?.url ?? '').origin === ctx.appOrigin;
    } catch {
      return false;
    }
  };
  ipcMain.on('lorekeeper:config', (e) => {
    e.returnValue = trusted(e) ? { baseUrl: ctx.baseUrl, token: ctx.token } : null;
  });
  ipcMain.handle('lorekeeper:open-data-dir', async (e) => {
    if (trusted(e)) await shell.openPath(ctx.dataDir);
  });
  ipcMain.on('lorekeeper:set-theme', (e, theme: unknown) => {
    if (trusted(e) && (theme === 'dark' || theme === 'light' || theme === 'system'))
      nativeTheme.themeSource = theme;
  });
  ipcMain.handle('lorekeeper:choose-data-dir', async (e, copyExisting: boolean) => {
    if (!trusted(e) || !win) return { ok: false };
    const picked = await dialog.showOpenDialog(win, {
      title: 'Choose data folder',
      properties: ['openDirectory', 'createDirectory'],
    });
    const dir = picked.filePaths[0];
    if (picked.canceled || !dir) return { ok: false };
    if (path.resolve(dir) === ctx.dataDir) return { ok: false };
    if (copyExisting) {
      for (const sub of ['config', 'data', 'activity']) {
        if (existsSync(path.join(ctx.dataDir, sub)))
          cpSync(path.join(ctx.dataDir, sub), path.join(dir, sub), { recursive: true });
      }
      if (existsSync(path.join(ctx.dataDir, 'meta.json')))
        cpSync(path.join(ctx.dataDir, 'meta.json'), path.join(dir, 'meta.json'));
    }
    writeFileSync(path.join(ctx.userData, 'bootstrap.json'), JSON.stringify({ dataDir: dir }));
    ctx.log('info', 'data folder changed; relaunching');
    app.relaunch();
    app.exit(0);
    return { ok: true };
  });
}

function createWindow(ctx: { userData: string; url: string; appOrigin: string }): void {
  const state = loadWindowState(ctx.userData);
  win = new BrowserWindow({
    ...state,
    minWidth: MIN_SIZE.width,
    minHeight: MIN_SIZE.height,
    show: false,
    title: APP_NAME,
    backgroundColor: '#0d0e13',
    webPreferences: {
      preload: path.join(__dirname, 'preload.cjs'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webSecurity: true,
      spellcheck: false,
      backgroundThrottling: !HEADLESS,
    },
  });
  trackWindowState(win, ctx.userData);
  if (state.maximized) win.maximize();
  if (!HEADLESS) win.once('ready-to-show', () => win?.show());

  const external = (url: string) => {
    if (/^https?:\/\//i.test(url)) void shell.openExternal(url);
  };
  win.webContents.setWindowOpenHandler(({ url }) => {
    external(url);
    return { action: 'deny' };
  });
  win.webContents.on('will-navigate', (e, url) => {
    if (new URL(url).origin !== ctx.appOrigin) {
      e.preventDefault();
      external(url);
    }
  });
  win.on('closed', () => {
    win = null;
  });
  void win.loadURL(ctx.url);
}

process.on('SIGTERM', () => app.quit());

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});

let quitting = false;
app.on('before-quit', (e) => {
  if (quitting) return;
  quitting = true;
  e.preventDefault();
  void (closeServer?.() ?? Promise.resolve()).finally(() => {
    releaseLock?.();
    app.exit(0);
  });
});

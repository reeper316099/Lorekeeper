import { contextBridge, ipcRenderer } from 'electron';

/**
 * The renderer only gets a narrow bridge: where the API lives, the per-launch token, and a few
 * native actions. No Node, no file paths from the page.
 */
const cfg = ipcRenderer.sendSync('lorekeeper:config') as { baseUrl: string; token: string };

contextBridge.exposeInMainWorld('lorekeeper', {
  baseUrl: cfg.baseUrl,
  token: cfg.token,
  platform: process.platform,
  openDataDir: () => ipcRenderer.invoke('lorekeeper:open-data-dir'),
  chooseDataDir: (copyExisting: boolean) =>
    ipcRenderer.invoke('lorekeeper:choose-data-dir', copyExisting === true),
  setTheme: (theme: 'dark' | 'light' | 'system') => ipcRenderer.send('lorekeeper:set-theme', theme),
  onMenu: (cb: (action: string) => void) => {
    const handler = (_e: unknown, action: string) => cb(action);
    ipcRenderer.on('lorekeeper:menu', handler);
    return () => ipcRenderer.removeListener('lorekeeper:menu', handler);
  },
});

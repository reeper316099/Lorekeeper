/** Native bridge exposed by the Electron preload. Undefined when running in a plain browser. */
export interface NativeBridge {
  baseUrl: string;
  token: string;
  platform: string;
  openDataDir(): Promise<void>;
  chooseDataDir(copyExisting: boolean): Promise<{ ok: boolean }>;
  setTheme(theme: 'dark' | 'light' | 'system'): void;
  onMenu(cb: (action: string) => void): () => void;
}

export const getBridge = (): NativeBridge | undefined =>
  (window as unknown as { lorekeeper?: NativeBridge }).lorekeeper;

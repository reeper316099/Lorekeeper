import { app, BrowserWindow, Menu, shell, type MenuItemConstructorOptions } from 'electron';
import { APP_NAME } from '@lorekeeper/shared';

export type MenuAction =
  'new' | 'palette' | 'import' | 'settings' | 'dashboard' | 'library' | 'help';

export function buildMenu(opts: { dataDir: string; dev: boolean }): Menu {
  const isMac = process.platform === 'darwin';
  const send = (action: MenuAction) =>
    BrowserWindow.getFocusedWindow()?.webContents.send('lorekeeper:menu', action);
  const item = (
    label: string,
    action: MenuAction,
    accelerator?: string,
  ): MenuItemConstructorOptions => ({
    label,
    accelerator,
    click: () => send(action),
  });

  const template: MenuItemConstructorOptions[] = [
    ...(isMac
      ? [
          {
            label: APP_NAME,
            submenu: [
              { role: 'about' as const },
              { type: 'separator' as const },
              item('Settings…', 'settings', 'Cmd+,'),
              { type: 'separator' as const },
              { role: 'services' as const },
              { type: 'separator' as const },
              { role: 'hide' as const },
              { role: 'hideOthers' as const },
              { role: 'unhide' as const },
              { type: 'separator' as const },
              { role: 'quit' as const },
            ],
          },
        ]
      : []),
    {
      label: 'File',
      submenu: [
        item('New Title…', 'new', 'CmdOrCtrl+N'),
        item('Import / Export…', 'import', 'CmdOrCtrl+Shift+I'),
        { type: 'separator' },
        { label: 'Open Data Folder', click: () => void shell.openPath(opts.dataDir) },
        ...(isMac
          ? []
          : [
              { type: 'separator' as const },
              item('Settings…', 'settings', 'Ctrl+,'),
              { role: 'quit' as const },
            ]),
        ...(isMac ? [{ type: 'separator' as const }, { role: 'close' as const }] : []),
      ],
    },
    { role: 'editMenu' },
    {
      label: 'View',
      submenu: [
        item('Dashboard', 'dashboard', 'CmdOrCtrl+1'),
        item('Library', 'library', 'CmdOrCtrl+2'),
        item('Command Palette', 'palette', 'CmdOrCtrl+K'),
        { type: 'separator' },
        { role: 'resetZoom' },
        { role: 'zoomIn' },
        { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
        ...(opts.dev
          ? [
              { type: 'separator' as const },
              { role: 'reload' as const },
              { role: 'toggleDevTools' as const },
            ]
          : []),
      ],
    },
    { role: 'windowMenu' },
    {
      role: 'help',
      submenu: [
        item('Keyboard Shortcuts', 'help'),
        { label: 'Open Data Folder', click: () => void shell.openPath(opts.dataDir) },
      ],
    },
  ];
  void app;
  return Menu.buildFromTemplate(template);
}

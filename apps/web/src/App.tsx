import { useEffect, useRef } from 'react';
import { readableTextColor } from '@lorekeeper/shared';
import { Dashboard } from './components/Dashboard';
import { Drawer } from './components/Drawer';
import { Library } from './components/Library';
import { CategoriesPage, TagsPage } from './components/Managers';
import { ImportPage } from './components/ImportWizard';
import { Soon } from './components/Pages';
import { Palette } from './components/Palette';
import { SettingsPage, useSyncSettings } from './components/Settings';
import { QuickAdd } from './components/QuickAdd';
import { HelpModal, Sidebar, TopBar } from './components/Shell';
import { Toasts } from './components/ui';
import { isTyping } from './lib/helpers';
import { useServerEvents } from './lib/queries';
import { useUI } from './lib/store';

function useTheme() {
  const { theme, accent } = useUI();
  useEffect(() => {
    const apply = () => {
      const dark =
        theme === 'system'
          ? window.matchMedia('(prefers-color-scheme: dark)').matches
          : theme === 'dark';
      document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    };
    apply();
    if (theme !== 'system') return;
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    mq.addEventListener('change', apply);
    return () => mq.removeEventListener('change', apply);
  }, [theme]);
  useEffect(() => {
    document.documentElement.style.setProperty('--accent', accent);
    document.documentElement.style.setProperty(
      '--accent-ink',
      readableTextColor(accent) === '#000000' ? '#1a0d08' : '#ffffff',
    );
  }, [accent]);
}

export function App() {
  useTheme();
  useServerEvents();
  useSyncSettings();
  const searchRef = useRef<HTMLInputElement>(null);
  const { route, sidebarCollapsed, set } = useUI();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (mod && e.key.toLowerCase() === 'n') {
        e.preventDefault();
        set({ quickAdd: true });
        return;
      }
      if (mod && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        set({ palette: !useUI.getState().palette });
        return;
      }
      if (isTyping(e.target) || mod || e.altKey) return;
      if (e.key === '/') {
        e.preventDefault();
        set({ route: 'library' });
        searchRef.current?.focus();
      } else if (e.key === 'n') {
        e.preventDefault();
        set({ quickAdd: true });
      } else if (e.key === '?') set({ help: true });
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [set]);

  return (
    <div className={`app ${sidebarCollapsed ? 'collapsed' : ''}`}>
      <Sidebar />
      <div className="main">
        <TopBar searchRef={searchRef} />
        <main className="page" id="main">
          {route === 'library' && <Library />}
          {route === 'dashboard' && <Dashboard />}
          {route === 'categories' && <CategoriesPage />}
          {route === 'tags' && <TagsPage />}
          {route === 'anilist' && <Soon title="AniList" milestone="M6" />}
          {route === 'import' && <ImportPage />}
          {route === 'settings' && <SettingsPage />}
        </main>
      </div>
      <Drawer />
      <QuickAdd />
      <Palette />
      <HelpModal />
      <Toasts />
    </div>
  );
}

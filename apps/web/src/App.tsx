import { useEffect, useRef } from 'react';
import { readableTextColor } from '@lorekeeper/shared';
import { Drawer } from './components/Drawer';
import { Library } from './components/Library';
import { ImportPage, SettingsPage, Soon } from './components/Pages';
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
          {route === 'dashboard' && <Soon title="Dashboard" milestone="M4" />}
          {route === 'categories' && <Soon title="Categories" milestone="M4" />}
          {route === 'tags' && <Soon title="Tags" milestone="M4" />}
          {route === 'anilist' && <Soon title="AniList" milestone="M6" />}
          {route === 'import' && <ImportPage />}
          {route === 'settings' && <SettingsPage />}
        </main>
      </div>
      <Drawer />
      <QuickAdd />
      <HelpModal />
      <Toasts />
    </div>
  );
}

import { APP_NAME } from '@lorekeeper/shared';
import { useCategories, useMeta } from '../lib/queries';
import { useUI, type Route } from '../lib/store';
import { Icon, Modal } from './ui';

const NAV: { route: Route; label: string; icon: string }[] = [
  { route: 'dashboard', label: 'Dashboard', icon: 'dash' },
  { route: 'library', label: 'Library', icon: 'library' },
  { route: 'categories', label: 'Categories', icon: 'folder' },
  { route: 'tags', label: 'Tags', icon: 'tag' },
  { route: 'import', label: 'Import / Export', icon: 'import' },
  { route: 'anilist', label: 'AniList', icon: 'link' },
  { route: 'settings', label: 'Settings', icon: 'settings' },
];

export function Sidebar() {
  const { route, set, filters, setFilters, theme, sidebarCollapsed } = useUI();
  const cats = useCategories().data ?? [];
  const nonEmpty = (lib: string) => cats.filter((c) => c.library === lib);
  const go = (r: Route) => set({ route: r });
  const openCat = (id: string) => {
    setFilters({ category: [id], library: 'all' });
    go('library');
  };
  return (
    <nav className="sidebar" aria-label="Main">
      <div className="brand">
        <span className="brand-mark">
          <Icon name="book" size={18} />
        </span>
        <span className="brand-name">{APP_NAME}</span>
      </div>
      <div className="nav">
        {NAV.map((n) => (
          <button
            key={n.route}
            className="nav-item"
            aria-current={route === n.route ? 'page' : undefined}
            onClick={() => go(n.route)}
            title={n.label}
          >
            <Icon name={n.icon} /> <span className="nav-text">{n.label}</span>
          </button>
        ))}
      </div>
      <div
        className="cat-list"
        style={{ display: sidebarCollapsed ? 'none' : 'flex', flexDirection: 'column', gap: 14 }}
      >
        {(['reading', 'watching'] as const).map((lib) => (
          <div key={lib} className="nav">
            <div className="nav-label">{lib === 'reading' ? 'Reading' : 'Watching'}</div>
            {nonEmpty(lib).map((c) => (
              <button
                key={c.id}
                className="nav-item"
                onClick={() => openCat(c.id)}
                aria-current={
                  route === 'library' &&
                  filters.category.length === 1 &&
                  filters.category[0] === c.id
                    ? 'true'
                    : undefined
                }
                style={{ padding: '5px 10px' }}
              >
                <span className="cat-dot" style={{ background: c.color }} />
                <span className="nav-text truncate">{c.name}</span>
                <span className="nav-count">{c.count}</span>
              </button>
            ))}
          </div>
        ))}
      </div>
      <div className="side-foot">
        <button
          className="nav-item"
          onClick={() => set({ theme: theme === 'dark' ? 'light' : 'dark' })}
          aria-label="Toggle theme"
        >
          <Icon name={theme === 'light' ? 'sun' : 'moon'} />{' '}
          <span className="nav-text">
            {theme === 'light' ? 'Light' : theme === 'dark' ? 'Dark' : 'System'} theme
          </span>
        </button>
        <button
          className="nav-item"
          onClick={() => set({ sidebarCollapsed: !sidebarCollapsed })}
          aria-label="Collapse sidebar"
        >
          <Icon name="rows" /> <span className="nav-text">Collapse</span>
        </button>
      </div>
    </nav>
  );
}

export function TopBar({ searchRef }: { searchRef: React.RefObject<HTMLInputElement | null> }) {
  const { filters, setFilters, set, route } = useUI();
  const meta = useMeta().data;
  return (
    <header className="topbar">
      <div className="search">
        <Icon name="search" size={16} />
        <input
          ref={searchRef}
          type="search"
          aria-label="Search titles"
          placeholder={`Search ${meta?.entries?.toLocaleString() ?? ''} titles, tags, notes…`}
          value={filters.q}
          onChange={(e) => {
            setFilters({ q: e.target.value });
            if (route !== 'library') set({ route: 'library' });
          }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              (e.target as HTMLInputElement).blur();
            }
          }}
        />
        <kbd>/</kbd>
      </div>
      <div className="spacer" />
      <button
        className="btn ghost"
        onClick={() => set({ help: true })}
        aria-label="Keyboard shortcuts"
      >
        <kbd>?</kbd>
      </button>
      <button className="btn primary" onClick={() => set({ quickAdd: true })}>
        <Icon name="plus" size={16} /> Add title
      </button>
    </header>
  );
}

export function HelpModal() {
  const { help, set } = useUI();
  if (!help) return null;
  const rows: [string, string][] = [
    ['/', 'Focus search'],
    ['N  or  ⌘/Ctrl+N', 'Add a title'],
    ['J / K', 'Move down / up'],
    ['E', 'Edit selected title'],
    ['+  /  −', 'Progress up / down'],
    ['X', 'Select row'],
    ['F', 'Toggle favorite'],
    ['Esc', 'Close panel'],
    ['?', 'This help'],
  ];
  return (
    <Modal title="Keyboard shortcuts" onClose={() => set({ help: false })}>
      <div className="help-grid">
        {rows.map(([k, v]) => (
          <>
            <kbd key={k}>{k}</kbd>
            <span key={v}>{v}</span>
          </>
        ))}
      </div>
    </Modal>
  );
}

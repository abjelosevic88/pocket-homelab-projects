// The app shell: sidebar, top bar with breadcrumbs, ⌘K palette, the
// "New issue" dialog and the global keyboard shortcuts.
import { useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { setKnownKeys } from '../lib/markdown'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { ErrorBoundary } from './ErrorBoundary'
import { CreateIssueModal, openCreateIssue, type IssueDefaults } from './CreateIssue'
import { PersonAvatar } from './issue-bits'
import { type MenuItem, MenuButton, modalOpen, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'
import { IssueTabs } from './IssueTabs'

const SIDEBAR_KEY = 'pp-sidebar'
const section = (path: string) => path.split('/').slice(0, 3).join('/')

/** Is the keyboard busy in a field (or a menu)? Then page shortcuts stay out of the way. */
export function keysBusy(e: KeyboardEvent): boolean {
  const t = e.target as HTMLElement
  return !!t.closest?.('input, textarea, select, [contenteditable="true"]') || modalOpen() || !!document.querySelector('.menu, .popover, .select-pop, .pick-menu, .sheet-backdrop')
}

export default function Layout() {
  const navigate = useNavigate()
  // Installed app: a Projects link opened from elsewhere (Pocket AI's sidebar,
  // a notification) focuses this window (manifest launch_handler) and lands
  // here, so the issue opens as a tab instead of in a second window.
  useEffect(() => {
    const lq = (window as unknown as { launchQueue?: { setConsumer: (f: (p: { targetURL?: string }) => void) => void } }).launchQueue
    if (!lq || window.self !== window.top) return
    lq.setConsumer(p => {
      if (!p.targetURL) return
      const u = new URL(p.targetURL)
      const path = u.pathname + u.search + u.hash
      if (u.origin === location.origin && path !== location.pathname + location.search + location.hash) navigate(path)
      window.focus()
    })
  }, [navigate])
  const loc = useLocation()
  const app = useApp()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { if (drawer) setSidebarOpen(false) }, [loc.pathname]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setKnownKeys(app.teams.map(t => t.key)) }, [app.teams])

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [newIssue, setNewIssue] = useState<IssueDefaults | null>(null)

  useEffect(() => {
    const sc = () => setShortcuts(true)
    const ni = (e: Event) => setNewIssue((e as CustomEvent<IssueDefaults>).detail || {})
    window.addEventListener('pp:shortcuts', sc)
    window.addEventListener('pp:new-issue', ni)
    return () => { window.removeEventListener('pp:shortcuts', sc); window.removeEventListener('pp:new-issue', ni) }
  }, [])

  // Global keys: ⌘K palette, C new issue, G then a letter to go somewhere.
  const goPending = useRef(0)
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key.toLowerCase() === 'k' && !e.shiftKey) { e.preventDefault(); setPalette(p => !p); return }
      if (mod || e.altKey || keysBusy(e)) return
      if (performance.now() - goPending.current < 1200) {
        goPending.current = 0
        const to: Record<string, string> = { i: '/inbox', m: '/my-issues', p: '/projects', r: '/roadmap', v: '/views', c: '/calendar', t: '/time', n: '/insights', s: '/settings' }
        if (to[e.key.toLowerCase()]) { e.preventDefault(); navigate(to[e.key.toLowerCase()]) }
        return
      }
      switch (e.key) {
        case 'c': case 'C': e.preventDefault(); openCreateIssue(currentDefaults(loc.pathname)); break
        case 'g': case 'G': goPending.current = performance.now(); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case ',': e.preventDefault(); navigate('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
        case '/': e.preventDefault(); navigate('/search'); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  /** New issues made on a team or project page land there. */
  const currentDefaults = (path: string): IssueDefaults => {
    const [, a, b] = path.split('/')
    if (a === 'team') { const t = app.teamByKey(b); if (t) return { team_id: t.id } }
    if (a === 'project') { const p = app.project(Number(b)); if (p) return { project_id: p.id, team_id: p.team_ids.find(id => app.canEdit(id)) } }
    return {}
  }

  const commands: PaletteCommand[] = [
    { id: 'new', label: 'New issue', icon: 'plus', hint: 'C', keywords: 'add create bug task', run: () => openCreateIssue(currentDefaults(loc.pathname)) },
    { id: 'new-project', label: 'New project', icon: 'briefcase', keywords: 'add create', run: () => navigate('/projects?new=1') },
    { id: 'new-team', label: 'New team', icon: 'users', keywords: 'add create workspace', run: () => navigate('/teams/new') },
    { id: 'inbox', label: 'Go to Inbox', icon: 'inbox', hint: 'G I', run: () => navigate('/inbox') },
    { id: 'mine', label: 'Go to My issues', icon: 'target', hint: 'G M', run: () => navigate('/my-issues') },
    { id: 'projects', label: 'Go to Projects', icon: 'briefcase', hint: 'G P', run: () => navigate('/projects') },
    { id: 'roadmap', label: 'Go to Roadmap', icon: 'route', hint: 'G R', keywords: 'timeline gantt', run: () => navigate('/roadmap') },
    { id: 'views', label: 'Go to Views', icon: 'layers', hint: 'G V', keywords: 'saved filters', run: () => navigate('/views') },
    { id: 'calendar', label: 'Go to Calendar', icon: 'calendar', hint: 'G C', keywords: 'due dates deadlines', run: () => navigate('/calendar') },
    { id: 'time', label: 'Go to Time', icon: 'timer', hint: 'G T', keywords: 'timesheet hours tracking', run: () => navigate('/time') },
    { id: 'insights', label: 'Go to Insights', icon: 'bar-chart', hint: 'G N', keywords: 'charts velocity analytics reports', run: () => navigate('/insights') },
    { id: 'search', label: 'Search everything', icon: 'search', hint: '/', run: () => navigate('/search') },
    { id: 'trash', label: 'Trash', icon: 'trash', keywords: 'deleted restore', run: () => navigate('/trash') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'notifications', label: 'Notification settings', icon: 'bell', keywords: 'push ntfy apprise', run: () => navigate('/settings/notifications') },
    { id: 'apps', label: 'Pocket apps (Calendar, Contacts, Tasks, Notes, AI)', icon: 'grid', keywords: 'integrations links core', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
    { id: 'sidebar', label: sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar', icon: 'sidebar', hint: '[', run: toggleSidebar },
    ...app.teams.flatMap(t => [
      { id: 'team-' + t.id, label: `${t.name}: issues`, icon: 'layers', keywords: t.key, run: () => navigate(`/team/${t.key}/issues`) },
      { id: 'team-board-' + t.id, label: `${t.name}: board`, icon: 'kanban', keywords: t.key + ' kanban', run: () => navigate(`/team/${t.key}/issues?layout=board`) },
      ...(t.cycles_enabled ? [{ id: 'team-cycles-' + t.id, label: `${t.name}: cycles`, icon: 'cycle', keywords: t.key + ' sprint', run: () => navigate(`/team/${t.key}/cycles`) }] : []),
      { id: 'team-settings-' + t.id, label: `${t.name}: settings`, icon: 'settings', keywords: t.key + ' workflow labels members', run: () => navigate(`/team/${t.key}/settings`) },
    ]),
  ]

  return (
    <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
      {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
      <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}><Sidebar onNavigate={closeDrawer} /></div>
      <div className="main">
        <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} />
        {!mobile && <IssueTabs />}
        <div className="content-row">
          <main className="content"><ErrorBoundary key={section(loc.pathname)} where={loc.pathname}><Outlet /></ErrorBoundary></main>
        </div>
      </div>
      {mobile && <button type="button" className="fab" aria-label="New issue" title="New issue" onClick={() => openCreateIssue(currentDefaults(loc.pathname))}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
      {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
      {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
      {newIssue && <CreateIssueModal defaults={newIssue} onClose={() => setNewIssue(null)} />}
    </div>
  )
}

const TITLES: Record<string, string> = { inbox: 'Inbox', 'my-issues': 'My issues', projects: 'Projects', roadmap: 'Roadmap', views: 'Views', insights: 'Insights', calendar: 'Calendar', time: 'Time', search: 'Search', trash: 'Trash', teams: 'Teams', doc: 'Document' }
const TEAM_TABS: Record<string, string> = { issues: 'Issues', active: 'Active', backlog: 'Backlog', triage: 'Triage', cycles: 'Cycles', projects: 'Projects', settings: 'Settings' }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean }) {
  const app = useApp()
  const { user, themeMode, setThemeMode, status, setUser } = app
  const navigate = useNavigate()
  const loc = useLocation()
  const [, first, second, third] = loc.pathname.split('/')
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pp:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  let crumbs: { label: string; to?: string }[] = []
  if (first === 'team') {
    const t = app.teamByKey(second)
    crumbs = [{ label: t?.name || second, to: `/team/${second}/issues` }, { label: TEAM_TABS[third] || '' }]
  } else if (first === 'project') {
    crumbs = [{ label: 'Projects', to: '/projects' }, { label: app.project(Number(second))?.name || 'Project' }]
  } else if (first === 'issue') {
    crumbs = [{ label: 'Issue' }, { label: decodeURIComponent(second || '').toUpperCase() }]
  } else if (first === 'view') {
    crumbs = [{ label: 'Views', to: '/views' }, { label: app.boot?.views.find(v => String(v.id) === second)?.name || 'View' }]
  } else if (first === 'settings') crumbs = [{ label: 'Settings' }]
  else crumbs = [{ label: TITLES[first || ''] || '' }]
  crumbs = crumbs.filter(c => c.label)
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumbs.map((c, i) => <span key={i} className="crumb-part">{i > 0 && <><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="sr-only"> › </span></>}
          {c.to && i < crumbs.length - 1 ? <a href={c.to} className="ellipsis crumb" onClick={e => { e.preventDefault(); navigate(c.to!) }}>{c.label}</a> : <span className={`ellipsis ${i < crumbs.length - 1 ? 'crumb' : ''}`}>{c.label}</span>}
        </span>)}
      </h1>
      <span className="spacer" />
      {!mobile && (!sidebarOpen || compact) && <button type="button" className="btn ghost icon-only" onClick={() => openCreateIssue()} aria-label="New issue" title="New issue (C)"><Icon name="plus" size={19} /></button>}
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search and commands (⌘K)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <PersonAvatar person={user} size={30} />
      </MenuButton>
    </header>
  )
}

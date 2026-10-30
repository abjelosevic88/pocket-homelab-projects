// Left column: new issue, inbox and my issues, the workspace pages,
// favourites, and each team with its views — the Pocket Calendar sidebar.
import { useEffect, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { REMOTE } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useRemotes, watchAgentWorkspace } from '../lib/remotes'
import { useAIActiveList } from '../lib/ai-active'
import { useTeamDeploy } from '../lib/deploy-live'
import type { Favorite, Team } from '../lib/types'
import { Icon } from './icons'
import { ProjectMark, StatusIcon } from './issue-bits'
import { TeamMark } from './pickers'
import { openCreateIssue } from './CreateIssue'
import { type Anchor, type MenuItem, Menu, rectOf } from './ui'

export function useFold(key: string, def = true) {
  const [open, setOpen] = useState(() => { const v = localStorage.getItem(key); return v === null ? def : v === '1' })
  useEffect(() => { localStorage.setItem(key, open ? '1' : '0') }, [key, open])
  return [open, setOpen] as const
}

/** "With AI": the team's issues a Pocket AI conversation is working on or waiting
 *  about right now; the count twinkles like the marks in lists (amber: all waiting). */
function WithAIItem({ to, teamKey, onNavigate }: { to: string; teamKey: string; onNavigate: () => void }) {
  const mine = useAIActiveList().filter(a => a.ref.toUpperCase().startsWith(teamKey.toUpperCase() + '-'))
  const waiting = mine.length > 0 && mine.every(a => a.status === 'waiting')
  return (
    <NavLink to={to} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
      <span className="sb-ico"><Icon name="sparkles" size={15} /></span><span className="ellipsis">With AI</span>
      {mine.length > 0 && <span className={`sb-ai-count ${waiting ? 'waiting' : 'running'}`} title={waiting ? 'Waiting for you' : 'Pocket AI is working'}>{mine.length}</span>}
    </NavLink>
  )
}

/** The team's deploy server (Team settings → Pocket AI → Deploy): which issue runs
 *  there now. A click opens the server page (branches, redeploy); the arrow opens the server's app. */
function DeployItem({ teamKey, onNavigate }: { teamKey: string; onNavigate: () => void }) {
  const d = useTeamDeploy(teamKey)
  const navigate = useNavigate()
  const { pathname } = useLocation()
  if (!d) return null
  // the feature: the issue's title when it came from an issue, else the branch
  const feature = d.ref ? `${d.ref} · ${d.title || d.branch}` : d.branch
  const what = d.state === 'running' ? `Deploying ${feature}…` : d.state === 'live' ? feature
    : d.state === 'replaced' ? (d.now ? `${d.now.slice(0, 8)} — not from here` : 'replaced since') : 'nothing deployed yet'
  const tip = d.state === 'live' || d.state === 'running' ? `${d.host_name}${d.state === 'running' ? ' (deploying)' : ''}: ${d.ref ? d.ref + (d.title ? ' ' + d.title : '') + '\n' : ''}${d.branch} @ ${d.sha.slice(0, 8)}`
    : d.state === 'replaced' ? `${d.host_name}: ${d.branch} was deployed from here; something else runs there since` : `${d.host_name}: ${what}`
  const page = `/team/${teamKey}/server`
  return (
    <div className={`sb-nav-item sb-deploy ${d.state} ${pathname.toLowerCase() === page.toLowerCase() ? 'active' : ''}`} role="link" tabIndex={0} title={tip}
      onClick={() => { onNavigate(); navigate(page) }}
      onKeyDown={e => { if (e.key === 'Enter') (e.currentTarget as HTMLElement).click() }}>
      <span className="sb-ico"><Icon name="server" size={15} /></span>
      <span className="sb-deploy-text">
        <span className="ellipsis">{d.host_name || 'Deploy'}</span>
        <small className={`sb-deploy-what ${d.state} ${d.ref ? '' : 'mono'}`}>{what}</small>
      </span>
      {d.url && <a className="sb-deploy-open" href={d.url} target="_blank" rel="noreferrer" onClick={e => e.stopPropagation()} aria-label={`Open ${d.url}`} title={`Open ${d.url}`}><Icon name="external" size={12} /></a>}
    </div>
  )
}

function Item({ to, icon, label, count, warn, onNavigate, end }: { to: string; icon: React.ReactNode; label: string; count?: number; warn?: boolean; onNavigate: () => void; end?: boolean }) {
  return (
    <NavLink to={to} end={end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
      <span className="sb-ico">{icon}</span><span className="ellipsis">{label}</span>
      {!!count && (warn ? <span className="badge accent">{count}</span> : <span className="sb-count">{count}</span>)}
    </NavLink>
  )
}

export function Sidebar({ onNavigate }: { onNavigate: () => void }) {
  const app = useApp()
  const { teams, status, settings, unread, boot } = app
  const [favOpen, setFavOpen] = useFold('pp-sb-fav')
  const [teamsOpen, setTeamsOpen] = useFold('pp-sb-teams')
  const title = settings?.title || status?.title || 'Pocket Projects'
  const live = teams.filter(t => !t.archived_at)
  const favorites = boot?.favorites || []
  const remotes = useRemotes()
  useEffect(() => watchAgentWorkspace(), [])
  const [wsMenu, setWsMenu] = useState<Anchor | null>(null)
  const here = remotes.find(r => r.id === REMOTE)
  const otherUnread = remotes.filter(r => r.id !== REMOTE).reduce((n, r) => n + (r.unread || 0), 0)
  // workspaces: this instance and the ones the person connected (a page load each)
  const wsItems: MenuItem[] = [
    { heading: 'Workspaces' },
    { label: 'Your own', icon: 'home', hint: location.host, checked: !REMOTE, onSelect: () => { location.href = '/' } },
    ...remotes.map(r => ({ label: r.name, icon: 'globe', hint: r.unread ? `${r.unread} unread · ${r.host}` : r.host, checked: r.id === REMOTE, onSelect: () => { location.href = `/r/${r.id}/` } })),
    'sep',
    { label: 'Connect another…', icon: 'plus', onSelect: () => { location.href = '/settings/connections' } },
  ]

  return (
    <aside className="sidebar" aria-label="Projects">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Projects' ? title : <>Pocket <b>Projects</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      {(REMOTE > 0 || remotes.length > 0) && <button type="button" className={`sb-ws ${REMOTE ? 'remote' : ''}`} onClick={e => setWsMenu(rectOf(e.currentTarget))}
        title={(REMOTE ? `${here?.host || 'A connected instance'}: its teams, as you there. Your Pocket AI conversations and notes stay yours, here.` : 'Switch workspace') + ' Pocket AI works in the workspace open here; the others are closed to it.'}>
        <Icon name={REMOTE ? 'globe' : 'home'} size={14} /><span className="ellipsis">{REMOTE ? here?.name || 'Connected instance' : 'Your workspace'}</span>
        {REMOTE > 0 && here && <span className="sb-ws-host ellipsis">{here.host}</span>}
        {otherUnread > 0 && <span className="sb-ws-badge" title={`${otherUnread} unread in your other workspaces`}>{otherUnread}</span>}<Icon name="chevron-down" size={13} />
      </button>}
      {wsMenu && <Menu items={wsItems} anchor={wsMenu} onClose={() => setWsMenu(null)} width={270} />}
      <button type="button" className="btn primary create-btn" onClick={() => { onNavigate(); openCreateIssue() }} title="New issue (C)">
        <Icon name="edit" size={16} strokeWidth={2.1} />New issue<kbd className="kbd-on-primary">C</kbd>
      </button>

      <nav className="sb-nav" aria-label="You">
        <Item to="/inbox" icon={<Icon name="inbox" size={16} />} label="Inbox" count={unread} warn onNavigate={onNavigate} />
        <Item to="/my-issues" icon={<Icon name="target" size={16} />} label="My issues" onNavigate={onNavigate} />
        <Item to="/calendar" icon={<Icon name="calendar" size={16} />} label="Calendar" onNavigate={onNavigate} />
        <Item to="/time" icon={<Icon name="timer" size={16} />} label="Time" onNavigate={onNavigate} />
      </nav>

      <nav className="sb-nav sb-sec" aria-label="Workspace">
        <h4 className="sb-h"><span className="sb-h-label">Workspace</span></h4>
        <Item to="/projects" icon={<Icon name="briefcase" size={16} />} label="Projects" onNavigate={onNavigate} end />
        <Item to="/roadmap" icon={<Icon name="route" size={16} />} label="Roadmap" onNavigate={onNavigate} />
        <Item to="/views" icon={<Icon name="layers" size={16} />} label="Views" onNavigate={onNavigate} />
        <Item to="/insights" icon={<Icon name="bar-chart" size={16} />} label="Insights" onNavigate={onNavigate} />
      </nav>

      {favorites.length > 0 && <section className="sb-sec">
        <h4 className="sb-h"><button type="button" className="sb-h-btn" aria-expanded={favOpen} onClick={() => setFavOpen(o => !o)}><Icon name={favOpen ? 'chevron-down' : 'chevron-right'} size={13} />Favorites</button></h4>
        {favOpen && <div className="sb-nav">{favorites.map(f => <FavoriteItem key={f.id} fav={f} onNavigate={onNavigate} />)}</div>}
      </section>}

      <section className="sb-sec">
        <h4 className="sb-h">
          <button type="button" className="sb-h-btn" aria-expanded={teamsOpen} onClick={() => setTeamsOpen(o => !o)}><Icon name={teamsOpen ? 'chevron-down' : 'chevron-right'} size={13} />Your teams</button>
          <Link to="/teams/new" className="btn ghost icon-only xs" aria-label="New team" title="New team" onClick={onNavigate}><Icon name="plus" size={14} /></Link>
        </h4>
        {teamsOpen && live.map(t => <TeamGroup key={t.id} team={t} onNavigate={onNavigate} />)}
        {teamsOpen && !live.length && <p className="sb-empty">No teams yet. <Link to="/teams/new" onClick={onNavigate}>Create one</Link></p>}
      </section>

      <div className="sb-bottom">
        <TimerPill />
        <div className="sb-foot">
          <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
          <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pp:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
          <NavLink to="/trash" className={({ isActive }) => `sb-foot-link sb-trash ${isActive ? 'active' : ''}`} onClick={onNavigate} title="Trash" aria-label="Trash"><Icon name="trash" size={15} /></NavLink>
        </div>
      </div>
    </aside>
  )
}

function FavoriteItem({ fav, onNavigate }: { fav: Favorite; onNavigate: () => void }) {
  const app = useApp()
  switch (fav.kind) {
    case 'project': { const p = app.project(fav.ref_id); return p ? <Item to={`/project/${p.id}`} icon={<ProjectMark icon={p.icon} color={p.color} size={15} />} label={p.name} onNavigate={onNavigate} /> : null }
    case 'team': { const t = app.team(fav.ref_id); return t ? <Item to={`/team/${t.key}/issues`} icon={<TeamMark team={t} />} label={t.name} onNavigate={onNavigate} /> : null }
    case 'view': { const v = app.boot?.views.find(x => x.id === fav.ref_id); return v ? <Item to={`/view/${v.id}`} icon={<span style={{ color: v.color || 'var(--muted)' }}>{v.icon || <Icon name="layers" size={15} />}</span>} label={v.name} onNavigate={onNavigate} /> : null }
    case 'cycle': { const c = app.cycle(fav.ref_id); const t = c && app.team(c.team_id); return c && t ? <Item to={`/team/${t.key}/cycles/${c.id}`} icon={<Icon name="cycle" size={15} />} label={`${t.key} · ${c.name || 'Cycle ' + c.number}`} onNavigate={onNavigate} /> : null }
    case 'issue': return <Item to={`/issue/${fav.ref_id}`} icon={<StatusIcon type="unstarted" />} label={`Issue ${fav.ref_id}`} onNavigate={onNavigate} />
    case 'doc': return <Item to={`/doc/${fav.ref_id}`} icon={<Icon name="file-text" size={15} />} label="Document" onNavigate={onNavigate} />
  }
  return null
}

function TeamGroup({ team, onNavigate }: { team: Team; onNavigate: () => void }) {
  const [open, setOpen] = useFold('pp-sb-team:' + team.id)
  const [menu, setMenu] = useState<Anchor | null>(null)
  const navigate = useNavigate()
  const base = `/team/${team.key}`
  const items: MenuItem[] = [
    { label: 'New issue', icon: 'plus', onSelect: () => openCreateIssue({ team_id: team.id }) },
    { label: 'Team settings', icon: 'settings', onSelect: () => navigate(`${base}/settings`) },
    { label: 'Copy team key', icon: 'copy', onSelect: () => navigator.clipboard?.writeText(team.key) },
  ]
  return (
    <div className="sb-team">
      <div className="cal-row sb-row area" onContextMenu={e => { e.preventDefault(); setMenu({ x: e.clientX, y: e.clientY, w: 0, h: 0 }) }}>
        <button type="button" className="cal-check" aria-expanded={open} onClick={() => setOpen(o => !o)}>
          <span className="sb-row-ico"><TeamMark team={team} /></span>
          <span className="cal-name ellipsis">{team.name}</span>
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size={12} className="sb-twist" />
        </button>
        <button type="button" className="btn ghost icon-only xs cal-more" aria-label={`Options for ${team.name}`} aria-haspopup="menu" onClick={e => setMenu(rectOf(e.currentTarget))}><Icon name="more" size={15} /></button>
        {menu && <Menu anchor={menu} items={items} onClose={() => setMenu(null)} width={200} />}
      </div>
      {open && <div className="sb-nav sb-nested">
        <DeployItem teamKey={team.key} onNavigate={onNavigate} />
        {team.triage_enabled && <Item to={`${base}/triage`} icon={<Icon name="inbox" size={15} />} label="Triage" count={team.counts?.triage} warn onNavigate={onNavigate} />}
        <Item to={`${base}/issues`} icon={<Icon name="layers" size={15} />} label="Issues" onNavigate={onNavigate} />
        <Item to={`${base}/active`} icon={<Icon name="zap" size={15} />} label="Active" count={team.counts?.started} onNavigate={onNavigate} />
        <WithAIItem to={`${base}/ai`} teamKey={team.key} onNavigate={onNavigate} />
        <Item to={`${base}/backlog`} icon={<StatusIcon type="backlog" size={15} />} label="Backlog" onNavigate={onNavigate} />
        {team.cycles_enabled && <Item to={`${base}/cycles`} icon={<Icon name="cycle" size={15} />} label="Cycles" onNavigate={onNavigate} />}
        <Item to={`${base}/projects`} icon={<Icon name="briefcase" size={15} />} label="Projects" onNavigate={onNavigate} />
        {team.notes_folder && <Item to={`${base}/notes`} icon={<Icon name="note" size={15} />} label="Notes" onNavigate={onNavigate} />}
      </div>}
    </div>
  )
}

function TimerPill() {
  const app = useApp()
  const t = app.boot?.timer
  const [, tick] = useState(0)
  useEffect(() => { if (!t) return; const i = window.setInterval(() => tick(x => x + 1), 30000); return () => window.clearInterval(i) }, [t])
  if (!t || !t.started_at) return null
  const mins = Math.max(0, Math.round((Date.now() - new Date(t.started_at).getTime()) / 60000)) + t.minutes
  return <Link to={`/issue/${t.issue_id}`} className="timer-pill"><span className="rec-dot" /><Icon name="timer" size={14} />Timer running<span className="spacer" /><b>{Math.floor(mins / 60)}:{String(mins % 60).padStart(2, '0')}</b></Link>
}

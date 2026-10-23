// One cycle: name, dates and progress, the burn-up, who/what the work is
// split across, and the cycle's issues.
import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { emitChange, useApp, useLive } from '../lib/app-context'
import { shortDate, today } from '../lib/dates'
import type { Cycle, CycleDetail, Part } from '../lib/types'
import { Icon } from '../components/icons'
import { PersonAvatar, ProjectMark } from '../components/issue-bits'
import { TeamMark } from '../components/pickers'
import { type MenuItem, Confirm, Empty, Loading, MenuButton, Segmented, useIsMobile, useToast } from '../components/ui'
import { IssueView } from '../views/IssueView'
import { BURN_LEGEND, BurnUp, ChartCard, HBars, type HBarRow, StatTile, compact } from '../components/planning/charts'
import { CompleteCycle, CycleForm, STATUS_LABEL, cycleName, cycleRange, cycleWhen } from '../components/planning/cycle-bits'
import '../styles/planning.css'

type Mode = 'issues' | 'points'
type By = 'assignee' | 'label' | 'project'

export default function CyclePage() {
  const { key, id } = useParams()
  const cid = Number(id)
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const team = app.teamByKey(key)
  const [c, setC] = useState<CycleDetail | null>(null)
  const [err, setErr] = useState('')
  const [name, setName] = useState('')
  const [mode, setMode] = useState<Mode>(() => (localStorage.getItem('pp-burn-mode') as Mode) || 'issues')
  const [by, setBy] = useState<By>('assignee')
  const [dialog, setDialog] = useState<'' | 'edit' | 'complete' | 'delete'>('')
  const [siblings, setSiblings] = useState<Cycle[]>([])

  const load = useCallback(() => {
    api.get<CycleDetail>(`${V1}/cycles/${cid}`).then(d => { setC(d); setName(d.name); setErr('') }).catch(e => setErr((e as Error).message))
  }, [cid])
  useEffect(() => { setC(null); load() }, [load])
  useLive(load, ['cycle', 'issue'], team ? [team.id] : undefined)
  useEffect(() => { if (team && dialog === 'edit') api.get<Cycle[]>(`${V1}/teams/${team.key}/cycles`).then(setSiblings).catch(() => {}) }, [dialog, team])

  if (err) return <div className="page"><Empty title="Cycle not found" icon="cycle">{err}</Empty></div>
  if (!c || !team) return <div className="page"><Loading /></div>
  const canEdit = app.canEdit(c.team_id)
  const isAdmin = app.team(c.team_id)?.role === 'admin'
  const p = c.progress
  const hasPoints = p.points > 0 || c.snapshots.some(s => s.scope_points > 0)
  const m: Mode = hasPoints ? mode : 'issues'
  const unfinished = Math.max(0, p.total - p.completed - p.canceled)
  const fav = app.boot?.favorites.find(f => f.kind === 'cycle' && f.ref_id === c.id)
  const done = !!c.completed_at
  const status = done ? 'Completed' : STATUS_LABEL[c.status]

  const saveName = async () => {
    const v = name.trim()
    if (v === c.name) return
    try {
      await api.patch(`${V1}/cycles/${c.id}`, { name: v })
      emitChange({ kind: 'cycle', team_ids: [c.team_id], ids: [c.id] })
      app.reload().catch(() => {})
      load()
    } catch (e) { toast((e as Error).message, 'err'); setName(c.name) }
  }
  const toggleFav = async () => {
    try {
      if (fav) await api.del(`${V1}/favorites/${fav.id}`)
      else await api.post(`${V1}/favorites`, { kind: 'cycle', ref_id: c.id })
      await app.reload()
      toast(fav ? 'Removed from favorites' : 'Added to favorites')
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const del = async () => {
    setDialog('')
    try {
      await api.del(`${V1}/cycles/${c.id}`)
      emitChange({ kind: 'cycle', team_ids: [c.team_id], ids: [c.id] })
      app.reload().catch(() => {})
      toast(`${cycleName(c)} deleted`)
      navigate(`/team/${team.key}/cycles`)
    } catch (e) { toast((e as Error).message, 'err') }
  }

  const menu: MenuItem[] = [
    { label: fav ? 'Remove from favorites' : 'Add to favorites', icon: 'star', onSelect: toggleFav },
    ...(canEdit ? [{ label: 'Edit name and dates', icon: 'edit', onSelect: () => setDialog('edit') }] : []),
    ...(canEdit && !done && c.status !== 'upcoming' && mobile ? [{ label: 'Complete cycle', icon: 'check', onSelect: () => setDialog('complete') }] : []),
    ...(isAdmin ? ['sep' as const, { label: 'Delete cycle', icon: 'trash', danger: true, onSelect: () => setDialog('delete') }] : []),
  ]

  const pick = (s: CycleDetail['snapshots'][number]) => m === 'points'
    ? { day: s.day, scope: s.scope_points, started: s.started_points, completed: s.completed_points }
    : { day: s.day, scope: s.scope, started: s.started, completed: s.completed }
  const burn = c.snapshots.map(pick)
  const parts = by === 'assignee' ? c.by_assignee : by === 'label' ? c.by_label : c.by_project
  const rows: HBarRow[] = parts.map((x: Part) => ({
    key: x.id, name: x.name, value: x.completed, total: x.completed + x.open,
    detail: x.points ? `${compact(x.points)} pts` : undefined,
    label: <>{by === 'assignee' ? <PersonAvatar person={x.id ? app.person(x.id) || { id: x.id, name: x.name, email: '', username: '', color: x.color } : null} size={18} />
      : by === 'project' ? (x.id ? <ProjectMark icon={app.project(x.id)?.icon} color={x.color || undefined} size={14} /> : <Icon name="briefcase" size={14} className="faint" />)
        : <span className="dot" style={{ background: x.color }} />}<span className="ellipsis">{x.name}</span></>,
  }))

  return (
    <div className="full-page cycle-page">
      <div className="page-head">
        <h1 className="cy-crumbs">
          <TeamMark team={team} size={18} />
          <Link to={`/team/${team.key}/cycles`} className="crumb-link">Cycles</Link>
          <Icon name="chevron-right" size={15} className="crumb-sep" />
          <input className="cy-name-input" value={name} placeholder={`Cycle ${c.number}`} disabled={!canEdit} aria-label="Cycle name"
            onChange={e => setName(e.target.value)} onBlur={saveName} onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') { setName(c.name); (e.target as HTMLInputElement).blur() } }}
            style={{ width: `${Math.max(8, (name || `Cycle ${c.number}`).length + 2)}ch` }} />
        </h1>
        <span className={`cy-status s-${done ? 'done' : c.status}`}>{status}</span>
        <span className="pl-head-actions">
          <button type="button" className={`btn ghost icon-only sm cy-fav ${fav ? 'on' : ''}`} onClick={toggleFav} aria-pressed={!!fav} title={fav ? 'Remove from favorites' : 'Add to favorites'} aria-label="Favorite">
            <Icon name="star" size={16} fill={!!fav} stroke={fav ? 1.4 : 1.85} />
          </button>
          {canEdit && !done && c.status !== 'upcoming' && !mobile && <button type="button" className="btn sm" onClick={() => setDialog('complete')}><Icon name="check" size={14} />Complete cycle</button>}
          <MenuButton label="Cycle options" items={menu} align="right"><Icon name="more" size={16} /></MenuButton>
        </span>
      </div>
      <div className="pl-wrap">
        <div className="cy-meta">
          <span><Icon name="calendar" size={14} />{cycleRange(c)}</span>
          <span><Icon name="clock" size={14} />{done ? `Completed ${shortDate(c.completed_at!.slice(0, 10))}` : cycleWhen(c)}</span>
          <span><Icon name="target" size={14} />{Math.round(p.percent)}% done</span>
          {c.description && <span className="muted">{c.description}</span>}
        </div>
        <div className="stat-tiles cy-tiles">
          <StatTile label="Scope" value={<>{p.total}{hasPoints && <small>{compact(p.points)} pts</small>}</>} sub={`${unfinished} not finished`} />
          <StatTile label="Started" value={p.started} sub="in progress now" />
          <StatTile label="Completed" value={<>{p.completed}{hasPoints && <small>{compact(p.completed_points)} pts</small>}</>} sub={`${Math.round(p.percent)}% of scope`} />
          <StatTile label={c.status === 'upcoming' ? 'Length' : 'Days left'} value={c.status === 'upcoming' ? `${c.days} d` : done || c.status === 'past' ? '—' : c.days_left} sub={`${c.days}-day cycle`} />
        </div>
        <div className="cy-grid">
          <ChartCard title="Burn-up" explain="Completed work climbing toward the scope; the dashed line is the even pace to finish on the last day."
            legend={BURN_LEGEND}
            actions={hasPoints ? <Segmented size="sm" label="Count" value={m} onChange={v => { setMode(v); localStorage.setItem('pp-burn-mode', v) }} options={[{ value: 'issues', label: 'Issues' }, { value: 'points', label: 'Points' }]} /> : undefined}
            table={{ head: ['Day', 'Scope', 'Started', 'Completed'], rows: burn.map(s => [s.day, compact(s.scope), compact(s.started), compact(s.completed)]) }}>
            {c.snapshots.length === 0
              ? <p className="muted small viz-none">The chart fills in day by day once the cycle starts.</p>
              : <BurnUp start={c.starts_on} end={c.ends_on} points={burn} today={today()} height={mobile ? 200 : 240} unit={m === 'points' ? ' pts' : ''} />}
          </ChartCard>
          <ChartCard title="Breakdown" explain="Completed out of all the work in the cycle, by who, label or project."
            actions={<Segmented size="sm" label="Break down by" value={by} onChange={setBy} options={[{ value: 'assignee', label: 'Assignees' }, { value: 'label', label: 'Labels' }, { value: 'project', label: 'Projects' }]} />}
            table={{ head: ['Name', 'Completed', 'Open', 'Points'], rows: parts.map(x => [x.name, x.completed, x.open, compact(x.points)]) }}>
            <HBars rows={rows} doneLabel="completed" restLabel="open" />
          </ChartCard>
        </div>
        <section className="pl-section cy-issues">
          <h2 className="pl-section-h">Issues <span className="count">{p.total}</span></h2>
          <div className="cy-issue-box">
            <IssueView filter={{ cycle_ids: [c.id] }} storageKey={'cycle-' + c.id} newIssue={{ team_id: c.team_id, cycle_id: c.id }} teamIds={[c.team_id]}
              emptyTitle="No issues in this cycle" emptyText="Add issues to the cycle from their Cycle property, or create one here." />
          </div>
        </section>
      </div>
      {dialog === 'edit' && <CycleForm team={team} cycle={c} cycles={siblings.filter(x => x.id !== c.id)} onClose={() => setDialog('')} onSaved={() => { setDialog(''); load(); app.reload().catch(() => {}) }} />}
      {dialog === 'complete' && <CompleteCycle cycle={c} unfinished={unfinished} onClose={() => setDialog('')} onDone={() => { setDialog(''); load(); app.reload().catch(() => {}) }} />}
      {dialog === 'delete' && <Confirm title={`Delete ${cycleName(c)}?`} message="Its issues stay, just without a cycle. This can't be undone." confirmLabel="Delete cycle" onConfirm={del} onCancel={() => setDialog('')} />}
    </div>
  )
}

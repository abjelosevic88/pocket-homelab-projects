// A team's cycles: the current one with its burn-up, what's coming and what's done.
import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp, useLive } from '../lib/app-context'
import { shortDate, today } from '../lib/dates'
import type { Cycle, CycleDetail } from '../lib/types'
import { Icon } from '../components/icons'
import { TeamMark } from '../components/pickers'
import { Empty, Loading } from '../components/ui'
import { ProgressBar } from '../components/issue-bits'
import { BurnUp, Ring, VIZ, VIZ_INK, compact } from '../components/planning/charts'
import { CycleForm, cycleName, cycleRange, cycleWhen } from '../components/planning/cycle-bits'
import '../styles/planning.css'

export default function Cycles() {
  const { key } = useParams()
  const app = useApp()
  const navigate = useNavigate()
  const team = app.teamByKey(key)
  const [cycles, setCycles] = useState<Cycle[] | null>(null)
  const [cur, setCur] = useState<CycleDetail | null>(null)
  const [err, setErr] = useState('')
  const [creating, setCreating] = useState(false)
  const load = useCallback(() => {
    if (!team) return
    api.get<Cycle[]>(`${V1}/teams/${team.key}/cycles`).then(list => {
      setCycles(list); setErr('')
      const c = list.find(x => x.status === 'current' && !x.completed_at)
      if (c) api.get<CycleDetail>(`${V1}/cycles/${c.id}`).then(setCur).catch(() => setCur(null))
      else setCur(null)
    }).catch(e => setErr((e as Error).message))
  }, [team?.key]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setCycles(null); load() }, [load])
  useLive(load, ['cycle', 'issue'], team ? [team.id] : undefined)

  if (!team) return <div className="page"><Empty title="Team not found" icon="users">It may have been archived, or you're not a member.</Empty></div>
  const canEdit = app.canEdit(team.id)
  const list = cycles || []
  const upcoming = list.filter(c => c.status === 'upcoming').sort((a, b) => a.starts_on.localeCompare(b.starts_on))
  const past = list.filter(c => c.status === 'past' || (c.completed_at && c.status !== 'upcoming')).filter(c => c.id !== cur?.id).sort((a, b) => b.starts_on.localeCompare(a.starts_on))
  const off = !team.cycles_enabled

  return (
    <div className="full-page">
      <div className="page-head">
        <h1><TeamMark team={team} size={18} />Cycles{list.length > 0 && <span className="count-faint">{list.length}</span>}</h1>
        <span className="pl-head-actions">
          {app.team(team.id)?.role === 'admin' && <Link to={`/team/${team.key}/settings/cycles`} className="btn ghost sm"><Icon name="settings" size={14} />Cycle settings</Link>}
          {canEdit && <button type="button" className="btn sm primary" onClick={() => setCreating(true)}><Icon name="plus" size={14} />New cycle</button>}
        </span>
      </div>
      <div className="pl-wrap narrow">
        {err && <div className="callout danger">{err}</div>}
        {!cycles && !err && <Loading />}
        {cycles && off && !list.length && (
          <div className="card cy-off">
            <div className="cy-off-ico"><Icon name="cycle" size={24} /></div>
            <h2>Cycles are off for {team.name}</h2>
            <p className="muted">Cycles are short, repeating stretches of work — usually one or two weeks. With them on, the next few are planned ahead,
              each day's progress is tracked for a burn-up chart, and anything unfinished rolls over to the next cycle.</p>
            <div className="row wrap center-row">
              <button type="button" className="btn primary" onClick={() => navigate(`/team/${team.key}/settings/cycles`)}><Icon name="settings" size={15} />Turn on cycles</button>
              {canEdit && <button type="button" className="btn" onClick={() => setCreating(true)}><Icon name="plus" size={15} />Make one by hand</button>}
            </div>
          </div>
        )}
        {cycles && off && list.length > 0 && <div className="callout info mb"><Icon name="info" size={16} /><span>Automatic cycles are off for this team, so these were made by hand. <Link to={`/team/${team.key}/settings/cycles`}>Turn them on</Link> to plan ahead and roll work over.</span></div>}

        {cycles && cur && <CurrentCycle c={cur} teamKey={team.key} />}
        {cycles && !cur && list.length > 0 && <div className="cy-none"><Icon name="cycle" size={16} />No cycle is running right now.{upcoming[0] && <> The next one starts {shortDate(upcoming[0].starts_on)}.</>}</div>}

        {upcoming.length > 0 && <section className="pl-section">
          <h2 className="pl-section-h">Upcoming <span className="count">{upcoming.length}</span></h2>
          <div className="pl-list">{upcoming.map(c => <CycleRow key={c.id} c={c} teamKey={team.key} />)}</div>
        </section>}
        {past.length > 0 && <section className="pl-section">
          <h2 className="pl-section-h">Past <span className="count">{past.length}</span></h2>
          <div className="pl-list">{past.map(c => <CycleRow key={c.id} c={c} teamKey={team.key} />)}</div>
        </section>}
      </div>
      {creating && <CycleForm team={team} cycles={list} onClose={() => setCreating(false)} onSaved={c => { setCreating(false); navigate(`/team/${team.key}/cycles/${c.id}`) }} />}
    </div>
  )
}

function CurrentCycle({ c, teamKey }: { c: CycleDetail; teamKey: string }) {
  const p = c.progress
  const pts = p.points > 0
  const snaps = c.snapshots.map(s => ({ day: s.day, scope: s.scope, started: s.started, completed: s.completed }))
  return (
    <Link to={`/team/${teamKey}/cycles/${c.id}`} className="card cy-current" data-pocket-ask>
      <div className="cy-cur-main">
        <div className="cy-cur-label"><span className="cy-live" />Current cycle</div>
        <div className="cy-cur-title">
          <Ring percent={p.percent} size={58} stroke={5} />
          <div className="grow">
            <h2>{cycleName(c)}</h2>
            <div className="muted small">{cycleRange(c)} · <b className="cy-left">{cycleWhen(c)}</b></div>
          </div>
        </div>
        <div className="cy-stats">
          <div><span className="cy-stat-n">{p.total}</span><span className="cy-stat-l">Scope{pts && <small> · {compact(p.points)} pts</small>}</span></div>
          <div><span className="cy-stat-n"><i style={{ background: VIZ[1] }} />{p.started}</span><span className="cy-stat-l">Started</span></div>
          <div><span className="cy-stat-n"><i style={{ background: VIZ[0] }} />{p.completed}</span><span className="cy-stat-l">Completed{pts && <small> · {compact(p.completed_points)} pts</small>}</span></div>
        </div>
      </div>
      <div className="cy-cur-chart">
        <BurnUp start={c.starts_on} end={c.ends_on} points={snaps} today={today()} height={118} mini />
        <div className="cy-mini-legend"><span><i style={{ background: VIZ_INK }} />Scope</span><span><i style={{ background: VIZ[1] }} />Started</span><span><i style={{ background: VIZ[0] }} />Completed</span></div>
      </div>
    </Link>
  )
}

function CycleRow({ c, teamKey }: { c: Cycle; teamKey: string }) {
  const p = c.progress
  const done = c.status === 'past' || !!c.completed_at
  return (
    <Link to={`/team/${teamKey}/cycles/${c.id}`} className="pl-row cy-row" data-pocket-ask>
      <span className={`cy-row-ico ${done ? 'done' : ''}`}><Icon name={done ? 'check' : 'cycle'} size={14} /></span>
      <span className="grow">
        <span className="cy-row-name">{cycleName(c)}</span>
        <span className="pl-sub">{cycleRange(c)} · {cycleWhen(c)}</span>
      </span>
      {done ? <>
        <span className="cy-row-bar"><ProgressBar percent={p.total ? (p.completed / p.total) * 100 : 0} /></span>
        <span className="cy-row-num tnum" title="Completed of scope">{p.completed}/{p.total}</span>
        <span className="cy-row-velo tnum" title="Velocity">{p.points > 0 ? `${compact(p.completed_points)} pts` : `${p.completed} done`}</span>
      </> : <span className="pl-sub tnum">{p.total ? `${p.total} issue${p.total === 1 ? '' : 's'}${p.points ? ` · ${compact(p.points)} pts` : ''}` : 'Nothing planned yet'}</span>}
      <Icon name="chevron-right" size={15} className="faint" />
    </Link>
  )
}

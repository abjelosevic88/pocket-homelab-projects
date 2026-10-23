// Insights: how work flows for my teams, a team or a project over a period.
import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, qs, V1 } from '../lib/api'
import { useApp, useLive } from '../lib/app-context'
import { addDays, shortDate, today } from '../lib/dates'
import { STATE_TYPE_LABEL, STATE_TYPE_ORDER } from '../lib/issues'
import type { Insights as InsightsData, Part, StateType } from '../lib/types'
import { Icon } from '../components/icons'
import { PersonAvatar, PriorityIcon, ProjectMark, StatusIcon } from '../components/issue-bits'
import { TeamMark } from '../components/pickers'
import { Select, type Option } from '../components/Select'
import { Empty, Loading, Segmented, useIsMobile } from '../components/ui'
import { ChartCard, ColumnChart, HBars, type HBarRow, StackedArea, StatTile, VIZ, VIZ_CTX, compact } from '../components/planning/charts'
import '../styles/planning.css'

type Weeks = 4 | 12 | 26 | 52
type By = 'assignee' | 'label' | 'priority' | 'project'
const FLOW: { type: StateType; color: string }[] = [
  { type: 'completed', color: VIZ[0] }, { type: 'started', color: VIZ[1] }, { type: 'unstarted', color: VIZ[2] }, { type: 'backlog', color: VIZ[3] }, { type: 'triage', color: VIZ[4] },
]

const days = (v: number) => (v >= 10 ? `${Math.round(v)}` : v >= 1 ? v.toFixed(1).replace(/\.0$/, '') : '<1')

export default function Insights() {
  const app = useApp()
  const mobile = useIsMobile()
  const [scope, setScopeRaw] = useState(() => localStorage.getItem('pp-insights-scope') || 'all')
  const [weeks, setWeeksRaw] = useState<Weeks>(() => (Number(localStorage.getItem('pp-insights-weeks')) as Weeks) || 12)
  const setScope = (v: string) => { setScopeRaw(v); localStorage.setItem('pp-insights-scope', v) }
  const setWeeks = (v: Weeks) => { setWeeksRaw(v); localStorage.setItem('pp-insights-weeks', String(v)) }
  const [data, setData] = useState<InsightsData | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [by, setBy] = useState<By>('assignee')
  const [velPoints, setVelPoints] = useState(false)

  // a scope that no longer exists falls back to all teams
  const valid = scope === 'all' || (scope.startsWith('t') && app.team(Number(scope.slice(1)))) || (scope.startsWith('p') && app.project(Number(scope.slice(1))))
  const sc = valid ? scope : 'all'
  const to = today()
  const from = addDays(to, -weeks * 7 + 1)
  const load = useCallback(() => {
    setBusy(true)
    const q = qs({ from, to, team_id: sc.startsWith('t') ? sc.slice(1) : undefined, project_id: sc.startsWith('p') ? sc.slice(1) : undefined })
    api.get<InsightsData>(`${V1}/insights${q}`).then(d => { setData(d); setErr('') }).catch(e => setErr((e as Error).message)).finally(() => setBusy(false))
  }, [sc, from, to])
  useEffect(() => { load() }, [load])
  useLive(load, ['issue', 'cycle'])

  const options: Option<string>[] = useMemo(() => [
    { value: 'all', label: 'All my teams', icon: <Icon name="layers" size={15} className="faint" /> },
    ...app.teams.map(t => ({ value: 't' + t.id, label: t.name, group: 'Teams', keywords: t.key, icon: <TeamMark team={t} size={16} /> })),
    ...app.projects.filter(p => !p.deleted_at).map(p => ({ value: 'p' + p.id, label: p.name, group: 'Projects', icon: <ProjectMark icon={p.icon} color={p.color} size={15} /> })),
  ], [app.teams, app.projects])

  const multiTeam = !sc.startsWith('t') && app.teams.length > 1
  return (
    <div className="full-page insights-page">
      <div className="page-head ins-head">
        <h1><Icon name="bar-chart" size={17} />Insights</h1>
        <div className="pl-filters ins-filters">
          <div className="ins-scope"><Select value={sc} options={options} onChange={setScope} label="Scope" searchable size="sm" /></div>
          <Segmented size="sm" label="Period" value={weeks} onChange={setWeeks} options={[{ value: 4, label: mobile ? '4w' : '4 weeks' }, { value: 12, label: mobile ? '12w' : '12 weeks' }, { value: 26, label: mobile ? '6m' : '6 months' }, { value: 52, label: mobile ? '1y' : '1 year' }]} />
          {!mobile && <span className="muted small">{shortDate(from)} – {shortDate(to)}</span>}
          {busy && data && <span className="spinner" aria-label="Updating" />}
        </div>
      </div>
      <div className="pl-wrap">
        {err && <div className="callout danger mt">{err}</div>}
        {!data && !err && <Loading />}
        {data && <div className={`ins-body ${busy ? 'refetch' : ''}`}>
          <Tiles d={data} />
          {data.totals.open === 0 && data.totals.created === 0 && data.totals.completed === 0
            ? <div className="card mt"><Empty title="Nothing to chart yet" icon="bar-chart">Create and finish a few issues and the charts fill in.</Empty></div>
            : <div className="pl-grid-2 mt">
              <WeeklyChart d={data} />
              {sc.startsWith('p')
                ? <ChartCard title="Cumulative flow" explain="How much work sat in each state, day by day."><p className="muted small viz-none">Cumulative flow is kept per team — pick a team or all teams to see it.</p></ChartCard>
                : <FlowChart d={data} />}
              <VelocityChart d={data} points={velPoints} setPoints={setVelPoints} multiTeam={multiTeam} />
              <OpenWork d={data} by={by} setBy={setBy} />
              <StateChart d={data} multiTeam={multiTeam} />
            </div>}
        </div>}
      </div>
    </div>
  )
}

function Tiles({ d }: { d: InsightsData }) {
  const t = d.totals, tm = d.times
  const hours = t.minutes_logged / 60
  return (
    <div className="stat-tiles ins-tiles">
      <StatTile label="Open" value={t.open} sub={t.open_points ? `${compact(t.open_points)} points` : 'issues not done'} />
      <StatTile label="In progress" value={t.started} sub={t.open ? `${Math.round((t.started / t.open) * 100)}% of open` : 'nothing open'} />
      <StatTile label="Completed" value={t.completed} sub="in this period" />
      <StatTile label="Created" value={t.created} sub="in this period" />
      <StatTile label="Overdue" value={t.overdue} sub={t.overdue ? 'past their due date' : 'nothing late'} tone={t.overdue ? 'danger' : undefined} />
      <StatTile label="Lead time (median)" value={tm.completed ? <>{days(tm.lead_days_median)}<small>days</small></> : '—'} sub={tm.completed ? `80% within ${days(tm.lead_days_p80)} d` : 'created → done'} />
      <StatTile label="Cycle time (median)" value={tm.completed ? <>{days(tm.cycle_days_median)}<small>days</small></> : '—'} sub={tm.completed ? `80% within ${days(tm.cycle_days_p80)} d` : 'started → done'} />
      <StatTile label="Hours logged" value={<>{compact(Math.round(hours * 10) / 10)}<small>h</small></>} sub="time entries in period" />
    </div>
  )
}

function WeeklyChart({ d }: { d: InsightsData }) {
  const w = d.weekly
  const series = [
    { label: 'Created', color: VIZ[0], values: w.map(x => x.created) },
    { label: 'Completed', color: VIZ[1], values: w.map(x => x.completed) },
  ]
  return (
    <ChartCard title="Created vs completed" explain="New issues against finished ones each week — completed above created means the pile is shrinking."
      legend={series.map(s => ({ label: s.label, color: s.color }))}
      table={{ head: ['Week of', 'Created', 'Completed', 'Canceled'], rows: w.map(x => [shortDate(x.week), x.created, x.completed, x.canceled]) }}>
      <ColumnChart labels={w.map(x => x.week)} series={series} xFormat={l => shortDate(l)} tipTitle={i => `Week of ${shortDate(w[i].week)}`} ariaLabel="Issues created and completed per week" />
    </ChartCard>
  )
}

function FlowChart({ d }: { d: InsightsData }) {
  const flow = d.flow
  const used = FLOW.filter(f => f.type !== 'triage' || flow.some(x => (x.counts.triage || 0) > 0))
  const series = used.map(f => ({ label: STATE_TYPE_LABEL[f.type], color: f.color, values: flow.map(x => x.counts[f.type] || 0) }))
  return (
    <ChartCard title="Cumulative flow" explain="Issues in each state, day by day — a widening band shows where work piles up."
      legend={series.map(s => ({ label: s.label, color: s.color }))}
      table={{ head: ['Day', ...series.map(s => s.label)], rows: flow.map((x, i) => [shortDate(x.day), ...series.map(s => s.values[i])]) }}>
      {flow.length < 2
        ? <p className="muted small viz-none">The flow is recorded once a day, so this fills in from tomorrow.</p>
        : <StackedArea days={flow.map(x => x.day)} series={series} ariaLabel="Cumulative flow of issues by state" />}
    </ChartCard>
  )
}

function VelocityChart({ d, points, setPoints, multiTeam }: { d: InsightsData; points: boolean; setPoints: (v: boolean) => void; multiTeam: boolean }) {
  const app = useApp()
  const v = [...d.velocity].sort((a, b) => a.starts_on.localeCompare(b.starts_on))
  const hasPoints = v.some(x => x.points > 0)
  const pts = hasPoints && points
  const label = (x: typeof v[number]) => {
    const key = multiTeam ? app.team(app.cycle(x.cycle_id)?.team_id)?.key : ''
    return `${key ? key + ' ' : ''}${x.name || `C${x.number}`}`
  }
  const series = [
    { label: 'Scope', color: VIZ_CTX, values: v.map(x => (pts ? x.points : x.scope)) },
    { label: 'Completed', color: VIZ[0], values: v.map(x => (pts ? x.completed_points : x.completed)) },
  ]
  return (
    <ChartCard title="Cycle velocity" explain={`What each cycle planned (grey) against what it finished${pts ? ', in points' : ''}.`}
      legend={series.map(s => ({ label: s.label, color: s.color }))}
      actions={hasPoints ? <Segmented size="sm" label="Count" value={pts ? 'p' : 'i'} onChange={x => setPoints(x === 'p')} options={[{ value: 'i', label: 'Issues' }, { value: 'p', label: 'Points' }]} /> : undefined}
      table={{ head: ['Cycle', 'Dates', 'Scope', 'Completed', 'Points', 'Done points'], rows: v.map(x => [label(x), `${shortDate(x.starts_on)} – ${shortDate(x.ends_on)}`, x.scope, x.completed, compact(x.points), compact(x.completed_points)]) }}>
      {v.length ? <ColumnChart labels={v.map(label)} series={series} integer={!pts} tipTitle={i => `${label(v[i])} · ${shortDate(v[i].starts_on)} – ${shortDate(v[i].ends_on)}`} ariaLabel="Scope and completed work per cycle" />
        : <p className="muted small viz-none">No cycles in this scope yet.</p>}
    </ChartCard>
  )
}

function OpenWork({ d, by, setBy }: { d: InsightsData; by: By; setBy: (b: By) => void }) {
  const app = useApp()
  const parts: Part[] = by === 'assignee' ? d.by_assignee : by === 'label' ? d.by_label : by === 'priority' ? d.by_priority : d.by_project
  const sorted = by === 'priority' ? [...parts].sort((a, b) => (a.id || 9) - (b.id || 9)) : parts
  const rows: HBarRow[] = sorted.map(x => ({
    key: x.id, name: x.name, value: x.open, detail: `open${x.points ? ` · ${compact(x.points)} pts` : ''}`,
    label: <>{by === 'assignee' ? <PersonAvatar person={x.id ? app.person(x.id) || { id: x.id, name: x.name, email: '', username: '', color: x.color } : null} size={18} />
      : by === 'label' ? <span className="dot" style={{ background: x.color }} />
        : by === 'priority' ? <PriorityIcon priority={x.id} />
          : x.id ? <ProjectMark icon={app.project(x.id)?.icon} color={x.color || undefined} size={14} /> : <Icon name="briefcase" size={14} className="faint" />}
      <span className="ellipsis">{x.name}</span></>,
  }))
  return (
    <ChartCard title="Open work" explain="Issues not yet done, split by who has them, label, priority or project."
      actions={<Segmented size="sm" label="Split by" value={by} onChange={setBy} options={[{ value: 'assignee', label: 'Who' }, { value: 'label', label: 'Label' }, { value: 'priority', label: 'Priority' }, { value: 'project', label: 'Project' }]} />}
      table={{ head: ['Name', 'Open', 'Points'], rows: sorted.map(x => [x.name, x.open, compact(x.points)]) }}>
      <HBars rows={rows} />
    </ChartCard>
  )
}

function StateChart({ d, multiTeam }: { d: InsightsData; multiTeam: boolean }) {
  const app = useApp()
  const parts = [...d.by_state].sort((a, b) => {
    const sa = app.state(a.id), sb = app.state(b.id)
    return (STATE_TYPE_ORDER[sa?.type || 'backlog'] - STATE_TYPE_ORDER[sb?.type || 'backlog']) || ((sa?.team_id || 0) - (sb?.team_id || 0)) || ((sa?.position || 0) - (sb?.position || 0))
  })
  const name = (x: Part) => { const s = app.state(x.id); const k = multiTeam && s ? app.team(s.team_id)?.key : ''; return k ? `${x.name} · ${k}` : x.name }
  const rows: HBarRow[] = parts.map(x => ({ key: x.id, name: name(x), value: x.open, detail: `open${x.points ? ` · ${compact(x.points)} pts` : ''}`, label: <><StatusIcon state={app.state(x.id)} /><span className="ellipsis">{name(x)}</span></> }))
  return (
    <ChartCard title="By state" explain="Where the open issues sit in each team's workflow right now."
      table={{ head: ['State', 'Open', 'Points'], rows: parts.map(x => [name(x), x.open, compact(x.points)]) }}>
      <HBars rows={rows} fold={12} />
    </ChartCard>
  )
}

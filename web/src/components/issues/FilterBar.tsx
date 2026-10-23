// The filter of an issue view: an "add filter" menu, one removable chip per
// condition (click to change it) and a text search.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useApp } from '../../lib/app-context'
import { STATE_TYPE_LABEL, priorityLabel } from '../../lib/issues'
import { shortDate } from '../../lib/dates'
import { STATE_TYPES, type IssueFilter, type StateType } from '../../lib/types'
import { Icon } from '../icons'
import { PersonAvatar, PriorityIcon, ProjectMark, StatusIcon } from '../issue-bits'
import { PickerMenu, type PickItem } from '../pickers'
import { type Anchor, Menu, rectOf } from '../ui'

export type FilterKey = 'status' | 'assignee' | 'creator' | 'priority' | 'labels' | 'project' | 'milestone' | 'cycle' | 'due' | 'blocked' | 'subscribed'
type V = string | number

const DUE_LABEL: Record<string, string> = { overdue: 'Overdue', today: 'Due today', week: 'Due this week', month: 'Due this month', none: 'No due date', any: 'Has a due date' }
const CYCLE_REL: Record<string, string> = { current: 'Current cycle', next: 'Next cycle', previous: 'Previous cycle' }

export const FILTER_META: { key: FilterKey; label: string; icon: string }[] = [
  { key: 'status', label: 'Status', icon: 'circle' },
  { key: 'assignee', label: 'Assignee', icon: 'user' },
  { key: 'creator', label: 'Creator', icon: 'user-plus' },
  { key: 'priority', label: 'Priority', icon: 'bar-chart' },
  { key: 'labels', label: 'Labels', icon: 'tag' },
  { key: 'project', label: 'Project', icon: 'briefcase' },
  { key: 'milestone', label: 'Milestone', icon: 'diamond' },
  { key: 'cycle', label: 'Cycle', icon: 'cycle' },
  { key: 'due', label: 'Due date', icon: 'calendar' },
  { key: 'blocked', label: 'Blocked', icon: 'alert-triangle' },
  { key: 'subscribed', label: 'Subscribed by me', icon: 'bell' },
]

/** The picker items, current values and setter of each filter, for the teams in view. */
export function useFilterModel(teamIds: number[]) {
  const app = useApp()
  const me = app.user?.id || 0
  const teams = teamIds.length ? app.teams.filter(t => teamIds.includes(t.id)) : app.teams
  const single = teams.length === 1 ? teams[0] : undefined
  const people = useMemo(() => {
    const seen = new Map<number, { id: number; name: string; email: string; color: string; username: string }>()
    for (const t of teams) for (const m of t.members || []) seen.set(m.id, m)
    if (!seen.size) for (const p of app.people) seen.set(p.id, p)
    return [...seen.values()].sort((a, b) => (a.id === me ? -1 : b.id === me ? 1 : (a.name || a.email).localeCompare(b.name || b.email)))
  }, [teams, app.people, me])
  const tids = new Set(teams.map(t => t.id))
  const projects = app.projects.filter(p => p.team_ids.some(id => tids.has(id)))
  const pids = new Set(projects.map(p => p.id))

  const items = (k: FilterKey): PickItem<V>[] => {
    switch (k) {
      case 'status':
        if (single) return (single.states || []).map(s => ({ value: s.id, label: s.name, icon: <StatusIcon state={s} />, keywords: s.type }))
        return STATE_TYPES.map(t => ({ value: t, label: STATE_TYPE_LABEL[t], icon: <StatusIcon type={t} /> }))
      case 'assignee': return [{ value: 0, label: 'Unassigned', icon: <PersonAvatar person={null} size={18} /> }, ...people.map(p => ({ value: p.id, label: (p.name || p.email) + (p.id === me ? ' (me)' : ''), icon: <PersonAvatar person={p} size={18} />, keywords: p.email + (p.id === me ? ' me' : '') }))]
      case 'creator': return people.map(p => ({ value: p.id, label: (p.name || p.email) + (p.id === me ? ' (me)' : ''), icon: <PersonAvatar person={p} size={18} />, keywords: p.email }))
      case 'priority': return [1, 2, 3, 4, 0].map(p => ({ value: p, label: priorityLabel(p), icon: <PriorityIcon priority={p} /> }))
      case 'labels': return teams.flatMap(t => (t.labels || []).filter(l => !l.is_group).map(l => ({ value: l.id, label: l.name, icon: <span className="dot" style={{ background: l.color }} />, group: teams.length > 1 ? t.name : undefined })))
      case 'project': return [{ value: 0, label: 'No project', icon: <Icon name="x" size={13} /> }, ...projects.map(p => ({ value: p.id, label: p.name, icon: <ProjectMark icon={p.icon} color={p.color} size={14} /> }))]
      case 'milestone': return (app.boot?.milestones || []).filter(m => pids.has(m.project_id)).map(m => ({ value: m.id, label: m.name, icon: <Icon name="diamond" size={12} />, group: app.project(m.project_id)?.name, hint: m.target_date ? shortDate(m.target_date) : undefined }))
      case 'cycle': return [
        ...Object.entries(CYCLE_REL).map(([v, l]) => ({ value: v, label: l, icon: <Icon name="cycle" size={13} /> })),
        { value: 0, label: 'No cycle', icon: <Icon name="x" size={13} /> },
        ...app.cycles.filter(c => tids.has(c.team_id)).sort((a, b) => b.starts_on.localeCompare(a.starts_on)).map(c => ({ value: c.id, label: c.name || `Cycle ${c.number}`, icon: <Icon name="cycle" size={13} />, hint: `${shortDate(c.starts_on)} – ${shortDate(c.ends_on)}`, group: teams.length > 1 ? app.team(c.team_id)?.name : 'Cycles' })),
      ]
      case 'due': return Object.entries(DUE_LABEL).map(([v, l]) => ({ value: v, label: l, icon: <Icon name="calendar" size={13} /> }))
      default: return []
    }
  }
  const get = (f: IssueFilter, k: FilterKey): V[] => {
    switch (k) {
      case 'status': return [...(f.state_ids || []), ...(f.state_types || [])]
      case 'assignee': return f.assignee_ids || []
      case 'creator': return f.creator_ids || []
      case 'priority': return f.priorities || []
      case 'labels': return f.label_ids || []
      case 'project': return f.project_ids || []
      case 'milestone': return f.milestone_ids || []
      case 'cycle': return [...(f.cycle ? [f.cycle] : []), ...(f.cycle_ids || [])]
      case 'due': return f.due ? [f.due] : []
      case 'blocked': return f.blocked ? [1] : []
      case 'subscribed': return f.subscribed_by ? [f.subscribed_by] : []
    }
  }
  const set = (f: IssueFilter, k: FilterKey, vals: V[]): IssueFilter => {
    const n: IssueFilter = { ...f }
    const nums = vals.filter((v): v is number => typeof v === 'number')
    const strs = vals.filter((v): v is string => typeof v === 'string')
    const put = <K extends keyof IssueFilter>(key: K, v: IssueFilter[K] | undefined) => { if (v === undefined || (Array.isArray(v) && !v.length)) delete n[key]; else n[key] = v }
    switch (k) {
      case 'status': put('state_ids', nums); put('state_types', strs as StateType[]); break
      case 'assignee': put('assignee_ids', nums); break
      case 'creator': put('creator_ids', nums); break
      case 'priority': put('priorities', nums); break
      case 'labels': put('label_ids', nums); break
      case 'project': put('project_ids', nums); break
      case 'milestone': put('milestone_ids', nums); break
      case 'cycle': put('cycle', (strs[strs.length - 1] as IssueFilter['cycle']) || undefined); put('cycle_ids', nums); break
      case 'due': put('due', (strs[strs.length - 1] as IssueFilter['due']) || undefined); break
      case 'blocked': put('blocked', vals.length ? true : undefined); break
      case 'subscribed': put('subscribed_by', vals.length ? me : undefined); break
    }
    return n
  }
  /** Readable names of a filter's values. */
  const names = (f: IssueFilter, k: FilterKey): string[] => {
    const vals = get(f, k)
    switch (k) {
      case 'status': return vals.map(v => (typeof v === 'number' ? app.state(v)?.name || '?' : STATE_TYPE_LABEL[v as StateType] || String(v)))
      case 'assignee': case 'creator': return vals.map(v => (v === 0 ? 'Unassigned' : v === me ? 'Me' : app.person(v as number)?.name || 'Someone'))
      case 'priority': return vals.map(v => priorityLabel(v as number))
      case 'labels': return vals.map(v => app.label(v as number)?.name || '?')
      case 'project': return vals.map(v => (v === 0 ? 'No project' : app.project(v as number)?.name || '?'))
      case 'milestone': return vals.map(v => app.milestone(v as number)?.name || '?')
      case 'cycle': return vals.map(v => (typeof v === 'string' ? CYCLE_REL[v] || v : v === 0 ? 'No cycle' : (() => { const c = app.cycle(v); return c ? c.name || `Cycle ${c.number}` : '?' })()))
      case 'due': return vals.map(v => DUE_LABEL[v as string] || String(v))
      case 'blocked': return vals.length ? ['Blocked'] : []
      case 'subscribed': return vals.length ? ['Me'] : []
    }
  }
  const multi = (k: FilterKey) => !['due', 'blocked', 'subscribed'].includes(k)
  return { items, get, set, names, multi, single, teams }
}

/** "Status: Todo, In progress · Assignee: Me" — for view lists. */
export function useDescribeFilter(teamIds: number[] = []) {
  const m = useFilterModel(teamIds)
  const app = useApp()
  return (f: IssueFilter): string => {
    const parts: string[] = []
    if (f.team_ids?.length) parts.push(f.team_ids.map(id => app.team(id)?.name || '?').join(', '))
    for (const meta of FILTER_META) {
      const n = m.names(f, meta.key)
      if (n.length) parts.push(meta.key === 'blocked' || meta.key === 'subscribed' ? meta.label : `${meta.label}: ${n.join(', ')}`)
    }
    if (f.q) parts.push(`“${f.q}”`)
    return parts.join(' · ')
  }
}

export function FilterBar({ filter, onChange, teamIds, extra }: { filter: IssueFilter; onChange: (f: IssueFilter) => void; teamIds: number[]; extra?: React.ReactNode }) {
  const m = useFilterModel(teamIds)
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [pick, setPick] = useState<{ key: FilterKey; at: Anchor } | null>(null)
  const active = FILTER_META.filter(f => m.get(filter, f.key).length)
  const open = (key: FilterKey, at: Anchor) => {
    if (key === 'blocked' || key === 'subscribed') { onChange(m.set(filter, key, m.get(filter, key).length ? [] : [1])); return }
    setPick({ key, at })
  }
  const toggle = (key: FilterKey, v: V) => {
    const cur = m.get(filter, key)
    if (!m.multi(key)) { onChange(m.set(filter, key, cur.includes(v) ? [] : [v])); return }
    // cycle: one relative choice at a time
    let next = cur.includes(v) ? cur.filter(x => x !== v) : [...cur, v]
    if (key === 'cycle' && typeof v === 'string' && !cur.includes(v)) next = next.filter(x => typeof x === 'number' || x === v)
    onChange(m.set(filter, key, next))
  }
  return (
    <>
      <button type="button" className={`btn ghost sm filter-btn ${active.length ? 'has' : ''}`} onClick={e => setMenu(rectOf(e.currentTarget))} aria-haspopup="menu" title="Add a filter (F)" data-filter-btn>
        <Icon name="filter" size={14} />{active.length ? null : 'Filter'}
      </button>
      {active.map(f => {
        const names = m.names(filter, f.key)
        const solo = f.key === 'blocked' || f.key === 'subscribed'
        return (
          <span key={f.key} className="filter-chip">
            <button type="button" className="fc-part fc-key" onClick={e => !solo && open(f.key, rectOf(e.currentTarget.parentElement!))}><Icon name={f.icon} size={13} />{f.label}</button>
            {!solo && <>
              <span className="fc-part fc-op">{m.multi(f.key) && names.length > 1 ? 'is any of' : 'is'}</span>
              <button type="button" className="fc-part fc-val" onClick={e => open(f.key, rectOf(e.currentTarget.parentElement!))} title={names.join(', ')}>
                {names.length > 2 ? `${names[0]}, ${names[1]} +${names.length - 2}` : names.join(', ')}
              </button>
            </>}
            <button type="button" className="fc-part fc-x" aria-label={`Remove the ${f.label} filter`} onClick={() => onChange(m.set(filter, f.key, []))}><Icon name="x" size={12} /></button>
          </span>
        )
      })}
      {active.length > 1 && <button type="button" className="btn ghost xs clear-filters" onClick={() => onChange(filter.q ? { q: filter.q } : {})} title="Clear the filters">Clear</button>}
      {extra}
      {menu && <Menu anchor={menu} width={220} onClose={() => setMenu(null)} items={[
        { heading: 'Filter by' },
        ...FILTER_META.filter(f => f.key !== 'milestone' || (m.items('milestone').length > 0)).filter(f => f.key !== 'cycle' || m.teams.some(t => t.cycles_enabled))
          .map(f => ({ label: f.label, icon: f.icon, checked: f.key === 'blocked' || f.key === 'subscribed' ? m.get(filter, f.key).length > 0 : undefined, onSelect: () => open(f.key, menu) })),
      ]} />}
      {pick && <PickerMenu<V> anchor={pick.at} items={m.items(pick.key)} multi={m.multi(pick.key)} selected={m.get(filter, pick.key)}
        placeholder={`${FILTER_META.find(f => f.key === pick.key)?.label}…`} onPick={v => toggle(pick.key, v)} onClose={() => setPick(null)} />}
    </>
  )
}

/** The search box of the view bar (debounced). */
export function SearchBox({ value, onChange }: { value: string; onChange: (q: string) => void }) {
  const [v, setV] = useState(value)
  const [open, setOpen] = useState(!!value)
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => { setV(value) }, [value])
  useEffect(() => {
    if (v === value) return
    const t = window.setTimeout(() => onChange(v.trim()), 300)
    return () => window.clearTimeout(t)
  }, [v]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!open) return <button type="button" className="btn ghost icon-only sm" aria-label="Search these issues" title="Search these issues" onClick={() => { setOpen(true); requestAnimationFrame(() => ref.current?.focus()) }}><Icon name="search" size={15} /></button>
  return (
    <label className="view-search">
      <Icon name="search" size={14} />
      <input ref={ref} value={v} onChange={e => setV(e.target.value)} placeholder="Search…" aria-label="Search these issues"
        onKeyDown={e => { if (e.key === 'Escape') { e.preventDefault(); setV(''); onChange(''); setOpen(false); (e.target as HTMLInputElement).blur() } }}
        onBlur={() => { if (!v) setOpen(false) }} />
      {v && <button type="button" className="vs-x" aria-label="Clear the search" onMouseDown={e => e.preventDefault()} onClick={() => { setV(''); onChange('') }}><Icon name="x" size={12} /></button>}
    </label>
  )
}


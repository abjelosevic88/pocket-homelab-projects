// Small pieces every issue view shows: the status circle, priority bars,
// label pills, people, due dates, project marks and health.
import { Link } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { dueState, isDone, priorityLabel } from '../lib/issues'
import { shortDate } from '../lib/dates'
import type { Health, Person, ProjectStatus, State, StateType } from '../lib/types'
import { Icon } from './icons'

const TYPE_COLOR: Record<StateType, string> = { triage: '#f97316', backlog: '#94a3b8', unstarted: '#8b8b96', started: '#f59e0b', completed: '#5e6ad2', canceled: '#94a3b8' }

/** Linear-style status mark: dashed (backlog), empty (todo), a pie (started), check (done), cross (canceled). */
export function StatusIcon({ state, type, size = 14, progress }: { state?: State; type?: StateType; size?: number; progress?: number }) {
  const t = state?.type || type || 'unstarted'
  const c = state?.color || TYPE_COLOR[t]
  const r = 6
  switch (t) {
    case 'triage':
      return <svg className="status-ico" width={size} height={size} viewBox="0 0 16 16" aria-hidden><circle cx="8" cy="8" r="7" fill={c} /><path d="M5 6.5h6l-1.6-1.6M11 9.5H5l1.6 1.6" fill="none" stroke="#fff" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" /></svg>
    case 'backlog':
      return <svg className="status-ico" width={size} height={size} viewBox="0 0 16 16" aria-hidden><circle cx="8" cy="8" r={r} fill="none" stroke={c} strokeWidth="1.8" strokeDasharray="2.2 2.1" /></svg>
    case 'unstarted':
      return <svg className="status-ico" width={size} height={size} viewBox="0 0 16 16" aria-hidden><circle cx="8" cy="8" r={r} fill="none" stroke={c} strokeWidth="1.8" /></svg>
    case 'started': {
      const p = Math.min(.9, Math.max(.25, progress ?? (state ? .5 : .5)))
      const a = 2 * Math.PI * p
      const x = 8 + 3.6 * Math.sin(a), y = 8 - 3.6 * Math.cos(a)
      return <svg className="status-ico" width={size} height={size} viewBox="0 0 16 16" aria-hidden><circle cx="8" cy="8" r={r} fill="none" stroke={c} strokeWidth="1.8" /><path d={`M8 8V4.4A3.6 3.6 0 ${p > .5 ? 1 : 0} 1 ${x.toFixed(2)} ${y.toFixed(2)}Z`} fill={c} /></svg>
    }
    case 'completed':
      return <svg className="status-ico" width={size} height={size} viewBox="0 0 16 16" aria-hidden><circle cx="8" cy="8" r="7" fill={c} /><path d="M5 8.2l2 2 4-4.2" fill="none" stroke="#fff" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
    default:
      return <svg className="status-ico" width={size} height={size} viewBox="0 0 16 16" aria-hidden><circle cx="8" cy="8" r="7" fill={c} /><path d="M5.8 5.8l4.4 4.4M10.2 5.8l-4.4 4.4" stroke="#fff" strokeWidth="1.6" strokeLinecap="round" /></svg>
  }
}

/** How far along a started state is in its team's workflow (for the pie). */
export function useStateProgress() {
  const { team } = useApp()
  return (s?: State) => {
    if (!s || s.type !== 'started') return undefined
    const started = (team(s.team_id)?.states || []).filter(x => x.type === 'started')
    const i = started.findIndex(x => x.id === s.id)
    return started.length ? (i + 1) / (started.length + 1) : .5
  }
}

export function PriorityIcon({ priority, size = 14 }: { priority: number; size?: number }) {
  if (priority === 1) return <svg className="prio-ico urgent" width={size} height={size} viewBox="0 0 16 16" aria-label="Urgent"><rect x="1.5" y="1.5" width="13" height="13" rx="3.5" fill="#f97316" /><path d="M8 4.5v4.2M8 11.2v.3" stroke="#fff" strokeWidth="1.9" strokeLinecap="round" /></svg>
  if (!priority) return <svg className="prio-ico none" width={size} height={size} viewBox="0 0 16 16" aria-label="No priority"><path d="M3 8h2M7 8h2M11 8h2" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg>
  const on = priority === 2 ? 3 : priority === 3 ? 2 : 1
  return (
    <svg className="prio-ico" width={size} height={size} viewBox="0 0 16 16" aria-label={priorityLabel(priority)}>
      {[0, 1, 2].map(i => <rect key={i} x={2.5 + i * 4.2} y={10 - i * 3.5} width="2.8" height={4 + i * 3.5} rx=".9" fill="currentColor" opacity={i < on ? 1 : .25} />)}
    </svg>
  )
}

export function LabelPill({ name, color, onRemove }: { name: string; color?: string; onRemove?: () => void }) {
  return (
    <span className="label-pill" style={{ ['--lc' as string]: color || '#94a3b8' }}>
      <span className="label-dot" />{name}
      {onRemove && <button type="button" aria-label={`Remove ${name}`} onClick={e => { e.stopPropagation(); onRemove() }}><Icon name="x" size={11} /></button>}
    </span>
  )
}

export function PersonAvatar({ person, size = 20, title }: { person?: Person | null; size?: number; title?: string }) {
  if (!person) return <span className="avatar-empty" style={{ width: size, height: size }} title={title || 'Unassigned'}><Icon name="user" size={Math.round(size * .62)} /></span>
  const label = (person.name || person.email || '?').trim()
  const initials = label.split(/[\s@.]+/).filter(Boolean).slice(0, 2).map(s => s[0]!.toUpperCase()).join('')
  return <span className="avatar" title={title || person.name || person.email} style={{ width: size, height: size, fontSize: size * .42, background: person.color || '#5e6ad2' }}>{initials}</span>
}

export function personName(p?: Person | null): string { return p ? p.name || p.email.split('@')[0] : 'Unassigned' }

export function DueChip({ due, done, compact }: { due: string; done?: boolean; compact?: boolean }) {
  const { boot } = useApp()
  if (!due) return null
  const st = dueState(due, boot?.today || new Date().toISOString().slice(0, 10), !!done)
  return <span className={`due-chip ${st}`} title={`Due ${due}`}><Icon name="calendar" size={12} />{compact && st === 'today' ? 'Today' : shortDate(due)}</span>
}

export function ProjectMark({ icon, color, size = 15 }: { icon?: string; color?: string; size?: number }) {
  if (icon) return <span className="project-mark emoji" style={{ fontSize: size * .9, width: size, height: size }}>{icon}</span>
  return <svg className="project-mark" width={size} height={size} viewBox="0 0 16 16" aria-hidden><rect x="2" y="2" width="12" height="12" rx="3.5" fill={color || '#5e6ad2'} fillOpacity=".2" stroke={color || '#5e6ad2'} strokeWidth="1.6" /></svg>
}

export const PROJECT_STATUS_LABEL: Record<ProjectStatus, string> = { backlog: 'Backlog', planned: 'Planned', started: 'In progress', paused: 'Paused', completed: 'Completed', canceled: 'Canceled' }
const PROJECT_STATUS_COLOR: Record<ProjectStatus, string> = { backlog: '#94a3b8', planned: '#8b8b96', started: '#f59e0b', paused: '#a855f7', completed: '#5e6ad2', canceled: '#94a3b8' }

export function ProjectStatusIcon({ status, size = 14 }: { status: ProjectStatus; size?: number }) {
  const map: Record<ProjectStatus, StateType> = { backlog: 'backlog', planned: 'unstarted', started: 'started', paused: 'started', completed: 'completed', canceled: 'canceled' }
  if (status === 'paused') return <svg className="status-ico" width={size} height={size} viewBox="0 0 16 16" aria-hidden><circle cx="8" cy="8" r="6" fill="none" stroke={PROJECT_STATUS_COLOR.paused} strokeWidth="1.8" /><path d="M6.6 5.8v4.4M9.4 5.8v4.4" stroke={PROJECT_STATUS_COLOR.paused} strokeWidth="1.6" strokeLinecap="round" /></svg>
  return <StatusIcon type={map[status]} size={size} state={{ id: 0, team_id: 0, name: '', type: map[status], color: PROJECT_STATUS_COLOR[status], description: '', position: 0 }} />
}

export const HEALTH_LABEL: Record<string, string> = { on_track: 'On track', at_risk: 'At risk', off_track: 'Off track', '': 'No update' }
export function HealthBadge({ health }: { health: Health }) {
  return <span className={`health ${health || 'none'}`}><span className="health-dot" />{HEALTH_LABEL[health || '']}</span>
}

/** A thin progress ring (projects, cycles, milestones). */
export function ProgressRing({ percent, size = 16, color = 'var(--accent)' }: { percent: number; size?: number; color?: string }) {
  const r = 6, c = 2 * Math.PI * r
  return (
    <svg className="progress-ring" width={size} height={size} viewBox="0 0 16 16" aria-label={`${Math.round(percent)}%`}>
      <circle cx="8" cy="8" r={r} fill="none" stroke="var(--surface-3)" strokeWidth="2.2" />
      <circle cx="8" cy="8" r={r} fill="none" stroke={color} strokeWidth="2.2" strokeDasharray={`${(c * Math.min(100, percent)) / 100} ${c}`} transform="rotate(-90 8 8)" strokeLinecap="round" />
    </svg>
  )
}

export function ProgressBar({ percent, color }: { percent: number; color?: string }) {
  return <span className="progress-bar"><span style={{ width: `${Math.min(100, Math.max(0, percent))}%`, background: color }} /></span>
}

/** "WEB-12" linking to the issue. */
export function IssueLink({ identifier, title, done }: { identifier: string; title?: string; done?: boolean }) {
  return <Link to={`/issue/${identifier}`} className={`issue-link ${done ? 'done' : ''}`}><span className="ident">{identifier}</span>{title && <span className="ellipsis">{title}</span>}</Link>
}

export { isDone }

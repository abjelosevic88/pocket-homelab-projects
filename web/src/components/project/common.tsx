// Shared pieces of the project pages: constants, favorites, pickers for a
// project's properties, the icon picker, avatar stacks and date chips.
import React, { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { diffDays, shortDate } from '../../lib/dates'
import { PRIORITIES } from '../../lib/issues'
import type { Person, Project, ProjectStatus } from '../../lib/types'
import { PROJECT_STATUSES } from '../../lib/types'
import { Icon } from '../icons'
import { PersonAvatar, PriorityIcon, ProjectMark, ProjectStatusIcon, PROJECT_STATUS_LABEL } from '../issue-bits'
import { PickerMenu, type PickItem, TeamMark } from '../pickers'
import { type Anchor, Popover, useToast } from '../ui'

export const PROJECT_EMOJIS = ['🚀', '🎯', '📦', '🛠️', '🏠', '🍳', '🌱', '📱', '💻', '🎨', '📚', '✈️', '🏗️', '🧪', '📈', '💡', '🔒', '🎉', '🧰', '🚗', '🏡', '🛒', '💰', '🧹', '🎵', '📷', '🐶', '⚡', '🌍', '🔧', '❤️', '⭐']
export const PROJECT_COLORS = ['#5e6ad2', '#8b5cf6', '#ec4899', '#ef4444', '#f97316', '#f59e0b', '#10b981', '#14b8a6', '#0ea5e9', '#64748b']
export const CADENCES: { value: number; label: string }[] = [
  { value: 0, label: 'No reminders' }, { value: 7, label: 'Every week' }, { value: 14, label: 'Every 2 weeks' }, { value: 30, label: 'Every month' },
]
export const cadenceLabel = (d: number) => CADENCES.find(c => c.value === d)?.label || (d ? `Every ${d} days` : 'No reminders')
export const STATUS_GROUP_ORDER: ProjectStatus[] = ['started', 'planned', 'paused', 'backlog', 'completed', 'canceled']

export const statusItems = (): PickItem<ProjectStatus>[] => PROJECT_STATUSES.map(s => ({ value: s, label: PROJECT_STATUS_LABEL[s], icon: <ProjectStatusIcon status={s} /> }))
export const priorityItems = (): PickItem<number>[] => [1, 2, 3, 4, 0].map(p => ({ value: p, label: PRIORITIES.find(x => x.value === p)!.label, icon: <PriorityIcon priority={p} /> }))

/** People on any of the teams (the only ones who can lead or join the project). */
export function useTeamPeople(teamIds: number[]): Person[] {
  const app = useApp()
  const seen = new Map<number, Person>()
  for (const id of teamIds) for (const m of app.team(id)?.members || []) if (!seen.has(m.id)) seen.set(m.id, m)
  const me = app.user?.id
  return [...seen.values()].sort((a, b) => (a.id === me ? -1 : b.id === me ? 1 : (a.name || a.email).localeCompare(b.name || b.email)))
}

export function peopleItems(people: Person[], meId: number | undefined, none?: string): PickItem<number>[] {
  return [...(none ? [{ value: 0, label: none, icon: <PersonAvatar person={null} size={18} /> }] : []),
    ...people.map(p => ({ value: p.id, label: (p.name || p.email) + (p.id === meId ? ' (you)' : ''), icon: <PersonAvatar person={p} size={18} />, keywords: p.email + ' ' + p.username }))]
}

/** Favorites: is the project pinned, and a toggle. */
export function useFavorite(kind: 'project' | 'doc', id: number) {
  const app = useApp()
  const { toast } = useToast()
  const fav = app.boot?.favorites.find(f => f.kind === kind && f.ref_id === id)
  const toggle = async () => {
    try {
      if (fav) await api.del(`${V1}/favorites/${fav.id}`)
      else await api.post(`${V1}/favorites`, { kind, ref_id: id })
      await app.reload()
      toast(fav ? 'Removed from favorites' : 'Added to favorites')
    } catch (e) { toast((e as Error).message, 'err') }
  }
  return { on: !!fav, toggle }
}

export function FavoriteStar({ kind = 'project', id, className = 'btn ghost icon-only sm' }: { kind?: 'project' | 'doc'; id: number; className?: string }) {
  const f = useFavorite(kind, id)
  return (
    <button type="button" className={`${className} fav-star ${f.on ? 'on' : ''}`} aria-pressed={f.on} aria-label={f.on ? 'Remove from favorites' : 'Add to favorites'} title={f.on ? 'Remove from favorites' : 'Add to favorites'}
      onClick={e => { e.stopPropagation(); e.preventDefault(); f.toggle() }}>
      <Icon name="star" size={15} fill={f.on} />
    </button>
  )
}

/** Overlapping avatars, "+3" after max. */
export function AvatarStack({ ids, max = 4, size = 20 }: { ids: number[]; max?: number; size?: number }) {
  const { person } = useApp()
  const list = ids.map(id => person(id)).filter(Boolean) as Person[]
  if (!list.length) return null
  return (
    <span className="avatar-stack" title={list.map(p => p.name || p.email).join(', ')}>
      {list.slice(0, max).map(p => <PersonAvatar key={p.id} person={p} size={size} />)}
      {list.length > max && <span className="avatar-more" style={{ height: size, minWidth: size }}>+{list.length - max}</span>}
    </span>
  )
}

export function TeamMarks({ ids, size = 16 }: { ids: number[]; size?: number }) {
  const { team } = useApp()
  return <span className="team-marks">{ids.map(id => { const t = team(id); return t ? <span key={id} title={t.name}><TeamMark team={t} size={size} /></span> : null })}</span>
}

export function isClosed(p: Pick<Project, 'status'>) { return p.status === 'completed' || p.status === 'canceled' }

/** "21 Nov" — red when it has passed and the project is still open. */
export function TargetChip({ project, today, compact }: { project: Project; today: string; compact?: boolean }) {
  if (!project.target_date) return compact ? null : <span className="target-chip none">No target</span>
  const late = !isClosed(project) && project.target_date < today
  const soon = !late && !isClosed(project) && diffDays(project.target_date, today) <= 7
  return <span className={`target-chip ${late ? 'late' : soon ? 'soon' : ''}`} title={`Target ${project.target_date}${late ? ' (passed)' : ''}`}><Icon name="target" size={12} />{shortDate(project.target_date)}</span>
}

/** The project's icon + colour chooser. */
export function IconPicker({ anchor, icon, color, onChange, onClose }: { anchor: Anchor; icon: string; color: string; onChange: (v: { icon?: string; color?: string }) => void; onClose: () => void }) {
  return (
    <Popover anchor={anchor} onClose={onClose} prefer="below" width={300} label="Project icon" className="icon-pop">
      <div className="icon-pop-body">
        <div className="section-title">Icon</div>
        <div className="emoji-grid">
          <button type="button" className={`emoji-btn ${!icon ? 'on' : ''}`} title="No emoji, use the colour" onClick={() => onChange({ icon: '' })}><ProjectMark color={color} size={18} /></button>
          {PROJECT_EMOJIS.map(e => <button key={e} type="button" className={`emoji-btn ${icon === e ? 'on' : ''}`} onClick={() => { onChange({ icon: e }); }}>{e}</button>)}
        </div>
        <div className="section-title mt-s">Colour</div>
        <div className="palette">
          {PROJECT_COLORS.map(c => <button key={c} type="button" className={`swatch ${color === c ? 'active' : ''}`} style={{ background: c }} aria-label={c} onClick={() => onChange({ color: c })} />)}
        </div>
      </div>
    </Popover>
  )
}

/** A chip button that opens a picker (status, priority, lead…). */
export function PropChip({ icon, label, onOpen, title, disabled, className = '' }: { icon?: React.ReactNode; label: React.ReactNode; onOpen: (a: Anchor) => void; title?: string; disabled?: boolean; className?: string }) {
  return (
    <button type="button" className={`chip prop-chip ${className}`} title={title} disabled={disabled}
      onClick={e => { e.stopPropagation(); const r = e.currentTarget.getBoundingClientRect(); onOpen({ x: r.left, y: r.top, w: r.width, h: r.height }) }}>
      {icon}<span className="ellipsis">{label}</span>
    </button>
  )
}

/** Patch a project and report errors; returns the fresh project or null. */
export function useProjectPatch() {
  const app = useApp()
  const { toast } = useToast()
  return async (id: number, body: Record<string, unknown>): Promise<Project | null> => {
    try {
      const p = await api.patch<Project>(`${V1}/projects/${id}`, body)
      app.reload().catch(() => {})
      return p
    } catch (e) { toast((e as Error).message, 'err'); return null }
  }
}

/** Status / priority / lead pickers for a project. */
export function ProjectPicker({ kind, project, anchor, onPatch, onClose }: { kind: 'status' | 'priority' | 'lead'; project: Project; anchor: Anchor; onPatch: (b: Record<string, unknown>) => void; onClose: () => void }) {
  const app = useApp()
  const people = useTeamPeople(project.team_ids)
  if (kind === 'status') return <PickerMenu anchor={anchor} items={statusItems()} selected={project.status} placeholder="Change status…" onPick={v => onPatch({ status: v })} onClose={onClose} />
  if (kind === 'priority') return <PickerMenu anchor={anchor} items={priorityItems()} selected={project.priority} placeholder="Set priority…" onPick={v => onPatch({ priority: v })} onClose={onClose} />
  return <PickerMenu anchor={anchor} items={peopleItems(people, app.user?.id, 'No lead')} selected={project.lead_id || 0} placeholder="Choose the lead…" onPick={v => onPatch({ lead_id: v })} onClose={onClose} />
}

export function usePickerState<K extends string>() {
  const [open, setOpen] = useState<{ kind: K; anchor: Anchor } | null>(null)
  return { open, show: (kind: K, anchor: Anchor) => setOpen({ kind, anchor }), close: () => setOpen(null) }
}

/** Trash a project with an Undo toast. */
export function useDeleteProject() {
  const app = useApp()
  const { toast } = useToast()
  return async (p: Project, after?: () => void) => {
    try {
      await api.del(`${V1}/projects/${p.id}`)
      await app.reload()
      after?.()
      toast(<>Moved “{p.name}” to the trash</>, 'ok', { label: 'Undo', run: async () => { await api.post(`${V1}/projects/${p.id}/restore`); await app.reload() } })
    } catch (e) { toast((e as Error).message, 'err') }
  }
}

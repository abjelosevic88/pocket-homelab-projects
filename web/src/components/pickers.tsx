// Property pickers: one keyboard-friendly searchable menu (single or multi
// choice) and the pickers built on it for every issue property.
import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useApp } from '../lib/app-context'
import { addDays, nextWeek, today, tomorrow, shortDate } from '../lib/dates'
import { estimateOptions, PRIORITIES } from '../lib/issues'
import type { Issue, Team } from '../lib/types'
import { Icon } from './icons'
import { LabelPill, PersonAvatar, PriorityIcon, ProjectMark, StatusIcon, useStateProgress } from './issue-bits'
import { type Anchor, rectOf, useDismiss, useIsMobile } from './ui'

export interface PickItem<T> { value: T; label: string; icon?: React.ReactNode; hint?: React.ReactNode; group?: string; keywords?: string }

export function PickerMenu<T extends string | number>({ anchor, items, selected, multi, onPick, onClose, placeholder = 'Search…', width = 260, onCreate, createLabel, footer }: {
  anchor: Anchor; items: PickItem<T>[]; selected?: T | T[] | null; multi?: boolean; onPick: (v: T) => void; onClose: () => void
  placeholder?: string; width?: number; onCreate?: (text: string) => void; createLabel?: (text: string) => string; footer?: React.ReactNode
}) {
  const ref = useRef<HTMLDivElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const [q, setQ] = useState('')
  const [hi, setHi] = useState(0)
  const [pos, setPos] = useState<{ left: number; top: number; maxH: number } | null>(null)
  const mobile = useIsMobile()
  useDismiss(ref, onClose)
  const sel = useMemo(() => new Set(Array.isArray(selected) ? selected : selected === undefined || selected === null ? [] : [selected]), [selected])
  const shown = useMemo(() => {
    const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean)
    return items.filter(it => words.every(w => (it.label + ' ' + (it.keywords || '')).toLowerCase().includes(w)))
  }, [items, q])
  const canCreate = !!onCreate && q.trim() !== '' && !items.some(it => it.label.toLowerCase() === q.trim().toLowerCase())
  const total = shown.length + (canCreate ? 1 : 0)
  useEffect(() => { setHi(0) }, [q])
  useLayoutEffect(() => {
    if (mobile) return
    const h = ref.current?.offsetHeight || 300
    const vw = window.innerWidth, vh = window.innerHeight
    let left = Math.min(anchor.x, vw - width - 8)
    let top = anchor.y + anchor.h + 4
    let maxH = Math.min(420, vh - top - 10)
    if (maxH < 200 && anchor.y > vh / 2) { maxH = Math.min(420, anchor.y - 14); top = Math.max(8, anchor.y - 4 - Math.min(h, maxH)) }
    setPos({ left: Math.max(8, left), top, maxH })
  }, [anchor, width, mobile, shown.length])
  useEffect(() => { list.current?.querySelector('.pick-item.hi')?.scrollIntoView({ block: 'nearest' }) }, [hi])
  const choose = (i: number) => {
    if (i < shown.length) { onPick(shown[i].value); if (!multi) onClose() }
    else if (canCreate) { onCreate!(q.trim()); setQ('') }
  }
  const key = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(total - 1, h + 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
    else if (e.key === 'Enter') { e.preventDefault(); choose(hi) }
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
    else if (!q && /^[0-9]$/.test(e.key) && items.length <= 10) { const i = Number(e.key) - 1; if (i >= 0 && i < shown.length) { e.preventDefault(); choose(i) } }
  }
  let lastGroup: string | undefined
  const body = (
    <div ref={ref} className={`pick-menu ${mobile ? 'as-sheet' : ''}`} style={mobile ? undefined : { left: pos?.left ?? -9999, top: pos?.top ?? -9999, width }} onKeyDown={key} role="dialog">
      {mobile && <div className="sheet-grip" />}
      <div className="pick-search"><Icon name="search" size={14} /><input autoFocus={!mobile} value={q} onChange={e => setQ(e.target.value)} placeholder={placeholder} aria-label={placeholder} /></div>
      <div className="pick-list" ref={list} role="listbox" aria-multiselectable={multi} style={mobile ? undefined : { maxHeight: (pos?.maxH ?? 380) - 46 }}>
        {shown.map((it, i) => {
          const head = it.group && it.group !== lastGroup ? it.group : null
          lastGroup = it.group
          return (
            <React.Fragment key={String(it.value)}>
              {head && <div className="pick-group">{head}</div>}
              <div role="option" aria-selected={sel.has(it.value)} className={`pick-item ${i === hi ? 'hi' : ''} ${sel.has(it.value) ? 'on' : ''}`}
                onMouseEnter={() => setHi(i)} onMouseDown={e => e.preventDefault()} onClick={() => choose(i)}>
                {multi && <span className={`pick-check ${sel.has(it.value) ? 'on' : ''}`}>{sel.has(it.value) && <Icon name="check" size={11} strokeWidth={3} />}</span>}
                {it.icon && <span className="pick-ico">{it.icon}</span>}
                <span className="pick-label ellipsis">{it.label}</span>
                {it.hint && <span className="pick-hint">{it.hint}</span>}
                {!multi && sel.has(it.value) && <Icon name="check" size={14} className="pick-tick" />}
                {!multi && items.length <= 10 && !q && <span className="pick-num">{i + 1}</span>}
              </div>
            </React.Fragment>
          )
        })}
        {canCreate && <div role="option" aria-selected={false} className={`pick-item create ${hi === shown.length ? 'hi' : ''}`} onMouseEnter={() => setHi(shown.length)} onMouseDown={e => e.preventDefault()} onClick={() => choose(shown.length)}>
          <span className="pick-ico"><Icon name="plus" size={14} /></span><span className="pick-label">{createLabel ? createLabel(q.trim()) : `Create “${q.trim()}”`}</span>
        </div>}
        {!total && <div className="pick-empty">Nothing matches</div>}
      </div>
      {footer}
    </div>
  )
  return createPortal(mobile ? <div className="sheet-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>{body}</div> : body, document.body)
}

/** A trigger + picker pair: manages the anchor. */
export function usePicker() {
  const [anchor, setAnchor] = useState<Anchor | null>(null)
  return { anchor, open: (el: Element) => setAnchor(rectOf(el)), openAt: (a: Anchor) => setAnchor(a), close: () => setAnchor(null) }
}

// ---------- item builders ----------

export function useItems() {
  const app = useApp()
  const progress = useStateProgress()
  return {
    states: (teamId: number): PickItem<number>[] => (app.team(teamId)?.states || []).map(s => ({ value: s.id, label: s.name, icon: <StatusIcon state={s} progress={progress(s)} />, keywords: s.type })),
    priorities: (): PickItem<number>[] => [1, 2, 3, 4, 0].map(p => ({ value: p, label: PRIORITIES.find(x => x.value === p)!.label, icon: <PriorityIcon priority={p} /> })),
    people: (teamId?: number | null, withNone = true): PickItem<number>[] => {
      const t = app.team(teamId)
      const list = t?.members || app.people
      const me = app.user?.id
      const sorted = [...list].sort((a, b) => (a.id === me ? -1 : b.id === me ? 1 : (a.name || a.email).localeCompare(b.name || b.email)))
      return [...(withNone ? [{ value: 0, label: 'No assignee', icon: <PersonAvatar person={null} size={18} /> }] : []),
        ...sorted.map(p => ({ value: p.id, label: (p.name || p.email) + (p.id === me ? ' (you)' : ''), icon: <PersonAvatar person={p} size={18} />, keywords: p.email + ' ' + p.username }))]
    },
    labels: (teamId: number): PickItem<number>[] => {
      const all = app.team(teamId)?.labels || []
      return all.filter(l => !l.is_group).map(l => {
        const g = l.parent_id ? all.find(x => x.id === l.parent_id) : undefined
        return { value: l.id, label: l.name, icon: <span className="dot" style={{ background: l.color }} />, group: g?.name, keywords: g?.name }
      }).sort((a, b) => (a.group || '').localeCompare(b.group || ''))
    },
    projects: (teamId?: number | null, withNone = true): PickItem<number>[] => [
      ...(withNone ? [{ value: 0, label: 'No project', icon: <Icon name="x" size={13} /> }] : []),
      ...app.projects.filter(p => !teamId || p.team_ids.includes(teamId) || app.team(teamId)).filter(p => p.status !== 'canceled')
        .sort((a, b) => Number(b.team_ids.includes(teamId || 0)) - Number(a.team_ids.includes(teamId || 0)) || a.name.localeCompare(b.name))
        .map(p => ({ value: p.id, label: p.name, icon: <ProjectMark icon={p.icon} color={p.color} size={14} />, hint: p.status === 'completed' ? 'done' : undefined })),
    ],
    cycles: (teamId: number, withNone = true): PickItem<number>[] => [
      ...(withNone ? [{ value: 0, label: 'No cycle', icon: <Icon name="x" size={13} /> }] : []),
      ...app.cycles.filter(c => c.team_id === teamId && c.status !== 'past').map(c => ({ value: c.id, label: c.name || `Cycle ${c.number}`, icon: <Icon name="cycle" size={14} />,
        hint: `${shortDate(c.starts_on)} – ${shortDate(c.ends_on)}${c.status === 'current' ? ' · current' : ''}` })),
    ],
    milestones: (projectId: number | null, withNone = true): PickItem<number>[] => [
      ...(withNone ? [{ value: 0, label: 'No milestone', icon: <Icon name="x" size={13} /> }] : []),
      ...(app.boot?.milestones || []).filter(m => m.project_id === projectId).map(m => ({ value: m.id, label: m.name, icon: <Icon name="diamond" size={13} />, hint: m.target_date ? shortDate(m.target_date) : undefined })),
    ],
    estimates: (team?: Team): PickItem<number>[] => [{ value: -1, label: 'No estimate', icon: <Icon name="x" size={13} /> }, ...estimateOptions(team?.estimate_scale || 'linear').map(o => ({ value: o.value, label: o.label, icon: <Icon name="triangle" size={13} /> }))],
    teams: (writable = true): PickItem<number>[] => app.teams.filter(t => !t.archived_at && (!writable || t.role !== 'guest')).map(t => ({ value: t.id, label: t.name, icon: <TeamMark team={t} />, hint: t.key })),
  }
}

export function TeamMark({ team, size = 16 }: { team?: Team; size?: number }) {
  if (!team) return null
  return <span className="team-mark" style={{ width: size, height: size, background: team.color, fontSize: size * .62 }}>{team.icon || team.key.slice(0, 1)}</span>
}

// ---------- date ----------

export function DateMenu({ anchor, value, onPick, onClose, title = 'Due date' }: { anchor: Anchor; value: string; onPick: (v: string) => void; onClose: () => void; title?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  useDismiss(ref, onClose)
  const mobile = useIsMobile()
  const [v, setV] = useState(value)
  const quick: [string, string][] = [['Today', today()], ['Tomorrow', tomorrow()], ['In 3 days', addDays(today(), 3)], ['Next week', nextWeek()], ['In 2 weeks', addDays(today(), 14)], ['In a month', addDays(today(), 30)]]
  const style = mobile ? undefined : { left: Math.max(8, Math.min(anchor.x, window.innerWidth - 268)), top: Math.min(anchor.y + anchor.h + 4, window.innerHeight - 330), width: 260 }
  const body = (
    <div ref={ref} className={`pick-menu date-menu ${mobile ? 'as-sheet' : ''}`} style={style} onKeyDown={e => { if (e.key === 'Escape') { e.stopPropagation(); onClose() } }}>
      {mobile && <div className="sheet-grip" />}
      <div className="pick-group">{title}</div>
      <div className="date-quick">{quick.map(([l, d]) => <button key={l} type="button" onClick={() => { onPick(d); onClose() }}><span>{l}</span><small>{shortDate(d)}</small></button>)}</div>
      <div className="row" style={{ padding: '8px 10px 10px' }}>
        <input type="date" value={v} onChange={e => setV(e.target.value)} onKeyDown={e => { if (e.key === 'Enter' && v) { onPick(v); onClose() } }} />
        <button type="button" className="btn sm primary" disabled={!v} onClick={() => { onPick(v); onClose() }}>Set</button>
      </div>
      {value && <button type="button" className="pick-item clear" onClick={() => { onPick(''); onClose() }}><Icon name="x" size={13} />Remove date</button>}
    </div>
  )
  return createPortal(mobile ? <div className="sheet-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>{body}</div> : body, document.body)
}

// ---------- the property chip + picker used by forms and the issue sidebar ----------

export type Prop = 'state' | 'priority' | 'assignee' | 'labels' | 'project' | 'milestone' | 'cycle' | 'estimate' | 'due'

/** Opens the picker for one property of an issue (or a draft) and reports the patch. */
export function PropertyPicker({ prop, issue, anchor, onPatch, onClose }: {
  prop: Prop; issue: Pick<Issue, 'team_id' | 'state_id' | 'priority' | 'assignee_id' | 'label_ids' | 'project_id' | 'milestone_id' | 'cycle_id' | 'estimate' | 'due_date'>
  anchor: Anchor; onPatch: (patch: Record<string, unknown>) => void; onClose: () => void
}) {
  const app = useApp()
  const items = useItems()
  const team = app.team(issue.team_id)
  switch (prop) {
    case 'state': return <PickerMenu anchor={anchor} items={items.states(issue.team_id)} selected={issue.state_id} placeholder="Change status…" onPick={v => onPatch({ state_id: v })} onClose={onClose} />
    case 'priority': return <PickerMenu anchor={anchor} items={items.priorities()} selected={issue.priority} placeholder="Set priority…" onPick={v => onPatch({ priority: v })} onClose={onClose} />
    case 'assignee': return <PickerMenu anchor={anchor} items={items.people(issue.team_id)} selected={issue.assignee_id || 0} placeholder="Assign to…" onPick={v => onPatch({ assignee_id: v })} onClose={onClose} />
    case 'project': return <PickerMenu anchor={anchor} items={items.projects(issue.team_id)} selected={issue.project_id || 0} placeholder="Move to project…" onPick={v => onPatch({ project_id: v })} onClose={onClose} />
    case 'milestone': return <PickerMenu anchor={anchor} items={items.milestones(issue.project_id)} selected={issue.milestone_id || 0} placeholder="Set milestone…" onPick={v => onPatch({ milestone_id: v })} onClose={onClose} />
    case 'cycle': return <PickerMenu anchor={anchor} items={items.cycles(issue.team_id)} selected={issue.cycle_id || 0} placeholder="Move to cycle…" onPick={v => onPatch({ cycle_id: v })} onClose={onClose} />
    case 'estimate': return <PickerMenu anchor={anchor} items={items.estimates(team)} selected={issue.estimate ?? -1} placeholder="Estimate…" onPick={v => onPatch({ estimate: v < 0 ? null : v })} onClose={onClose} />
    case 'due': return <DateMenu anchor={anchor} value={issue.due_date} onPick={v => onPatch({ due_date: v })} onClose={onClose} />
    case 'labels': return <LabelsPicker anchor={anchor} teamId={issue.team_id} selected={issue.label_ids} onChange={ids => onPatch({ label_ids: ids })} onClose={onClose} />
  }
}

export function LabelsPicker({ anchor, teamId, selected, onChange, onClose }: { anchor: Anchor; teamId: number; selected: number[]; onChange: (ids: number[]) => void; onClose: () => void }) {
  const app = useApp()
  const items = useItems()
  const [cur, setCur] = useState(selected)
  const canCreate = app.canEdit(teamId)
  const toggle = (id: number) => {
    const all = app.team(teamId)?.labels || []
    const l = all.find(x => x.id === id)
    let next = cur.includes(id) ? cur.filter(x => x !== id) : [...cur, id]
    // One label per group.
    if (l?.parent_id && !cur.includes(id)) next = next.filter(x => x === id || all.find(y => y.id === x)?.parent_id !== l.parent_id)
    setCur(next)
    onChange(next)
  }
  const create = async (name: string) => {
    const { api, V1 } = await import('../lib/api')
    const l = await api.post<{ id: number }>(`${V1}/teams/${teamId}/labels`, { name, color: ['#ef4444', '#f59e0b', '#10b981', '#3b82f6', '#8b5cf6', '#ec4899'][name.length % 6] })
    await app.reload()
    const next = [...cur, l.id]
    setCur(next)
    onChange(next)
  }
  return <PickerMenu anchor={anchor} multi items={items.labels(teamId)} selected={cur} placeholder="Labels…" onPick={toggle} onClose={onClose}
    onCreate={canCreate ? create : undefined} createLabel={t => `Create label “${t}”`} />
}

/** Chips showing an issue's labels. */
export function LabelChips({ ids, max = 3 }: { ids: number[]; max?: number }) {
  const { label } = useApp()
  if (!ids.length) return null
  const shown = ids.slice(0, max)
  return <span className="label-chips">{shown.map(id => { const l = label(id); return l ? <LabelPill key={id} name={l.name} color={l.color} /> : null })}{ids.length > max && <span className="label-more">+{ids.length - max}</span>}</span>
}

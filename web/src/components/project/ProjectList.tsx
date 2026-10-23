// The projects list (rows) and board (a column per status, drag to move).
import React, { useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '../../lib/app-context'
import type { Project, ProjectStatus } from '../../lib/types'
import { Icon } from '../icons'
import { HealthBadge, PersonAvatar, ProgressRing, ProjectMark, ProjectStatusIcon, PROJECT_STATUS_LABEL } from '../issue-bits'
import { useIsMobile } from '../ui'
import { AvatarStack, FavoriteStar, ProjectPicker, TargetChip, TeamMarks, usePickerState } from './common'
import { startDrag } from './drag'

export interface ProjectGroup { key: string; label: string; icon?: React.ReactNode; items: Project[] }

type PickKind = 'status' | 'priority' | 'lead'

export function ProjectRow({ p, onPatch }: { p: Project; onPatch: (p: Project, body: Record<string, unknown>) => void }) {
  const app = useApp()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const pick = usePickerState<PickKind>()
  const today = app.boot?.today || ''
  const canEdit = p.team_ids.some(id => app.canEdit(id))
  const lead = app.person(p.lead_id)
  const members = p.member_ids.filter(id => id !== p.lead_id)
  const open = (kind: PickKind) => (e: React.MouseEvent) => { e.stopPropagation(); if (!canEdit) return; const r = e.currentTarget.getBoundingClientRect(); pick.show(kind, { x: r.left, y: r.top, w: r.width, h: r.height }) }
  const pct = Math.round(p.progress.percent)
  return (
    <div className="proj-row" role="link" tabIndex={0} data-pocket-ask={`project:${p.id}`} onClick={() => navigate(`/project/${p.id}`)} onKeyDown={e => { if (e.key === 'Enter') navigate(`/project/${p.id}`) }}>
      <FavoriteStar id={p.id} className="btn ghost icon-only xs" />
      <button type="button" className="cell-btn" title={PROJECT_STATUS_LABEL[p.status]} aria-label={`Status: ${PROJECT_STATUS_LABEL[p.status]}`} onClick={open('status')}><ProjectStatusIcon status={p.status} /></button>
      <span className="proj-mark-box" style={{ ['--pc' as string]: p.color || 'var(--accent)' }}><ProjectMark icon={p.icon} color={p.color} size={16} /></span>
      <div className="proj-name">
        <span className="t ellipsis">{p.name}</span>
        {p.summary && !mobile && <span className="s ellipsis">{p.summary}</span>}
      </div>
      <div className="proj-cells">
        <span className="c-health">{(p.last_update || !isClosedStatus(p.status)) && <HealthBadge health={p.health} />}</span>
        <button type="button" className="cell-btn c-lead" title={lead ? `Lead: ${lead.name || lead.email}` : 'No lead'} onClick={open('lead')}><PersonAvatar person={lead} size={20} title={lead ? `Lead: ${lead.name || lead.email}` : 'No lead'} /></button>
        <span className="c-members hide-mobile">{members.length > 0 ? <AvatarStack ids={members} max={3} size={20} /> : null}</span>
        <span className="c-target"><TargetChip project={p} today={today} compact={mobile} /></span>
        <span className="c-progress" title={`${p.progress.completed} of ${p.progress.total} issues done`}><ProgressRing percent={pct} size={16} color={p.progress.total ? 'var(--accent)' : 'var(--faint)'} /><span className="tnum">{pct}%</span></span>
        <span className="c-issues hide-mobile tnum" title={`${p.progress.completed} done of ${p.progress.total} issues`}>{p.progress.completed}/{p.progress.total}</span>
        <span className="c-teams hide-mobile"><TeamMarks ids={p.team_ids} /></span>
      </div>
      {pick.open && <ProjectPicker kind={pick.open.kind} project={p} anchor={pick.open.anchor} onPatch={b => onPatch(p, b)} onClose={pick.close} />}
    </div>
  )
}

const isClosedStatus = (s: ProjectStatus) => s === 'completed' || s === 'canceled'

export function ProjectList({ groups, onPatch, grouped }: { groups: ProjectGroup[]; onPatch: (p: Project, body: Record<string, unknown>) => void; grouped: boolean }) {
  return (
    <div className="proj-list">
      {groups.map(g => (
        <section key={g.key}>
          {grouped && <header className="group-head">{g.icon}<span className="group-name">{g.label}</span><span className="group-count">{g.items.length}</span></header>}
          {g.items.map(p => <ProjectRow key={g.key + ':' + p.id} p={p} onPatch={onPatch} />)}
        </section>
      ))}
    </div>
  )
}

// ---------- board ----------

export function ProjectBoard({ columns, projects, onMove, onNew }: { columns: ProjectStatus[]; projects: Project[]; onMove: (p: Project, status: ProjectStatus) => void; onNew: (status: ProjectStatus) => void }) {
  const app = useApp()
  const navigate = useNavigate()
  const today = app.boot?.today || ''
  const [drag, setDrag] = useState<{ id: number; x: number; y: number; ox: number; oy: number; w: number; over: ProjectStatus | null } | null>(null)
  const ghost = useRef<HTMLDivElement>(null)
  const dragged = drag ? projects.find(p => p.id === drag.id) : undefined

  const colAt = (x: number, y: number): ProjectStatus | null => {
    const el = document.elementFromPoint(x, y)?.closest<HTMLElement>('[data-status]')
    return (el?.dataset.status as ProjectStatus) || null
  }
  const down = (p: Project, canEdit: boolean) => (e: React.PointerEvent<HTMLDivElement>) => {
    if (!canEdit || (e.target as Element).closest('button')) return
    const card = e.currentTarget
    const r = card.getBoundingClientRect()
    const ox = e.clientX - r.left, oy = e.clientY - r.top
    startDrag(e, {
      onStart: m => setDrag({ id: p.id, x: m.x, y: m.y, ox, oy, w: r.width, over: p.status }),
      onMove: m => {
        setDrag(d => d && { ...d, x: m.x, y: m.y, over: colAt(m.x, m.y) })
        // Scroll the board sideways near its edges.
        const sc = card.closest('.proj-board')
        if (sc) { const b = sc.getBoundingClientRect(); if (m.x > b.right - 40) sc.scrollLeft += 12; else if (m.x < b.left + 40) sc.scrollLeft -= 12 }
      },
      onEnd: (m, moved) => {
        setDrag(null)
        if (!moved) return
        const to = colAt(m.x, m.y)
        if (to && to !== p.status) onMove(p, to)
      },
      onCancel: () => setDrag(null),
    })
  }

  return (
    <div className={`proj-board ${drag ? 'is-dragging' : ''}`}>
      {columns.map(st => {
        const items = projects.filter(p => p.status === st)
        return (
          <section key={st} className={`pb-col ${drag?.over === st ? 'over' : ''}`} data-status={st}>
            <header className="pb-head">
              <ProjectStatusIcon status={st} /><span className="group-name">{PROJECT_STATUS_LABEL[st]}</span><span className="group-count">{items.length}</span>
              <span className="spacer" />
              <button type="button" className="btn ghost icon-only xs" aria-label={`New ${PROJECT_STATUS_LABEL[st]} project`} title="New project" onClick={() => onNew(st)}><Icon name="plus" size={14} /></button>
            </header>
            <div className="pb-cards">
              {items.map(p => {
                const canEdit = p.team_ids.some(id => app.canEdit(id))
                return (
                  <div key={p.id} className={`pb-card ${drag?.id === p.id ? 'lifted' : ''}`} data-pocket-ask={`project:${p.id}`} data-project-id={p.id} role="link" tabIndex={0}
                    onPointerDown={down(p, canEdit)} onClick={() => navigate(`/project/${p.id}`)} onKeyDown={e => { if (e.key === 'Enter') navigate(`/project/${p.id}`) }}
                    onContextMenu={e => { if (drag) e.preventDefault() }}>
                    <CardBody p={p} today={today} />
                  </div>
                )
              })}
              {!items.length && <div className="pb-empty">{drag ? 'Drop here' : 'No projects'}</div>}
            </div>
          </section>
        )
      })}
      {drag && dragged && <div ref={ghost} className="pb-card pb-ghost" style={{ width: drag.w, left: drag.x - drag.ox, top: drag.y - drag.oy }}><CardBody p={dragged} today={today} /></div>}
    </div>
  )
}

function CardBody({ p, today }: { p: Project; today: string }) {
  const app = useApp()
  const pct = Math.round(p.progress.percent)
  return <>
    <div className="pb-title">
      <ProjectMark icon={p.icon} color={p.color} size={16} />
      <span className="ellipsis">{p.name}</span>
      <FavoriteStar id={p.id} className="btn ghost icon-only xs" />
    </div>
    {p.summary && <div className="pb-sum">{p.summary}</div>}
    <div className="pb-meta">
      {(p.last_update || !isClosedStatus(p.status)) && <HealthBadge health={p.health} />}
      <span className="spacer" />
      <TargetChip project={p} today={today} compact />
    </div>
    <div className="pb-meta">
      <PersonAvatar person={app.person(p.lead_id)} size={18} />
      <TeamMarks ids={p.team_ids} size={14} />
      <span className="spacer" />
      <span className="c-progress"><ProgressRing percent={pct} size={14} /><span className="tnum">{pct}%</span></span>
    </div>
  </>
}

// One issue in a list, and the actions every issue offers (context menu,
// property pickers) — shared by the list, the board, My issues, projects and cycles.
import React, { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { emitChange, useApp } from '../lib/app-context'
import { estimateLabel, isDone, patchIssue } from '../lib/issues'
import { ago } from '../lib/format'
import type { Issue } from '../lib/types'
import { Icon } from './icons'
import { DueChip, PersonAvatar, PriorityIcon, ProjectMark, StatusIcon, useStateProgress } from './issue-bits'
import { LabelChips, PropertyPicker, type Prop } from './pickers'
import { type Anchor, type MenuItem, Menu, copyText, rectOf, useToast } from './ui'

/** Apply a patch to an issue, report the fresh issue, offer Undo for the main fields. */
export function useIssuePatch(onUpdated?: (is: Issue) => void) {
  const { toast } = useToast()
  return async (issue: Issue, patch: Record<string, unknown>, undo = true) => {
    const before: Record<string, unknown> = {}
    for (const k of Object.keys(patch)) {
      if (k in issue) before[k] = (issue as unknown as Record<string, unknown>)[k]
    }
    try {
      const fresh = await patchIssue(issue.id, patch)
      onUpdated?.(fresh)
      emitChange({ kind: 'issue', team_ids: [fresh.team_id], ids: [fresh.id] })
      if (undo && Object.keys(before).length && !('sort_order' in patch)) {
        toast(<>{fresh.identifier} updated</>, 'ok', { label: 'Undo', run: async () => { const back = await patchIssue(issue.id, before); onUpdated?.(back); emitChange({ kind: 'issue', team_ids: [back.team_id], ids: [back.id] }) } })
      }
      return fresh
    } catch (e) { toast((e as Error).message, 'err'); return null }
  }
}

/** The context menu of an issue. */
export function issueMenu(issue: Issue, ctx: { openProp: (p: Prop) => void; navigate: (to: string) => void; toast: ReturnType<typeof useToast>['toast']; canEdit: boolean; onDeleted?: () => void; base: string }): MenuItem[] {
  const url = `${ctx.base}/issue/${issue.identifier}`
  const edit: MenuItem[] = ctx.canEdit ? [
    { label: 'Status', icon: 'circle', hint: 'S', onSelect: () => ctx.openProp('state') },
    { label: 'Assignee', icon: 'user', hint: 'A', onSelect: () => ctx.openProp('assignee') },
    { label: 'Priority', icon: 'bar-chart', hint: 'P', onSelect: () => ctx.openProp('priority') },
    { label: 'Labels', icon: 'tag', hint: 'L', onSelect: () => ctx.openProp('labels') },
    { label: 'Project', icon: 'briefcase', onSelect: () => ctx.openProp('project') },
    { label: 'Cycle', icon: 'cycle', onSelect: () => ctx.openProp('cycle') },
    { label: 'Due date', icon: 'calendar', hint: 'D', onSelect: () => ctx.openProp('due') },
    { label: 'Estimate', icon: 'triangle', hint: 'E', onSelect: () => ctx.openProp('estimate') },
    'sep',
  ] : []
  return [
    { label: 'Open', icon: 'arrow-up-right', onSelect: () => ctx.navigate(`/issue/${issue.identifier}`) },
    ...edit,
    { label: 'Copy ID', icon: 'copy', onSelect: async () => { await copyText(issue.identifier); ctx.toast(`Copied ${issue.identifier}`) } },
    { label: 'Copy link', icon: 'link', onSelect: async () => { await copyText(url); ctx.toast('Link copied') } },
    { label: 'Copy as Markdown', icon: 'file-text', onSelect: async () => { await copyText(`[${issue.identifier} ${issue.title}](${url})`); ctx.toast('Copied') } },
    { label: 'Add to my Tasks', icon: 'check-circle', onSelect: async () => {
      try { await api.post(`${V1}/issues/${issue.id}/task`, {}); ctx.toast('Added to Pocket Tasks') } catch (e) { ctx.toast((e as Error).message, 'err') }
    } },
    ...(ctx.canEdit ? ['sep' as const, { label: 'Delete', icon: 'trash', danger: true, onSelect: async () => {
      try {
        await api.del(`${V1}/issues/${issue.id}`)
        emitChange({ kind: 'issue', team_ids: [issue.team_id], ids: [issue.id] })
        ctx.onDeleted?.()
        ctx.toast(`${issue.identifier} moved to the trash`, 'ok', { label: 'Undo', run: async () => { await api.post(`${V1}/issues/${issue.id}/restore`); emitChange({ kind: 'issue', team_ids: [issue.team_id], ids: [issue.id] }) } })
      } catch (e) { ctx.toast((e as Error).message, 'err') }
    } }] : []),
  ]
}

/** Hook bundling the menu + picker state for one issue element. */
export function useIssueActions(issue: Issue, onUpdated?: (is: Issue) => void) {
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const patch = useIssuePatch(onUpdated)
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [prop, setProp] = useState<{ p: Prop; at: Anchor } | null>(null)
  const canEdit = app.canEdit(issue.team_id)
  const openProp = (p: Prop, at?: Anchor) => setProp({ p, at: at || menu || { x: window.innerWidth / 2 - 130, y: 120, w: 0, h: 0 } })
  const items = issueMenu(issue, { openProp: p => openProp(p, menu || undefined), navigate, toast, canEdit, base: window.location.origin })
  const ui = (
    <>
      {menu && <Menu anchor={menu} items={items} onClose={() => setMenu(null)} width={220} />}
      {prop && <PropertyPicker prop={prop.p} issue={issue} anchor={prop.at} onClose={() => setProp(null)} onPatch={p => patch(issue, p)} />}
    </>
  )
  return { ui, openMenu: (a: Anchor) => setMenu(a), openProp, patch: (p: Record<string, unknown>) => patch(issue, p), canEdit }
}

export function IssueRow({ issue, properties, selected, focused, onToggleSelect, onUpdated, onOpen, showTeam, depth = 0, dragHandle }: {
  issue: Issue; properties: string[]; selected?: boolean; focused?: boolean; onToggleSelect?: (e: React.MouseEvent) => void
  onUpdated?: (is: Issue) => void; onOpen?: (is: Issue) => void; showTeam?: boolean; depth?: number; dragHandle?: React.HTMLAttributes<HTMLElement>
}) {
  const app = useApp()
  const navigate = useNavigate()
  const progress = useStateProgress()
  const act = useIssueActions(issue, onUpdated)
  const state = app.state(issue.state_id)
  const project = app.project(issue.project_id)
  const team = app.team(issue.team_id)
  const has = (p: string) => properties.includes(p)
  const done = isDone(issue.state_type)
  const click = (p: Prop) => (e: React.MouseEvent) => { e.stopPropagation(); if (act.canEdit) act.openProp(p, rectOf(e.currentTarget)) }
  const open = () => (onOpen ? onOpen(issue) : navigate(`/issue/${issue.identifier}`))
  return (
    <div className={`issue-row ${selected ? 'selected' : ''} ${focused ? 'focused' : ''} ${done ? 'done' : ''}`} style={depth ? { paddingLeft: 12 + depth * 22 } : undefined}
      data-issue-id={issue.id} data-pocket-ask role="row" tabIndex={-1}
      onClick={e => { if (e.metaKey || e.ctrlKey || e.shiftKey) { e.preventDefault(); onToggleSelect?.(e) } else open() }}
      onContextMenu={e => { e.preventDefault(); act.openMenu({ x: e.clientX, y: e.clientY, w: 0, h: 0 }) }} {...dragHandle}>
      {onToggleSelect && <span className={`row-check ${selected ? 'on' : ''}`} onClick={e => { e.stopPropagation(); onToggleSelect(e) }} role="checkbox" aria-checked={!!selected} aria-label="Select">{selected && <Icon name="check" size={11} strokeWidth={3} />}</span>}
      {has('priority') && <button type="button" className="cell-btn prio" onClick={click('priority')} title="Priority"><PriorityIcon priority={issue.priority} /></button>}
      {has('id') && <span className="ident">{issue.identifier}</span>}
      {has('status') && <button type="button" className="cell-btn" onClick={click('state')} title={state?.name}><StatusIcon state={state} progress={progress(state)} /></button>}
      <span className="issue-title ellipsis">
        <span className="t">{issue.title}</span>
        {has('sub_issues') && issue.sub_total > 0 && <span className="subcount" title="Sub-issues"><Icon name="checklist" size={12} />{issue.sub_done}/{issue.sub_total}</span>}
        {issue.blocked_by > 0 && <span className="blocked" title="Blocked"><Icon name="alert-triangle" size={12} /></span>}
        <a href={`/issue/${issue.identifier}`} className="pocket-ask-link" hidden>{issue.identifier}</a>
      </span>
      <span className="row-props">
        {has('comments') && issue.comments > 0 && <span className="meta-count"><Icon name="message" size={12} />{issue.comments}</span>}
        {has('labels') && <LabelChips ids={issue.label_ids} max={2} />}
        {has('project') && project && <span className="proj-chip" title={project.name}><ProjectMark icon={project.icon} color={project.color} size={13} /><span className="ellipsis">{project.name}</span></span>}
        {has('milestone') && issue.milestone_id && <span className="proj-chip"><Icon name="diamond" size={11} />{app.milestone(issue.milestone_id)?.name}</span>}
        {has('cycle') && issue.cycle_id && <span className="proj-chip"><Icon name="cycle" size={12} />{(() => { const c = app.cycle(issue.cycle_id); return c ? c.name || `Cycle ${c.number}` : '' })()}</span>}
        {showTeam && team && <span className="team-key">{team.key}</span>}
        {has('estimate') && issue.estimate !== null && <span className="est" title="Estimate">{estimateLabel(team?.estimate_scale, issue.estimate)}</span>}
        {has('due') && issue.due_date && <DueChip due={issue.due_date} done={done} compact />}
        {has('created') && <span className="date-meta" title={issue.created_at}>{ago(issue.created_at)}</span>}
        {has('updated') && <span className="date-meta" title={issue.updated_at}>{ago(issue.updated_at)}</span>}
        {has('assignee') && <button type="button" className="cell-btn" onClick={click('assignee')}><PersonAvatar person={app.person(issue.assignee_id)} size={20} /></button>}
      </span>
      {act.ui}
    </div>
  )
}

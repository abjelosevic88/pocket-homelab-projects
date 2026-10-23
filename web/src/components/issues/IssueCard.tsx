// One issue on the board: identifier, title, labels, priority, assignee,
// due date, estimate and sub-issue progress. Right-click (or long-press menu
// button on phones) opens the same menu as a list row.
import React from 'react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '../../lib/app-context'
import { estimateLabel, isDone } from '../../lib/issues'
import type { GroupKey, Issue } from '../../lib/types'
import { Icon } from '../icons'
import { DueChip, PersonAvatar, PriorityIcon, ProjectMark, ProgressRing, StatusIcon, useStateProgress } from '../issue-bits'
import { useIssueActions } from '../IssueRow'
import { LabelChips, type Prop } from '../pickers'
import { rectOf } from '../ui'
import { AIMark } from '../issue/AIMark'

export function IssueCard({ issue, properties, groupBy, selected, focused, onToggleSelect, onUpdated, showTeam, dragProps, dragging }: {
  issue: Issue; properties: string[]; groupBy: GroupKey; selected?: boolean; focused?: boolean; onToggleSelect?: (e: React.MouseEvent) => void
  onUpdated?: (is: Issue) => void; showTeam?: boolean; dragProps?: React.HTMLAttributes<HTMLElement>; dragging?: boolean
}) {
  const app = useApp()
  const navigate = useNavigate()
  const act = useIssueActions(issue, onUpdated)
  const progress = useStateProgress()
  const state = app.state(issue.state_id)
  const team = app.team(issue.team_id)
  const project = app.project(issue.project_id)
  const has = (p: string) => properties.includes(p)
  const done = isDone(issue.state_type)
  const click = (p: Prop) => (e: React.MouseEvent) => { e.stopPropagation(); if (act.canEdit) act.openProp(p, rectOf(e.currentTarget)) }
  const assignee = app.person(issue.assignee_id)
  const showStatus = groupBy !== 'state' && has('status')
  return (
    <div className={`issue-card ${selected ? 'selected' : ''} ${focused ? 'focused' : ''} ${done ? 'done' : ''} ${dragging ? 'drag-src' : ''}`}
      data-issue-id={issue.id} data-pocket-ask role="button" tabIndex={-1} aria-label={`${issue.identifier} ${issue.title}`}
      onClick={e => { if (e.metaKey || e.ctrlKey || e.shiftKey) { e.preventDefault(); onToggleSelect?.(e) } else navigate(`/issue/${issue.identifier}`) }}
      onContextMenu={e => { e.preventDefault(); act.openMenu({ x: e.clientX, y: e.clientY, w: 0, h: 0 }) }} {...dragProps}>
      <div className="ic-top">
        {onToggleSelect && <span className={`row-check ${selected ? 'on' : ''}`} onClick={e => { e.stopPropagation(); onToggleSelect(e) }} role="checkbox" aria-checked={!!selected} aria-label="Select">{selected && <Icon name="check" size={11} strokeWidth={3} />}</span>}
        {has('id') && <span className="ident">{issue.identifier}</span>}
        {showTeam && team && !has('id') && <span className="team-key">{team.key}</span>}
        <AIMark identifier={issue.identifier} />
        <span className="spacer" />
        <button type="button" className="ic-more" aria-label="Issue menu" onClick={e => { e.stopPropagation(); act.openMenu(rectOf(e.currentTarget)) }}><Icon name="more" size={14} /></button>
        {has('assignee') && <button type="button" className="cell-btn" onClick={click('assignee')} title={assignee ? assignee.name : 'Assign'}><PersonAvatar person={assignee} size={20} /></button>}
      </div>
      <div className="ic-title">
        {showStatus && <button type="button" className="cell-btn inline" onClick={click('state')} title={state?.name}><StatusIcon state={state} progress={progress(state)} /></button>}
        <span>{issue.title}</span>
      </div>
      <div className="ic-props">
        {has('priority') && issue.priority > 0 && <button type="button" className="ic-chip" onClick={click('priority')} title="Priority"><PriorityIcon priority={issue.priority} /></button>}
        {issue.blocked_by > 0 && <span className="ic-chip blocked" title="Blocked"><Icon name="alert-triangle" size={12} /></span>}
        {has('due') && issue.due_date && <button type="button" className="ic-chip" onClick={click('due')}><DueChip due={issue.due_date} done={done} compact /></button>}
        {has('estimate') && issue.estimate !== null && <span className="ic-chip" title="Estimate"><Icon name="triangle" size={11} />{estimateLabel(team?.estimate_scale, issue.estimate)}</span>}
        {has('sub_issues') && issue.sub_total > 0 && <span className="ic-chip" title={`${issue.sub_done} of ${issue.sub_total} sub-issues done`}><ProgressRing percent={(issue.sub_done / issue.sub_total) * 100} size={13} />{issue.sub_done}/{issue.sub_total}</span>}
        {has('comments') && issue.comments > 0 && <span className="ic-chip"><Icon name="message" size={12} />{issue.comments}</span>}
        {has('project') && project && groupBy !== 'project' && <span className="ic-chip proj" title={project.name}><ProjectMark icon={project.icon} color={project.color} size={12} /><span className="ellipsis">{project.name}</span></span>}
        {has('cycle') && issue.cycle_id && groupBy !== 'cycle' && <span className="ic-chip"><Icon name="cycle" size={12} />{(() => { const c = app.cycle(issue.cycle_id); return c ? c.name || `Cycle ${c.number}` : '' })()}</span>}
        {has('labels') && issue.label_ids.length > 0 && <LabelChips ids={issue.label_ids} max={3} />}
      </div>
      {act.ui}
    </div>
  )
}

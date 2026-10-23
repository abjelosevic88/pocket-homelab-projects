// Changing several issues at once: the floating bar shown while issues are
// selected, and a picker that works for a whole selection (across teams).
import { useState } from 'react'
import { useApp } from '../../lib/app-context'
import { STATE_TYPE_LABEL } from '../../lib/issues'
import { STATE_TYPES, type Issue } from '../../lib/types'
import { Icon } from '../icons'
import { StatusIcon } from '../issue-bits'
import { DateMenu, PickerMenu, useItems, type Prop } from '../pickers'
import { type Anchor, rectOf } from '../ui'

/** A property picker for many issues: reports a bulk "set". */
export function BulkPicker({ prop, issues, anchor, onPatch, onClose }: { prop: Prop; issues: Issue[]; anchor: Anchor; onPatch: (set: Record<string, unknown>) => void; onClose: () => void }) {
  const app = useApp()
  const items = useItems()
  const teamIds = [...new Set(issues.map(i => i.team_id))]
  const one = teamIds.length === 1 ? teamIds[0] : 0
  const same = <T,>(f: (i: Issue) => T): T | undefined => { const v = issues.map(f); return v.every(x => x === v[0]) ? v[0] : undefined }
  const [labels, setLabels] = useState(() => {
    const all = new Set(issues.flatMap(i => i.label_ids))
    return [...all].filter(id => issues.every(i => i.label_ids.includes(id)))
  })
  switch (prop) {
    case 'state':
      if (one) return <PickerMenu anchor={anchor} items={items.states(one)} selected={same(i => i.state_id)} placeholder={`Status of ${issues.length} issues…`} onPick={v => onPatch({ state_id: v })} onClose={onClose} />
      return <PickerMenu anchor={anchor} items={STATE_TYPES.map(t => ({ value: t, label: STATE_TYPE_LABEL[t], icon: <StatusIcon type={t} /> }))} selected={same(i => i.state_type)} placeholder={`Status of ${issues.length} issues…`} onPick={v => onPatch({ state: v })} onClose={onClose} />
    case 'priority': return <PickerMenu anchor={anchor} items={items.priorities()} selected={same(i => i.priority)} placeholder="Set priority…" onPick={v => onPatch({ priority: v })} onClose={onClose} />
    case 'assignee': return <PickerMenu anchor={anchor} items={items.people(one || null)} selected={same(i => i.assignee_id || 0)} placeholder="Assign to…" onPick={v => onPatch({ assignee_id: v })} onClose={onClose} />
    case 'project': return <PickerMenu anchor={anchor} items={items.projects(one || null)} selected={same(i => i.project_id || 0)} placeholder="Move to project…" onPick={v => onPatch({ project_id: v })} onClose={onClose} />
    case 'cycle':
      if (!one) return <PickerMenu anchor={anchor} items={[{ value: 0, label: 'No cycle', icon: <Icon name="x" size={13} /> }]} placeholder="Cycles belong to one team" onPick={v => onPatch({ cycle_id: v })} onClose={onClose} />
      return <PickerMenu anchor={anchor} items={items.cycles(one)} selected={same(i => i.cycle_id || 0)} placeholder="Move to cycle…" onPick={v => onPatch({ cycle_id: v })} onClose={onClose} />
    case 'milestone': {
      const pid = same(i => i.project_id)
      return <PickerMenu anchor={anchor} items={items.milestones(pid || null)} selected={same(i => i.milestone_id || 0)} placeholder={pid ? 'Set milestone…' : 'Pick issues of one project'} onPick={v => onPatch({ milestone_id: v })} onClose={onClose} />
    }
    case 'estimate': return <PickerMenu anchor={anchor} items={items.estimates(app.team(one || issues[0]?.team_id))} selected={same(i => i.estimate ?? -1)} placeholder="Estimate…" onPick={v => onPatch({ estimate: v < 0 ? null : v })} onClose={onClose} />
    case 'due': return <DateMenu anchor={anchor} value={same(i => i.due_date) || ''} onPick={v => onPatch({ due_date: v })} onClose={onClose} />
    case 'labels': {
      const list = teamIds.flatMap(t => items.labels(t).map(it => ({ ...it, group: teamIds.length > 1 ? app.team(t)?.name : it.group })))
      return <PickerMenu anchor={anchor} multi items={list} selected={labels} placeholder={`Labels of ${issues.length} issues…`} onClose={onClose}
        onPick={id => {
          const on = labels.includes(id)
          setLabels(on ? labels.filter(x => x !== id) : [...labels, id])
          onPatch(on ? { remove_label_ids: [id] } : { add_label_ids: [id] })
        }} />
    }
  }
}

export function BulkBar({ issues, onClear, onPatch, onDelete, onSelectAll, total }: { issues: Issue[]; onClear: () => void; onPatch: (set: Record<string, unknown>) => void; onDelete: () => void; onSelectAll: () => void; total: number }) {
  const app = useApp()
  const [pick, setPick] = useState<{ prop: Prop; at: Anchor } | null>(null)
  const canCycle = new Set(issues.map(i => i.team_id)).size === 1 && !!app.team(issues[0]?.team_id)?.cycles_enabled
  const btn = (prop: Prop, icon: string, label: string) => (
    <button type="button" className="bb-btn" onClick={e => setPick({ prop, at: rectOf(e.currentTarget) })} title={label}><Icon name={icon} size={14} /><span className="bb-l">{label}</span></button>
  )
  return (
    <div className="bulk-bar" role="toolbar" aria-label="Change the selected issues">
      <span className="bb-count"><b>{issues.length}</b> selected</span>
      {issues.length < total && <button type="button" className="bb-link" onClick={onSelectAll}>Select all {total}</button>}
      <span className="bb-sep" />
      {btn('state', 'circle', 'Status')}
      {btn('assignee', 'user', 'Assignee')}
      {btn('priority', 'bar-chart', 'Priority')}
      {btn('labels', 'tag', 'Labels')}
      {btn('project', 'briefcase', 'Project')}
      {canCycle && btn('cycle', 'cycle', 'Cycle')}
      {btn('due', 'calendar', 'Due date')}
      <button type="button" className="bb-btn danger" onClick={onDelete} title="Delete (⌫)"><Icon name="trash" size={14} /><span className="bb-l">Delete</span></button>
      <span className="bb-sep" />
      <button type="button" className="bb-btn icon" onClick={onClear} aria-label="Clear the selection" title="Clear the selection (Esc)"><Icon name="x" size={15} /></button>
      {pick && <BulkPicker prop={pick.prop} issues={issues} anchor={pick.at} onClose={() => setPick(null)} onPatch={onPatch} />}
    </div>
  )
}

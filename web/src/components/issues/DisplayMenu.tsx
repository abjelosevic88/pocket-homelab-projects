// The "Display" popover of an issue view: layout, grouping, ordering, which
// completed issues show, sub-issues, empty groups and the visible properties.
import { useState } from 'react'
import { ALL_PROPERTIES, DEFAULT_DISPLAY, GROUP_LABEL, PROPERTY_LABEL, SHOW_COMPLETED_LABEL, SORT_LABEL, displayOf } from '../../lib/issues'
import type { Display, GroupKey, SortKey } from '../../lib/types'
import { Icon } from '../icons'
import { Select } from '../Select'
import { type Anchor, Popover, Segmented, Switch, rectOf } from '../ui'

export function DisplayButton({ display, onChange, multiTeam, cyclesOn, defaults }: { display: ReturnType<typeof displayOf>; onChange: (d: Display) => void; multiTeam: boolean; cyclesOn: boolean; defaults?: Display }) {
  const [at, setAt] = useState<Anchor | null>(null)
  const set = (p: Display) => onChange({ ...display, ...p })
  const groups: GroupKey[] = ['state', 'assignee', 'priority', 'project', 'label', ...(cyclesOn ? ['cycle' as const] : []), 'milestone', ...(multiTeam ? ['team' as const] : []), 'none']
  const sorts: SortKey[] = ['manual', 'priority', 'updated', 'created', 'due', 'title', 'estimate', 'identifier']
  const board = display.layout === 'board'
  const props = display.properties
  const toggleProp = (p: string) => set({ properties: props.includes(p) ? props.filter(x => x !== p) : [...props, p] })
  const changed = JSON.stringify(display) !== JSON.stringify(displayOf(defaults))
  return (
    <>
      <button type="button" className="btn ghost sm" onClick={e => setAt(at ? null : rectOf(e.currentTarget))} aria-expanded={!!at} title="Display options">
        <Icon name="sliders" size={14} /><span className="hide-mobile">Display</span>
      </button>
      {at && <Popover anchor={at} onClose={() => setAt(null)} prefer="below" width={330} className="display-pop" label="Display options">
        <div className="dp-section">
          <Segmented value={display.layout} onChange={v => set({ layout: v })} className="dp-layout" label="Layout" options={[
            { value: 'list', label: <span className="row" style={{ gap: 6 }}><Icon name="rows" size={14} />List</span> },
            { value: 'board', label: <span className="row" style={{ gap: 6 }}><Icon name="columns" size={14} />Board</span> },
          ]} />
        </div>
        <div className="dp-section">
          <div className="dp-row"><span>{board ? 'Columns' : 'Grouping'}</span>
            <Select size="sm" className="inline" value={display.group} onChange={v => set({ group: v })} options={groups.map(g => ({ value: g, label: g === 'none' ? 'No grouping' : GROUP_LABEL[g] }))} /></div>
          <div className="dp-row"><span>{board ? 'Rows' : 'Sub-grouping'}</span>
            <Select size="sm" className="inline" value={display.sub_group} onChange={v => set({ sub_group: v })} options={groups.filter(g => g !== display.group || g === 'none').map(g => ({ value: g, label: g === 'none' ? 'No grouping' : GROUP_LABEL[g] }))} /></div>
          <div className="dp-row"><span>Ordering</span>
            <Select size="sm" className="inline" value={display.sort} onChange={v => set({ sort: v })} options={sorts.map(s => ({ value: s, label: SORT_LABEL[s] }))} /></div>
          <div className="dp-row"><span>Completed issues</span>
            <Select size="sm" className="inline" value={display.show_completed || 'default'} onChange={v => set({ show_completed: v === 'default' ? undefined : v as Display['show_completed'] })}
              options={[{ value: 'default', label: 'Default' }, ...(['all', 'month', 'week', 'day', 'none'] as const).map(v => ({ value: v, label: SHOW_COMPLETED_LABEL[v] }))]} /></div>
        </div>
        <div className="dp-section">
          <label className="dp-row"><span>Show sub-issues</span><Switch size="sm" checked={display.show_sub_issues} onChange={v => set({ show_sub_issues: v })} /></label>
          {!board && <label className="dp-row"><span>Nest sub-issues under parents</span><Switch size="sm" checked={!!display.nest_sub_issues} disabled={!display.show_sub_issues} onChange={v => set({ nest_sub_issues: v })} /></label>}
          <label className="dp-row"><span>Show empty groups</span><Switch size="sm" checked={display.show_empty_groups} onChange={v => set({ show_empty_groups: v })} /></label>
        </div>
        <div className="dp-section">
          <div className="dp-label">Display properties</div>
          <div className="dp-props">
            {ALL_PROPERTIES.map(p => <button key={p} type="button" className={`dp-prop ${props.includes(p) ? 'on' : ''}`} aria-pressed={props.includes(p)} onClick={() => toggleProp(p)}>{PROPERTY_LABEL[p] || p}</button>)}
          </div>
        </div>
        {changed && <div className="dp-foot"><button type="button" className="btn ghost sm" onClick={() => onChange({ ...DEFAULT_DISPLAY, ...defaults })}><Icon name="undo" size={14} />Reset to default</button></div>}
      </Popover>}
    </>
  )
}

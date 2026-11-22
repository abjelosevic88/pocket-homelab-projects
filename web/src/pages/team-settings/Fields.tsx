// Which issue fields the team uses. A field switched off disappears from the
// issue page, the new-issue dialog, lists, boards, filters, grouping, ordering,
// the bulk bar, menus and shortcuts; values already set are kept.
import { HIDEABLE_FIELDS } from '../../lib/issues'
import type { HideableField } from '../../lib/types'
import { Icon } from '../../components/icons'
import { SettingRow, Switch } from '../../components/ui'
import { Section } from '../settings/common'
import { ReadOnly, type TabProps, usePatchTeam } from './common'

const ALWAYS: { label: string; icon: string; help: string }[] = [
  { label: 'Status', icon: 'circle', help: 'Where the issue is in the workflow.' },
  { label: 'Assignee', icon: 'user', help: 'Who works on it.' },
  { label: 'Project', icon: 'briefcase', help: 'The project it belongs to.' },
  { label: 'Parent', icon: 'subtasks', help: 'The issue it is part of.' },
]

export function FieldsTab({ team, admin }: TabProps) {
  const patch = usePatchTeam(team)
  const hidden = team.hidden_fields || []
  const setHidden = (f: HideableField, on: boolean, label: string) =>
    patch({ hidden_fields: on ? hidden.filter(x => x !== f) : [...hidden, f] }, on ? `${label} is on` : `${label} is off`)
  const estimatesOn = team.estimate_scale !== 'none'
  const title = (icon: string, label: string) => <span className="row" style={{ gap: 8 }}><Icon name={icon} size={14} />{label}</span>

  return (
    <div className="stack loose">
      {!admin && <ReadOnly />}
      <Section title="Issue fields">
        <p className="muted small" style={{ margin: '0 0 8px' }}>
          Switch off what this team doesn’t use. The field disappears from issues, the new-issue dialog, lists, boards, filters and grouping.
          Values already set are kept and come back when you switch the field on again.
        </p>
        {HIDEABLE_FIELDS.map(f => (
          <SettingRow key={f.key} title={title(f.icon, f.label)} help={f.help}>
            <Switch label={f.label} checked={!hidden.includes(f.key)} disabled={!admin} onChange={v => setHidden(f.key, v, f.label)} />
          </SettingRow>
        ))}
        <SettingRow title={title('cycle', 'Cycle')} help="Time-boxed sprints. Set their length on the Cycles tab.">
          <Switch label="Cycle" checked={team.cycles_enabled} disabled={!admin} onChange={v => patch({ cycles_enabled: v }, v ? 'Cycles are on' : 'Cycle is off')} />
        </SettingRow>
        <SettingRow title={title('triangle', 'Estimate')} help="How big an issue is. Pick the scale under General → Estimates.">
          <Switch label="Estimate" checked={estimatesOn} disabled={!admin} onChange={v => patch({ estimate_scale: v ? 'linear' : 'none' }, v ? 'Estimates are on' : 'Estimate is off')} />
        </SettingRow>
      </Section>

      <Section title="Always on">
        {ALWAYS.map(f => (
          <SettingRow key={f.label} title={title(f.icon, f.label)} help={f.help}>
            <Switch label={f.label} checked disabled onChange={() => {}} />
          </SettingRow>
        ))}
      </Section>

      {hidden.includes('priority') && <div className="callout"><Icon name="info" size={15} /><span>Without priority, lists show issues in <b>manual order</b>: drag them into the order they should be done.</span></div>}
    </div>
  )
}

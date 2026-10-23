import { useApp } from '../../lib/app-context'
import { Icon } from '../../components/icons'
import { TeamMark } from '../../components/pickers'
import { type Option, Select } from '../../components/Select'
import { Loading, Segmented, SettingRow } from '../../components/ui'
import { browserZone, Section, TimezoneSelect, useSaveSettings, useSettingsResponse } from './common'

export function PreferencesTab() {
  const { settings, themeMode, setThemeMode, teams } = useApp()
  const { sr } = useSettingsResponse()
  const save = useSaveSettings()
  if (!settings) return <Loading />
  const s = settings
  const live = teams.filter(t => !t.archived_at)
  const box = sr?.box_timezone || browserZone()
  const teamOpts: Option<number>[] = [
    { value: 0, label: 'None', description: 'New issues ask which team' },
    ...live.map(t => ({ value: t.id, label: t.name, icon: <TeamMark team={t} size={16} />, hint: t.key })),
  ]
  const homeOpts: Option<string>[] = [
    { value: 'my-issues', label: 'My issues', icon: <Icon name="target" size={15} />, description: 'What is assigned to you' },
    { value: 'inbox', label: 'Inbox', icon: <Icon name="inbox" size={15} />, description: 'Mentions, assignments and updates' },
    { value: 'projects', label: 'Projects', icon: <Icon name="briefcase" size={15} />, description: 'All projects of your teams' },
    { value: 'team', label: live[0] ? `${live[0].name} issues` : 'First team', icon: live[0] ? <TeamMark team={live[0]} size={16} /> : <Icon name="layers" size={15} />, description: 'The first team in the sidebar' },
  ]

  return (
    <div className="stack loose">
      <Section title="Appearance">
        <SettingRow title="Theme" help="System follows your device’s light or dark setting. Saved on this device.">
          <Segmented label="Theme" value={themeMode} onChange={setThemeMode} options={[
            { value: 'system', label: <span className="row"><Icon name="monitor" size={14} />System</span> },
            { value: 'light', label: <span className="row"><Icon name="sun" size={14} />Light</span> },
            { value: 'dark', label: <span className="row"><Icon name="moon" size={14} />Dark</span> },
          ]} />
        </SettingRow>
        <SettingRow title="Density" help="Compact fits more issues on the screen.">
          <Segmented label="Density" value={s.density} onChange={v => save({ density: v })} options={[{ value: 'comfortable', label: 'Comfortable' }, { value: 'compact', label: 'Compact' }]} />
        </SettingRow>
      </Section>

      <Section title="Date & time">
        <SettingRow title="Time zone" help={<>Due dates, “today” and reminders use this zone. Right now: <b>{s.timezone || box}</b>.</>}>
          <div className="ui-ctl-w"><TimezoneSelect value={s.timezone} onChange={v => save({ timezone: v }, 'Time zone saved')} emptyLabel={`Box default (${box})`} /></div>
        </SettingRow>
        <SettingRow title="Week starts on" help="For calendars, cycles and the date pickers.">
          <Segmented label="Week starts on" value={s.week_start} onChange={v => save({ week_start: v })} options={[{ value: 1, label: 'Monday' }, { value: 0, label: 'Sunday' }, { value: 6, label: 'Saturday' }]} />
        </SettingRow>
      </Section>

      <Section title="Starting point">
        <SettingRow title="Home page" help="What opens when you visit the app.">
          <div className="ui-ctl-w"><Select value={s.home_view || 'my-issues'} options={homeOpts} onChange={v => save({ home_view: v })} label="Home page" /></div>
        </SettingRow>
        <SettingRow title="Default team" help="New issues go here unless you are on another team’s page.">
          <div className="ui-ctl-w"><Select value={live.some(t => t.id === s.default_team_id) ? s.default_team_id : 0} options={teamOpts} onChange={v => save({ default_team_id: v })} label="Default team" /></div>
        </SettingRow>
      </Section>
    </div>
  )
}

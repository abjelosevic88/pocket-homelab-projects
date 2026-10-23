import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useApp } from '../../lib/app-context'
import { addDays, today as todayStr, wdDate, weekday } from '../../lib/dates'
import { Icon } from '../../components/icons'
import { type Option, Select } from '../../components/Select'
import { SettingRow, Switch } from '../../components/ui'
import { Section } from '../settings/common'
import { ReadOnly, type TabProps, usePatchTeam } from './common'

const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const weeks = (n: number) => `${n} ${n === 1 ? 'week' : 'weeks'}`

interface Row { number: number; starts: string; ends: string; status: 'current' | 'upcoming' | 'new'; cooldown?: string }

export function CyclesTab({ team, admin }: TabProps) {
  const app = useApp()
  const patch = usePatchTeam(team)
  const blank = { cycles_enabled: team.cycles_enabled, cycle_weeks: team.cycle_weeks || 2, cycle_start_day: team.cycle_start_day, cycle_cooldown_weeks: team.cycle_cooldown_weeks, cycle_upcoming: team.cycle_upcoming || 2, cycle_rollover: team.cycle_rollover }
  const [f, setF] = useState(blank)
  const [busy, setBusy] = useState(false)
  useEffect(() => { setF(blank) }, [team]) // eslint-disable-line react-hooks/exhaustive-deps
  const dirty = (Object.keys(blank) as (keyof typeof blank)[]).some(k => f[k] !== blank[k])
  const today = app.boot?.today || todayStr()
  const mine = app.cycles.filter(c => c.team_id === team.id).sort((a, b) => a.starts_on.localeCompare(b.starts_on))

  // The next cycles, the way the server plans them: after the last one, else from the latest start day on or before today.
  const preview = useMemo<Row[]>(() => {
    if (!f.cycles_enabled) return []
    const rows: Row[] = mine.filter(c => c.status !== 'past').map(c => ({ number: c.number, starts: c.starts_on, ends: c.ends_on, status: c.status === 'current' ? 'current' : 'upcoming' }))
    const len = 7 * f.cycle_weeks, gap = 7 * f.cycle_cooldown_weeks
    const last = mine[mine.length - 1]
    let next = last ? addDays(last.ends_on, 1 + gap) : (() => { let d = today; while (weekday(d) !== f.cycle_start_day % 7) d = addDays(d, -1); return d })()
    while (addDays(next, len - 1) < today) next = addDays(next, len + gap)
    let number = (last?.number || 0) + 1
    let hasCurrent = rows.some(r => r.status === 'current')
    let upcoming = rows.filter(r => r.status === 'upcoming').length
    for (let i = 0; i < 12 && (!hasCurrent || upcoming < f.cycle_upcoming); i++) {
      const ends = addDays(next, len - 1)
      const status = next <= today && ends >= today ? 'current' : 'upcoming'
      rows.push({ number: number++, starts: next, ends, status: 'new', cooldown: gap ? `${addDays(ends, 1)} – ${addDays(ends, gap)}` : undefined })
      if (status === 'current') hasCurrent = true; else upcoming++
      next = addDays(next, len + gap)
    }
    return rows
  }, [f, mine, today])

  const save = async () => { setBusy(true); await patch(f, f.cycles_enabled ? 'Cycles saved' : 'Cycles are off'); setBusy(false) }
  const num = (from: number, to: number, label: (n: number) => string): Option<number>[] => Array.from({ length: to - from + 1 }, (_, i) => ({ value: from + i, label: label(from + i) }))

  return (
    <div className="stack loose">
      {!admin && <ReadOnly what="cycles" />}
      <Section title="Cycles" sub="Time-boxed stretches of work (sprints). Plan what fits, see the burn-up, and let unfinished work roll on to the next one.">
        <SettingRow title="Use cycles" help={team.cycles_enabled ? <>See them on the <Link to={`/team/${team.key}/cycles`}>Cycles page</Link>.</> : 'Off: issues are simply in a status, without a time box.'}>
          <Switch label="Use cycles" checked={f.cycles_enabled} disabled={!admin} onChange={v => setF({ ...f, cycles_enabled: v })} />
        </SettingRow>
        {f.cycles_enabled && <>
          <SettingRow title="Length"><div className="ui-ctl-s"><Select value={f.cycle_weeks} options={num(1, 8, weeks)} onChange={v => setF({ ...f, cycle_weeks: v })} disabled={!admin} label="Cycle length" /></div></SettingRow>
          <SettingRow title="Starts on"><div className="ui-ctl-s"><Select value={f.cycle_start_day} options={[1, 2, 3, 4, 5, 6, 0].map(d => ({ value: d, label: DAYS[d] }))} onChange={v => setF({ ...f, cycle_start_day: v })} disabled={!admin} label="Start day" /></div></SettingRow>
          <SettingRow title="Cooldown" help="A break between cycles for fixes, planning and rest."><div className="ui-ctl-s"><Select value={f.cycle_cooldown_weeks} options={num(0, 4, n => n ? weeks(n) : 'None')} onChange={v => setF({ ...f, cycle_cooldown_weeks: v })} disabled={!admin} label="Cooldown" /></div></SettingRow>
          <SettingRow title="Plan ahead" help="How many upcoming cycles exist at any time, ready to plan into."><div className="ui-ctl-s"><Select value={f.cycle_upcoming} options={num(1, 6, n => `${n} ${n === 1 ? 'cycle' : 'cycles'}`)} onChange={v => setF({ ...f, cycle_upcoming: v })} disabled={!admin} label="Upcoming cycles" /></div></SettingRow>
          <SettingRow title="Roll over unfinished issues" help="When a cycle ends, its open issues move to the next one.">
            <Switch label="Roll over" checked={f.cycle_rollover} disabled={!admin} onChange={v => setF({ ...f, cycle_rollover: v })} />
          </SettingRow>
        </>}
        {admin && <div className="row mt-s">
          <button className="btn primary" disabled={!dirty || busy} onClick={save}>Save</button>
          {dirty && <button className="btn ghost" onClick={() => setF(blank)}>Reset</button>}
        </div>}
      </Section>

      {f.cycles_enabled && (
        <Section title="What it looks like" sub={mine.length ? 'Cycles already planned keep their dates; new settings apply to the cycles made after them.' : 'The first cycle starts on the latest start day up to today.'}>
          <div className="ui-cycle-preview">
            {preview.map(r => (
              <div key={r.number + r.starts} className={`ui-cycle-row ${r.status}`}>
                <Icon name="cycle" size={15} />
                <span className="ui-cycle-name">Cycle {r.number}</span>
                <span className="tnum">{wdDate(r.starts)} – {wdDate(r.ends)}</span>
                <span className="spacer" />
                {r.status === 'current' ? <span className="badge accent">Current</span> : r.status === 'upcoming' ? <span className="badge">Planned</span> : <span className="badge ok">{dirty ? 'With these settings' : 'Next'}</span>}
              </div>
            ))}
            {!preview.length && <p className="muted small" style={{ margin: 0 }}>Nothing to show yet.</p>}
          </div>
        </Section>
      )}
    </div>
  )
}

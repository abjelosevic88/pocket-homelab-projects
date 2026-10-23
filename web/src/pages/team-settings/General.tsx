import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { estimateOptions } from '../../lib/issues'
import type { EstimateScale } from '../../lib/types'
import { Icon } from '../../components/icons'
import { StatusIcon } from '../../components/issue-bits'
import { TeamMark } from '../../components/pickers'
import { type Option, Select } from '../../components/Select'
import { Confirm, errMsg, Field, Modal, SettingRow, Switch, useToast } from '../../components/ui'
import { ColorSwatches, IconPicker, Section } from '../settings/common'
import { ReadOnly, type TabProps, usePatchTeam } from './common'

const KEY_RE = /^[A-Z][A-Z0-9]{0,6}$/

const SCALES: { value: EstimateScale; label: string }[] = [
  { value: 'none', label: 'No estimates' }, { value: 'linear', label: 'Linear' }, { value: 'fibonacci', label: 'Fibonacci' },
  { value: 'exponential', label: 'Exponential' }, { value: 'tshirt', label: 'T-shirt sizes' },
]
const ARCHIVE: Option<number>[] = [
  { value: 0, label: 'Never' }, { value: 1, label: 'After 1 month' }, { value: 3, label: 'After 3 months' }, { value: 6, label: 'After 6 months' },
  { value: 9, label: 'After 9 months' }, { value: 12, label: 'After 1 year' }, { value: 24, label: 'After 2 years' },
]

export function GeneralTab({ team, admin }: TabProps) {
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const patch = usePatchTeam(team)
  const blank = { name: team.name, key: team.key, icon: team.icon, color: team.color, description: team.description }
  const [f, setF] = useState(blank)
  const [busy, setBusy] = useState(false)
  const [leaving, setLeaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  useEffect(() => { setF({ name: team.name, key: team.key, icon: team.icon, color: team.color, description: team.description }) }, [team.name, team.key, team.icon, team.color, team.description])
  const dirty = (Object.keys(blank) as (keyof typeof blank)[]).some(k => f[k] !== blank[k])
  const keyOk = KEY_RE.test(f.key)
  const keyChanged = f.key !== team.key
  const otherKey = app.teams.some(t => t.id !== team.id && t.key === f.key)
  const states = team.states || []
  const archived = !!team.archived_at

  const save = async () => {
    setBusy(true)
    const t = await patch({ ...f, name: f.name.trim() }, 'Team saved')
    setBusy(false)
    if (t && t.key !== team.key) navigate(`/team/${t.key}/settings/general`, { replace: true })
  }
  const leave = async () => {
    setLeaving(false)
    try { await api.post(`${V1}/teams/${team.key}/leave`); await app.reload(); toast(`You left ${team.name}`); navigate('/teams') } catch (e) { toast(errMsg(e), 'err') }
  }
  const scaleOpts: Option<EstimateScale>[] = SCALES.map(s => ({ value: s.value, label: s.label, hint: s.value === 'none' ? undefined : estimateOptions(s.value).map(o => o.label.replace(/ points?$/, '')).join(' · ') }))
  const stateOpts: Option<number>[] = [
    { value: 0, label: 'Automatic', description: 'The first Todo state, else the first Backlog state' },
    ...states.filter(s => s.type === 'backlog' || s.type === 'unstarted' || s.type === 'started').map(s => ({ value: s.id, label: s.name, icon: <StatusIcon state={s} /> })),
  ]

  return (
    <div className="stack loose">
      {!admin && <ReadOnly />}
      <Section title="Team">
        <form className="stack" onSubmit={e => { e.preventDefault(); if (dirty && keyOk && f.name.trim()) save() }}>
          <div className="ui-ts-identity">
            <TeamMark team={{ ...team, icon: f.icon, color: f.color, key: f.key || team.key }} size={44} />
            <div className="grow two-col">
              <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} disabled={!admin} /></Field>
              <Field label="Key" help={keyOk ? `Issues are numbered ${f.key}-1, ${f.key}-2…` : 'A letter, then up to 6 letters or digits.'}>
                <input value={f.key} onChange={e => setF({ ...f, key: e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 7) })} disabled={!admin} className={!keyOk || otherKey ? 'ui-invalid' : ''} spellCheck={false} />
              </Field>
            </div>
          </div>
          {keyChanged && keyOk && <div className="callout warn"><Icon name="alert" size={15} /><span>Identifiers change: <b>{team.key}-12</b> becomes <b>{f.key}-12</b>. The old identifiers and links keep working.{otherKey ? ` Another team already uses ${f.key}.` : ''}</span></div>}
          <Field label="Icon" group help="An emoji, or leave empty to show the key’s first letter."><IconPicker value={f.icon} onChange={v => setF({ ...f, icon: v })} disabled={!admin} /></Field>
          <Field label="Colour" group><ColorSwatches value={f.color} onChange={c => setF({ ...f, color: c })} disabled={!admin} /></Field>
          <Field label="Description"><textarea value={f.description} onChange={e => setF({ ...f, description: e.target.value })} rows={2} placeholder="What does this team work on?" disabled={!admin} /></Field>
          {admin && <div className="row">
            <button className="btn primary" disabled={!dirty || busy || !keyOk || otherKey || !f.name.trim()}>Save</button>
            {dirty && <button type="button" className="btn ghost" onClick={() => setF(blank)}>Reset</button>}
          </div>}
        </form>
      </Section>

      <Section title="Issues">
        <SettingRow title="Estimates" help="How work is sized. The values appear in the estimate picker.">
          <div className="ui-ctl-w"><Select value={team.estimate_scale} options={scaleOpts} onChange={v => patch({ estimate_scale: v }, 'Estimates saved')} disabled={!admin} label="Estimate scale" /></div>
        </SettingRow>
        <SettingRow title="Default status" help="Where new issues start.">
          <div className="ui-ctl-w"><Select value={team.default_state_id || 0} options={stateOpts} onChange={v => patch({ default_state_id: v }, 'Default status saved')} disabled={!admin} label="Default status" /></div>
        </SettingRow>
        <SettingRow title="Triage" help="Issues from guests and other Pocket apps land in Triage first, so someone can accept, merge or decline them.">
          <Switch label="Triage" checked={team.triage_enabled} disabled={!admin} onChange={v => patch({ triage_enabled: v }, v ? 'Triage is on' : 'Triage is off')} />
        </SettingRow>
        <SettingRow title="Auto-archive" help="Completed and canceled issues are archived after this long. They stay searchable and reachable by link.">
          <div className="ui-ctl-w"><Select value={team.auto_archive_months} options={ARCHIVE.some(a => a.value === team.auto_archive_months) ? ARCHIVE : [...ARCHIVE, { value: team.auto_archive_months, label: `After ${team.auto_archive_months} months` }]} onChange={v => patch({ auto_archive_months: v }, 'Saved')} disabled={!admin} label="Auto-archive" /></div>
        </SettingRow>
      </Section>

      <Section title={archived ? 'Archived team' : 'Leave, archive or delete'} className="ui-danger-zone">
        <SettingRow title="Leave the team" help={admin ? 'If you are the only admin, make someone else an admin first.' : 'You lose access to its issues and projects until someone adds you again.'}>
          <button className="btn" onClick={() => setLeaving(true)}><Icon name="log-out" size={14} />Leave</button>
        </SettingRow>
        {admin && <SettingRow title={archived ? 'Restore the team' : 'Archive the team'} help={archived ? 'Bring it back to the sidebar.' : 'Hides it from the sidebar and pickers. Nothing is deleted; restore it any time from Teams.'}>
          <button className="btn" onClick={() => patch({ archived: !archived }, archived ? 'Team restored' : 'Team archived')}><Icon name={archived ? 'undo' : 'archive'} size={14} />{archived ? 'Restore' : 'Archive'}</button>
        </SettingRow>}
        {admin && <SettingRow title="Delete the team" help="Deletes the team with every issue, comment, cycle, label and template. This can’t be undone.">
          <button className="btn danger-ghost" onClick={() => setDeleting(true)}><Icon name="trash" size={14} />Delete…</button>
        </SettingRow>}
      </Section>

      {leaving && <Confirm title={`Leave ${team.name}?`} confirmLabel="Leave team" message="You stop seeing its issues, cycles and the projects only it works on." onCancel={() => setLeaving(false)} onConfirm={leave} />}
      {deleting && <DeleteTeam onClose={() => setDeleting(false)} teamKey={team.key} name={team.name} />}
    </div>
  )
}

function DeleteTeam({ teamKey, name, onClose }: { teamKey: string; name: string; onClose: () => void }) {
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const ok = typed.trim().toUpperCase() === teamKey
  const go = async () => {
    setBusy(true)
    try {
      await api.del(`${V1}/teams/${teamKey}?confirm=${encodeURIComponent(teamKey)}`)
      await app.reload()
      toast(`${name} was deleted`)
      navigate('/teams')
    } catch (e) { toast(errMsg(e), 'err'); setBusy(false) }
  }
  return (
    <Modal title={`Delete ${name}?`} size="sm" onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>Cancel</button>
      <button className="btn danger" disabled={!ok || busy} onClick={go}>Delete team</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); if (ok) go() }}>
        <p className="confirm-msg" style={{ margin: 0 }}>Every issue, comment, attachment, cycle, label and template of the team is deleted for everyone. Projects stay if other teams work on them.</p>
        <Field label={<>Type <b>{teamKey}</b> to confirm</>}><input value={typed} onChange={e => setTyped(e.target.value)} autoFocus spellCheck={false} autoComplete="off" /></Field>
      </form>
    </Modal>
  )
}

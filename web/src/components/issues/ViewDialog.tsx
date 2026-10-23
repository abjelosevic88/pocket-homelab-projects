// Create or edit a saved view: name, description, icon, team and sharing.
import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { emitChange, useApp } from '../../lib/app-context'
import type { Display, IssueFilter, View } from '../../lib/types'
import { Icon } from '../icons'
import { TeamMark } from '../pickers'
import { Select } from '../Select'
import { Field, Modal, Switch, useToast } from '../ui'

const ICONS = ['', '⭐', '🔥', '🐛', '🚀', '📌', '🎯', '🧹', '🏠', '🛒', '📅', '✅']

export function ViewDialog({ view, filters, display, teamId, onClose, onSaved }: {
  view?: View; filters?: IssueFilter; display?: Display; teamId?: number; onClose: () => void; onSaved: (v: View) => void
}) {
  const app = useApp()
  const { toast } = useToast()
  const [name, setName] = useState(view?.name || '')
  const [description, setDescription] = useState(view?.description || '')
  const [icon, setIcon] = useState(view?.icon || '')
  const [team, setTeam] = useState<number>(view ? view.team_id || 0 : teamId || 0)
  const [shared, setShared] = useState(view?.shared || false)
  const [busy, setBusy] = useState(false)
  const t = app.team(team)
  const canShare = !!t && t.role !== 'guest'
  const save = async () => {
    if (!name.trim() || busy) return
    setBusy(true)
    try {
      const body: Record<string, unknown> = { name: name.trim(), description, icon, team_id: team, shared: canShare && shared }
      if (filters) body.filters = filters
      if (display) body.display = display
      const v = view ? await api.patch<View>(`${V1}/views/${view.id}`, body) : await api.post<View>(`${V1}/views`, body)
      emitChange({ kind: 'view', ids: [v.id] })
      await app.reload()
      onSaved(v)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={view ? 'Edit view' : 'Save as view'} onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={!name.trim() || busy} onClick={save}>{busy ? 'Saving…' : view ? 'Save' : 'Save view'}</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); save() }}>
        <Field label="Name"><input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="e.g. My open bugs" /></Field>
        <Field label="Description"><input value={description} onChange={e => setDescription(e.target.value)} placeholder="Optional" /></Field>
        <Field label="Icon" group>
          <div className="icon-pick">{ICONS.map(i => <button key={i || 'none'} type="button" className={`ip ${icon === i ? 'on' : ''}`} onClick={() => setIcon(i)} aria-label={i || 'No icon'}>{i || <Icon name="layers" size={15} />}</button>)}</div>
        </Field>
        <Field label="Team" help={team ? 'The view appears under this team.' : 'A personal view, only for you.'} group>
          <Select value={team} onChange={setTeam} options={[{ value: 0, label: 'No team (just me)', icon: <Icon name="user" size={14} /> }, ...app.teams.filter(x => !x.archived_at).map(x => ({ value: x.id, label: x.name, icon: <TeamMark team={x} /> }))]} />
        </Field>
        <label className="row" style={{ justifyContent: 'space-between' }}>
          <span><b className="small">Share with the team</b><br /><span className="muted xsmall">{canShare ? `Everyone in ${t?.name} sees it and can change it.` : 'Choose a team you are a member of to share.'}</span></span>
          <Switch checked={canShare && shared} disabled={!canShare} onChange={setShared} label="Share with the team" />
        </label>
        <button type="submit" hidden />
      </form>
    </Modal>
  )
}

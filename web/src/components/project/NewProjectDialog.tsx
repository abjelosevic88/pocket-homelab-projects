// "New project": name, summary, icon and colour, teams, lead, members,
// status, priority, dates, update cadence and optional first milestones.
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { shortDate } from '../../lib/dates'
import { priorityLabel } from '../../lib/issues'
import type { Project, ProjectStatus } from '../../lib/types'
import { Icon } from '../icons'
import { PersonAvatar, PriorityIcon, ProjectMark, ProjectStatusIcon, PROJECT_STATUS_LABEL } from '../issue-bits'
import { DateMenu, PickerMenu, TeamMark, useItems } from '../pickers'
import { type Anchor, Modal, useToast } from '../ui'
import { AvatarStack, CADENCES, cadenceLabel, IconPicker, peopleItems, priorityItems, PROJECT_COLORS, PropChip, statusItems, useTeamPeople } from './common'

type Open = 'icon' | 'status' | 'priority' | 'lead' | 'members' | 'teams' | 'start' | 'target' | 'cadence' | `ms-${number}`

export function NewProjectDialog({ onClose, defaultTeamId, defaultStatus }: { onClose: () => void; defaultTeamId?: number; defaultStatus?: ProjectStatus }) {
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const items = useItems()
  const writable = app.teams.filter(t => !t.archived_at && app.canEdit(t.id))
  const firstTeam = (defaultTeamId && app.canEdit(defaultTeamId) ? defaultTeamId : undefined)
    ?? (app.settings?.default_team_id && app.canEdit(app.settings.default_team_id) ? app.settings.default_team_id : undefined)
    ?? writable[0]?.id
  const [name, setName] = useState('')
  const [summary, setSummary] = useState('')
  const [icon, setIcon] = useState('')
  const [color, setColor] = useState(() => PROJECT_COLORS[Math.floor(Math.random() * PROJECT_COLORS.length)])
  const [teamIds, setTeamIds] = useState<number[]>(firstTeam ? [firstTeam] : [])
  const [leadId, setLeadId] = useState<number>(app.user?.id || 0)
  const [memberIds, setMemberIds] = useState<number[]>([])
  const [status, setStatus] = useState<ProjectStatus>(defaultStatus || 'planned')
  const [priority, setPriority] = useState(0)
  const [start, setStart] = useState('')
  const [target, setTarget] = useState('')
  const [cadence, setCadence] = useState(7)
  const [milestones, setMilestones] = useState<{ name: string; target_date: string }[]>([])
  const [open, setOpen] = useState<{ kind: Open; anchor: Anchor } | null>(null)
  const [busy, setBusy] = useState(false)
  const people = useTeamPeople(teamIds)
  const lead = app.person(leadId)
  const show = (kind: Open) => (a: Anchor) => setOpen({ kind, anchor: a })
  const peopleIds = useMemo(() => new Set(people.map(p => p.id)), [people])

  const submit = async () => {
    if (!name.trim()) { toast('Give the project a name', 'err'); return }
    if (!teamIds.length) { toast('Choose at least one team', 'err'); return }
    if (start && target && target < start) { toast('The target date is before the start date', 'err'); return }
    setBusy(true)
    try {
      const p = await api.post<Project>(`${V1}/projects`, {
        name: name.trim(), summary: summary.trim(), icon, color, team_ids: teamIds, lead_id: leadId && peopleIds.has(leadId) ? leadId : 0,
        member_ids: memberIds.filter(id => peopleIds.has(id)), status, priority, start_date: start, target_date: target, update_cadence_days: cadence,
        milestones: milestones.filter(m => m.name.trim()),
      })
      await app.reload()
      toast(<>Created “{p.name}”</>)
      onClose()
      navigate(`/project/${p.id}`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }

  return (
    <Modal onClose={onClose} bare size="lg" className="np-modal" labelledBy="np-title">
      <form onSubmit={e => { e.preventDefault(); submit() }} onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); submit() } }}>
        <div className="np-head">
          <span className="np-crumb" id="np-title">New project</span>
          <span className="spacer" />
          <button type="button" className="btn ghost icon-only sm" onClick={onClose} aria-label="Close"><Icon name="x" size={17} /></button>
        </div>
        <div className="np-body">
          <button type="button" className="np-icon" aria-label="Choose an icon and colour" title="Icon and colour" onClick={e => show('icon')({ x: e.currentTarget.getBoundingClientRect().left, y: e.currentTarget.getBoundingClientRect().top, w: 40, h: 40 })}>
            <ProjectMark icon={icon} color={color} size={26} />
          </button>
          <input className="np-name" autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="Project name" aria-label="Project name" maxLength={120} />
          <input className="np-summary" value={summary} onChange={e => setSummary(e.target.value)} placeholder="Add a short summary…" aria-label="Summary" maxLength={240} />
          <div className="np-props">
            <PropChip icon={<ProjectStatusIcon status={status} />} label={PROJECT_STATUS_LABEL[status]} onOpen={show('status')} title="Status" />
            <PropChip icon={<PriorityIcon priority={priority} />} label={priority ? priorityLabel(priority) : 'Priority'} onOpen={show('priority')} title="Priority" />
            <PropChip icon={<PersonAvatar person={lead} size={16} />} label={lead ? (lead.name || lead.email) : 'Lead'} onOpen={show('lead')} title="Lead" />
            <PropChip icon={memberIds.length ? <AvatarStack ids={memberIds} size={16} max={3} /> : <Icon name="users" size={14} />} label={memberIds.length ? `${memberIds.length} member${memberIds.length > 1 ? 's' : ''}` : 'Members'} onOpen={show('members')} title="Members" />
            <PropChip icon={teamIds.length ? <span className="team-marks">{teamIds.map(id => <TeamMark key={id} team={app.team(id)} size={15} />)}</span> : <Icon name="users" size={14} />}
              label={teamIds.length === 1 ? app.team(teamIds[0])?.name : teamIds.length ? `${teamIds.length} teams` : 'Teams'} onOpen={show('teams')} title="Teams" className={!teamIds.length ? 'warn' : ''} />
            <PropChip icon={<Icon name="calendar" size={14} />} label={start ? `Start ${shortDate(start)}` : 'Start date'} onOpen={show('start')} title="Start date" />
            <PropChip icon={<Icon name="target" size={14} />} label={target ? `Target ${shortDate(target)}` : 'Target date'} onOpen={show('target')} title="Target date" />
            <PropChip icon={<Icon name="bell" size={14} />} label={cadenceLabel(cadence)} onOpen={show('cadence')} title="Update reminders for the lead" />
          </div>
          <div className="np-ms">
            {milestones.map((m, i) => (
              <div key={i} className="np-ms-row">
                <Icon name="diamond" size={13} className="ms-ico" />
                <input value={m.name} placeholder="Milestone name" aria-label="Milestone name" autoFocus={!m.name} onChange={e => setMilestones(ms => ms.map((x, j) => j === i ? { ...x, name: e.target.value } : x))} />
                <PropChip icon={<Icon name="calendar" size={13} />} label={m.target_date ? shortDate(m.target_date) : 'Target'} onOpen={show(`ms-${i}`)} />
                <button type="button" className="btn ghost icon-only sm" aria-label="Remove milestone" onClick={() => setMilestones(ms => ms.filter((_, j) => j !== i))}><Icon name="x" size={14} /></button>
              </div>
            ))}
            <button type="button" className="btn ghost sm np-add-ms" onClick={() => setMilestones(ms => [...ms, { name: '', target_date: '' }])}><Icon name="diamond" size={13} />Add milestone</button>
          </div>
        </div>
        <div className="np-foot">
          <span className="faint xsmall hide-mobile">{teamIds.length ? 'Visible to the members of ' + teamIds.map(id => app.team(id)?.name).join(', ') : 'Pick a team'}</span>
          <span className="spacer" />
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={busy || !name.trim() || !teamIds.length}>{busy ? 'Creating…' : 'Create project'}</button>
        </div>
      </form>
      {open?.kind === 'icon' && <IconPicker anchor={open.anchor} icon={icon} color={color} onChange={v => { if (v.icon !== undefined) setIcon(v.icon); if (v.color) setColor(v.color) }} onClose={() => setOpen(null)} />}
      {open?.kind === 'status' && <PickerMenu anchor={open.anchor} items={statusItems()} selected={status} onPick={setStatus} onClose={() => setOpen(null)} placeholder="Status…" />}
      {open?.kind === 'priority' && <PickerMenu anchor={open.anchor} items={priorityItems()} selected={priority} onPick={setPriority} onClose={() => setOpen(null)} placeholder="Priority…" />}
      {open?.kind === 'lead' && <PickerMenu anchor={open.anchor} items={peopleItems(people, app.user?.id, 'No lead')} selected={leadId} onPick={setLeadId} onClose={() => setOpen(null)} placeholder="Lead…" />}
      {open?.kind === 'members' && <PickerMenu anchor={open.anchor} multi items={peopleItems(people, app.user?.id)} selected={memberIds} onPick={v => setMemberIds(ids => ids.includes(v) ? ids.filter(x => x !== v) : [...ids, v])} onClose={() => setOpen(null)} placeholder="Members…" />}
      {open?.kind === 'teams' && <PickerMenu anchor={open.anchor} multi items={items.teams(true)} selected={teamIds} onPick={v => setTeamIds(ids => ids.includes(v) ? ids.filter(x => x !== v) : [...ids, v])} onClose={() => setOpen(null)} placeholder="Teams…" />}
      {open?.kind === 'start' && <DateMenu anchor={open.anchor} value={start} title="Start date" onPick={setStart} onClose={() => setOpen(null)} />}
      {open?.kind === 'target' && <DateMenu anchor={open.anchor} value={target} title="Target date" onPick={setTarget} onClose={() => setOpen(null)} />}
      {open?.kind === 'cadence' && <PickerMenu anchor={open.anchor} items={CADENCES.map(c => ({ value: c.value, label: c.label, icon: <Icon name={c.value ? 'bell' : 'bell-off'} size={14} /> }))} selected={cadence} onPick={setCadence} onClose={() => setOpen(null)} placeholder="Update reminders…" />}
      {open && open.kind.startsWith('ms-') && (() => {
        const i = Number(open.kind.slice(3))
        return <DateMenu anchor={open.anchor} value={milestones[i]?.target_date || ''} title="Milestone target" onPick={v => setMilestones(ms => ms.map((x, j) => j === i ? { ...x, target_date: v } : x))} onClose={() => setOpen(null)} />
      })()}
    </Modal>
  )
}

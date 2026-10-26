// The project overview: summary, description, progress, milestones,
// resources, stakeholders, related notes and meetings, and the properties panel.
import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useApp } from '../../lib/app-context'
import { diffDays, shortDate } from '../../lib/dates'
import { Markdown } from '../../lib/markdown'
import { priorityLabel } from '../../lib/issues'
import type { ProjectDetail } from '../../lib/types'
import { Icon } from '../icons'
import { HealthBadge, PersonAvatar, PriorityIcon, ProjectStatusIcon, PROJECT_STATUS_LABEL } from '../issue-bits'
import { MarkdownEditor } from '../MarkdownEditor'
import { DateMenu, PickerMenu, TeamMark, useItems } from '../pickers'
import { type Anchor, errMsg, Modal, rectOf, useIsMobile, useToast } from '../ui'
import { FolderInput } from '../FolderInput'
import { api, V1 } from '../../lib/api'
import { CADENCES, cadenceLabel, peopleItems, ProjectPicker, useTeamPeople } from './common'
import { Milestones } from './Milestones'
import { Related, Resources, Stakeholders } from './Resources'
import { ProgressCard } from './ProgressCard'

type Save = (b: Record<string, unknown>) => Promise<void>

export function Overview({ p, save, reload, onMeeting }: { p: ProjectDetail; save: Save; reload: () => void; onMeeting: () => void }) {
  return (
    <div className="ov">
      <div className="ov-main">
        <SummaryInput p={p} save={save} />
        <Description p={p} save={save} />
        <ProgressCard p={p} />
        <Milestones p={p} reload={reload} />
        <Resources p={p} reload={reload} />
        <Stakeholders p={p} reload={reload} onMeeting={onMeeting} />
        <Related p={p} onMeeting={onMeeting} />
      </div>
      <aside className="ov-side"><Properties p={p} save={save} /></aside>
    </div>
  )
}

function SummaryInput({ p, save }: { p: ProjectDetail; save: Save }) {
  const [v, setV] = useState(p.summary)
  const ref = useRef<HTMLTextAreaElement>(null)
  useEffect(() => { if (document.activeElement !== ref.current) setV(p.summary) }, [p.summary])
  useEffect(() => { const el = ref.current; if (el) { el.style.height = 'auto'; el.style.height = el.scrollHeight + 'px' } }, [v])
  return <textarea ref={ref} className="ov-summary" rows={1} value={v} disabled={!p.can_edit} placeholder={p.can_edit ? 'Add a short summary…' : ''} aria-label="Summary" maxLength={240}
    onChange={e => setV(e.target.value.replace(/\n/g, ' '))} onBlur={() => { if (v.trim() !== p.summary) save({ summary: v.trim() }) }}
    onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); ref.current?.blur() } }} />
}

/** The project's description (Markdown), shown rendered; click to edit, saved as you type. */
function Description({ p, save }: { p: ProjectDetail; save: Save }) {
  const [editing, setEditing] = useState(false)
  const [text, setText] = useState(p.content)
  const [state, setState] = useState<'' | 'saving' | 'saved'>('')
  const timer = useRef(0)
  const last = useRef(p.content)
  useEffect(() => { if (!editing) { setText(p.content); last.current = p.content } }, [p.content, editing])
  const flush = async (t: string) => {
    window.clearTimeout(timer.current)
    if (t === last.current) return
    last.current = t
    setState('saving')
    await save({ content: t })
    setState('saved')
  }
  const change = (t: string) => { setText(t); window.clearTimeout(timer.current); timer.current = window.setTimeout(() => flush(t), 900) }
  useEffect(() => () => window.clearTimeout(timer.current), [])
  return (
    <section className="ov-sec ov-desc">
      {editing ? <>
        <MarkdownEditor value={text} onChange={change} autoFocus minRows={6} teamId={p.team_ids[0]} placeholder="What is this project about? Goals, scope, links…"
          onBlur={() => { flush(text) }} onSubmit={() => { flush(text); setEditing(false) }} />
        <div className="row ov-desc-foot">
          <span className="xsmall faint">{state === 'saving' ? 'Saving…' : state === 'saved' ? 'Saved' : 'Saves as you type'}</span>
          <span className="spacer" />
          <button type="button" className="btn sm" onClick={() => { flush(text); setEditing(false) }}>Done</button>
        </div>
      </> : p.content.trim() ? (
        <div className={`ov-desc-view ${p.can_edit ? 'editable' : ''}`} onClick={e => { if (p.can_edit && !(e.target as Element).closest('a, input')) setEditing(true) }} title={p.can_edit ? 'Click to edit' : undefined}>
          <Markdown text={p.content} />
        </div>
      ) : p.can_edit ? (
        <button type="button" className="ov-desc-empty" onClick={() => setEditing(true)}><Icon name="edit" size={14} />Write a description: goals, scope, links…</button>
      ) : null}
    </section>
  )
}

// ---------- properties panel ----------

type PropKind = 'status' | 'priority' | 'lead' | 'members' | 'teams' | 'start' | 'target' | 'cadence'

function Properties({ p, save }: { p: ProjectDetail; save: Save }) {
  const app = useApp()
  const items = useItems()
  const people = useTeamPeople(p.team_ids)
  const [open, setOpen] = useState<{ kind: PropKind; anchor: Anchor } | null>(null)
  const mobile = useIsMobile()
  const [all, setAll] = useState(false)
  const brief = mobile && !all
  const show = (kind: PropKind) => (e: React.MouseEvent) => { if (p.can_edit) setOpen({ kind, anchor: rectOf(e.currentTarget) }) }
  const lead = app.person(p.lead_id)
  const close = () => setOpen(null)
  const toggleMember = (uid: number) => save({ member_ids: p.member_ids.includes(uid) ? p.member_ids.filter(x => x !== uid) : [...p.member_ids, uid] })
  const toggleTeam = (tid: number) => {
    const next = p.team_ids.includes(tid) ? p.team_ids.filter(x => x !== tid) : [...p.team_ids, tid]
    if (next.length) save({ team_ids: next })
  }
  const Row = ({ label, kind, children }: { label: string; kind?: PropKind; children: React.ReactNode }) => (
    <div className="prop-row">
      <span className="prop-label">{label}</span>
      {kind && p.can_edit ? <button type="button" className="prop-val" onClick={show(kind)}>{children}</button> : <span className="prop-val static">{children}</span>}
    </div>
  )
  return (
    <div className="ov-props">
      <div className="section-title">Properties</div>
      {!brief && <Row label="Status" kind="status"><ProjectStatusIcon status={p.status} />{PROJECT_STATUS_LABEL[p.status]}</Row>}
      <div className="prop-row">
        <span className="prop-label">Health</span>
        <Link className="prop-val" to={`/project/${p.id}/updates`} title={p.last_update ? 'From the latest update' : 'Post an update to set the health'}>
          <HealthBadge health={p.health} />{p.last_update && <span className="faint xsmall">· {relUpdate(p.last_update.created_at)}</span>}
        </Link>
      </div>
      {!brief && <Row label="Priority" kind="priority"><PriorityIcon priority={p.priority} />{p.priority ? priorityLabel(p.priority) : 'No priority'}</Row>}
      <Row label="Lead" kind="lead"><PersonAvatar person={lead} size={18} />{lead ? lead.name || lead.email : 'No lead'}</Row>
      {!brief && <>
      <div className="prop-row top">
        <span className="prop-label">Members</span>
        <div className="prop-people">
          {p.member_ids.map(id => { const m = app.person(id); return m ? <span key={id} className="person-pill" title={m.email}><PersonAvatar person={m} size={18} /><span className="ellipsis">{m.name || m.email}</span>
            {p.can_edit && <button type="button" aria-label={`Remove ${m.name}`} onClick={() => toggleMember(id)}><Icon name="x" size={11} /></button>}</span> : null })}
          {p.can_edit && <button type="button" className="btn ghost xs add-person" aria-label="Add members" title="Add members" onClick={show('members')}><Icon name="user-plus" size={14} /></button>}
          {!p.member_ids.length && !p.can_edit && <span className="faint">No members</span>}
        </div>
      </div>
      <div className="prop-row top">
        <span className="prop-label">Teams</span>
        <div className="prop-people">
          {p.team_ids.map(id => { const t = app.team(id); return t ? <Link key={id} className="person-pill" to={`/team/${t.key}/projects`}><TeamMark team={t} size={16} /><span className="ellipsis">{t.name}</span></Link> : null })}
          {p.can_edit && <button type="button" className="btn ghost xs add-person" aria-label="Change teams" title="Teams" onClick={show('teams')}><Icon name="plus" size={14} /></button>}
        </div>
      </div>
      </>}
      {!brief && <Row label="Start" kind="start"><Icon name="calendar" size={14} className="muted" />{p.start_date ? shortDate(p.start_date) : <span className="faint">Not set</span>}</Row>}
      <Row label="Target" kind="target"><Icon name="target" size={14} className="muted" />{p.target_date ? <span className={!['completed', 'canceled'].includes(p.status) && p.target_date < (app.boot?.today || '') ? 'danger-text' : ''}>{shortDate(p.target_date)}</span> : <span className="faint">Not set</span>}</Row>
      <MiniTimeline p={p} today={app.boot?.today || ''} />
      {!brief && <Row label="Updates" kind="cadence"><Icon name={p.update_cadence_days ? 'bell' : 'bell-off'} size={14} className="muted" />{cadenceLabel(p.update_cadence_days)}</Row>}
      {!brief && <CodeFolder p={p} />}
      {mobile && <button type="button" className="btn ghost sm props-more" onClick={() => setAll(a => !a)}><Icon name={all ? 'chevron-up' : 'chevron-down'} size={14} />{all ? 'Fewer properties' : 'All properties'}</button>}

      {open && (open.kind === 'status' || open.kind === 'priority' || open.kind === 'lead') && <ProjectPicker kind={open.kind} project={p} anchor={open.anchor} onPatch={save} onClose={close} />}
      {open?.kind === 'members' && <PickerMenu anchor={open.anchor} multi items={peopleItems(people, app.user?.id)} selected={p.member_ids} onPick={toggleMember} onClose={close} placeholder="Add or remove members…" />}
      {open?.kind === 'teams' && <PickerMenu anchor={open.anchor} multi items={[...items.teams(true), ...p.team_ids.filter(id => !app.canEdit(id)).map(id => ({ value: id, label: app.team(id)?.name || 'Team' }))]} selected={p.team_ids} onPick={toggleTeam} onClose={close} placeholder="Teams…" />}
      {open?.kind === 'start' && <DateMenu anchor={open.anchor} value={p.start_date} title="Start date" onPick={v => save({ start_date: v })} onClose={close} />}
      {open?.kind === 'target' && <DateMenu anchor={open.anchor} value={p.target_date} title="Target date" onPick={v => save({ target_date: v })} onClose={close} />}
      {open?.kind === 'cadence' && <PickerMenu anchor={open.anchor} items={CADENCES.map(c => ({ value: c.value, label: c.label, icon: <Icon name={c.value ? 'bell' : 'bell-off'} size={14} /> }))} selected={p.update_cadence_days} onPick={v => save({ update_cadence_days: v })} onClose={close} placeholder="Remind the lead…" />}
    </div>
  )
}

/** The project's own code folder for "Code with Pocket AI" (else the team's). */
function CodeFolder({ p }: { p: ProjectDetail }) {
  const { toast } = useToast()
  const [cwd, setCwd] = useState<string | null>(null)
  const [host, setHost] = useState(0)
  const [edit, setEdit] = useState(false)
  useEffect(() => { api.get<{ cwd: string; host_id?: number }>(`${V1}/projects/${p.id}/ai`).then(r => { setCwd(r.cwd); setHost(r.host_id || 0) }).catch(() => setCwd('')) }, [p.id])
  const save = async (v: string) => {
    setEdit(false)
    try { const r = await api.put<{ cwd: string }>(`${V1}/projects/${p.id}/ai`, { cwd: v }); setCwd(r.cwd); toast(r.cwd ? 'Code folder saved' : 'Uses the team’s code folder') } catch (e) { toast(errMsg(e), 'err') }
  }
  if (cwd === null) return null
  const val = <><Icon name="code" size={14} className="muted" />{cwd ? <span className="ellipsis mono" title={cwd}>{cwd}</span> : <span className="faint">Team’s folder</span>}</>
  return (
    <div className="prop-row">
      <span className="prop-label">Code</span>
      {p.can_edit ? <button type="button" className="prop-val" onClick={() => setEdit(true)} title="Folder for Code with Pocket AI">{val}</button> : <span className="prop-val static">{val}</span>}
      {edit && <CodeFolderDialog value={cwd} host={host} onDone={save} onClose={() => setEdit(false)} />}
    </div>
  )
}

function CodeFolderDialog({ value, host, onDone, onClose }: { value: string; host: number; onDone: (v: string) => void; onClose: () => void }) {
  const [v, setV] = useState(value)
  return (
    <Modal title="Code folder" onClose={onClose} size="sm">
      <form onSubmit={e => { e.preventDefault(); onDone(v.trim().replace(/(.)\/+$/, '$1')) }} className="stack">
        <FolderInput value={v} onChange={setV} host={host} autoFocus />
        <p className="muted xsmall">Where “Code with Pocket AI” opens for this project’s issues. Empty = the team’s folder (Team settings → Pocket AI).</p>
        {value && <button type="button" className="btn ghost sm" onClick={() => onDone('')}>Use the team’s folder</button>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="submit" className="btn primary">Save</button></div>
      </form>
    </Modal>
  )
}

function relUpdate(t: string) {
  const days = Math.floor((Date.now() - new Date(t).getTime()) / 86400000)
  return days <= 0 ? 'today' : days === 1 ? 'yesterday' : `${days} days ago`
}

/** Start → target with a marker for today. */
function MiniTimeline({ p, today }: { p: ProjectDetail; today: string }) {
  if (!p.start_date || !p.target_date) return null
  const total = Math.max(1, diffDays(p.target_date, p.start_date))
  const at = Math.min(1, Math.max(0, diffDays(today, p.start_date) / total))
  const left = diffDays(p.target_date, today)
  const done = p.status === 'completed' || p.status === 'canceled'
  return (
    <div className="mini-tl" title={`${shortDate(p.start_date)} → ${shortDate(p.target_date)}`}>
      <div className="mini-tl-bar"><span className="mini-tl-fill" style={{ width: `${at * 100}%` }} /><span className="mini-tl-now" style={{ left: `${at * 100}%` }} /></div>
      <div className="mini-tl-legend xsmall">
        <span>{Math.round(at * 100)}% of the time used</span>
        <span className={!done && left < 0 ? 'danger-text' : 'muted'}>{done ? '' : left > 0 ? `${left} days left` : left === 0 ? 'Due today' : `${-left} days over`}</span>
      </div>
    </div>
  )
}

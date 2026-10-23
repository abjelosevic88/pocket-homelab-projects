// The "New issue" dialog: team, template, title, description, every property
// as a chip, Pocket AI to write the description, "create more", a kept draft.
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { emitChange, useApp } from '../lib/app-context'
import { estimateLabel, priorityLabel } from '../lib/issues'
import { shortDate } from '../lib/dates'
import type { Issue, Template } from '../lib/types'
import { Icon } from './icons'
import { MarkdownEditor } from './MarkdownEditor'
import { LabelPill, PersonAvatar, PriorityIcon, ProjectMark, StatusIcon, personName } from './issue-bits'
import { PickerMenu, PropertyPicker, TeamMark, useItems, type Prop } from './pickers'
import { type Anchor, Modal, Switch, rectOf, useToast } from './ui'

export interface IssueDefaults {
  team_id?: number; state_id?: number; priority?: number; assignee_id?: number | null; label_ids?: number[]; project_id?: number | null
  milestone_id?: number | null; cycle_id?: number | null; parent_id?: number | null; parent_identifier?: string; due_date?: string; estimate?: number | null
  title?: string; description?: string
}

/** Open the dialog from anywhere. */
export function openCreateIssue(defaults: IssueDefaults = {}) { window.dispatchEvent(new CustomEvent<IssueDefaults>('pp:new-issue', { detail: defaults })) }

const DRAFT = 'pp-issue-draft'

export function CreateIssueModal({ defaults, onClose }: { defaults: IssueDefaults; onClose: () => void }) {
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const items = useItems()
  const writable = app.teams.filter(t => !t.archived_at && t.role !== 'guest')
  const firstTeam = defaults.team_id || app.settings?.default_team_id || writable[0]?.id || 0
  const saved = useMemo(() => { try { return JSON.parse(localStorage.getItem(DRAFT) || 'null') } catch { return null } }, [])
  const [teamId, setTeamId] = useState<number>(writable.some(t => t.id === firstTeam) ? firstTeam : writable[0]?.id || 0)
  const team = app.team(teamId)
  const defaultState = () => team?.default_state_id || team?.states?.find(s => s.type === 'unstarted')?.id || team?.states?.[0]?.id || 0
  const [d, setD] = useState({
    title: defaults.title ?? (saved?.title || ''), description: defaults.description ?? (saved?.description || ''),
    state_id: defaults.state_id || 0, priority: defaults.priority ?? 0, assignee_id: defaults.assignee_id ?? null,
    label_ids: defaults.label_ids || [], project_id: defaults.project_id ?? null, milestone_id: defaults.milestone_id ?? null,
    cycle_id: defaults.cycle_id ?? null, due_date: defaults.due_date || '', estimate: defaults.estimate ?? null,
  })
  // The team's default assignee (Team settings → General) fills an untouched assignee
  const teamDefaultAssignee = (): number | null => {
    const v = team?.default_assignee || 'creator'
    if (v === 'none') return null
    if (v === 'creator') return team?.role && team.role !== 'guest' ? app.user?.id ?? null : null
    const id = Number(v)
    return id > 0 && (!team?.members || team.members.some(m => m.id === id)) ? id : null
  }
  const assigneeTouched = useRef(defaults.assignee_id !== undefined)
  useEffect(() => {
    if (!assigneeTouched.current) setD(x => ({ ...x, assignee_id: teamDefaultAssignee() }))
  }, [teamId, team?.default_assignee]) // eslint-disable-line react-hooks/exhaustive-deps
  const [templateId, setTemplateId] = useState(0)
  const [more, setMore] = useState(false)
  const [busy, setBusy] = useState(false)
  const [ai, setAi] = useState(false)
  const [prop, setProp] = useState<{ p: Prop | 'team' | 'template'; at: Anchor } | null>(null)
  const titleRef = useRef<HTMLInputElement>(null)
  const stateId = d.state_id && team?.states?.some(s => s.id === d.state_id) ? d.state_id : defaultState()

  // Keep a draft of what was typed (not when prefilled from somewhere).
  useEffect(() => {
    if (defaults.title !== undefined) return
    if (d.title || d.description) localStorage.setItem(DRAFT, JSON.stringify({ title: d.title, description: d.description }))
    else localStorage.removeItem(DRAFT)
  }, [d.title, d.description, defaults.title])

  const templates = (app.boot?.templates || []).filter(t => t.team_id === teamId)
  const applyTemplate = (t: Template) => {
    setTemplateId(t.id)
    if (t.data.assignee_id != null) assigneeTouched.current = true
    setD(x => ({ ...x, title: x.title || t.title, description: x.description || t.description, state_id: t.data.state_id || x.state_id, priority: t.data.priority ?? x.priority,
      assignee_id: t.data.assignee_id ?? x.assignee_id, project_id: t.data.project_id ?? x.project_id, label_ids: t.data.label_ids?.length ? t.data.label_ids : x.label_ids, estimate: t.data.estimate ?? x.estimate }))
  }

  const submit = async () => {
    if (!d.title.trim() || !teamId || busy) return
    setBusy(true)
    try {
      const body: Record<string, unknown> = { team_id: teamId, title: d.title.trim(), description: d.description, state_id: stateId, priority: d.priority,
        assignee_id: d.assignee_id || 0, label_ids: d.label_ids, project_id: d.project_id || 0, milestone_id: d.milestone_id || 0, cycle_id: d.cycle_id || 0,
        due_date: d.due_date, estimate: d.estimate }
      if (defaults.parent_id) body.parent_id = defaults.parent_id
      if (templateId) body.template_id = templateId
      const is = await api.post<Issue>(`${V1}/issues`, body)
      localStorage.removeItem(DRAFT)
      emitChange({ kind: 'issue', team_ids: [is.team_id], ids: [is.id] })
      toast(<>Created <b>{is.identifier}</b></>, 'ok', { label: 'Open', run: () => navigate(`/issue/${is.identifier}`) })
      if (more) { setD(x => ({ ...x, title: '', description: '' })); setTemplateId(0); titleRef.current?.focus() } else onClose()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }

  const writeWithAI = async () => {
    if (!d.title.trim()) { toast('Write a title first', 'err'); return }
    setAi(true)
    try {
      const r = await api.post<{ description: string }>(`${V1}/ai/describe`, { title: d.title, description: d.description, team_id: teamId, project_id: d.project_id || 0 })
      setD(x => ({ ...x, description: r.description }))
    } catch (e) { toast((e as Error).message, 'err') } finally { setAi(false) }
  }

  const draftIssue = { team_id: teamId, state_id: stateId, priority: d.priority, assignee_id: d.assignee_id, label_ids: d.label_ids, project_id: d.project_id, milestone_id: d.milestone_id, cycle_id: d.cycle_id, estimate: d.estimate, due_date: d.due_date }
  const open = (p: Prop | 'team' | 'template') => (e: React.MouseEvent) => setProp({ p, at: rectOf(e.currentTarget) })
  const state = app.state(stateId)
  const project = app.project(d.project_id)
  const cycle = app.cycle(d.cycle_id)

  if (!writable.length) return (
    <Modal title="New issue" onClose={onClose} size="sm">
      <p className="confirm-msg">You are not a member of any team yet. Create a team first — issues always belong to a team.</p>
      <div className="form-actions"><button className="btn primary" onClick={() => { onClose(); navigate('/teams/new') }}>Create a team</button></div>
    </Modal>
  )

  return (
    <Modal onClose={onClose} size="lg" className="create-issue" bare>
      <div className="ci-head">
        <button type="button" className="chip team-chip" onClick={open('team')}><TeamMark team={team} />{team?.key}</button>
        <Icon name="chevron-right" size={13} className="faint" />
        <span className="ci-crumb">{defaults.parent_identifier ? <>Sub-issue of <b>{defaults.parent_identifier}</b></> : 'New issue'}</span>
        {templates.length > 0 && <button type="button" className="chip" onClick={open('template')}><Icon name="file-text" size={13} />{templates.find(t => t.id === templateId)?.name || 'Template'}</button>}
        <span className="spacer" />
        <button type="button" className="btn ghost icon-only sm" onClick={onClose} aria-label="Close"><Icon name="x" size={16} /></button>
      </div>
      <div className="ci-body">
        <input ref={titleRef} className="ci-title bare" placeholder="Issue title" value={d.title} autoFocus onChange={e => setD(x => ({ ...x, title: e.target.value }))}
          onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); submit() } }} />
        <MarkdownEditor value={d.description} onChange={v => setD(x => ({ ...x, description: v }))} onSubmit={submit} placeholder="Add a description… (Markdown, @ to mention)" teamId={teamId} minRows={4} bare />
      </div>
      <div className="ci-props">
        <button type="button" className="chip" onClick={open('state')}><StatusIcon state={state} />{state?.name || 'Status'}</button>
        <button type="button" className="chip" onClick={open('priority')}><PriorityIcon priority={d.priority} />{d.priority ? priorityLabel(d.priority) : 'Priority'}</button>
        <button type="button" className="chip" onClick={open('assignee')}><PersonAvatar person={app.person(d.assignee_id)} size={16} />{d.assignee_id ? personName(app.person(d.assignee_id)) : 'Assignee'}</button>
        <button type="button" className="chip" onClick={open('labels')}>{d.label_ids.length ? d.label_ids.map(id => { const l = app.label(id); return l ? <LabelPill key={id} name={l.name} color={l.color} /> : null }) : <><Icon name="tag" size={13} />Labels</>}</button>
        <button type="button" className="chip" onClick={open('project')}>{project ? <><ProjectMark icon={project.icon} color={project.color} size={13} />{project.name}</> : <><Icon name="briefcase" size={13} />Project</>}</button>
        {project && <button type="button" className="chip" onClick={open('milestone')}><Icon name="diamond" size={12} />{app.milestone(d.milestone_id)?.name || 'Milestone'}</button>}
        {team?.cycles_enabled && <button type="button" className="chip" onClick={open('cycle')}><Icon name="cycle" size={13} />{cycle ? cycle.name || `Cycle ${cycle.number}` : 'Cycle'}</button>}
        {team?.estimate_scale !== 'none' && <button type="button" className="chip" onClick={open('estimate')}><Icon name="triangle" size={12} />{d.estimate !== null ? estimateLabel(team?.estimate_scale, d.estimate) : 'Estimate'}</button>}
        <button type="button" className="chip" onClick={open('due')}><Icon name="calendar" size={13} />{d.due_date ? shortDate(d.due_date) : 'Due date'}</button>
      </div>
      <div className="ci-foot">
        <button type="button" className="btn ghost sm" onClick={writeWithAI} disabled={ai} title="Pocket AI writes a description with acceptance criteria"><Icon name="sparkles" size={14} />{ai ? 'Writing…' : 'Write with AI'}</button>
        <span className="spacer" />
        <label className="row small muted" style={{ gap: 8 }}><Switch checked={more} onChange={setMore} size="sm" />Create more</label>
        <button type="button" className="btn primary" disabled={!d.title.trim() || busy} onClick={submit}>{busy ? 'Creating…' : 'Create issue'}<kbd className="kbd-on-primary">⌘↵</kbd></button>
      </div>
      {prop?.p === 'team' && <PickerMenu anchor={prop.at} items={items.teams()} selected={teamId} onPick={v => { setTeamId(v); setD(x => ({ ...x, state_id: 0, label_ids: [], cycle_id: null })); setTemplateId(0) }} onClose={() => setProp(null)} />}
      {prop?.p === 'template' && <PickerMenu anchor={prop.at} items={templates.map(t => ({ value: t.id, label: t.name, icon: <Icon name="file-text" size={13} />, hint: t.recur ? 'repeats' : undefined }))} selected={templateId}
        onPick={v => { const t = templates.find(x => x.id === v); if (t) applyTemplate(t) }} onClose={() => setProp(null)} />}
      {prop && prop.p !== 'team' && prop.p !== 'template' && <PropertyPicker prop={prop.p} issue={draftIssue} anchor={prop.at} onClose={() => setProp(null)}
        onPatch={p => setD(x => {
          if ('assignee_id' in p) assigneeTouched.current = true
          const n = { ...x, ...p } as typeof x & Record<string, unknown>
          for (const k of ['assignee_id', 'project_id', 'milestone_id', 'cycle_id'] as const) if (n[k] === 0) (n as Record<string, unknown>)[k] = null
          if ('project_id' in p) n.milestone_id = null
          return n
        })} />}
    </Modal>
  )
}

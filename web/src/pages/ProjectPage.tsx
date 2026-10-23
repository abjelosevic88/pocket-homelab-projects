// One project: header with its properties, and tabs for the overview,
// issues, status updates, documents, activity and insights.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Navigate, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import '../styles/projects-ui.css'
import { api, ApiError, V1 } from '../lib/api'
import { useApp, useLive } from '../lib/app-context'
import { priorityLabel } from '../lib/issues'
import type { ProjectDetail } from '../lib/types'
import { Icon } from '../components/icons'
import { HealthBadge, PersonAvatar, PriorityIcon, ProjectMark, ProjectStatusIcon, PROJECT_STATUS_LABEL } from '../components/issue-bits'
import { keysBusy } from '../components/Layout'
import { copyText, Empty, Loading, type MenuItem, MenuButton, useIsMobile, useToast } from '../components/ui'
import { FavoriteStar, IconPicker, ProjectPicker, PropChip, useDeleteProject, usePickerState } from '../components/project/common'
import { Overview } from '../components/project/Overview'
import { ProjectIssues } from '../components/project/ProjectIssues'
import { Updates } from '../components/project/Updates'
import { Docs } from '../components/project/Docs'
import { Activity } from '../components/project/Activity'
import { ProjectInsights } from '../components/project/ProjectInsights'
import { ScheduleMeeting } from '../components/project/ScheduleMeeting'

type Tab = 'overview' | 'issues' | 'updates' | 'docs' | 'activity' | 'insights'
const TABS: { id: Tab; label: string; icon: string }[] = [
  { id: 'overview', label: 'Overview', icon: 'file-text' },
  { id: 'issues', label: 'Issues', icon: 'layers' },
  { id: 'updates', label: 'Updates', icon: 'activity' },
  { id: 'docs', label: 'Docs', icon: 'file' },
  { id: 'activity', label: 'Activity', icon: 'clock' },
  { id: 'insights', label: 'Insights', icon: 'bar-chart' },
]

export default function ProjectPage() {
  const { id: idParam, tab: tabParam } = useParams()
  const [search] = useSearchParams()
  const id = Number(idParam)
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const [p, setP] = useState<ProjectDetail | null>(null)
  const [err, setErr] = useState<{ status: number; msg: string } | null>(null)
  const [meeting, setMeeting] = useState(false)
  const [iconAnchor, setIconAnchor] = useState<{ x: number; y: number; w: number; h: number } | null>(null)
  const pick = usePickerState<'status' | 'priority' | 'lead'>()
  const del = useDeleteProject()
  const tab: Tab = (TABS.find(t => t.id === (tabParam || search.get('tab')))?.id) || 'overview'

  const load = useCallback(() => {
    api.get<ProjectDetail>(`${V1}/projects/${id}`).then(d => { setP(d); setErr(null) })
      .catch(e => setErr({ status: (e as ApiError).status || 0, msg: (e as Error).message }))
  }, [id])
  useEffect(() => { setP(null); load() }, [load])
  useLive(load, ['project', 'issue'])

  const save = async (body: Record<string, unknown>) => {
    if (!p) return
    setP({ ...p, ...body } as ProjectDetail)
    try {
      await api.patch(`${V1}/projects/${p.id}`, body)
      load()
      app.reload().catch(() => {})
    } catch (e) { toast((e as Error).message, 'err'); load() }
  }

  // 1–6 switch tabs.
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || keysBusy(e)) return
      const n = Number(e.key)
      if (n >= 1 && n <= TABS.length) { e.preventDefault(); navigate(`/project/${id}/${TABS[n - 1].id}`) }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [id, navigate])

  if (!id) return <Navigate to="/projects" replace />
  if (err) return <div className="page"><Empty title={err.status === 404 ? 'Project not found' : 'Could not load the project'} icon="briefcase">
    {err.status === 404 ? 'It may have been deleted, or it belongs to a team you are not on.' : err.msg}
    <div className="mt-s"><button type="button" className="btn sm" onClick={() => navigate('/projects')}>All projects</button></div>
  </Empty></div>
  if (!p) return <Loading />

  const canEdit = p.can_edit
  const lead = app.person(p.lead_id)
  const menu = (): MenuItem[] => [
    { label: 'Copy link', icon: 'link', onSelect: async () => toast(await copyText(p.url || location.origin + `/project/${p.id}`) ? 'Link copied' : 'Could not copy', 'ok') },
    { label: 'Schedule meeting…', icon: 'calendar-plus', onSelect: () => setMeeting(true) },
    { label: 'New issue in this project', icon: 'plus', disabled: !canEdit, onSelect: () => window.dispatchEvent(new CustomEvent('pp:new-issue', { detail: { project_id: p.id, team_id: p.team_ids.find(t => app.canEdit(t)) } })) },
    { label: 'Open on the roadmap', icon: 'route', onSelect: () => navigate('/roadmap') },
    'sep',
    { label: 'Delete project…', icon: 'trash', danger: true, disabled: !canEdit, onSelect: () => del(p, () => navigate('/projects')) },
  ]

  return (
    <div className="full-page project-page">
      <div className="pp-header">
        <div className="pp-title-row">
          <button type="button" className="pp-icon" disabled={!canEdit} aria-label="Change icon" title="Icon and colour"
            onClick={e => { const r = e.currentTarget.getBoundingClientRect(); setIconAnchor({ x: r.left, y: r.top, w: r.width, h: r.height }) }}>
            <ProjectMark icon={p.icon} color={p.color} size={22} />
          </button>
          <NameInput value={p.name} disabled={!canEdit} onSave={name => save({ name })} />
          {!mobile && (p.last_update || p.status === 'started') && <HealthBadge health={p.health} />}
          <span className="spacer" />
          <FavoriteStar id={p.id} />
          <MenuButton items={menu} label="Project menu" align="right" width={240}><Icon name="more" size={18} /></MenuButton>
        </div>
        <div className="pp-chips">
          <PropChip icon={<ProjectStatusIcon status={p.status} />} label={PROJECT_STATUS_LABEL[p.status]} disabled={!canEdit} title="Status" onOpen={a => pick.show('status', a)} />
          <PropChip icon={<PriorityIcon priority={p.priority} />} label={p.priority ? priorityLabel(p.priority) : 'No priority'} disabled={!canEdit} title="Priority" onOpen={a => pick.show('priority', a)} />
          <PropChip icon={<PersonAvatar person={lead} size={16} />} label={lead ? lead.name || lead.email : 'No lead'} disabled={!canEdit} title="Lead" onOpen={a => pick.show('lead', a)} />
          {mobile && (p.last_update || p.status === 'started') && <HealthBadge health={p.health} />}
        </div>
        <nav className="tabs pp-tabs" role="tablist">
          {TABS.map((t, i) => <button key={t.id} type="button" role="tab" aria-selected={tab === t.id} className={tab === t.id ? 'active' : ''} title={`${t.label} (${i + 1})`} onClick={() => navigate(`/project/${p.id}/${t.id}`)}>
            <Icon name={t.icon} size={14} />{t.label}
            {t.id === 'updates' && p.update_due && <span className="due-dot" title="An update is due" />}
            {t.id === 'docs' && p.docs.length > 0 && <span className="tab-count">{p.docs.length}</span>}
          </button>)}
        </nav>
      </div>
      <div className={`pp-body tab-${tab}`}>
        {tab === 'overview' && <Overview p={p} save={save} reload={load} onMeeting={() => setMeeting(true)} />}
        {tab === 'issues' && <ProjectIssues p={p} />}
        {tab === 'updates' && <Updates p={p} reload={load} />}
        {tab === 'docs' && <Docs p={p} reload={load} />}
        {tab === 'activity' && <Activity p={p} />}
        {tab === 'insights' && <ProjectInsights p={p} />}
      </div>
      {pick.open && <ProjectPicker kind={pick.open.kind} project={p} anchor={pick.open.anchor} onPatch={save} onClose={pick.close} />}
      {iconAnchor && <IconPicker anchor={iconAnchor} icon={p.icon} color={p.color} onChange={v => save(v)} onClose={() => setIconAnchor(null)} />}
      {meeting && <ScheduleMeeting p={p} onClose={() => setMeeting(false)} />}
    </div>
  )
}

/** The project name, edited in place (Enter or leaving the field saves). */
export function NameInput({ value, onSave, disabled, className = 'pp-name', placeholder = 'Project name' }: { value: string; onSave: (v: string) => void; disabled?: boolean; className?: string; placeholder?: string }) {
  const [v, setV] = useState(value)
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => { if (document.activeElement !== ref.current) setV(value) }, [value])
  const commit = () => { const t = v.trim(); if (t && t !== value) onSave(t); else setV(value) }
  return <input ref={ref} className={className} value={v} size={Math.max(6, v.length + 1)} disabled={disabled} aria-label={placeholder} placeholder={placeholder} onChange={e => setV(e.target.value)} onBlur={commit}
    onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); ref.current?.blur() } else if (e.key === 'Escape') { setV(value); requestAnimationFrame(() => ref.current?.blur()) } }} />
}

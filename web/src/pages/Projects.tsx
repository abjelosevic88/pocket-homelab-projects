// Projects: every project of your teams (or one team's), as a list or a
// board, filtered by status and lead, grouped and searchable.
import { useEffect, useMemo, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import '../styles/projects-ui.css'
import { useApp } from '../lib/app-context'
import type { Project, ProjectStatus } from '../lib/types'
import { PROJECT_STATUSES } from '../lib/types'
import { Icon } from '../components/icons'
import { PersonAvatar, ProjectStatusIcon, PROJECT_STATUS_LABEL } from '../components/issue-bits'
import { TeamMark } from '../components/pickers'
import { Select } from '../components/Select'
import { Empty, Segmented, useDebounce, useIsMobile, useToast } from '../components/ui'
import { keysBusy } from '../components/Layout'
import { NewProjectDialog } from '../components/project/NewProjectDialog'
import { ProjectBoard, ProjectList, type ProjectGroup } from '../components/project/ProjectList'
import { STATUS_GROUP_ORDER, useProjectPatch } from '../components/project/common'

type Tab = 'all' | 'active' | 'backlog' | 'completed' | 'canceled'
type GroupBy = 'status' | 'team' | 'lead' | 'none'
interface Prefs { tab: Tab; lead: 'anyone' | 'me'; group: GroupBy; layout: 'list' | 'board' }

const TABS: { id: Tab; label: string; statuses: ProjectStatus[] }[] = [
  { id: 'all', label: 'All', statuses: PROJECT_STATUSES },
  { id: 'active', label: 'Active', statuses: ['started', 'planned', 'paused'] },
  { id: 'backlog', label: 'Backlog', statuses: ['backlog'] },
  { id: 'completed', label: 'Completed', statuses: ['completed'] },
  { id: 'canceled', label: 'Canceled', statuses: ['canceled'] },
]
const PREFS_KEY = 'pp-projects-prefs'
const DEFAULT_PREFS: Prefs = { tab: 'active', lead: 'anyone', group: 'status', layout: 'list' }

export default function Projects() {
  const app = useApp()
  const { key } = useParams()
  const [params, setParams] = useSearchParams()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const patch = useProjectPatch()
  const team = key ? app.teamByKey(key) : undefined
  const [prefs, setPrefsRaw] = useState<Prefs>(() => ({ ...DEFAULT_PREFS, ...JSON.parse(localStorage.getItem(PREFS_KEY) || '{}') }))
  const setPrefs = (p: Partial<Prefs>) => setPrefsRaw(cur => { const n = { ...cur, ...p }; localStorage.setItem(PREFS_KEY, JSON.stringify(n)); return n })
  const layoutParam = params.get('layout')
  const layout: 'list' | 'board' = layoutParam === 'board' || layoutParam === 'list' ? layoutParam : prefs.layout
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 120).trim().toLowerCase()
  const [creating, setCreating] = useState<{ status?: ProjectStatus } | null>(null)
  // Optimistic status moves until the bootstrap catches up.
  const [moved, setMoved] = useState<Record<number, ProjectStatus>>({})

  useEffect(() => { if (params.get('new') === '1') { setCreating({}); params.delete('new'); setParams(params, { replace: true }) } }, [params]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || keysBusy(e)) return
      if (e.key === 'n' || e.key === 'N') { e.preventDefault(); setCreating({}) }
      else if (e.key === 'b' || e.key === 'B') { e.preventDefault(); setPrefs({ layout: layout === 'board' ? 'list' : 'board' }); if (layoutParam) { params.delete('layout'); setParams(params, { replace: true }) } }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  })
  useEffect(() => { setMoved({}) }, [app.projects])

  const all = useMemo(() => app.projects.map(p => (moved[p.id] ? { ...p, status: moved[p.id] } : p)), [app.projects, moved])
  const scoped = useMemo(() => all
    .filter(p => !team || p.team_ids.includes(team.id))
    .filter(p => prefs.lead === 'anyone' || p.lead_id === app.user?.id)
    .filter(p => !dq || (p.name + ' ' + p.summary).toLowerCase().includes(dq)), [all, team, prefs.lead, dq, app.user?.id])
  const tab = TABS.find(t => t.id === prefs.tab) || TABS[0]
  const counts = useMemo(() => Object.fromEntries(TABS.map(t => [t.id, scoped.filter(p => t.statuses.includes(p.status)).length])), [scoped])
  const shown = useMemo(() => scoped.filter(p => tab.statuses.includes(p.status))
    .sort((a, b) => STATUS_GROUP_ORDER.indexOf(a.status) - STATUS_GROUP_ORDER.indexOf(b.status) || (a.priority || 9) - (b.priority || 9) || (a.target_date || '9999').localeCompare(b.target_date || '9999') || a.name.localeCompare(b.name)), [scoped, tab])

  const groups: ProjectGroup[] = useMemo(() => {
    switch (prefs.group) {
      case 'status': return STATUS_GROUP_ORDER.map(s => ({ key: s, label: PROJECT_STATUS_LABEL[s], icon: <ProjectStatusIcon status={s} />, items: shown.filter(p => p.status === s) })).filter(g => g.items.length)
      case 'team': {
        const ids = [...new Set(shown.flatMap(p => p.team_ids))]
        return ids.map(id => { const t = app.team(id); return { key: 't' + id, label: t?.name || 'Team', icon: <TeamMark team={t} size={16} />, items: shown.filter(p => p.team_ids.includes(id)) } })
          .sort((a, b) => a.label.localeCompare(b.label))
      }
      case 'lead': {
        const ids = [...new Set(shown.map(p => p.lead_id || 0))]
        return ids.map(id => { const pr = app.person(id); return { key: 'l' + id, label: pr ? pr.name || pr.email : 'No lead', icon: <PersonAvatar person={pr} size={18} />, items: shown.filter(p => (p.lead_id || 0) === id) } })
          .sort((a, b) => (a.key === 'l0' ? 1 : b.key === 'l0' ? -1 : a.label.localeCompare(b.label)))
      }
      default: return [{ key: 'all', label: 'All', items: shown }]
    }
  }, [shown, prefs.group, app])

  const onPatch = async (p: Project, body: Record<string, unknown>) => {
    if (body.status) setMoved(m => ({ ...m, [p.id]: body.status as ProjectStatus }))
    const r = await patch(p.id, body)
    if (!r) setMoved(m => { const n = { ...m }; delete n[p.id]; return n })
  }
  const onMove = async (p: Project, status: ProjectStatus) => {
    const from = p.status
    setMoved(m => ({ ...m, [p.id]: status }))
    const r = await patch(p.id, { status })
    if (!r) { setMoved(m => { const n = { ...m }; delete n[p.id]; return n }); return }
    toast(<>“{p.name}” is now {PROJECT_STATUS_LABEL[status].toLowerCase()}</>, 'ok', { label: 'Undo', duration: 6000, run: async () => { setMoved(m => ({ ...m, [p.id]: from })); await patch(p.id, { status: from }) } })
  }
  const boardColumns = prefs.tab === 'all' ? STATUS_GROUP_ORDER : tab.statuses
  const canCreate = app.teams.some(t => app.canEdit(t.id))

  return (
    <div className="full-page flex projects-page">
      <div className="page-head">
        <nav className="tabs proj-tabs" role="tablist" aria-label={team ? `${team.name} projects` : 'Projects'}>
          {TABS.map(t => <button key={t.id} type="button" role="tab" aria-selected={prefs.tab === t.id} className={prefs.tab === t.id ? 'active' : ''} onClick={() => setPrefs({ tab: t.id })}>
            {t.label}{counts[t.id] > 0 && <span className="tab-count">{counts[t.id]}</span>}
          </button>)}
        </nav>
        <span className="spacer" />
        {canCreate && <button type="button" className={`btn primary sm ${mobile ? 'icon-only' : ''}`} onClick={() => setCreating({})} title="New project (N)" aria-label="New project"><Icon name="plus" size={15} />{mobile ? null : 'New project'}</button>}
      </div>
      <div className="view-bar">
        <label className="search-box">
          <Icon name="search" size={14} />
          <input value={q} onChange={e => setQ(e.target.value)} placeholder="Filter projects…" aria-label="Filter projects" onKeyDown={e => { if (e.key === 'Escape') { setQ(''); (e.target as HTMLInputElement).blur() } }} />
        </label>
        <Segmented size="sm" label="Lead" value={prefs.lead} onChange={v => setPrefs({ lead: v })} options={[{ value: 'anyone', label: 'Anyone' }, { value: 'me', label: 'Led by me' }]} />
        <span className="spacer" />
        {layout === 'list' && <div className="group-select">
          <span className="muted small hide-mobile">Group</span>
          <Select size="sm" className="inline" label="Group by" value={prefs.group} onChange={v => setPrefs({ group: v })}
            options={[{ value: 'status', label: 'Status' }, { value: 'team', label: 'Team' }, { value: 'lead', label: 'Lead' }, { value: 'none', label: 'No grouping' }]} />
        </div>}
        <Segmented size="sm" label="Layout" value={layout} onChange={v => { setPrefs({ layout: v }); if (layoutParam) { params.delete('layout'); setParams(params, { replace: true }) } }}
          options={[{ value: 'list', label: <Icon name="rows" size={14} />, title: 'List (B)' }, { value: 'board', label: <Icon name="columns" size={14} />, title: 'Board (B)' }]} />
      </div>
      <div className="scroll-area">
        {!app.projects.length ? (
          <Empty title="No projects yet" icon="briefcase">
            <p>A project groups issues towards one goal, with a lead, a target date and weekly updates.</p>
            {canCreate && <button type="button" className="btn primary sm mt-s" onClick={() => setCreating({})}><Icon name="plus" size={14} />New project</button>}
          </Empty>
        ) : layout === 'board' ? (
          <ProjectBoard columns={boardColumns} projects={shown} onMove={onMove} onNew={st => setCreating({ status: st })} />
        ) : !shown.length ? (
          <Empty title={dq ? 'Nothing matches' : `No ${tab.id === 'all' ? '' : tab.label.toLowerCase() + ' '}projects`} icon="briefcase">
            {dq ? <>No project matches “{q}”.</> : prefs.lead === 'me' ? 'You lead none of them. Try “Anyone”.' : team ? `${team.name} has none here.` : 'Try another tab.'}
          </Empty>
        ) : (
          <ProjectList groups={groups} onPatch={onPatch} grouped={prefs.group !== 'none'} />
        )}
      </div>
      {creating && <NewProjectDialog onClose={() => setCreating(null)} defaultTeamId={team?.id} defaultStatus={creating.status} />}
    </div>
  )
}

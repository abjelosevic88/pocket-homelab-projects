// All my teams as cards, archived ones below.
import { Link, useNavigate } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Team } from '../lib/types'
import { Icon } from '../components/icons'
import { TeamMark } from '../components/pickers'
import { Empty, errMsg, PageHeader, useToast } from '../components/ui'
import { api, V1 } from '../lib/api'
import { ROLE_LABEL } from './team-settings/common'
import '../styles/settings-ui.css'

export default function Teams() {
  const { teams } = useApp()
  const navigate = useNavigate()
  const live = teams.filter(t => !t.archived_at)
  const archived = teams.filter(t => t.archived_at)
  return (
    <div className="page">
      <div className="ui-teams">
        <PageHeader title="Teams" sub="The teams you are on. Each has its own issues, workflow and members."
          actions={<button className="btn primary" onClick={() => navigate('/teams/new')}><Icon name="plus" size={15} />New team</button>} />
        {!live.length ? (
          <Empty title="You are not on a team yet" icon="users">
            <p>Make one for your household, a hobby or a client — or ask someone to add you to theirs.</p>
            <button className="btn primary" onClick={() => navigate('/teams/new?welcome=1')}><Icon name="plus" size={15} />Create a team</button>
          </Empty>
        ) : <div className="ui-team-grid">{live.map(t => <TeamCard key={t.id} team={t} />)}</div>}
        {archived.length > 0 && <>
          <h2 className="ui-teams-h">Archived</h2>
          <div className="ui-team-grid">{archived.map(t => <TeamCard key={t.id} team={t} />)}</div>
        </>}
      </div>
    </div>
  )
}

function TeamCard({ team: t }: { team: Team }) {
  const { reload } = useApp()
  const { toast } = useToast()
  const c = t.counts || { open: 0, started: 0, triage: 0, members: 0, projects: 0 }
  const restore = async () => {
    try { await api.patch(`${V1}/teams/${t.key}`, { archived: false }); await reload(); toast(`${t.name} restored`) } catch (e) { toast(errMsg(e), 'err') }
  }
  const stats: [number, string, string][] = [[c.open, 'open', 'circle'], [c.started, 'in progress', 'activity'], [c.members, c.members === 1 ? 'member' : 'members', 'users'], [c.projects, c.projects === 1 ? 'project' : 'projects', 'briefcase']]
  return (
    <section className={`ui-team-card ${t.archived_at ? 'archived' : ''}`} data-pocket-ask style={{ ['--tc' as string]: t.color }}>
      <Link to={`/team/${t.key}/issues`} className="ui-team-top">
        <TeamMark team={t} size={36} />
        <div className="grow">
          <div className="ui-team-name ellipsis">{t.name}</div>
          <div className="row ui-team-sub"><span className="ui-key">{t.key}</span>{t.role && <span className={`badge ${t.role === 'admin' ? 'accent' : ''}`}>{ROLE_LABEL[t.role]}</span>}{c.triage > 0 && <span className="badge warn">{c.triage} in triage</span>}</div>
        </div>
      </Link>
      {t.description && <p className="muted small ui-team-desc">{t.description}</p>}
      <div className="ui-team-stats">
        {stats.map(([n, label, icon]) => <div key={label}><Icon name={icon} size={13} /><b className="tnum">{n}</b><span>{label}</span></div>)}
      </div>
      <div className="ui-team-links">
        {t.archived_at ? <>
          <span className="muted small grow">Archived</span>
          {t.role === 'admin' && <button className="btn sm" onClick={restore}><Icon name="undo" size={13} />Restore</button>}
        </> : <>
          <Link className="btn sm ghost" to={`/team/${t.key}/issues`}><Icon name="layers" size={13} />Issues</Link>
          <Link className="btn sm ghost" to={`/team/${t.key}/issues?layout=board`}><Icon name="kanban" size={13} />Board</Link>
          {t.cycles_enabled && <Link className="btn sm ghost" to={`/team/${t.key}/cycles`}><Icon name="cycle" size={13} />Cycles</Link>}
          <span className="spacer" />
          <Link className="btn sm ghost icon-only" to={`/team/${t.key}/settings`} title="Team settings" aria-label={`${t.name} settings`}><Icon name="settings" size={14} /></Link>
        </>}
      </div>
    </section>
  )
}

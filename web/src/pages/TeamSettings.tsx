// A team's settings: general, members, workflow, labels, cycles, templates,
// integrations, Pocket AI and import/export. Team admins edit; everyone else reads.
import { useEffect } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { Icon } from '../components/icons'
import { TeamMark } from '../components/pickers'
import { Empty, Tabs } from '../components/ui'
import { GeneralTab } from './team-settings/General'
import { MembersTab } from './team-settings/Members'
import { WorkflowTab } from './team-settings/Workflow'
import { LabelsTab } from './team-settings/Labels'
import { CyclesTab } from './team-settings/Cycles'
import { TemplatesTab } from './team-settings/Templates'
import { IntegrationsTab } from './team-settings/Integrations'
import { ImportExportTab } from './team-settings/ImportExport'
import { PocketAITab } from './team-settings/PocketAI'
import { AttachmentsTab } from './team-settings/Attachments'
import { ROLE_LABEL } from './team-settings/common'
import '../styles/settings-ui.css'

type TabId = 'general' | 'members' | 'workflow' | 'labels' | 'cycles' | 'templates' | 'integrations' | 'ai' | 'files' | 'import'
const TABS: { id: TabId; label: string; icon: string }[] = [
  { id: 'general', label: 'General', icon: 'settings' },
  { id: 'members', label: 'Members', icon: 'users' },
  { id: 'workflow', label: 'Workflow', icon: 'columns' },
  { id: 'labels', label: 'Labels', icon: 'tag' },
  { id: 'cycles', label: 'Cycles', icon: 'cycle' },
  { id: 'templates', label: 'Templates', icon: 'file-text' },
  { id: 'integrations', label: 'Integrations', icon: 'git-branch' },
  { id: 'ai', label: 'Pocket AI', icon: 'sparkles' },
  { id: 'files', label: 'Attachments', icon: 'paperclip' },
  { id: 'import', label: 'Import / export', icon: 'upload' },
]

export default function TeamSettings() {
  const { key, tab: raw } = useParams()
  const navigate = useNavigate()
  const app = useApp()
  const team = app.teamByKey(key)
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'general'
  useEffect(() => {
    document.querySelector('.content .page')?.scrollTo({ top: 0 })
    document.querySelector('.ui-ts-tabs .tabs button.active')?.scrollIntoView({ inline: 'center', block: 'nearest' })
  }, [tab])
  if (!team) return <div className="page"><Empty title="No such team" icon="users">It may have been renamed or deleted, or you are not a member. <Link to="/teams">See your teams</Link></Empty></div>
  const admin = team.role === 'admin'
  const canWrite = admin || team.role === 'member'
  const props = { team, admin, canWrite }

  return (
    <div className="page ui-ts-page">
      <div className="ui-ts">
        <div className="ui-ts-head">
          <TeamMark team={team} size={38} />
          <div className="grow">
            <h1 className="ellipsis">{team.name}</h1>
            <div className="muted small row wrap ui-ts-sub">
              <span className="ui-key">{team.key}</span><span>Team settings</span>
              {team.role && <span className={`badge ${admin ? 'accent' : ''}`}>{ROLE_LABEL[team.role]}</span>}
              {team.archived_at && <span className="badge warn">Archived</span>}
            </div>
          </div>
          <Link to={`/team/${team.key}/issues`} className="btn"><Icon name="layers" size={15} />Issues</Link>
        </div>
        <div className="ui-ts-tabs"><Tabs tabs={TABS} value={tab} onChange={t => navigate(`/team/${team.key}/settings/${t}`, { replace: true })} /></div>
        <div className="ui-ts-body" key={tab + team.id}>
          {tab === 'general' && <GeneralTab {...props} />}
          {tab === 'members' && <MembersTab {...props} />}
          {tab === 'workflow' && <WorkflowTab {...props} />}
          {tab === 'labels' && <LabelsTab {...props} />}
          {tab === 'cycles' && <CyclesTab {...props} />}
          {tab === 'templates' && <TemplatesTab {...props} />}
          {tab === 'integrations' && <IntegrationsTab {...props} />}
          {tab === 'ai' && <PocketAITab {...props} />}
          {tab === 'files' && <AttachmentsTab {...props} />}
          {tab === 'import' && <ImportExportTab {...props} />}
        </div>
      </div>
    </div>
  )
}

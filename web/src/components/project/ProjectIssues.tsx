// The project's issues: the shared issue view (its Display menu groups by
// status, milestone, assignee…), without the redundant project chip.
import { useApp } from '../../lib/app-context'
import { DEFAULT_PROPERTIES } from '../../lib/issues'
import type { ProjectDetail } from '../../lib/types'
import { IssueView } from '../../views/IssueView'
import { openCreateIssue } from '../CreateIssue'
import { Icon } from '../icons'

const PROPS = [...DEFAULT_PROPERTIES.filter(x => x !== 'project'), 'milestone']

export function ProjectIssues({ p }: { p: ProjectDetail }) {
  const app = useApp()
  const teamId = p.team_ids.find(id => app.canEdit(id))
  return (
    <div className="pj-issues">
      <IssueView filter={{ project_ids: [p.id] }} storageKey={'project-' + p.id} teamIds={p.team_ids}
        defaultDisplay={{ group: (p.milestones || []).length ? 'milestone' : 'state', properties: PROPS }}
        newIssue={teamId ? { project_id: p.id, team_id: teamId } : undefined}
        toolbarExtra={teamId ? <button type="button" className="btn sm" onClick={() => openCreateIssue({ project_id: p.id, team_id: teamId })}><Icon name="plus" size={14} />New issue</button> : undefined}
        emptyTitle="No issues in this project" emptyText="Create one here, or move existing issues into the project from their menu." />
    </div>
  )
}

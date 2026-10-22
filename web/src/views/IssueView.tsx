// The issue view every page embeds (team issues, my issues, a project's or a
// cycle's issues, saved views): fetches the issues of a filter, groups and
// shows them as a list. Props are the contract other pages rely on.
import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { useApp, useLive } from '../lib/app-context'
import { displayOf, fetchIssues, groupIssues, sortIssues, type Lookups } from '../lib/issues'
import type { Display, Issue, IssueFilter } from '../lib/types'
import { IssueRow } from '../components/IssueRow'
import { openCreateIssue, type IssueDefaults } from '../components/CreateIssue'
import { Icon } from '../components/icons'
import { StatusIcon } from '../components/issue-bits'
import { Empty, Loading } from '../components/ui'

export interface IssueViewProps {
  /** The base filter of the page (team, project, cycle, assignee…). */
  filter: IssueFilter
  /** Remembers the person's display choices for this page (localStorage). */
  storageKey: string
  defaultDisplay?: Display
  /** Defaults for issues created from this view ("+" buttons). */
  newIssue?: IssueDefaults
  /** Teams shown (their workflows give the board's columns). */
  teamIds?: number[]
  emptyTitle?: string
  emptyText?: React.ReactNode
  /** Extra controls in the view's toolbar (right side). */
  toolbarExtra?: React.ReactNode
  /** Hide the toolbar (embedded small lists). */
  bare?: boolean
}

export function useLookups(): Lookups {
  const app = useApp()
  return useMemo(() => ({ teams: app.teams, state: app.state, person: app.person, project: app.project, cycle: app.cycle, milestone: app.milestone, label: app.label, team: app.team }), [app])
}

export function IssueView({ filter, storageKey, defaultDisplay, newIssue, teamIds, emptyTitle = 'No issues', emptyText, bare }: IssueViewProps) {
  const L = useLookups()
  const display = displayOf({ ...defaultDisplay, ...(JSON.parse(localStorage.getItem('pp-display:' + storageKey) || '{}') as Display) })
  const [issues, setIssues] = useState<Issue[] | null>(null)
  const [err, setErr] = useState('')
  const fkey = JSON.stringify(filter)
  const load = useCallback(() => {
    fetchIssues({ ...filter, show_completed: filter.show_completed || 'week' }).then(r => { setIssues(r.issues); setErr('') }).catch(e => setErr((e as Error).message))
  }, [fkey]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setIssues(null); load() }, [load])
  useLive(load, ['issue', 'comment', 'team', 'cycle'])

  if (err) return <div className="callout danger m">{err}</div>
  if (!issues) return <Loading />
  const groups = groupIssues(sortIssues(issues, display.sort), display.group, L, { teamIds, showEmpty: false })
  const update = (fresh: Issue) => setIssues(list => (list || []).map(i => (i.id === fresh.id ? fresh : i)))
  if (!issues.length) return <Empty title={emptyTitle} icon="layers">{emptyText}{newIssue && <button className="btn sm mt-s" onClick={() => openCreateIssue(newIssue)}><Icon name="plus" size={14} />New issue</button>}</Empty>
  return (
    <div className={`issue-view ${bare ? 'bare' : ''}`}>
      {groups.map(g => (
        <section key={g.key} className="issue-group">
          {display.group !== 'none' && <header className="group-head">
            {g.state && <StatusIcon state={g.state} />}<span className="group-name">{g.label}</span><span className="group-count">{g.items.length}</span>
            <span className="spacer" />
            {newIssue && <button type="button" className="btn ghost icon-only xs" aria-label="New issue here" onClick={() => openCreateIssue({ ...newIssue, ...(g.state ? { state_id: g.state.id, team_id: g.state.team_id } : {}) })}><Icon name="plus" size={14} /></button>}
          </header>}
          {g.items.map(is => <IssueRow key={is.id} issue={is} properties={display.properties} onUpdated={update} showTeam={!teamIds || teamIds.length !== 1} />)}
        </section>
      ))}
    </div>
  )
}

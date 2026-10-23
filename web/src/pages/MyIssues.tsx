// My issues: /my-issues/:tab? — Assigned (default), Created, Subscribed and
// Activity (recently updated issues I'm involved in), with a summary strip.
import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp, useLive } from '../lib/app-context'
import { DEFAULT_PROPERTIES } from '../lib/issues'
import { addDays, today } from '../lib/dates'
import type { Display, IssueFilter } from '../lib/types'
import { IssueView } from '../views/IssueView'
import { openCreateIssue } from '../components/CreateIssue'
import { Icon } from '../components/icons'
import '../styles/issues.css'

type Tab = 'assigned' | 'created' | 'subscribed' | 'activity'
const TABS: { id: Tab; label: string }[] = [{ id: 'assigned', label: 'Assigned' }, { id: 'created', label: 'Created' }, { id: 'subscribed', label: 'Subscribed' }, { id: 'activity', label: 'Activity' }]
interface Dash { assigned_open: number; overdue: number; due_this_week: number; in_progress: number }

export default function MyIssues() {
  const { tab: raw } = useParams()
  const tab: Tab = (TABS.some(t => t.id === raw) ? raw : 'assigned') as Tab
  const app = useApp()
  const navigate = useNavigate()
  const me = app.user?.id || 0
  const [dash, setDash] = useState<Dash | null>(null)
  const load = useCallback(() => { api.get<Dash>(`${V1}/dashboard`).then(setDash).catch(() => {}) }, [])
  useEffect(() => { load() }, [load])
  useLive(load, ['issue'])

  let filter: IssueFilter
  let defaults: Display = { group: 'state' }
  let empty = { title: 'Nothing assigned to you', text: 'Issues assigned to you show up here.' }
  switch (tab) {
    case 'created': filter = { creator_ids: [me] }; empty = { title: 'You haven’t created issues yet', text: 'Press C to create one.' }; break
    case 'subscribed': filter = { subscribed_by: me }; empty = { title: 'No subscriptions', text: 'Issues you create, are assigned or comment on are followed automatically.' }; break
    case 'activity':
      filter = { subscribed_by: me, updated_since: addDays(today(), -14), sort: 'updated' }
      defaults = { group: 'none', sort: 'updated', show_completed: 'all', properties: [...DEFAULT_PROPERTIES, 'updated'] }
      empty = { title: 'No recent activity', text: 'Issues you follow that changed in the last two weeks show up here.' }
      break
    default: filter = { assignee_ids: [me] }; defaults = { group: 'state', show_completed: 'week' }
  }
  const stats: { label: string; value?: number; warn?: boolean }[] = [
    { label: 'Assigned, open', value: dash?.assigned_open }, { label: 'In progress', value: dash?.in_progress },
    { label: 'Overdue', value: dash?.overdue, warn: !!dash?.overdue }, { label: 'Due this week', value: dash?.due_this_week },
  ]
  return (
    <div className="iss-page">
      <div className="iss-head">
        <h1><Icon name="target" size={17} />My issues</h1>
        <nav className="tabs" role="tablist">
          {TABS.map(t => <button key={t.id} type="button" role="tab" aria-selected={t.id === tab} className={t.id === tab ? 'active' : ''} onClick={() => navigate(t.id === 'assigned' ? '/my-issues' : `/my-issues/${t.id}`)}>{t.label}</button>)}
        </nav>
        <span className="spacer" />
        <button type="button" className="btn primary sm" onClick={() => openCreateIssue({ assignee_id: me })}><Icon name="plus" size={14} />New issue</button>
      </div>
      {tab === 'assigned' && <div className="mi-stats" aria-label="Summary">
        {stats.map(s => <div key={s.label} className={`mi-stat ${s.warn ? 'warn' : ''}`} style={{ cursor: 'default' }}><b>{s.value ?? '–'}</b><span>{s.label}</span></div>)}
      </div>}
      <IssueView key={tab} fill filter={filter} storageKey={`my:${tab}`} defaultDisplay={defaults} newIssue={tab === 'assigned' ? { assignee_id: me } : undefined}
        emptyTitle={empty.title} emptyText={empty.text} />
    </div>
  )
}

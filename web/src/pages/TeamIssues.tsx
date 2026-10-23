// A team's issues: All / Active / Backlog / Triage tabs, list or board.
// /team/:key/:tab with tab ∈ issues | active | backlog | triage | board.
import { useCallback, useEffect, useState } from 'react'
import { Navigate, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { emitChange, useApp, useLive } from '../lib/app-context'
import { fetchIssues } from '../lib/issues'
import { ago } from '../lib/format'
import { shortDate } from '../lib/dates'
import type { Display, Issue, IssueFilter, Team } from '../lib/types'
import { IssueView, useStoredDisplay } from '../views/IssueView'
import { openCreateIssue } from '../components/CreateIssue'
import { Icon } from '../components/icons'
import { PersonAvatar, StatusIcon } from '../components/issue-bits'
import { DateMenu, LabelChips, PickerMenu, TeamMark, useItems } from '../components/pickers'
import { IssueSearchPicker } from '../components/issues/IssueSearch'
import { Empty, Loading, Segmented, useToast, type Anchor, rectOf } from '../components/ui'
import '../styles/issues.css'

const TABS = ['issues', 'active', 'backlog', 'triage', 'board'] as const
type Tab = typeof TABS[number]

export default function TeamIssues() {
  const { key, tab: rawTab = 'issues' } = useParams()
  const app = useApp()
  const navigate = useNavigate()
  const team = app.teamByKey(key)
  const tab = (TABS as readonly string[]).includes(rawTab) ? rawTab as Tab : null
  const storageKey = `team:${team?.id || key}:${tab}`
  const [display, setDisplay] = useStoredDisplay(storageKey)
  if (!team) return <div className="page"><Empty title="Team not found" icon="users">You may not be a member of this team. <a href="/teams" onClick={e => { e.preventDefault(); navigate('/teams') }}>See your teams</a></Empty></div>
  if (!tab || (tab === 'triage' && !team.triage_enabled)) return <Navigate to={`/team/${team.key}/issues`} replace />

  const base: IssueFilter = { team_ids: [team.id] }
  if (tab === 'active') base.state_types = ['unstarted', 'started']
  if (tab === 'backlog') base.state_types = ['backlog']
  const defaults: Display = tab === 'board' ? { layout: 'board', show_empty_groups: true } : tab === 'backlog' ? { group: 'none' } : { layout: 'list', group: 'state' }
  const layout = display.layout || defaults.layout || 'list'
  const setLayout = (l: 'list' | 'board') => {
    if (tab === 'board' && l === 'list') { navigate(`/team/${team.key}/issues`); return }
    setDisplay({ ...display, layout: l, ...(l === 'board' && display.show_empty_groups === undefined ? { show_empty_groups: true } : {}) })
  }
  const triage = team.counts?.triage || 0
  const tabs: { id: Tab; label: string; count?: number }[] = [
    { id: 'issues', label: 'All issues' }, { id: 'active', label: 'Active' }, { id: 'backlog', label: 'Backlog' },
    ...(team.triage_enabled ? [{ id: 'triage' as Tab, label: 'Triage', count: triage }] : []),
    { id: 'board', label: 'Board' },
  ]
  const emptyText: Record<Tab, string> = {
    issues: 'Create the first issue for this team.', active: 'Nothing is planned or in progress.', backlog: 'The backlog is empty.',
    triage: 'Nothing waits for triage.', board: 'Create the first issue for this team.',
  }
  return (
    <div className="iss-page">
      <div className="iss-head">
        <h1><TeamMark team={team} size={20} />{team.name}</h1>
        <nav className="tabs" role="tablist">
          {tabs.map(t => <button key={t.id} type="button" role="tab" aria-selected={t.id === tab} className={t.id === tab ? 'active' : ''} onClick={() => navigate(`/team/${team.key}/${t.id}`)}>
            {t.label}{!!t.count && <span className="badge accent">{t.count}</span>}
          </button>)}
        </nav>
        <span className="spacer" />
        {tab !== 'triage' && <Segmented size="sm" value={layout} onChange={setLayout} label="Layout" options={[
          { value: 'list', label: <Icon name="rows" size={14} />, title: 'List' },
          { value: 'board', label: <Icon name="columns" size={14} />, title: 'Board' },
        ]} />}
        {team.role !== 'guest' && <button type="button" className="btn primary sm" onClick={() => openCreateIssue({ team_id: team.id })}><Icon name="plus" size={14} />New issue</button>}
      </div>
      {tab === 'triage' ? <TriageList team={team} /> :
        <IssueView key={storageKey} fill filter={base} storageKey={storageKey} teamIds={[team.id]} newIssue={{ team_id: team.id }}
          display={display} onDisplayChange={setDisplay} defaultDisplay={defaults} emptyTitle="No issues here" emptyText={emptyText[tab]} />}
    </div>
  )
}

/** Issues waiting in triage: accept into the workflow, decline, mark duplicate or snooze. */
function TriageList({ team }: { team: Team }) {
  const app = useApp()
  const navigate = useNavigate()
  const items = useItems()
  const { toast } = useToast()
  const [list, setList] = useState<Issue[] | null>(null)
  const [act, setAct] = useState<{ kind: 'accept' | 'dup' | 'snooze'; is: Issue; at: Anchor } | null>(null)
  const load = useCallback(() => { fetchIssues({ team_ids: [team.id], state_types: ['triage'], sort: 'created' }).then(r => setList(r.issues)).catch(e => toast((e as Error).message, 'err')) }, [team.id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { load() }, [load])
  useLive(load, ['issue', 'team'], [team.id])
  const canEdit = app.canEdit(team.id)
  const states = team.states || []
  const patch = async (is: Issue, body: Record<string, unknown>, msg: string, undo?: Record<string, unknown>) => {
    setList(l => (l || []).filter(x => x.id !== is.id))
    try {
      await api.patch(`${V1}/issues/${is.id}`, body)
      emitChange({ kind: 'issue', team_ids: [team.id], ids: [is.id] })
      app.reload().catch(() => {})
      toast(msg, 'ok', undo ? { label: 'Undo', run: async () => { await api.patch(`${V1}/issues/${is.id}`, undo); emitChange({ kind: 'issue', team_ids: [team.id], ids: [is.id] }); load(); app.reload().catch(() => {}) } } : undefined)
    } catch (e) { toast((e as Error).message, 'err'); load() }
  }
  const back = (is: Issue) => ({ state_id: is.state_id })
  const decline = (is: Issue) => {
    const st = states.find(s => s.type === 'canceled' && !/dup/i.test(s.name)) || states.find(s => s.type === 'canceled')
    patch(is, st ? { state_id: st.id } : { state: 'canceled' }, `${is.identifier} declined`, back(is))
  }
  const duplicate = async (is: Issue, of: Issue) => {
    try {
      await api.post(`${V1}/issues/${is.id}/relations`, { type: 'duplicate', issue: of.identifier })
      const st = states.find(s => s.type === 'canceled' && /dup/i.test(s.name)) || states.find(s => s.type === 'canceled')
      await patch(is, st ? { state_id: st.id } : { state: 'canceled' }, `${is.identifier} marked as a duplicate of ${of.identifier}`)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  if (!list) return <Loading />
  if (!list.length) return <Empty title="Triage is clear" icon="inbox">New issues from other apps, people outside the team and forms land here first.</Empty>
  const accepts = items.states(team.id).filter(it => states.find(s => s.id === it.value)?.type !== 'triage')
  return (
    <div className="triage-list scroll-area">
      {list.map(is => {
        const creator = app.person(is.creator_id)
        return (
          <div key={is.id} className="triage-row" data-pocket-ask onClick={() => navigate(`/issue/${is.identifier}`)}>
            <StatusIcon state={app.state(is.state_id)} />
            <div className="tr-main">
              <div className="tr-title"><span className="ident">{is.identifier}</span> {is.title}</div>
              <div className="tr-meta">
                <PersonAvatar person={creator} size={16} />{creator?.name || (is.source_app ? `Pocket ${is.source_app}` : 'Someone')}
                <span>· {ago(is.created_at)}</span>
                {is.label_ids.length > 0 && <LabelChips ids={is.label_ids} max={3} />}
              </div>
            </div>
            {canEdit && <div className="tr-actions" onClick={e => e.stopPropagation()}>
              <button type="button" className="btn sm" onClick={e => setAct({ kind: 'accept', is, at: rectOf(e.currentTarget) })}><Icon name="check" size={14} />Accept</button>
              <button type="button" className="btn ghost sm" onClick={() => decline(is)} title="Decline (cancel)"><Icon name="x-circle" size={14} /><span className="hide-mobile">Decline</span></button>
              <button type="button" className="btn ghost sm" onClick={e => setAct({ kind: 'dup', is, at: rectOf(e.currentTarget) })} title="Mark as duplicate"><Icon name="copy" size={14} /><span className="hide-mobile">Duplicate</span></button>
              <button type="button" className="btn ghost sm icon-only" onClick={e => setAct({ kind: 'snooze', is, at: rectOf(e.currentTarget) })} title="Snooze" aria-label="Snooze"><Icon name="clock" size={14} /></button>
            </div>}
          </div>
        )
      })}
      {act?.kind === 'accept' && <PickerMenu anchor={act.at} items={accepts} selected={team.default_state_id || undefined} placeholder="Accept into…"
        onPick={v => patch(act.is, { state_id: v }, `${act.is.identifier} accepted`, back(act.is))} onClose={() => setAct(null)} />}
      {act?.kind === 'dup' && <IssueSearchPicker anchor={act.at} exclude={[act.is.id]} teamId={team.id} title={`${act.is.identifier} is a duplicate of…`} onPick={of => duplicate(act.is, of)} onClose={() => setAct(null)} />}
      {act?.kind === 'snooze' && <DateMenu anchor={act.at} value="" title="Snooze until" onClose={() => setAct(null)}
        onPick={d => d && patch(act.is, { snoozed_until: d }, `Snoozed until ${shortDate(d)}`, { snoozed_until: '' })} />}
    </div>
  )
}


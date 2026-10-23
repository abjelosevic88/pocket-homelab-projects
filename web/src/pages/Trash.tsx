// Deleted issues: restore them within 30 days.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { emitChange, useApp, useLive } from '../lib/app-context'
import { fetchIssues } from '../lib/issues'
import { ago, stamp } from '../lib/format'
import type { Issue } from '../lib/types'
import { Icon } from '../components/icons'
import { StatusIcon } from '../components/issue-bits'
import { TeamMark } from '../components/pickers'
import { Empty, Loading, useToast } from '../components/ui'
import '../styles/issues.css'

export default function Trash() {
  const app = useApp()
  const { toast } = useToast()
  const [list, setList] = useState<Issue[] | null>(null)
  const load = useCallback(() => { fetchIssues({}, { trash: 1, archived: 'include' }).then(r => setList(r.issues.sort((a, b) => (b.deleted_at || '').localeCompare(a.deleted_at || '')))).catch(e => toast((e as Error).message, 'err')) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { load() }, [load])
  useLive(load, ['issue'])
  const restore = async (ids: number[]) => {
    try {
      await api.post(`${V1}/issues/bulk`, { ids, action: 'restore' })
      setList(l => (l || []).filter(i => !ids.includes(i.id)))
      emitChange({ kind: 'issue', ids })
      toast(ids.length === 1 ? 'Restored' : `Restored ${ids.length} issues`)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const writable = (list || []).filter(i => app.canEdit(i.team_id))
  return (
    <div className="iss-page">
      <div className="iss-head">
        <h1><Icon name="trash" size={16} />Trash</h1>
        <span className="muted small hide-mobile">Deleted issues stay here for 30 days, then they are gone for good.</span>
        <span className="spacer" />
        {writable.length > 1 && <button type="button" className="btn sm" onClick={() => restore(writable.map(i => i.id))}><Icon name="undo" size={14} />Restore all</button>}
      </div>
      <div className="scroll-area" style={{ paddingBottom: 90 }}>
        {!list ? <Loading /> : !list.length ? <Empty title="The trash is empty" icon="trash">Deleted issues stay here for 30 days, so you can bring them back.</Empty> :
          list.map(is => {
            const team = app.team(is.team_id)
            const left = is.deleted_at ? Math.max(0, 30 - Math.floor((Date.now() - new Date(is.deleted_at).getTime()) / 86400000)) : 30
            return (
              <div key={is.id} className="trash-row" data-pocket-ask>
                <StatusIcon state={app.state(is.state_id)} />
                <span className="ident">{is.identifier}</span>
                <Link className="t" to={`/issue/${is.identifier}`}>{is.title}</Link>
                {team && <span className="row xsmall muted hide-mobile" style={{ gap: 5 }}><TeamMark team={team} size={14} />{team.name}</span>}
                <span className="when" title={is.deleted_at ? stamp(is.deleted_at) : ''}>Deleted {ago(is.deleted_at || '')} · {left} day{left === 1 ? '' : 's'} left</span>
                {app.canEdit(is.team_id) && <button type="button" className="btn sm" onClick={() => restore([is.id])}><Icon name="undo" size={13} />Restore</button>}
              </div>
            )
          })}
      </div>
    </div>
  )
}

// A team's issues that have a Pocket AI conversation working or waiting right
// now (the person's own conversations), live from the shared store.
import { useEffect, useState } from 'react'
import { Navigate, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useAIActiveList } from '../lib/ai-active'
import type { Issue } from '../lib/types'
import { IssueRow } from '../components/IssueRow'
import { Icon } from '../components/icons'
import { TeamMark } from '../components/pickers'
import { Empty, Loading } from '../components/ui'
import '../styles/issues.css'

export default function TeamAI() {
  const { key = '' } = useParams()
  const app = useApp()
  const navigate = useNavigate()
  const team = app.teams.find(t => t.key.toLowerCase() === key.toLowerCase())
  const active = useAIActiveList().filter(a => team && a.ref.toUpperCase().startsWith(team.key.toUpperCase() + '-'))
  const refs = active.map(a => a.ref).sort().join(',')
  const [issues, setIssues] = useState<Record<string, Issue>>({})
  const [loaded, setLoaded] = useState(false)
  useEffect(() => {
    let live = true
    const want = refs ? refs.split(',') : []
    Promise.all(want.map(r => issues[r] ? Promise.resolve(issues[r]) : api.get<Issue>(`${V1}/issues/${encodeURIComponent(r)}`).catch(() => null)))
      .then(list => {
        if (!live) return
        const next: Record<string, Issue> = {}
        list.forEach((is, i) => { if (is) next[want[i]] = is })
        setIssues(next)
        setLoaded(true)
      })
    return () => { live = false }
  }, [refs]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!team) return app.teams.length ? <Navigate to="/" replace /> : <Loading />
  const rows = active.filter(a => issues[a.ref]).sort((a, b) => (a.status === b.status ? a.ref.localeCompare(b.ref) : a.status === 'waiting' ? -1 : 1))
  return (
    <div className="iss-page">
      <div className="iss-head">
        <h1><TeamMark team={team} size={20} />{team.name}</h1>
        <span className="team-ai-sub"><Icon name="sparkles" size={14} />With Pocket AI</span>
        <span className="spacer" />
      </div>
      {!loaded && active.length > 0 ? <Loading /> : rows.length === 0 ? (
        <Empty title="Nothing running" icon="sparkles">No Pocket AI conversations are working on this team's issues right now.</Empty>
      ) : (
        <div className="team-ai-list">
          {rows.map(a => (
            <div key={a.ref} className="team-ai-row">
              <IssueRow issue={issues[a.ref]} properties={['status', 'id', 'priority', 'assignee']} onOpen={is => navigate(`/issue/${is.identifier}`)} />
              <span className={`team-ai-state ${a.status}`}>{a.status === 'waiting' ? 'Waiting for you' : 'Working…'}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

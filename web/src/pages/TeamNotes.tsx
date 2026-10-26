// A team's folder in Pocket Notes (Team settings → Notes): the person's own
// notes in it, newest first, with subfolders.
import { useEffect, useState } from 'react'
import { Link, Navigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { ago } from '../lib/format'
import type { RelatedSection } from '../lib/types'
import { Icon } from '../components/icons'
import { TeamMark } from '../components/pickers'
import { Empty, Loading } from '../components/ui'
import '../styles/issues.css'

interface FolderNote { id: string; title: string; folder: string; snippet: string; modified: number; pinned: boolean; url: string }
interface TeamNotesData { folder: string; folder_url?: string; notes?: RelatedSection<FolderNote> }

export default function TeamNotes() {
  const { key = '' } = useParams()
  const app = useApp()
  const team = app.teams.find(t => t.key.toLowerCase() === key.toLowerCase())
  const [data, setData] = useState<TeamNotesData | null>(null)
  const [err, setErr] = useState('')
  const [tick, setTick] = useState(0)
  useEffect(() => {
    if (!team) return
    let live = true
    api.get<TeamNotesData>(`${V1}/teams/${team.key}/notes`).then(d => { if (live) { setData(d); setErr('') } }).catch(e => live && setErr((e as Error).message))
    return () => { live = false }
  }, [team?.key, team?.notes_folder, tick]) // eslint-disable-line react-hooks/exhaustive-deps
  // notes are written in Pocket Notes, in another tab: look again when the person comes back
  useEffect(() => {
    const again = () => { if (document.visibilityState === 'visible') setTick(t => t + 1) }
    document.addEventListener('visibilitychange', again)
    return () => document.removeEventListener('visibilitychange', again)
  }, [])
  if (!team) return app.teams.length ? <Navigate to="/" replace /> : <Loading />
  const sec = data?.notes
  const sub = (n: FolderNote) => n.folder && data && n.folder !== data.folder ? n.folder.slice(data.folder.length + 1) : ''
  return (
    <div className="iss-page">
      <div className="iss-head">
        <h1><TeamMark team={team} size={20} />{team.name}</h1>
        <span className="team-ai-sub notes"><Icon name="note" size={14} />Notes{data?.folder && <> · <code>{data.folder}</code></>}</span>
        <span className="spacer" />
        {data?.folder_url && <a className="btn ghost sm" href={data.folder_url} target="_blank" rel="noreferrer"><Icon name="external" size={14} />Open in Pocket Notes</a>}
      </div>
      {err ? <div className="callout danger m"><Icon name="alert" size={15} /><span>{err}</span></div>
        : !data ? <Loading />
        : !data.folder ? <Empty title="No Notes folder yet" icon="note">Connect {team.name} to a folder in Pocket Notes in <Link to={`/team/${team.key}/settings/notes`}>Team settings → Notes</Link>.</Empty>
        : sec && sec.status !== 'ok' ? <Empty title="Pocket Notes can’t be reached" icon="note">{sec.detail}</Empty>
        : !sec?.items.length ? <Empty title="No notes here yet" icon="note">Notes you put in <code>{data.folder}</code> in Pocket Notes, and notes started from {team.name}’s issues, show up here.</Empty>
        : <div className="team-notes-list">
          {sec.items.map(n => (
            <a key={n.id} className="team-note" href={n.url} target="_blank" rel="noreferrer">
              <Icon name="note" size={15} className="faint" />
              <span className="grow team-note-main">
                <span className="ellipsis team-note-title">{n.title || 'Untitled'}</span>
                {n.snippet && <span className="xsmall muted ellipsis">{n.snippet}</span>}
              </span>
              {sub(n) && <span className="badge">{sub(n)}</span>}
              <span className="xsmall muted nowrap">{ago(n.modified)}</span>
            </a>
          ))}
        </div>}
    </div>
  )
}

// A project's documents: specs, plans, meeting notes. Open one at /doc/:id.
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { ago, stamp } from '../../lib/format'
import type { Doc, ProjectDetail } from '../../lib/types'
import { Icon } from '../icons'
import { PersonAvatar, personName } from '../issue-bits'
import { Empty, useToast } from '../ui'

export function Docs({ p }: { p: ProjectDetail; reload: () => void }) {
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const create = async () => {
    setBusy(true)
    try {
      const d = await api.post<Doc>(`${V1}/projects/${p.id}/docs`, { title: 'Untitled', body: '' })
      navigate(`/doc/${d.id}?new=1`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const docs = [...p.docs].sort((a, b) => b.updated_at.localeCompare(a.updated_at))
  return (
    <div className="docs-tab">
      <div className="ov-sec-head">
        <h3>Documents</h3>{docs.length > 0 && <span className="count">{docs.length}</span>}
        <span className="spacer" />
        {p.can_edit && <button type="button" className="btn primary sm" disabled={busy} onClick={create}><Icon name="plus" size={14} />New document</button>}
      </div>
      {!docs.length ? (
        <Empty title="No documents yet" icon="file-text">
          Keep the plan, a spec, or meeting notes with the project.
          {p.can_edit && <div className="mt-s"><button type="button" className="btn sm" onClick={create} disabled={busy}><Icon name="plus" size={14} />Write the first one</button></div>}
        </Empty>
      ) : (
        <div className="doc-list">
          {docs.map(d => {
            const who = app.person(d.updated_by || d.created_by)
            return (
              <Link key={d.id} to={`/doc/${d.id}`} className="doc-row" data-pocket-ask={`doc:${d.id}`}>
                <span className="doc-ico">{d.icon || <Icon name="file-text" size={16} />}</span>
                <span className="grow ellipsis doc-title">{d.title || 'Untitled'}</span>
                {who && <span className="row xsmall muted hide-mobile"><PersonAvatar person={who} size={16} />{personName(who)}</span>}
                <span className="xsmall muted doc-when" title={stamp(d.updated_at)}>{ago(d.updated_at)}</span>
              </Link>
            )
          })}
        </div>
      )}
    </div>
  )
}

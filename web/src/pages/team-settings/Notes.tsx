// Notes: the team's folder in Pocket Notes. Notes are each person's own, so
// it is a folder name every member has in their own notes: notes started
// from the team's issues go there, the team's Notes page lists it, and
// Pocket AI conversations about its issues put their notes there.
import { useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { Icon } from '../../components/icons'
import { errMsg, Field, Loading, Spinner, useAsync, useToast } from '../../components/ui'
import { Section } from '../settings/common'
import { ReadOnly, type TabProps } from './common'

interface TeamNotes { folder: string; default_folder: string; folder_url?: string; notes?: { status: string; detail: string } }

export function NotesTab({ team, admin }: TabProps) {
  const { toast } = useToast()
  const { reload } = useApp()
  const [n, setN] = useState(0)
  const saved = useAsync(() => api.get<TeamNotes>(`${V1}/teams/${team.key}/notes`), [team.key, n])
  const folders = useAsync(() => api.get<{ path: string; count: number }[]>(`${V1}/notes/folders`), [])
  const [folder, setFolder] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (saved.data) setFolder(saved.data.folder) }, [saved.data])

  if (!admin) return <ReadOnly what="the team’s Notes folder" />
  if (saved.error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{saved.error}</span></div>
  const s = saved.data
  if (!s) return <Loading />
  const dirty = folder.trim() !== s.folder

  const save = async (value: string) => {
    setBusy(true)
    try {
      const v = await api.put<TeamNotes>(`${V1}/teams/${team.key}/notes`, { folder: value })
      toast(v.folder ? `${team.name} is connected to the “${v.folder}” folder` : 'No Notes folder for this team')
      setN(x => x + 1)
      await reload()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }

  return (
    <div className="ui-ts-stack">
      <Section title="Folder in Pocket Notes" sub="Where notes about this team’s work live. Each member’s notes stay their own: this is a folder of that name in everyone’s Pocket Notes.">
        <Field label="Folder" help={<>
          Notes started from an issue (<b>New note</b>) and the notes Pocket AI writes about an issue go here, and the team’s <b>Notes</b> page in the sidebar lists it (subfolders too).
          {' '}Without one, notes about issues go to <code>{s.default_folder}</code>. A folder that does not exist yet is made with the first note.
        </>}>
          <input value={folder} onChange={e => setFolder(e.target.value)} placeholder={`e.g. ${team.name}`} list="pp-note-folders" spellCheck={false} autoCapitalize="off" />
          <datalist id="pp-note-folders">{(folders.data || []).map(f => <option key={f.path} value={f.path}>{`${f.count} note${f.count === 1 ? '' : 's'}`}</option>)}</datalist>
        </Field>
        {folders.error && <p className="muted small" style={{ margin: '6px 0 0' }}>Your folders can’t be listed: {folders.error}</p>}
        {s.folder && s.notes && s.notes.status !== 'ok' && (
          <div className="callout danger" style={{ marginTop: 12 }}><Icon name="alert" size={15} /><span>{s.notes.detail}</span></div>
        )}
        <div className="row" style={{ marginTop: 14, gap: 8 }}>
          <button type="button" className="btn primary" disabled={!dirty || busy} onClick={() => save(folder.trim())}>{busy && <Spinner />}Save</button>
          {s.folder && <button type="button" className="btn" disabled={busy} onClick={() => save('')}>Disconnect</button>}
          {s.folder_url && <a className="btn ghost" href={s.folder_url} target="_blank" rel="noreferrer"><Icon name="external" size={14} />Open in Pocket Notes</a>}
        </div>
      </Section>
    </div>
  )
}

// Attachments: where the team's uploaded files are kept — Pocket Projects'
// own disk, or a shared folder in Pocket Files (one place for all files),
// reached through the admin's Pocket Core link. Switching to Pocket Files
// offers to move the files already attached.
import { useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { Icon } from '../../components/icons'
import { Confirm, errMsg, Field, Loading, Segmented, Spinner, Switch, useAsync, useToast } from '../../components/ui'
import { Section } from '../settings/common'
import { ReadOnly, type TabProps } from './common'

interface TeamFiles {
  mode: '' | 'pocket_files'
  folder: string
  default_folder: string
  owner: string
  owner_is_me: boolean
  ok: boolean
  error?: string
  local: number
  in_files: number
}

const plural = (n: number, w: string) => `${n} ${w}${n === 1 ? '' : 's'}`

export function AttachmentsTab({ team, admin }: TabProps) {
  const { toast } = useToast()
  const [n, setN] = useState(0)
  const saved = useAsync(() => api.get<TeamFiles>(`${V1}/teams/${team.key}/files`), [team.key, n])
  const [mode, setMode] = useState<TeamFiles['mode']>('')
  const [folder, setFolder] = useState('')
  const [busy, setBusy] = useState('')
  const [confirm, setConfirm] = useState(false)
  const [moveNow, setMoveNow] = useState(true)
  useEffect(() => { if (saved.data) { setMode(saved.data.mode); setFolder(saved.data.folder) } }, [saved.data])

  if (!admin) return <ReadOnly what="where attachments are kept" />
  if (saved.error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{saved.error}</span></div>
  const s = saved.data
  if (!s) return <Loading />
  const dirty = mode !== s.mode || (mode === 'pocket_files' && folder.trim() !== s.folder)

  const move = async () => {
    setBusy('move')
    try {
      const r = await api.post<{ moved: number; failed: string[] }>(`${V1}/teams/${team.key}/files/move`)
      if (r.failed.length) toast(`Moved ${r.moved}; not moved: ${r.failed.join('; ')}`, 'err')
      else toast(r.moved ? `Moved ${plural(r.moved, 'file')} to Pocket Files` : 'Nothing to move')
      setN(x => x + 1)
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  const save = async () => {
    setConfirm(false)
    setBusy('save')
    try {
      const v = await api.put<TeamFiles>(`${V1}/teams/${team.key}/files`, { mode, folder: folder.trim() })
      toast(v.mode === 'pocket_files' ? 'New attachments go to Pocket Files' : 'New attachments stay in Pocket Projects')
      setN(x => x + 1)
      if (v.mode === 'pocket_files' && moveNow && v.local > 0) await move()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }

  return (
    <div className="ui-ts-stack">
      <Section title="Where attachments are kept" sub="Files uploaded to this team’s issues. Links and pull requests are not files and stay as they are.">
        <Segmented value={mode} onChange={setMode} label="Storage" options={[
          { value: '', label: 'Pocket Projects' },
          { value: 'pocket_files', label: 'Pocket Files' },
        ]} />
        {mode === '' ? (
          <p className="muted small" style={{ margin: '10px 0 0' }}>Kept on Pocket Projects’ own disk, next to its database.</p>
        ) : <>
          <p className="muted small" style={{ margin: '10px 0 0' }}>
            Kept in one place with all your other files: each issue gets a folder (<code>{(folder || s.default_folder) + '/' + team.key + '-1'}</code>), removed attachments go to a <code>Deleted</code> folder there instead of vanishing.
            Projects reaches it through {s.mode === 'pocket_files' && s.owner && !s.owner_is_me ? <><b>{s.owner}</b>’s</> : 'your'} Pocket Core link to Pocket Files, so team members need nothing in Pocket Files themselves.
          </p>
          <Field label="Folder in Pocket Files" help={<>The shared <b>Pocket Projects</b> mount keeps the team’s files out of anyone’s own home; any folder you may edit works.</>}>
            <input value={folder} onChange={e => setFolder(e.target.value)} placeholder={s.default_folder} spellCheck={false} autoCapitalize="off" className="mono" />
          </Field>
        </>}
        {s.mode === 'pocket_files' && (
          <div className={`callout ${s.ok ? '' : 'danger'}`} style={{ marginTop: 12 }}>
            <Icon name={s.ok ? 'check-circle' : 'alert'} size={15} />
            <span>{s.ok ? <>In use: <code>{s.folder}</code>, through {s.owner_is_me ? 'your' : `${s.owner}’s`} link.</> : <>Pocket Files can’t be reached, so the team’s attachments there can’t be opened right now: {s.error}</>}</span>
          </div>
        )}
        <div className="row" style={{ marginTop: 14, gap: 8 }}>
          <button type="button" className="btn primary" disabled={!dirty || !!busy}
            onClick={() => mode === 'pocket_files' && s.mode !== 'pocket_files' && s.local > 0 ? setConfirm(true) : save()}>
            {busy === 'save' && <Spinner />}Save</button>
        </div>
      </Section>

      <Section title="Files" sub={`${plural(s.local, 'file')} on Pocket Projects’ disk · ${plural(s.in_files, 'file')} in Pocket Files`}
        actions={s.mode === 'pocket_files' && s.local > 0 && <button type="button" className="btn" disabled={!!busy || !s.ok} onClick={move}>{busy === 'move' ? <Spinner /> : <Icon name="upload" size={14} />}Move {plural(s.local, 'file')} to Pocket Files</button>}>
        <p className="muted small" style={{ margin: 0 }}>
          {s.mode === 'pocket_files'
            ? 'Moving copies each file, checks it arrived whole and only then removes it here — a failure halfway leaves every attachment readable.'
            : s.in_files > 0 ? 'Files already in Pocket Files stay there and keep working.' : 'New uploads land here.'}
        </p>
      </Section>

      {confirm && <Confirm title="Keep this team’s attachments in Pocket Files?" confirmLabel="Switch" danger={false} onCancel={() => setConfirm(false)} onConfirm={save}
        message={<div className="ui-ts-stack" style={{ gap: 10 }}>
          <span>New uploads go to <code>{folder.trim() || s.default_folder}</code> through your Pocket Core link.</span>
          <label className="row" style={{ gap: 10, alignItems: 'center' }}><Switch checked={moveNow} onChange={setMoveNow} size="sm" /><span>Move the {plural(s.local, 'existing file')} now</span></label>
        </div>} />}
    </div>
  )
}

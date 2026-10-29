// A shared note on an issue: title + Markdown that the whole team sees and
// edits, saved as you type. Also the panel beside the issue (embed: just the
// note, no app chrome). Saves carry the version they started from: when
// someone else saved in between, nothing is overwritten and the page asks.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import '../styles/projects-ui.css'
import { api, ApiError, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { ago, stamp } from '../lib/format'
import { Markdown } from '../lib/markdown'
import type { SharedNote } from '../lib/types'
import { Icon } from '../components/icons'
import { personName } from '../components/issue-bits'
import { MarkdownEditor } from '../components/MarkdownEditor'
import { Confirm, copyText, Empty, Loading, type MenuItem, MenuButton, Segmented, useToast } from '../components/ui'

type Loaded = SharedNote & { identifier: string; issue_title: string; team_id: number; can_edit: boolean }

/** The page's address for a shared note (the panel shows it under /embed). */
export const sharedNoteURL = (ident: string, id: number) => `/issue/${ident}/shared/${id}`

export default function SharedNotePage({ embed = false }: { embed?: boolean }) {
  const { ref, id } = useParams()
  const [search, setSearch] = useSearchParams()
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [note, setNote] = useState<Loaded | null>(null)
  const [err, setErr] = useState<{ status: number; msg: string } | null>(null)
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [mode, setMode] = useState<'edit' | 'preview'>('preview')
  const [state, setState] = useState<'' | 'saving' | 'saved' | 'error'>('')
  const [conflict, setConflict] = useState<{ msg: string; theirs: SharedNote } | null>(null)
  const [confirm, setConfirm] = useState(false)
  const saved = useRef<{ title: string; body: string; base: string }>({ title: '', body: '', base: '' })
  const timer = useRef(0)
  const titleRef = useRef<HTMLInputElement>(null)
  const url = `${V1}/issues/${ref}/shared-notes/${id}`

  useEffect(() => {
    let live = true
    setNote(null)
    api.get<Loaded>(url).then(n => {
      if (!live) return
      setNote(n); setTitle(n.title); setBody(n.body || ''); saved.current = { title: n.title, body: n.body || '', base: n.updated_at }
      // just made (or never written): straight to typing the title
      const fresh = search.get('new') === '1' || (n.title === 'Untitled' && !(n.body || '').trim())
      setMode(n.can_edit && (fresh || !(n.body || '').trim()) ? 'edit' : 'preview')
      if (fresh && n.can_edit) { if (search.has('new')) { search.delete('new'); setSearch(search, { replace: true }) } requestAnimationFrame(() => titleRef.current?.select()) }
    }).catch(e => live && setErr({ status: (e as ApiError).status || 0, msg: (e as Error).message }))
    return () => { live = false }
  }, [url]) // eslint-disable-line react-hooks/exhaustive-deps

  const flush = useCallback(async (next: { title?: string; body?: string }, force = false) => {
    if (!note || (conflict && !force)) return
    window.clearTimeout(timer.current)
    const patch: Record<string, unknown> = {}
    if (next.title !== undefined && (force || next.title.trim() !== saved.current.title)) patch.title = next.title.trim() || 'Untitled'
    if (next.body !== undefined && (force || next.body !== saved.current.body)) patch.body = next.body
    if (!Object.keys(patch).length) return
    setState('saving')
    try {
      const n = await api.patch<SharedNote>(url, { ...patch, base: saved.current.base, force })
      saved.current = { title: n.title, body: n.body ?? saved.current.body, base: n.updated_at }
      setNote(cur => cur && { ...cur, ...n })
      if (embed && patch.title) window.parent.postMessage({ type: 'pocket-projects-shared', id: n.id, title: n.title }, location.origin)
      setState('saved')
    } catch (e) {
      const ae = e as ApiError
      if (ae.status === 409 && ae.data?.note) { setState(''); setConflict({ msg: ae.message, theirs: ae.data.note as SharedNote }); return }
      setState('error'); toast(ae.message, 'err')
    }
  }, [note, conflict, url, toast, embed])
  const schedule = (next: { title?: string; body?: string }) => { window.clearTimeout(timer.current); timer.current = window.setTimeout(() => flush(next), 800) }
  // their version: what they wrote replaces what is on screen
  const takeTheirs = () => {
    if (!conflict) return
    const t = conflict.theirs
    saved.current = { title: t.title, body: t.body || '', base: t.updated_at }
    setTitle(t.title); setBody(t.body || ''); setNote(cur => cur && { ...cur, ...t }); setConflict(null)
  }
  // mine: saved over theirs, knowingly
  const keepMine = () => { setConflict(null); flush({ title, body }, true) }

  const pending = useRef({ title, body })
  pending.current = { title, body }
  useEffect(() => () => { window.clearTimeout(timer.current) }, [])
  useEffect(() => {
    const h = (e: BeforeUnloadEvent) => { if (pending.current.body !== saved.current.body || pending.current.title.trim() !== saved.current.title) e.preventDefault() }
    window.addEventListener('beforeunload', h)
    return () => {
      window.removeEventListener('beforeunload', h)
      const p = pending.current
      if (note && (p.body !== saved.current.body || p.title.trim() !== saved.current.title)) api.patch(url, { title: p.title.trim() || 'Untitled', body: p.body, base: saved.current.base }).catch(() => {})
    }
  }, [note, url])

  if (err) return <div className={embed ? 'shared-embed' : 'page'}><Empty title={err.status === 404 ? 'Shared note not found' : 'Could not open the shared note'} icon="users">{err.status === 404 ? 'It may have been deleted.' : err.msg}</Empty></div>
  if (!note) return <Loading />

  const canEdit = note.can_edit
  const link = `${location.origin}${sharedNoteURL(note.identifier, note.id)}`
  const remove = async () => {
    try {
      await api.del(url)
      saved.current = { ...saved.current, title, body }
      toast(`Deleted “${note.title}”`)
      if (embed) window.parent.postMessage({ type: 'pocket-projects-shared', closed: note.id }, location.origin)
      else navigate(`/issue/${note.identifier}`)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const menu: MenuItem[] = [
    ...(embed ? [{ label: 'Open as a page', icon: 'external', onSelect: () => window.open(link, '_blank', 'noopener') }] : []),
    { label: 'Copy link', icon: 'link', onSelect: async () => toast(await copyText(link) ? 'Link copied' : 'Could not copy', 'ok') },
    { label: 'Copy as Markdown', icon: 'copy', onSelect: async () => toast(await copyText(`# ${title}\n\n${body}`) ? 'Copied' : 'Could not copy', 'ok') },
    'sep',
    { label: 'Delete shared note…', icon: 'trash', danger: true, disabled: !canEdit, onSelect: () => setConfirm(true) },
  ]
  const editor = app.person(note.updated_by || note.created_by)
  const sharer = note.source_note ? app.person(note.created_by) : undefined

  return (
    <div className={`full-page doc-page ${embed ? 'shared-embed' : ''}`}>
      <div className="page-head doc-head">
        {embed ? <span className="shared-tag" title="Everyone on the team sees and edits this note"><Icon name="users" size={13} />Shared with the team</span>
          : <nav className="doc-crumbs ellipsis">
            <Link to={`/issue/${note.identifier}`} className="crumb-link"><span className="ident">{note.identifier}</span><span className="ellipsis">{note.issue_title}</span></Link>
            <Icon name="chevron-right" size={14} className="crumb-sep" />
            <span className="ellipsis">{title || 'Untitled'}</span>
          </nav>}
        <span className="spacer" />
        <span className={`xsmall doc-state ${state}`}>{state === 'saving' ? 'Saving…' : state === 'saved' ? 'Saved' : state === 'error' ? 'Not saved' : ''}</span>
        {canEdit && <Segmented size="sm" label="Mode" value={mode} onChange={m => { if (m === 'preview') flush({ title, body }); setMode(m) }} options={[{ value: 'edit', label: 'Edit' }, { value: 'preview', label: 'Preview' }]} />}
        <MenuButton items={menu} label="Shared note menu" align="right" width={220}><Icon name="more" size={18} /></MenuButton>
      </div>
      {conflict && <div className="shared-conflict" role="alert">
        <Icon name="alert" size={15} /><span className="grow">{conflict.msg}. Your last changes are not saved.</span>
        <button type="button" className="btn sm" onClick={takeTheirs}>Show theirs</button>
        <button type="button" className="btn sm danger" onClick={keepMine}>Keep mine</button>
      </div>}
      <article className="doc-body">
        {!embed && <span className="shared-tag" title="Everyone on the team sees and edits this note"><Icon name="users" size={13} />Shared with the team</span>}
        <input ref={titleRef} className="doc-title-input" value={title} disabled={!canEdit} placeholder="Untitled" aria-label="Title"
          onChange={e => { setTitle(e.target.value); schedule({ title: e.target.value }) }} onBlur={() => flush({ title })}
          onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); flush({ title }); setMode('edit') } }} />
        <div className="xsmall muted doc-meta" title={stamp(note.updated_at)}>
          {sharer && <>Shared by {personName(sharer)} from their notes{note.shared_at ? ` ${ago(note.shared_at)}` : ''} · </>}
          {editor ? `Edited by ${personName(editor)} · ` : ''}{ago(note.updated_at)}
        </div>
        {mode === 'edit' && canEdit ? (
          <MarkdownEditor value={body} onChange={v => { setBody(v); schedule({ body: v }) }} onBlur={() => flush({ body })} autoFocus={!!saved.current.title && saved.current.title !== 'Untitled'}
            minRows={14} bare teamId={note.team_id} placeholder="Write for the team. Markdown works: # headings, - lists, [ ] checklists, **bold**, links…" className="doc-editor" />
        ) : body.trim() ? (
          <div className="doc-preview" onDoubleClick={() => canEdit && setMode('edit')}><Markdown text={body} /></div>
        ) : <p className="faint">Empty. {canEdit && <button type="button" className="link-btn" onClick={() => setMode('edit')}>Start writing</button>}</p>}
      </article>
      {confirm && <Confirm title="Delete this shared note?" message={<>“{note.title}” will be deleted for everyone on the team. Notes it was shared from stay as they are. This can't be undone.</>} confirmLabel="Delete" onCancel={() => setConfirm(false)} onConfirm={() => { setConfirm(false); remove() }} />}
    </div>
  )
}

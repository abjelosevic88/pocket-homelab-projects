// A project document: title, emoji, Markdown body saved as you type.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import '../styles/projects-ui.css'
import { api, ApiError, appURL, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { ago, stamp } from '../lib/format'
import { Markdown } from '../lib/markdown'
import type { Doc } from '../lib/types'
import { Icon } from '../components/icons'
import { personName, ProjectMark } from '../components/issue-bits'
import { MarkdownEditor } from '../components/MarkdownEditor'
import { type Anchor, Confirm, copyText, Empty, Loading, type MenuItem, MenuButton, Popover, rectOf, Segmented, useToast } from '../components/ui'
import { FavoriteStar, PROJECT_EMOJIS } from '../components/project/common'

const DOC_EMOJIS = ['📄', '📝', '📋', '📌', '🗒️', '📐', '🧭', '🗺️', '✅', '💬', '📊', '🔍', ...PROJECT_EMOJIS.slice(0, 20)]

export default function DocPage() {
  const { id } = useParams()
  const [search, setSearch] = useSearchParams()
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [doc, setDoc] = useState<Doc | null>(null)
  const [err, setErr] = useState<{ status: number; msg: string } | null>(null)
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [mode, setMode] = useState<'edit' | 'preview'>('preview')
  const [state, setState] = useState<'' | 'saving' | 'saved' | 'error'>('')
  const [emoji, setEmoji] = useState<Anchor | null>(null)
  const [confirm, setConfirm] = useState(false)
  const saved = useRef<{ title: string; body: string }>({ title: '', body: '' })
  const timer = useRef(0)
  const titleRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    let live = true
    setDoc(null)
    api.get<Doc>(`${V1}/docs/${id}`).then(d => {
      if (!live) return
      setDoc(d); setTitle(d.title); setBody(d.body || ''); saved.current = { title: d.title, body: d.body || '' }
      const fresh = search.get('new') === '1'
      setMode(fresh || !(d.body || '').trim() ? 'edit' : 'preview')
      if (fresh) { search.delete('new'); setSearch(search, { replace: true }); requestAnimationFrame(() => titleRef.current?.select()) }
    }).catch(e => live && setErr({ status: (e as ApiError).status || 0, msg: (e as Error).message }))
    return () => { live = false }
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps

  const project = doc ? app.project(doc.project_id) : undefined
  const canEdit = !!project && project.team_ids.some(t => app.canEdit(t))

  const flush = useCallback(async (next: { title?: string; body?: string; icon?: string }) => {
    if (!doc) return
    window.clearTimeout(timer.current)
    const patch: Record<string, string> = {}
    if (next.title !== undefined && next.title.trim() !== saved.current.title) patch.title = next.title.trim() || 'Untitled'
    if (next.body !== undefined && next.body !== saved.current.body) patch.body = next.body
    if (next.icon !== undefined) patch.icon = next.icon
    if (!Object.keys(patch).length) return
    setState('saving')
    try {
      const d = await api.patch<Doc>(`${V1}/docs/${doc.id}`, patch)
      saved.current = { title: d.title, body: d.body ?? saved.current.body }
      setDoc(cur => cur && { ...cur, ...d })
      setState('saved')
    } catch (e) { setState('error'); toast((e as Error).message, 'err') }
  }, [doc, toast])
  const schedule = (next: { title?: string; body?: string }) => { window.clearTimeout(timer.current); timer.current = window.setTimeout(() => flush(next), 800) }
  // Save what's pending when leaving the page.
  const pending = useRef({ title, body })
  pending.current = { title, body }
  useEffect(() => () => { window.clearTimeout(timer.current) }, [])
  useEffect(() => {
    const h = (e: BeforeUnloadEvent) => { if (pending.current.body !== saved.current.body || pending.current.title.trim() !== saved.current.title) e.preventDefault() }
    window.addEventListener('beforeunload', h)
    return () => { window.removeEventListener('beforeunload', h); const p = pending.current; if (doc && (p.body !== saved.current.body || p.title.trim() !== saved.current.title)) api.patch(`${V1}/docs/${doc.id}`, { title: p.title.trim() || 'Untitled', body: p.body }).catch(() => {}) }
  }, [doc])

  if (err) return <div className="page"><Empty title={err.status === 404 ? 'Document not found' : 'Could not open the document'} icon="file-text">{err.status === 404 ? 'It may have been deleted.' : err.msg}</Empty></div>
  if (!doc) return <Loading />

  const remove = async () => {
    try {
      await api.del(`${V1}/docs/${doc.id}`)
      saved.current = { title, body }
      toast(`Deleted “${doc.title}”`)
      navigate(`/project/${doc.project_id}/docs`)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const menu: MenuItem[] = [
    { label: 'Copy link', icon: 'link', onSelect: async () => toast(await copyText(appURL(`/doc/${doc.id}`)) ? 'Link copied' : 'Could not copy', 'ok') },
    { label: 'Copy as Markdown', icon: 'copy', onSelect: async () => toast(await copyText(`# ${title}\n\n${body}`) ? 'Copied' : 'Could not copy', 'ok') },
    'sep',
    { label: 'Delete document…', icon: 'trash', danger: true, disabled: !canEdit, onSelect: () => setConfirm(true) },
  ]
  const editor = app.person(doc.updated_by || doc.created_by)

  return (
    <div className="full-page doc-page">
      <div className="page-head doc-head">
        <nav className="doc-crumbs ellipsis">
          {project ? <Link to={`/project/${project.id}/docs`} className="crumb-link"><ProjectMark icon={project.icon} color={project.color} size={15} /><span className="ellipsis">{project.name}</span></Link> : <span className="muted">Project</span>}
          <Icon name="chevron-right" size={14} className="crumb-sep" />
          <span className="ellipsis">{title || 'Untitled'}</span>
        </nav>
        <span className="spacer" />
        <span className={`xsmall doc-state ${state}`}>{state === 'saving' ? 'Saving…' : state === 'saved' ? 'Saved' : state === 'error' ? 'Not saved' : ''}</span>
        {canEdit && <Segmented size="sm" label="Mode" value={mode} onChange={m => { if (m === 'preview') flush({ title, body }); setMode(m) }} options={[{ value: 'edit', label: 'Edit' }, { value: 'preview', label: 'Preview' }]} />}
        <FavoriteStar kind="doc" id={doc.id} />
        <MenuButton items={menu} label="Document menu" align="right" width={220}><Icon name="more" size={18} /></MenuButton>
      </div>
      <article className="doc-body">
        <button type="button" className="doc-emoji" disabled={!canEdit} aria-label="Choose an icon" title="Icon" onClick={e => setEmoji(rectOf(e.currentTarget))}>
          {doc.icon || <Icon name="file-text" size={26} />}
        </button>
        <input ref={titleRef} className="doc-title-input" value={title} disabled={!canEdit} placeholder="Untitled" aria-label="Title"
          onChange={e => { setTitle(e.target.value); schedule({ title: e.target.value }) }} onBlur={() => flush({ title })}
          onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); flush({ title }); setMode('edit') } }} />
        <div className="xsmall muted doc-meta" title={stamp(doc.updated_at)}>{editor ? `Edited by ${personName(editor)} · ` : ''}{ago(doc.updated_at)}</div>
        {mode === 'edit' && canEdit ? (
          <MarkdownEditor value={body} onChange={v => { setBody(v); schedule({ body: v }) }} onBlur={() => flush({ body })} autoFocus={!!saved.current.title && saved.current.title !== 'Untitled'}
            minRows={14} bare teamId={project?.team_ids[0]} placeholder="Write here. Markdown works: # headings, - lists, [ ] checklists, **bold**, links…" className="doc-editor" />
        ) : body.trim() ? (
          <div className="doc-preview" onDoubleClick={() => canEdit && setMode('edit')}><Markdown text={body} /></div>
        ) : <p className="faint">Empty. {canEdit && <button type="button" className="link-btn" onClick={() => setMode('edit')}>Start writing</button>}</p>}
      </article>
      {emoji && <Popover anchor={emoji} onClose={() => setEmoji(null)} prefer="below" width={300} label="Document icon">
        <div className="icon-pop-body">
          <div className="emoji-grid">
            <button type="button" className={`emoji-btn ${!doc.icon ? 'on' : ''}`} title="No icon" onClick={() => { flush({ icon: '' }); setEmoji(null) }}><Icon name="x" size={15} /></button>
            {DOC_EMOJIS.map(e => <button key={e} type="button" className={`emoji-btn ${doc.icon === e ? 'on' : ''}`} onClick={() => { flush({ icon: e }); setEmoji(null) }}>{e}</button>)}
          </div>
        </div>
      </Popover>}
      {confirm && <Confirm title="Delete this document?" message={<>“{doc.title}” will be deleted for everyone on the project. This can't be undone.</>} confirmLabel="Delete" onCancel={() => setConfirm(false)} onConfirm={() => { setConfirm(false); remove() }} />}
    </div>
  )
}

// A Markdown textarea that grows with its text, completes @people, issue
// identifiers and [[shared notes (a link to the note), pastes images as attachments (when it knows the issue;
// before the issue exists, holdFile keeps them for the caller to upload)
// and can show a preview. Cmd/Ctrl+Enter submits. "Drawing" puts in a
// sketch or diagram from Pocket Draw at the cursor (greyed out, with why,
// while Pocket Draw isn't connected).
import { type MouseEvent as ReactMouseEvent, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { api, upload, V1 } from '../lib/api'
import { caretBox } from '../lib/caret'
import { useApp } from '../lib/app-context'
import { useDrawApp } from '../lib/draw'
import { Markdown } from '../lib/markdown'
import type { Attachment, Issue, SearchResult, SharedNote, TeamItem } from '../lib/types'
import { DrawingOff, DrawingPicker } from './DrawingPicker'
import { Icon } from './icons'
import { PersonAvatar, StatusIcon } from './issue-bits'
import { type Anchor, Popover, rectOf, useToast } from './ui'

interface Suggest { kind: '@' | '#' | 'note'; start: number; query: string }
/** A shared note [[ can link to: on an issue (/issue/WEB-1/shared/3) or in a team's space (/team/WEB/shared/4). */
interface NoteHit { key: string; title: string; path: string; where: string }
/** Where the list sits: under the caret's line, or above it when there's no room below. */
interface Spot { left: number; top?: number; bottom?: number }

const linkText = (t: string) => (t || 'Untitled').replace(/[[\]\n]/g, ' ').trim() || 'Untitled'

export function MarkdownEditor({ value, onChange, onSubmit, onBlur, placeholder, autoFocus, minRows = 3, teamId, issueRef, issueIdent, holdFile, className = '', preview: canPreview = true, bare }: {
  value: string; onChange: (v: string) => void; onSubmit?: () => void; onBlur?: () => void; placeholder?: string; autoFocus?: boolean; minRows?: number
  teamId?: number | null; issueRef?: string | number; className?: string; preview?: boolean; bare?: boolean
  /** The issue's identifier (WEB-12), for links to its shared notes. */
  issueIdent?: string
  /** No issue yet: keep a pasted file and return a local URL to show it with. */
  holdFile?: (f: File) => string
}) {
  const app = useApp()
  const { toast } = useToast()
  const ta = useRef<HTMLTextAreaElement>(null)
  const [sug, setSug] = useState<Suggest | null>(null)
  const [hi, setHi] = useState(0)
  const [issues, setIssues] = useState<Issue[]>([])
  const [notes, setNotes] = useState<NoteHit[]>([])
  const [spot, setSpot] = useState<Spot | null>(null)
  const box = useRef<HTMLDivElement>(null)
  const nearby = useRef<Promise<NoteHit[]> | null>(null)
  const [preview, setPreview] = useState(false)
  const [uploading, setUploading] = useState(false)
  const draw = useDrawApp()
  const [picker, setPicker] = useState(false)
  const [drawOff, setDrawOff] = useState<Anchor | null>(null)
  const sel = useRef<[number, number]>([0, 0])
  // While the drawing picker (or why it's greyed out) is open the textarea
  // loses focus; that must not end editing where the page listens to onBlur.
  const holdBlur = useRef(false)

  useLayoutEffect(() => {
    const el = ta.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.max(el.scrollHeight, minRows * 21 + 18) + 'px'
  }, [value, minRows, preview])

  const people = useMemo(() => {
    const t = app.team(teamId)
    return t?.members || app.people
  }, [app, teamId])

  const peopleHits = sug?.kind === '@' ? people.filter(p => (p.name + ' ' + p.email + ' ' + p.username).toLowerCase().includes(sug.query.toLowerCase())).slice(0, 6) : []
  useEffect(() => {
    if (sug?.kind !== '#' || sug.query.length < 1) { setIssues([]); return }
    let live = true
    api.get<{ issues: Issue[] }>(`${V1}/issues?q=${encodeURIComponent(sug.query)}&limit=6&archived=include`).then(r => live && setIssues(r.issues)).catch(() => {})
    return () => { live = false }
  }, [sug?.kind, sug?.query])
  // [[: this issue's shared notes and the team's first, then any shared note the search finds.
  const team = app.team(teamId)
  const loadNearby = () => nearby.current ??= Promise.all([
    issueRef ? api.get<SharedNote[]>(`${V1}/issues/${issueRef}/shared-notes`).then(l => (l || []).map(n => ({
      key: `s${n.id}`, title: linkText(n.title), path: `/issue/${issueIdent || issueRef}/shared/${n.id}`, where: issueIdent ? `${issueIdent} · this issue` : 'This issue',
    }))).catch(() => []) : Promise.resolve([]),
    team ? api.get<{ items: TeamItem[] }>(`${V1}/teams/${team.key}/shared`).then(r => r.items.filter(n => n.kind === 'note').map(n => ({
      key: `t${n.id}`, title: linkText(n.title), path: `/team/${team.key}/shared/${n.id}`, where: `${team.name} · shared`,
    }))).catch(() => []) : Promise.resolve([]),
  ]).then(([a, b]) => [...a, ...b])
  useEffect(() => { nearby.current = null }, [issueRef, issueIdent, team?.key])
  useEffect(() => {
    if (sug?.kind !== 'note') { setNotes([]); return }
    let live = true
    const q = sug.query.trim().toLowerCase()
    loadNearby().then(async near => {
      const mine = near.filter(n => !q || n.title.toLowerCase().includes(q))
      if (live) setNotes(mine.slice(0, 8))
      if (q.length < 2 || mine.length >= 8) return
      const r = await api.get<SearchResult>(`${V1}/search?q=${encodeURIComponent(q)}&limit=12`).catch(() => null)
      const found: NoteHit[] = (r?.docs || []).flatMap(d => d.kind === 'shared_note' ? [{ key: `s${d.id}`, title: linkText(d.title), path: `/issue/${d.identifier}/shared/${d.id}`, where: `${d.identifier} · shared note` }]
        : d.kind === 'team_note' ? [{ key: `t${d.id}`, title: linkText(d.title), path: `/team/${d.identifier}/shared/${d.id}`, where: `${app.teams.find(t => t.key === d.identifier)?.name || d.identifier} · shared` }] : [])
      if (live) setNotes([...mine, ...found.filter(f => !mine.some(m => m.key === f.key))].slice(0, 8))
    })
    return () => { live = false }
  }, [sug?.kind, sug?.query]) // eslint-disable-line react-hooks/exhaustive-deps
  const hits = sug?.kind === '@' ? peopleHits.length : sug?.kind === 'note' ? notes.length : issues.length

  // The list opens at the caret, not under the whole box.
  const place = (caret: number) => {
    const el = ta.current, wrap = box.current
    if (!el || !wrap) return
    const c = caretBox(el, caret), r = el.getBoundingClientRect(), w = wrap.getBoundingClientRect()
    const below = window.innerHeight - (r.top + c.top + c.height)
    const left = Math.max(0, Math.min(r.left - w.left + c.left - 8, w.width - 260))
    const up = below < 240 && r.top + c.top > below
    setSpot(up ? { left, bottom: w.bottom - (r.top + c.top) + 4 } : { left, top: r.top - w.top + c.top + c.height + 4 })
  }
  const detect = (text: string, caret: number) => {
    const before = text.slice(0, caret)
    const n = /\[\[([^[\]\n]{0,40})$/.exec(before)
    const m = n ? null : /(^|[\s(])([@#])([\w.-]{0,30})$/.exec(before)
    if (n) { setSug({ kind: 'note', start: caret - n[1].length - 2, query: n[1] }); setHi(0); place(caret - n[1].length - 2) }
    else if (m) { setSug({ kind: m[2] as '@' | '#', start: caret - m[3].length - 1, query: m[3] }); setHi(0); place(caret - m[3].length - 1) }
    else setSug(null)
  }
  const insert = (text: string) => {
    if (!sug || !ta.current) return
    const caret = ta.current.selectionStart
    let after = value.slice(caret)
    if (sug.kind === 'note') after = after.replace(/^\]{1,2}/, '') // a closing ]] typed ahead
    const next = value.slice(0, sug.start) + text + ' ' + after
    onChange(next)
    setSug(null)
    const pos = sug.start + text.length + 1
    requestAnimationFrame(() => { ta.current?.focus(); ta.current?.setSelectionRange(pos, pos) })
  }
  const pick = (i: number) => {
    if (sug?.kind === '@') { const p = peopleHits[i]; if (p) insert('@' + (p.username || p.email.split('@')[0])) }
    else if (sug?.kind === 'note') { const n = notes[i]; if (n) insert(`[${n.title}](${n.path})`) }
    else { const is = issues[i]; if (is) insert(is.identifier) }
  }

  // Pasted or dropped files go in at the cursor: images as pictures, others as links.
  const uploadFiles = async (files: File[]) => {
    if (!issueRef && !holdFile) { toast('Save the issue first, then paste images into it', 'err'); return }
    const el = ta.current
    const [a, b] = el ? [el.selectionStart, el.selectionEnd] : [value.length, value.length]
    const put = (links: string[]) => {
      const before = value.slice(0, a), after = value.slice(b)
      const text = (before && !before.endsWith('\n') ? '\n' : '') + links.join('\n') + (after.startsWith('\n') ? '' : '\n')
      onChange(before + text + after)
      requestAnimationFrame(() => { ta.current?.focus(); ta.current?.setSelectionRange(a + text.length, a + text.length) })
    }
    const link = (f: File, name: string, url: string) => f.type.startsWith('image/') ? `![${name}](${url})` : `[${name}](${url})`
    if (!issueRef) { put(files.map(f => link(f, f.name || 'image', holdFile!(f)))); return }
    setUploading(true)
    try {
      const links: string[] = []
      for (const f of files) {
        const at = await upload<Attachment>(`${V1}/issues/${issueRef}/attachments`, 'file', f)
        links.push(link(f, at.file_name, at.url))
      }
      put(links)
    } catch (e) { toast((e as Error).message, 'err') } finally { setUploading(false) }
  }

  const drawReady = !draw.loaded || draw.status === 'ok'
  const openDrawing = (e: ReactMouseEvent<HTMLButtonElement>) => {
    holdBlur.current = true
    if (!drawReady) { setDrawOff(rectOf(e.currentTarget)); return }
    const el = ta.current
    sel.current = el ? [el.selectionStart, el.selectionEnd] : [value.length, value.length]
    setPicker(true)
  }
  const backToText = (pos?: number) => {
    requestAnimationFrame(() => {
      const el = ta.current
      if (el) { el.focus(); if (pos !== undefined) el.setSelectionRange(pos, pos) }
      holdBlur.current = false
    })
  }
  const insertDrawing = (md: string) => {
    const [a, b] = sel.current
    const before = value.slice(0, a), after = value.slice(b)
    const text = (before && !before.endsWith('\n') ? '\n' : '') + md + (after.startsWith('\n') ? '' : '\n')
    onChange(before + text + after)
    setPicker(false)
    backToText(a + text.length)
  }
  const drawButton = (corner: boolean) => (
    <button type="button" className={`btn ghost xs md-draw-btn ${corner ? 'icon-only md-corner' : ''} ${drawReady ? '' : 'is-off'}`} aria-disabled={!drawReady}
      title={drawReady ? 'Insert a drawing from Pocket Draw' : 'Insert a drawing — Pocket Draw isn’t connected'} aria-label="Insert a drawing from Pocket Draw"
      onMouseDown={e => e.preventDefault()} onClick={openDrawing}>
      <Icon name="draw" size={13} />{!corner && <span>Drawing</span>}
    </button>
  )
  const tools = canPreview && !bare

  return (
    <div ref={box} className={`md-editor ${bare ? 'bare' : ''} ${tools ? '' : 'has-corner'} ${className}`}>
      {preview ? <div className="md-preview" onClick={() => setPreview(false)}>{value.trim() ? <Markdown text={value} /> : <p className="faint">Nothing to preview</p>}</div> :
        <textarea ref={ta} value={value} placeholder={placeholder} autoFocus={autoFocus} rows={minRows} className={bare ? 'bare' : ''}
          onChange={e => { onChange(e.target.value); detect(e.target.value, e.target.selectionStart) }}
          onBlur={() => { window.setTimeout(() => setSug(null), 150); if (!holdBlur.current) onBlur?.() }}
          onPaste={e => { const files = [...e.clipboardData.files]; if (files.length) { e.preventDefault(); uploadFiles(files) } }}
          onDrop={e => { const files = [...e.dataTransfer.files]; if (files.length) { e.preventDefault(); uploadFiles(files) } }}
          onKeyDown={e => {
            if (sug && hits) {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => (h + 1) % hits); return }
              if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => (h - 1 + hits) % hits); return }
              if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); pick(hi); return }
              if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); setSug(null); return }
            }
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && onSubmit) { e.preventDefault(); onSubmit() }
          }} />}
      {sug && hits > 0 && <div className="md-suggest" role="listbox" style={spot ? { left: spot.left, top: spot.top ?? 'auto', bottom: spot.bottom ?? 'auto' } : undefined}>
        {sug.kind === 'note' ? notes.map((n, i) => <button key={n.key} type="button" className={i === hi ? 'hi' : ''} title={n.path} onMouseDown={e => { e.preventDefault(); pick(i) }}><Icon name="users" size={14} /><span className="ellipsis">{n.title}</span><small className="ellipsis">{n.where}</small></button>)
          : sug.kind === '@' ? peopleHits.map((p, i) => <button key={p.id} type="button" className={i === hi ? 'hi' : ''} onMouseDown={e => { e.preventDefault(); pick(i) }}><PersonAvatar person={p} size={18} />{p.name || p.email}<small>@{p.username || p.email.split('@')[0]}</small></button>)
          : issues.map((is, i) => <button key={is.id} type="button" className={i === hi ? 'hi' : ''} onMouseDown={e => { e.preventDefault(); pick(i) }}><StatusIcon state={app.state(is.state_id)} /><small className="ident">{is.identifier}</small><span className="ellipsis">{is.title}</span></button>)}
      </div>}
      {!tools && !preview && drawButton(true)}
      {tools && <div className="md-tools">
        <span className="faint xsmall">Markdown · @ to mention · # for issues · [[ for a shared note{issueRef || holdFile ? ' · paste images' : ''}</span>
        {uploading && <span className="xsmall muted">Uploading…</span>}
        <span className="spacer" />
        {!preview && drawButton(false)}
        <button type="button" className="btn ghost xs" title={preview ? 'Edit' : 'Preview'} aria-label={preview ? 'Edit' : 'Preview'} onClick={() => setPreview(p => !p)}><Icon name={preview ? 'edit' : 'eye'} size={13} /></button>
      </div>}
      {picker && <DrawingPicker onPick={insertDrawing} onClose={() => { setPicker(false); backToText() }} />}
      {drawOff && <Popover anchor={drawOff} onClose={() => {
        // Closed by a click elsewhere: let the page see the textarea was left.
        setDrawOff(null); holdBlur.current = false
        requestAnimationFrame(() => { if (ta.current && document.activeElement !== ta.current) onBlur?.() })
      }} prefer="below" width={320} label="Pocket Draw">
        <div className="md-draw-off"><DrawingOff draw={draw} /></div>
      </Popover>}
    </div>
  )
}
